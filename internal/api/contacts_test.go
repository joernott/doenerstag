package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/joernott/doenerstag/internal/api"
)

type contactsResponse struct {
	Contacts []struct {
		ID              string `json:"id"`
		ContactTypeID   string `json:"contact_type_id"`
		ContactTypeCode string `json:"contact_type_code"`
		RenderAs        string `json:"render_as"`
		Value           string `json:"value"`
		Label           string `json:"label"`
		SortOrder       int    `json:"sort_order"`
	} `json:"contacts"`
}

func TestAddingAndListingContacts(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("anna")
	restaurant := f.createRestaurant("Reachable", cookies)
	path := "/restaurants/" + restaurant.ID + "/contacts"

	rec := f.post(path, map[string]any{
		"contact_type_id": f.contactTypeID("email"),
		"value":           "bestellung@example.invalid",
		"label":           "Orders",
		"sort_order":      5,
	}, cookies...)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	list := f.get(path)
	var body contactsResponse
	decode(t, list, &body)
	if len(body.Contacts) != 2 {
		t.Fatalf("listed %d contacts, want 2", len(body.Contacts))
	}

	var email bool
	for _, c := range body.Contacts {
		if c.ContactTypeCode == "email" {
			email = true
			if c.RenderAs != "mailto" {
				t.Errorf("an email contact renders as %q, want mailto", c.RenderAs)
			}
			if c.Label != "Orders" {
				t.Errorf("label is %q", c.Label)
			}
		}
	}
	if !email {
		t.Error("the added contact is missing from the list")
	}
}

// F3.1 again, from the other direction: removing the last contact would leave a
// restaurant nobody can call.
func TestTheLastContactCannotBeRemoved(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("bertil")
	restaurant := f.createRestaurant("Only Contact", cookies)
	path := "/restaurants/" + restaurant.ID + "/contacts"

	list := f.get(path)
	var body contactsResponse
	decode(t, list, &body)
	if len(body.Contacts) != 1 {
		t.Fatalf("the fixture made %d contacts, want 1", len(body.Contacts))
	}
	only := body.Contacts[0].ID

	rec := f.remove(path+"/"+only, cookies...)
	expectError(t, rec, http.StatusBadRequest, api.CodeRestaurantNeedsContact)

	// Adding a second makes the first removable.
	if add := f.post(path, map[string]any{
		"contact_type_id": f.contactTypeID("email"), "value": "a@example.invalid",
	}, cookies...); add.Code != http.StatusCreated {
		t.Fatalf("adding a second contact: %s", add.Body.String())
	}
	if rec := f.remove(path+"/"+only, cookies...); rec.Code != http.StatusNoContent {
		t.Errorf("removing one of two contacts answered %d: %s", rec.Code, rec.Body.String())
	}

	// And now the remaining one is protected in its turn.
	after := f.get(path)
	var remaining contactsResponse
	decode(t, after, &remaining)
	if len(remaining.Contacts) != 1 {
		t.Fatalf("%d contacts remain", len(remaining.Contacts))
	}
	last := f.remove(path+"/"+remaining.Contacts[0].ID, cookies...)
	expectError(t, last, http.StatusBadRequest, api.CodeRestaurantNeedsContact)
}

func TestReplacingAContact(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("carla")
	restaurant := f.createRestaurant("Changeable", cookies)
	path := "/restaurants/" + restaurant.ID + "/contacts"

	list := f.get(path)
	var body contactsResponse
	decode(t, list, &body)
	id := body.Contacts[0].ID

	rec := f.do(request{
		method: http.MethodPut, path: path + "/" + id,
		body: map[string]any{
			"contact_type_id": f.contactTypeID("website"),
			"value":           "https://example.invalid",
			"sort_order":      1,
		},
		cookies: cookies,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	var replaced struct {
		ID              string `json:"id"`
		ContactTypeCode string `json:"contact_type_code"`
		RenderAs        string `json:"render_as"`
		Value           string `json:"value"`
		Label           string `json:"label"`
	}
	decode(t, rec, &replaced)
	if replaced.ID != id {
		t.Error("the replacement got a new id")
	}
	if replaced.ContactTypeCode != "website" || replaced.Value != "https://example.invalid" {
		t.Errorf("replaced with %+v", replaced)
	}
	// PUT replaces: a label that was there and is not sent is gone.
	if replaced.Label != "" {
		t.Errorf("the old label survived a replace: %q", replaced.Label)
	}
}

// One restaurant's contact must not be reachable through another's URL.
func TestAContactCannotBeEditedThroughAnotherRestaurant(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("dora")

	mine := f.createRestaurant("Mine", cookies)
	theirs := f.createRestaurant("Theirs", cookies)

	list := f.get("/restaurants/" + theirs.ID + "/contacts")
	var body contactsResponse
	decode(t, list, &body)
	target := body.Contacts[0].ID

	// The path names my restaurant and their contact.
	put := f.do(request{
		method: http.MethodPut, path: "/restaurants/" + mine.ID + "/contacts/" + target,
		body: map[string]any{
			"contact_type_id": f.contactTypeID("phone"), "value": "hijacked",
		},
		cookies: cookies,
	})
	if put.Code != http.StatusNotFound {
		t.Errorf("editing across restaurants answered %d, want 404", put.Code)
	}

	del := f.remove("/restaurants/"+mine.ID+"/contacts/"+target, cookies...)
	if del.Code != http.StatusNotFound {
		t.Errorf("deleting across restaurants answered %d, want 404", del.Code)
	}

	// Their contact is untouched.
	after := f.get("/restaurants/" + theirs.ID + "/contacts")
	var stillThere contactsResponse
	decode(t, after, &stillThere)
	if len(stillThere.Contacts) != 1 || stillThere.Contacts[0].Value == "hijacked" {
		t.Errorf("the other restaurant's contact was changed: %+v", stillThere.Contacts)
	}
}

func TestContactValidation(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("erik")
	restaurant := f.createRestaurant("Validated Contacts", cookies)
	path := "/restaurants/" + restaurant.ID + "/contacts"

	cases := map[string]struct {
		body map[string]any
		code api.Code
	}{
		"no value": {
			map[string]any{"contact_type_id": f.contactTypeID("phone"), "value": ""},
			api.CodeMissingField,
		},
		"whitespace value": {
			map[string]any{"contact_type_id": f.contactTypeID("phone"), "value": "   "},
			api.CodeMissingField,
		},
		"value too long": {
			map[string]any{
				"contact_type_id": f.contactTypeID("phone"),
				"value":           strings.Repeat("x", 501),
			},
			api.CodeInvalidField,
		},
		"label too long": {
			map[string]any{
				"contact_type_id": f.contactTypeID("phone"),
				"value":           "+49 30 1",
				"label":           strings.Repeat("x", 101),
			},
			api.CodeInvalidField,
		},
		"unknown type": {
			map[string]any{
				"contact_type_id": "018f0000-0000-7000-8000-00000000dead",
				"value":           "+49 30 1",
			},
			api.CodeInvalidField,
		},
		"type is not a uuid": {
			map[string]any{"contact_type_id": "phone", "value": "+49 30 1"},
			api.CodeInvalidField,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := f.post(path, tc.body, cookies...)
			expectError(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}

func TestContactsAreReadableAnonymouslyButNotWritable(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("frieda")
	restaurant := f.createRestaurant("Public Contacts", cookies)
	path := "/restaurants/" + restaurant.ID + "/contacts"

	if rec := f.get(path); rec.Code != http.StatusOK {
		t.Errorf("an anonymous read answered %d", rec.Code)
	}

	rec := f.post(path, map[string]any{
		"contact_type_id": f.contactTypeID("phone"), "value": "+49 30 1",
	})
	expectError(t, rec, http.StatusUnauthorized, api.CodeNotAuthenticated)
}

// The pickup address is a contact whose type renders as an address; there is no
// separate address column, and this is the case that proves the modelling
// works.
func TestTheAddressIsAContact(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("gustav")
	restaurant := f.createRestaurant("Abholung", cookies)

	rec := f.post("/restaurants/"+restaurant.ID+"/contacts", map[string]any{
		"contact_type_id": f.contactTypeID("address"),
		"value":           "Hauptstr. 1\n10827 Berlin",
	}, cookies...)
	if rec.Code != http.StatusCreated {
		t.Fatalf("adding an address: %s", rec.Body.String())
	}

	var created struct {
		RenderAs string `json:"render_as"`
		Value    string `json:"value"`
	}
	decode(t, rec, &created)
	if created.RenderAs != "address" {
		t.Errorf("the address renders as %q", created.RenderAs)
	}
	if !strings.Contains(created.Value, "10827") {
		t.Errorf("the multi-line address was mangled: %q", created.Value)
	}
}
