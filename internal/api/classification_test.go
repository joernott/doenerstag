package api_test

import (
	"net/http"
	"sort"
	"testing"

	"github.com/joernott/doenerstag/internal/api"
)

type referenceListResponse struct {
	Tags []struct {
		ID   string `json:"id"`
		Code string `json:"code"`
		Name string `json:"name"`
	} `json:"tags"`
	Allergens []struct {
		ID        string `json:"id"`
		Code      string `json:"code"`
		Reference string `json:"reference"`
		Name      string `json:"name"`
	} `json:"allergens"`
	Additives []struct {
		ID        string `json:"id"`
		Code      string `json:"code"`
		Reference string `json:"reference"`
		Name      string `json:"name"`
	} `json:"additives"`
}

// referenceID looks up a seeded tag, allergen or additive by code.
func (m *menuFixture) referenceID(kind, code string) string {
	m.t.Helper()

	rec := m.get("/" + kind)
	if rec.Code != http.StatusOK {
		m.t.Fatalf("listing %s: %s", kind, rec.Body.String())
	}
	var body referenceListResponse
	decode(m.t, rec, &body)

	switch kind {
	case "tags":
		for _, t := range body.Tags {
			if t.Code == code {
				return t.ID
			}
		}
	case "allergens":
		for _, a := range body.Allergens {
			if a.Code == code {
				return a.ID
			}
		}
	case "additives":
		for _, a := range body.Additives {
			if a.Code == code {
				return a.ID
			}
		}
	}
	m.t.Fatalf("no seeded %s with code %q", kind, code)
	return ""
}

// The regulated lists are read-only and carry codes, not display names.
func TestAllergensAndAdditivesAreReadOnlyCodeLists(t *testing.T) {
	m := newMenuFixture(t)

	for _, kind := range []string{"allergens", "additives"} {
		rec := m.get("/" + kind)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %s", kind, rec.Body.String())
		}

		var body referenceListResponse
		decode(t, rec, &body)

		entries := body.Allergens
		if kind == "additives" {
			entries = body.Additives
		}
		if len(entries) == 0 {
			t.Fatalf("no %s are seeded", kind)
		}
		for _, e := range entries {
			if e.Name != "" {
				t.Errorf("%s %s carries a display name %q", kind, e.Code, e.Name)
			}
			if e.Code == "" {
				t.Errorf("a %s entry has no code", kind)
			}
		}

		// No write route exists at all, so a POST is 405 rather than 403.
		post := m.post("/"+kind, map[string]any{"code": "invented"}, m.cookies...)
		if post.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST /%s answered %d, want 405", kind, post.Code)
		}
	}
}

// Tags are the one reference table users may add to, because the point of them
// is to describe food in whatever terms a group uses.
func TestCreatingAFreeTag(t *testing.T) {
	m := newMenuFixture(t)

	rec := m.post("/tags", map[string]any{
		"code": "extra-scharf", "name": "Extra scharf",
	}, m.cookies...)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	var created struct {
		ID   string `json:"id"`
		Code string `json:"code"`
		Name string `json:"name"`
	}
	decode(t, rec, &created)
	if created.Code != "extra-scharf" {
		t.Errorf("the code is %q", created.Code)
	}
	// A user-created tag has no catalog entry, so it keeps the name that was
	// typed.
	if created.Name != "Extra scharf" {
		t.Errorf("the name is %q", created.Name)
	}

	// It appears in the list alongside the seeded ones.
	list := m.get("/tags")
	var body referenceListResponse
	decode(t, list, &body)
	var found bool
	for _, tag := range body.Tags {
		if tag.Code == "extra-scharf" {
			found = true
		}
	}
	if !found {
		t.Error("the new tag is missing from the list")
	}
}

func TestTagValidation(t *testing.T) {
	m := newMenuFixture(t)

	cases := map[string]struct {
		body map[string]any
		code api.Code
	}{
		"no code":     {map[string]any{"name": "Ohne Code"}, api.CodeMissingField},
		"no name":     {map[string]any{"code": "ohne-name"}, api.CodeMissingField},
		"upper case":  {map[string]any{"code": "Nicht Klein", "name": "x"}, api.CodeInvalidField},
		"spaces":      {map[string]any{"code": "mit leerzeichen", "name": "x"}, api.CodeInvalidField},
		"punctuation": {map[string]any{"code": "mit.punkt", "name": "x"}, api.CodeInvalidField},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := m.post("/tags", tc.body, m.cookies...)
			expectError(t, rec, http.StatusBadRequest, tc.code)
		})
	}

	anonymous := m.post("/tags", map[string]any{"code": "heimlich", "name": "x"})
	expectError(t, anonymous, http.StatusUnauthorized, api.CodeNotAuthenticated)
}

func TestDuplicateTagCodeIsRejected(t *testing.T) {
	m := newMenuFixture(t)

	if rec := m.post("/tags", map[string]any{"code": "hausgemacht", "name": "Hausgemacht"},
		m.cookies...); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body.String())
	}

	rec := m.post("/tags", map[string]any{"code": "hausgemacht", "name": "Nochmal"}, m.cookies...)
	expectError(t, rec, http.StatusConflict, api.CodeNameExistsHere)
}

// 7.6: classification is assigned to an item and comes back with it.
func TestAssigningClassificationToAnItem(t *testing.T) {
	m := newMenuFixture(t)

	vegan := m.referenceID("tags", "vegan")
	gluten := m.referenceID("allergens", "gluten")
	colourings := m.referenceID("additives", "colouring")

	created := m.addItem(map[string]any{
		"name": "Falafel", "price_cents": 550,
		"tag_ids":      []string{vegan},
		"allergen_ids": []string{gluten},
		"additive_ids": []string{colourings},
	})

	if len(created.Tags) != 1 || created.Tags[0].Code != "vegan" {
		t.Errorf("tags are %+v", created.Tags)
	}
	if len(created.Allergens) != 1 || created.Allergens[0].Code != "gluten" {
		t.Errorf("allergens are %+v", created.Allergens)
	}
	if len(created.Additives) != 1 || created.Additives[0].Code != "colouring" {
		t.Errorf("additives are %+v", created.Additives)
	}

	// The allergen carries its Annex II reference number.
	if created.Allergens[0].Reference == "" {
		t.Error("the allergen has no reference number")
	}

	// And it comes back on the list, not only on the create response.
	listed := m.listItems("")
	if len(listed) != 1 || len(listed[0].Tags) != 1 {
		t.Errorf("the list does not carry the classification: %+v", listed)
	}
}

// A PATCH that changes the price must not clear the allergens.
func TestPatchingAnItemLeavesUnmentionedClassificationAlone(t *testing.T) {
	m := newMenuFixture(t)
	gluten := m.referenceID("allergens", "gluten")
	vegan := m.referenceID("tags", "vegan")

	item := m.addItem(map[string]any{
		"name": "Seitan", "price_cents": 700,
		"tag_ids": []string{vegan}, "allergen_ids": []string{gluten},
	})

	rec := m.patch(m.menuPath("/menu-items/"+item.ID),
		map[string]any{"price_cents": 750}, m.cookies...)
	if rec.Code != http.StatusOK {
		t.Fatalf("patching: %s", rec.Body.String())
	}

	var patched menuItemResponse
	decode(t, rec, &patched)
	if patched.PriceCents != 750 {
		t.Errorf("the price is %d", patched.PriceCents)
	}
	if len(patched.Allergens) != 1 {
		t.Errorf("the allergens were cleared by an unrelated patch: %+v", patched.Allergens)
	}
	if len(patched.Tags) != 1 {
		t.Errorf("the tags were cleared by an unrelated patch: %+v", patched.Tags)
	}
}

// An explicit empty array does clear them, because that is the caller saying
// "this is now the whole set".
func TestAnEmptyClassificationArrayClearsIt(t *testing.T) {
	m := newMenuFixture(t)
	vegan := m.referenceID("tags", "vegan")

	item := m.addItem(map[string]any{
		"name": "Gemüse", "price_cents": 400, "tag_ids": []string{vegan},
	})

	rec := m.patch(m.menuPath("/menu-items/"+item.ID),
		map[string]any{"tag_ids": []string{}}, m.cookies...)
	if rec.Code != http.StatusOK {
		t.Fatalf("patching: %s", rec.Body.String())
	}

	var patched menuItemResponse
	decode(t, rec, &patched)
	if len(patched.Tags) != 0 {
		t.Errorf("an explicit empty array did not clear the tags: %+v", patched.Tags)
	}
}

func TestUnknownClassificationIsRejected(t *testing.T) {
	m := newMenuFixture(t)

	rec := m.post(m.menuPath("/menu-items"), map[string]any{
		"name": "Unbekannt", "price_cents": 500,
		"tag_ids": []string{"018f0000-0000-7000-8000-00000000dead"},
	}, m.cookies...)
	if rec.Code < 400 {
		t.Errorf("an unknown tag id was accepted: %s", rec.Body.String())
	}

	malformed := m.post(m.menuPath("/menu-items"), map[string]any{
		"name": "Falsch", "price_cents": 500, "tag_ids": []string{"vegan"},
	}, m.cookies...)
	expectError(t, malformed, http.StatusBadRequest, api.CodeInvalidField)
}

// The exit criterion: filtered by tag and by allergen exclusion.
func TestFilteringByTagAndByAllergenExclusion(t *testing.T) {
	m := newMenuFixture(t)

	vegan := m.referenceID("tags", "vegan")
	vegetarian := m.referenceID("tags", "vegetarian")
	gluten := m.referenceID("allergens", "gluten")
	nuts := m.referenceID("allergens", "nuts")

	m.addItem(map[string]any{
		"name": "Falafel", "external_id": "1", "price_cents": 550,
		"tag_ids": []string{vegan}, "allergen_ids": []string{gluten},
	})
	m.addItem(map[string]any{
		"name": "Nussteller", "external_id": "2", "price_cents": 600,
		"tag_ids": []string{vegan}, "allergen_ids": []string{nuts},
	})
	m.addItem(map[string]any{
		"name": "Käsepide", "external_id": "3", "price_cents": 650,
		"tag_ids": []string{vegetarian}, "allergen_ids": []string{gluten},
	})
	m.addItem(map[string]any{
		"name": "Döner", "external_id": "4", "price_cents": 500,
		"allergen_ids": []string{gluten},
	})

	cases := map[string]struct {
		query string
		want  []string
	}{
		"everything": {
			"", []string{"Falafel", "Nussteller", "Käsepide", "Döner"},
		},
		"one tag": {
			"?tag=vegan", []string{"Falafel", "Nussteller"},
		},
		"two tags are an OR": {
			"?tag=vegan&tag=vegetarian", []string{"Falafel", "Nussteller", "Käsepide"},
		},
		"excluding an allergen": {
			"?exclude_allergen=gluten", []string{"Nussteller"},
		},
		"excluding two allergens": {
			"?exclude_allergen=gluten&exclude_allergen=nuts", nil,
		},
		"a tag and an exclusion are an AND": {
			"?tag=vegan&exclude_allergen=nuts", []string{"Falafel"},
		},
		"an exclusion that matches nothing removes nothing": {
			"?exclude_allergen=celery",
			[]string{"Falafel", "Nussteller", "Käsepide", "Döner"},
		},
		"an unknown tag matches nothing": {
			"?tag=nicht-vorhanden", nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := names(m.listItems(tc.query))
			if tc.want == nil {
				if len(got) != 0 {
					t.Errorf("%s returned %v, want nothing", tc.query, got)
				}
				return
			}
			if !equal(got, tc.want) {
				t.Errorf("%s returned\n  %v\nwant\n  %v", tc.query, got, tc.want)
			}
		})
	}
}

// Every filter combined at once, which is the shape a real filter panel sends.
func TestFiltersCombine(t *testing.T) {
	m := newMenuFixture(t)
	category := m.addCategory("Hauptgerichte", 1)
	vegan := m.referenceID("tags", "vegan")
	gluten := m.referenceID("allergens", "gluten")

	// The one item that satisfies everything.
	m.addItem(map[string]any{
		"name": "Reisbowl", "external_id": "1", "price_cents": 700,
		"category_id": category.ID, "tag_ids": []string{vegan},
	})
	// Right category and tag, but carries the excluded allergen.
	m.addItem(map[string]any{
		"name": "Weizenbowl", "external_id": "2", "price_cents": 700,
		"category_id": category.ID, "tag_ids": []string{vegan},
		"allergen_ids": []string{gluten},
	})
	// Right everything, but unavailable.
	unavailable := m.addItem(map[string]any{
		"name": "Ausverkauft", "external_id": "3", "price_cents": 700,
		"category_id": category.ID, "tag_ids": []string{vegan},
	})
	if rec := m.patch(m.menuPath("/menu-items/"+unavailable.ID),
		map[string]any{"available": false}, m.cookies...); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	// Right tag, wrong category.
	m.addItem(map[string]any{
		"name": "Ohne Kategorie", "external_id": "4", "price_cents": 700,
		"tag_ids": []string{vegan},
	})

	got := names(m.listItems(
		"?category=" + category.ID + "&tag=vegan&exclude_allergen=gluten&available=true"))
	if !equal(got, []string{"Reisbowl"}) {
		t.Errorf("the combined filter returned %v, want [Reisbowl]", got)
	}
}

func TestFilterValidation(t *testing.T) {
	m := newMenuFixture(t)

	for _, query := range []string{"?category=not-a-uuid", "?available=maybe"} {
		rec := m.get(m.menuPath("/menu-items") + query)
		expectError(t, rec, http.StatusBadRequest, api.CodeInvalidField)
	}

	// An empty value is the same as omitting the parameter, not an error: a
	// filter panel with nothing selected sends exactly that.
	if rec := m.get(m.menuPath("/menu-items") + "?tag=&exclude_allergen="); rec.Code != http.StatusOK {
		t.Errorf("empty filter values answered %d: %s", rec.Code, rec.Body.String())
	}
}

// A soft-deleted item must not come back through any filter either.
func TestFiltersHideDeletedItems(t *testing.T) {
	m := newMenuFixture(t)
	vegan := m.referenceID("tags", "vegan")

	item := m.addItem(map[string]any{
		"name": "Gelöscht", "price_cents": 500, "tag_ids": []string{vegan},
	})
	if rec := m.remove(m.menuPath("/menu-items/"+item.ID), m.admin...); rec.Code != http.StatusNoContent {
		t.Fatal(rec.Body.String())
	}

	for _, query := range []string{"", "?tag=vegan", "?available=true", "?exclude_allergen=nuts"} {
		if got := m.listItems(query); len(got) != 0 {
			t.Errorf("%q returned the deleted item: %v", query, names(got))
		}
	}
}

// 7.4: modifications are a flat, freely combinable list.
func TestModifications(t *testing.T) {
	m := newMenuFixture(t)
	item := m.addItem(map[string]any{"name": "Döner", "price_cents": 500})
	path := m.menuPath("/menu-items/" + item.ID + "/modifications")

	// A negative delta is legal: "without meat" can reasonably cost less.
	for _, mod := range []map[string]any{
		{"name": "ohne Zwiebeln", "price_delta_cents": 0, "sort_order": 1},
		{"name": "extra Knoblauchsauce", "price_delta_cents": 50, "sort_order": 2},
		{"name": "ohne Fleisch", "price_delta_cents": -100, "sort_order": 3},
	} {
		rec := m.post(path, mod, m.cookies...)
		if rec.Code != http.StatusCreated {
			t.Fatalf("creating %v: %s", mod["name"], rec.Body.String())
		}
	}

	rec := m.get(path)
	var body struct {
		Modifications []struct {
			ID              string `json:"id"`
			Name            string `json:"name"`
			PriceDeltaCents int64  `json:"price_delta_cents"`
		} `json:"modifications"`
	}
	decode(t, rec, &body)
	if len(body.Modifications) != 3 {
		t.Fatalf("listed %d modifications, want 3", len(body.Modifications))
	}
	if body.Modifications[0].Name != "ohne Zwiebeln" {
		t.Errorf("the sort order is not respected: %+v", body.Modifications)
	}

	var negative bool
	for _, mod := range body.Modifications {
		if mod.PriceDeltaCents < 0 {
			negative = true
		}
	}
	if !negative {
		t.Error("the negative price delta was not stored")
	}

	// They come back on the item's own read, which is what an order form needs.
	detail := m.get(m.menuPath("/menu-items/" + item.ID))
	var detailed menuItemResponse
	decode(t, detail, &detailed)
	if len(detailed.Modifications) != 3 {
		t.Errorf("the item carries %d modifications", len(detailed.Modifications))
	}

	// But not on the list, where nothing shows them.
	listed := m.listItems("")
	if len(listed) != 1 {
		t.Fatal("the item vanished")
	}
	if len(listed[0].Modifications) != 0 {
		t.Error("the list carries modifications, which no list page shows")
	}
}

func TestModificationsBelongToTheirItem(t *testing.T) {
	m := newMenuFixture(t)
	mine := m.addItem(map[string]any{"name": "Meins", "price_cents": 500})
	theirs := m.addItem(map[string]any{"name": "Deins", "price_cents": 500})

	rec := m.post(m.menuPath("/menu-items/"+theirs.ID+"/modifications"),
		map[string]any{"name": "fremd"}, m.cookies...)
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	decode(t, rec, &created)

	// The other item's path must not reach it.
	patch := m.patch(m.menuPath("/menu-items/"+mine.ID+"/modifications/"+created.ID),
		map[string]any{"name": "übernommen"}, m.cookies...)
	if patch.Code != http.StatusNotFound {
		t.Errorf("editing across items answered %d, want 404", patch.Code)
	}

	del := m.remove(m.menuPath("/menu-items/"+mine.ID+"/modifications/"+created.ID), m.admin...)
	if del.Code != http.StatusNotFound {
		t.Errorf("deleting across items answered %d, want 404", del.Code)
	}
}

func TestASoftDeletedModificationDisappears(t *testing.T) {
	m := newMenuFixture(t)
	item := m.addItem(map[string]any{"name": "Döner", "price_cents": 500})
	path := m.menuPath("/menu-items/" + item.ID + "/modifications")

	rec := m.post(path, map[string]any{"name": "ohne Zwiebeln"}, m.cookies...)
	var created struct {
		ID string `json:"id"`
	}
	decode(t, rec, &created)

	if del := m.remove(path+"/"+created.ID, m.admin...); del.Code != http.StatusNoContent {
		t.Fatalf("deleting: %s", del.Body.String())
	}

	list := m.get(path)
	var body struct {
		Modifications []struct {
			ID string `json:"id"`
		} `json:"modifications"`
	}
	decode(t, list, &body)
	if len(body.Modifications) != 0 {
		t.Errorf("the deleted modification is still listed: %+v", body.Modifications)
	}

	// And its name is free again.
	if again := m.post(path, map[string]any{"name": "ohne Zwiebeln"}, m.cookies...); again.Code != http.StatusCreated {
		t.Errorf("the name was not released: %s", again.Body.String())
	}
}

func TestModificationNamesAreUniquePerItem(t *testing.T) {
	m := newMenuFixture(t)
	item := m.addItem(map[string]any{"name": "Döner", "price_cents": 500})
	path := m.menuPath("/menu-items/" + item.ID + "/modifications")

	if rec := m.post(path, map[string]any{"name": "scharf"}, m.cookies...); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body.String())
	}
	rec := m.post(path, map[string]any{"name": "SCHARF"}, m.cookies...)
	expectError(t, rec, http.StatusConflict, api.CodeNameExistsHere)
}

// The seeded tag list is what the fixture's lookups rely on, so a missing
// seed would show up as a confusing failure elsewhere. Assert it directly.
func TestTheSeededReferenceDataIsPresent(t *testing.T) {
	m := newMenuFixture(t)

	rec := m.get("/tags")
	var body referenceListResponse
	decode(t, rec, &body)

	codes := make([]string, 0, len(body.Tags))
	for _, tag := range body.Tags {
		codes = append(codes, tag.Code)
	}
	sort.Strings(codes)

	for _, want := range []string{"vegan", "vegetarian"} {
		var found bool
		for _, code := range codes {
			if code == want {
				found = true
			}
		}
		if !found {
			t.Errorf("the seeded tag %q is missing; seeded tags are %v", want, codes)
		}
	}
}
