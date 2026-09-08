package api_test

import (
	"net/http"
	"testing"

	"github.com/joernott/doenerstag/internal/api"
)

type restaurantResponse struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	LogoImageID        *string `json:"logo_image_id"`
	CurrencyCode       string  `json:"currency_code"`
	MinOrderValueCents *int64  `json:"min_order_value_cents"`
	DeliveryFeeCents   *int64  `json:"delivery_fee_cents"`
	Notes              string  `json:"notes"`
	Contacts           []struct {
		ID              string `json:"id"`
		ContactTypeID   string `json:"contact_type_id"`
		ContactTypeCode string `json:"contact_type_code"`
		RenderAs        string `json:"render_as"`
		Value           string `json:"value"`
		Label           string `json:"label"`
		SortOrder       int    `json:"sort_order"`
	} `json:"contacts"`
	OpeningHours []struct {
		ID              string `json:"id"`
		DayOfWeek       int    `json:"day_of_week"`
		Start           string `json:"start"`
		End             string `json:"end"`
		CrossesMidnight bool   `json:"crosses_midnight"`
	} `json:"opening_hours"`
}

// contactTypeID looks up a seeded contact type by its code.
func (f *apiFixture) contactTypeID(code string) string {
	f.t.Helper()

	rec := f.get("/contact-types")
	if rec.Code != http.StatusOK {
		f.t.Fatalf("listing contact types: %s", rec.Body.String())
	}
	var body struct {
		ContactTypes []struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		} `json:"contact_types"`
	}
	decode(f.t, rec, &body)
	for _, t := range body.ContactTypes {
		if t.Code == code {
			return t.ID
		}
	}
	f.t.Fatalf("no seeded contact type with code %q", code)
	return ""
}

// createRestaurant makes one with a single phone contact, which is the minimum
// F3.1 allows.
func (f *apiFixture) createRestaurant(name string, cookies []*http.Cookie) restaurantResponse {
	f.t.Helper()

	rec := f.post("/restaurants", map[string]any{
		"name":          name,
		"currency_code": "EUR",
		"contacts": []map[string]any{
			{"contact_type_id": f.contactTypeID("phone"), "value": "+49 30 123456"},
		},
	}, cookies...)
	if rec.Code != http.StatusCreated {
		f.t.Fatalf("creating %q: %d %s", name, rec.Code, rec.Body.String())
	}
	var body restaurantResponse
	decode(f.t, rec, &body)
	return body
}

func TestCreateAndReadARestaurant(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("anna")

	minValue := int64(2000)
	fee := int64(250)
	rec := f.post("/restaurants", map[string]any{
		"name":                  "Döner Palast",
		"currency_code":         "EUR",
		"min_order_value_cents": minValue,
		"delivery_fee_cents":    fee,
		"notes":                 "Ask for Ali",
		"contacts": []map[string]any{
			{"contact_type_id": f.contactTypeID("phone"), "value": "+49 30 123456", "label": "Counter"},
			{"contact_type_id": f.contactTypeID("address"), "value": "Hauptstr. 1, 10827 Berlin"},
		},
	}, cookies...)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d, want 201: %s", rec.Code, rec.Body.String())
	}

	var created restaurantResponse
	decode(t, rec, &created)
	if created.Name != "Döner Palast" || created.CurrencyCode != "EUR" {
		t.Errorf("created %+v", created)
	}
	if created.MinOrderValueCents == nil || *created.MinOrderValueCents != minValue {
		t.Errorf("minimum order value is %v", created.MinOrderValueCents)
	}
	if len(created.Contacts) != 2 {
		t.Fatalf("created with %d contacts, want 2", len(created.Contacts))
	}

	// The contact carries its type's code and render hint, so a client can
	// render one entry without fetching /contact-types.
	var phone bool
	for _, c := range created.Contacts {
		if c.ContactTypeCode == "phone" {
			phone = true
			if c.RenderAs != "tel" {
				t.Errorf("a phone contact renders as %q, want tel", c.RenderAs)
			}
			if c.Label != "Counter" {
				t.Errorf("label is %q", c.Label)
			}
		}
	}
	if !phone {
		t.Error("the phone contact is missing")
	}

	// Reading it back anonymously gives the same shape.
	read := f.get("/restaurants/" + created.ID)
	if read.Code != http.StatusOK {
		t.Fatalf("reading: %d %s", read.Code, read.Body.String())
	}
	var fetched restaurantResponse
	decode(t, read, &fetched)
	if fetched.ID != created.ID || len(fetched.Contacts) != 2 {
		t.Errorf("read back %+v", fetched)
	}
}

// F3.1: at least one contact, checked before anything is written.
func TestARestaurantNeedsAContact(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("bertil")

	rec := f.post("/restaurants", map[string]any{
		"name": "Contactless", "currency_code": "EUR", "contacts": []map[string]any{},
	}, cookies...)
	expectError(t, rec, http.StatusBadRequest, api.CodeRestaurantNeedsContact)

	// And nothing was created.
	list := f.get("/restaurants")
	if list.Code != http.StatusOK {
		t.Fatal(list.Body.String())
	}
	var body struct {
		Restaurants []restaurantResponse `json:"restaurants"`
	}
	decode(t, list, &body)
	for _, r := range body.Restaurants {
		if r.Name == "Contactless" {
			t.Error("the restaurant was created despite having no contact")
		}
	}
}

// The whole creation is one transaction, so a bad contact takes the restaurant
// with it rather than leaving one that breaks F3.1.
func TestABadContactRollsBackTheRestaurant(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("carla")

	rec := f.post("/restaurants", map[string]any{
		"name": "Half Created", "currency_code": "EUR",
		"contacts": []map[string]any{
			{"contact_type_id": f.contactTypeID("phone"), "value": "+49 30 1"},
			{"contact_type_id": "018f0000-0000-7000-8000-00000000dead", "value": "orphan"},
		},
	}, cookies...)
	if rec.Code < 400 {
		t.Fatalf("an unknown contact type was accepted: %s", rec.Body.String())
	}

	list := f.get("/restaurants")
	var body struct {
		Restaurants []restaurantResponse `json:"restaurants"`
	}
	decode(t, list, &body)
	for _, r := range body.Restaurants {
		if r.Name == "Half Created" {
			t.Error("the restaurant survived a failed contact insert")
		}
	}
}

func TestUnknownCurrencyIsRejected(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("dora")

	rec := f.post("/restaurants", map[string]any{
		"name": "Wrong Money", "currency_code": "XYZ",
		"contacts": []map[string]any{
			{"contact_type_id": f.contactTypeID("phone"), "value": "+49 30 1"},
		},
	}, cookies...)
	expectError(t, rec, http.StatusBadRequest, api.CodeUnknownCurrency)
}

// Anyone logged in may add and edit a restaurant, so the database can be
// crowdsourced. Anonymous callers may only read.
func TestCreatingARestaurantNeedsALogin(t *testing.T) {
	f := newAPIFixture(t)

	rec := f.post("/restaurants", map[string]any{
		"name": "Anonymous Kebab", "currency_code": "EUR",
		"contacts": []map[string]any{{"contact_type_id": f.contactTypeID("phone"), "value": "1"}},
	})
	expectError(t, rec, http.StatusUnauthorized, api.CodeNotAuthenticated)
}

func TestAnyLoggedInUserMayEditARestaurant(t *testing.T) {
	f := newAPIFixture(t)
	creator := f.register("erik")
	stranger := f.register("frieda")

	restaurant := f.createRestaurant("Shared Kebab", creator)

	rec := f.patch("/restaurants/"+restaurant.ID, map[string]any{
		"notes": "edited by somebody else entirely",
	}, stranger...)
	if rec.Code != http.StatusOK {
		t.Errorf("another user could not edit: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRestaurantNamesAreUnique(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("gustav")
	f.createRestaurant("Only One", cookies)

	rec := f.post("/restaurants", map[string]any{
		"name": "ONLY ONE", "currency_code": "EUR",
		"contacts": []map[string]any{{"contact_type_id": f.contactTypeID("phone"), "value": "1"}},
	}, cookies...)
	expectError(t, rec, http.StatusConflict, api.CodeNameExistsHere)
}

// An omitted field is left alone; an explicit null clears it. The two are the
// same nil pointer in Go, so the handler reads the raw keys to tell them apart.
func TestPatchDistinguishesAbsentFromNull(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("hanna")
	restaurant := f.createRestaurant("Nullable", cookies)

	fee := int64(300)
	if rec := f.patch("/restaurants/"+restaurant.ID, map[string]any{
		"delivery_fee_cents":    fee,
		"min_order_value_cents": 1500,
	}, cookies...); rec.Code != http.StatusOK {
		t.Fatalf("setting: %s", rec.Body.String())
	}

	// Patching something else must leave both alone.
	rec := f.patch("/restaurants/"+restaurant.ID, map[string]any{
		"notes": "unrelated change",
	}, cookies...)
	var afterUnrelated restaurantResponse
	decode(t, rec, &afterUnrelated)
	if afterUnrelated.DeliveryFeeCents == nil || *afterUnrelated.DeliveryFeeCents != fee {
		t.Errorf("an omitted field was changed: fee is %v", afterUnrelated.DeliveryFeeCents)
	}

	// An explicit null clears it.
	rec = f.patch("/restaurants/"+restaurant.ID, map[string]any{
		"delivery_fee_cents": nil,
	}, cookies...)
	var afterNull restaurantResponse
	decode(t, rec, &afterNull)
	if afterNull.DeliveryFeeCents != nil {
		t.Errorf("an explicit null did not clear the fee: %v", afterNull.DeliveryFeeCents)
	}
	if afterNull.MinOrderValueCents == nil {
		t.Error("clearing one nullable field cleared another")
	}
}

func TestNegativeMoneyIsRejected(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("ida")
	restaurant := f.createRestaurant("Negative", cookies)

	for _, field := range []string{"min_order_value_cents", "delivery_fee_cents"} {
		rec := f.patch("/restaurants/"+restaurant.ID, map[string]any{field: -1}, cookies...)
		expectError(t, rec, http.StatusBadRequest, api.CodeInvalidField)
	}
}

// F3.5: only the administrator may delete a restaurant, so a misclick cannot
// remove its whole menu.
func TestOnlyTheAdministratorMayDeleteARestaurant(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("jonas")
	restaurant := f.createRestaurant("Deletable", cookies)

	anonymous := f.remove("/restaurants/" + restaurant.ID)
	expectError(t, anonymous, http.StatusUnauthorized, api.CodeNotAuthenticated)

	asUser := f.remove("/restaurants/"+restaurant.ID, cookies...)
	expectError(t, asUser, http.StatusForbidden, api.CodeAdminRequired)

	admin := f.loginAsAdmin("chief")
	if rec := f.remove("/restaurants/"+restaurant.ID, admin...); rec.Code != http.StatusNoContent {
		t.Fatalf("the administrator could not delete: %d %s", rec.Code, rec.Body.String())
	}

	// Gone from the API, though the row survives for the orders that point at
	// it.
	if rec := f.get("/restaurants/" + restaurant.ID); rec.Code != http.StatusNotFound {
		t.Errorf("the deleted restaurant is still readable: %d", rec.Code)
	}
}

// F3.5's second half: a restaurant an order points at cannot be deleted,
// because deleting it would make that order unreadable.
func TestARestaurantInUseCannotBeDeleted(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("klara")
	admin := f.loginAsAdmin("chief")

	restaurant := f.createRestaurant("Busy", cookies)
	f.seedOrderAt(restaurant.ID, "klara", f.now.Add(2*hour))

	rec := f.remove("/restaurants/"+restaurant.ID, admin...)
	expectError(t, rec, http.StatusConflict, api.CodeRestaurantInUse)

	if read := f.get("/restaurants/" + restaurant.ID); read.Code != http.StatusOK {
		t.Error("the refused delete removed it anyway")
	}
}

// The name becomes free again once a restaurant is deleted: the unique index is
// partial, on the living only.
func TestADeletedNameCanBeReused(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("lena")
	admin := f.loginAsAdmin("chief")

	first := f.createRestaurant("Phoenix", cookies)
	if rec := f.remove("/restaurants/"+first.ID, admin...); rec.Code != http.StatusNoContent {
		t.Fatalf("deleting: %s", rec.Body.String())
	}

	second := f.createRestaurant("Phoenix", cookies)
	if second.ID == first.ID {
		t.Error("the second restaurant reused the deleted row")
	}
}

func TestUnknownRestaurantIsNotFound(t *testing.T) {
	f := newAPIFixture(t)

	for _, id := range []string{"018f0000-0000-7000-8000-00000000dead", "not-a-uuid"} {
		if rec := f.get("/restaurants/" + id); rec.Code != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", id, rec.Code)
		}
	}
}

// The seeded reference tables carry codes, not display names: they are named by
// the frontend's i18n catalog, so adding a language is not a migration.
func TestReferenceEndpointsCarryCodesNotNames(t *testing.T) {
	f := newAPIFixture(t)

	currencies := f.get("/currencies")
	if currencies.Code != http.StatusOK {
		t.Fatalf("currencies: %s", currencies.Body.String())
	}
	var currencyBody struct {
		Currencies []struct {
			Code      string `json:"code"`
			Symbol    string `json:"symbol"`
			MinorUnit int    `json:"minor_unit"`
			Name      string `json:"name"`
		} `json:"currencies"`
	}
	decode(t, currencies, &currencyBody)
	if len(currencyBody.Currencies) == 0 {
		t.Fatal("no currencies are seeded")
	}

	var euro bool
	for _, c := range currencyBody.Currencies {
		if c.Name != "" {
			t.Errorf("currency %s carries a display name %q", c.Code, c.Name)
		}
		if c.Code == "EUR" {
			euro = true
			if c.MinorUnit != 2 {
				t.Errorf("EUR has %d minor units, want 2", c.MinorUnit)
			}
		}
	}
	if !euro {
		t.Error("EUR is not seeded")
	}

	types := f.get("/contact-types")
	var typeBody struct {
		ContactTypes []struct {
			Code     string `json:"code"`
			RenderAs string `json:"render_as"`
			Name     string `json:"name"`
		} `json:"contact_types"`
	}
	decode(t, types, &typeBody)
	if len(typeBody.ContactTypes) == 0 {
		t.Fatal("no contact types are seeded")
	}
	for _, ct := range typeBody.ContactTypes {
		if ct.Name != "" {
			t.Errorf("contact type %s carries a display name %q", ct.Code, ct.Name)
		}
		if ct.RenderAs == "" {
			t.Errorf("contact type %s has no render hint", ct.Code)
		}
	}
}

// Reference data is public: the frontend needs it before anybody logs in.
func TestReferenceEndpointsArePublic(t *testing.T) {
	f := newAPIFixture(t)

	for _, path := range []string{"/currencies", "/contact-types"} {
		if rec := f.get(path); rec.Code != http.StatusOK {
			t.Errorf("%s answered %d for an anonymous caller", path, rec.Code)
		}
	}
}
