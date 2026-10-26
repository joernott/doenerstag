package api_test

import (
	"net/http"
	"testing"

	"github.com/joernott/doenerstag/internal/api"
)

// duplicateItem copies a dish and returns the copy.
func (m *menuFixture) duplicateItem(id, label string) menuItemResponse {
	m.t.Helper()

	rec := m.post(m.menuPath("/menu-items/"+id+"/duplicate"),
		map[string]any{"label": label}, m.cookies...)
	if rec.Code != http.StatusCreated {
		m.t.Fatalf("duplicating %s: %d %s", id, rec.Code, rec.Body.String())
	}
	var body menuItemResponse
	decode(m.t, rec, &body)
	return body
}

// readItem reads one dish with its modifications.
func (m *menuFixture) readItem(id string) menuItemResponse {
	m.t.Helper()

	rec := m.get(m.menuPath("/menu-items/" + id))
	if rec.Code != http.StatusOK {
		m.t.Fatalf("reading %s: %d %s", id, rec.Code, rec.Body.String())
	}
	var body menuItemResponse
	decode(m.t, rec, &body)
	return body
}

// 21.1: a copy carries everything the dish has, under a name that says it is
// a copy, in the language the caller named it in.
func TestDuplicatingADishCopiesEverythingItCarries(t *testing.T) {
	m := newMenuFixture(t)
	mains := m.addCategory("Hauptgerichte", 1)
	weekend := m.createFilter(map[string]any{"name": "Wochenende", "weekdays": []int{6, 7}})

	original := m.addItem(map[string]any{
		"name": "Döner", "external_id": "12", "price_cents": 950,
		"description": "Mit Salat", "category_id": mains.ID, "available": false,
		"tag_ids":      []string{m.referenceID("tags", "vegan")},
		"allergen_ids": []string{m.referenceID("allergens", "gluten")},
		"additive_ids": []string{m.referenceID("additives", "colouring")},
	})
	if rec := m.post(m.menuPath("/menu-items/"+original.ID+"/modifications"),
		map[string]any{"name": "ohne Zwiebeln", "price_delta_cents": -20, "sort_order": 3},
		m.cookies...); rec.Code != http.StatusCreated {
		t.Fatalf("adding a modification: %s", rec.Body.String())
	}
	if rec := m.put(m.menuPath("/menu-items/"+original.ID+"/availability"),
		map[string]any{"filter_ids": []string{weekend.ID}}, m.cookies...); rec.Code != http.StatusOK {
		t.Fatalf("attaching a filter: %s", rec.Body.String())
	}

	copied := m.duplicateItem(original.ID, "Kopie")
	if copied.ID == original.ID {
		t.Fatal("the copy has the original's id")
	}
	if copied.Name != "Döner (Kopie)" {
		t.Errorf("the copy is called %q", copied.Name)
	}
	if copied.ExternalID != "12" || copied.PriceCents != 950 || copied.Description != "Mit Salat" ||
		copied.Available || copied.CategoryID == nil || *copied.CategoryID != mains.ID {
		t.Errorf("the copy's fields are %+v", copied)
	}
	if len(copied.Tags) != 1 || len(copied.Allergens) != 1 || len(copied.Additives) != 1 {
		t.Errorf("the copy's classification: %d tags, %d allergens, %d additives",
			len(copied.Tags), len(copied.Allergens), len(copied.Additives))
	}
	// A single item's response leaves its rules to the menu list, which is
	// where they are read.
	for _, item := range m.listItems("") {
		if item.ID == copied.ID && (len(item.Availability) != 1 || item.Availability[0].ID != weekend.ID) {
			t.Errorf("the copy's availability is %+v", item.Availability)
		}
	}
	if len(copied.Modifications) != 1 || copied.Modifications[0].Name != "ohne Zwiebeln" ||
		copied.Modifications[0].PriceDeltaCents != -20 {
		t.Errorf("the copy's modifications are %+v", copied.Modifications)
	}

	// The modifications are the copy's own: changing one leaves the original's.
	if copied.Modifications[0].ID == m.readItem(original.ID).Modifications[0].ID {
		t.Error("the copy shares the original's modification row")
	}

	// It sits directly below the original, which is what its name is for.
	got := names(m.listItems(""))
	if !equal(got, []string{"Döner", "Döner (Kopie)"}) {
		t.Errorf("the menu reads %v", got)
	}
}

// A second copy, or a copy of a copy, finds a name nobody has yet.
func TestASecondCopyIsNumbered(t *testing.T) {
	m := newMenuFixture(t)
	original := m.addItem(map[string]any{"name": "Pide", "price_cents": 800})

	first := m.duplicateItem(original.ID, "")
	second := m.duplicateItem(original.ID, "")
	if first.Name != "Pide (copy)" || second.Name != "Pide (copy 2)" {
		t.Errorf("the copies are %q and %q", first.Name, second.Name)
	}
}

// 21.2: a category is copied with its rules and a copy of every dish in it;
// a deleted dish stays deleted, and dishes elsewhere are not touched.
func TestDuplicatingACategoryCopiesItsDishes(t *testing.T) {
	m := newMenuFixture(t)
	pasta := m.addCategory("Nudeln", 4)
	weekend := m.createFilter(map[string]any{"name": "Wochenende", "weekdays": []int{6, 7}})
	if rec := m.put(m.menuPath("/categories/"+pasta.ID+"/availability"),
		map[string]any{"filter_ids": []string{weekend.ID}}, m.cookies...); rec.Code != http.StatusOK {
		t.Fatalf("attaching a filter: %s", rec.Body.String())
	}

	m.addItem(map[string]any{"name": "Penne", "price_cents": 900, "category_id": pasta.ID})
	m.addItem(map[string]any{"name": "Lasagne", "price_cents": 1100, "category_id": pasta.ID})
	gone := m.addItem(map[string]any{"name": "Ravioli", "price_cents": 1000, "category_id": pasta.ID})
	m.addItem(map[string]any{"name": "Ayran", "price_cents": 250})
	if rec := m.remove(m.menuPath("/menu-items/"+gone.ID), m.admin...); rec.Code != http.StatusNoContent {
		t.Fatalf("deleting: %d %s", rec.Code, rec.Body.String())
	}

	rec := m.post(m.menuPath("/categories/"+pasta.ID+"/duplicate"),
		map[string]any{"label": "copie"}, m.cookies...)
	if rec.Code != http.StatusCreated {
		t.Fatalf("duplicating: %d %s", rec.Code, rec.Body.String())
	}
	var copied categoryResponse
	decode(t, rec, &copied)

	if copied.Name != "Nudeln (copie)" || copied.SortOrder != 4 {
		t.Errorf("the copy is %+v", copied)
	}
	if len(copied.Availability) != 1 || copied.Availability[0].ID != weekend.ID {
		t.Errorf("the copy's availability is %+v", copied.Availability)
	}

	got := names(m.listItems("?category=" + copied.ID))
	if !equal(got, []string{"Lasagne (copie)", "Penne (copie)"}) {
		t.Errorf("the copied category holds %v", got)
	}
	if got := names(m.listItems("?category=" + pasta.ID)); !equal(got, []string{"Lasagne", "Penne"}) {
		t.Errorf("the original category now holds %v", got)
	}

	categories := m.readCategories()
	if len(categories) != 2 || categories[0].ID != pasta.ID || categories[1].ID != copied.ID {
		t.Errorf("the categories read %+v", categories)
	}
}

// Copying is creating, so it is open to the same people: anybody signed in,
// and nobody who is not.
func TestDuplicatingNeedsASignedInUser(t *testing.T) {
	m := newMenuFixture(t)
	item := m.addItem(map[string]any{"name": "Döner", "price_cents": 950})
	category := m.addCategory("Hauptgerichte", 1)

	for _, path := range []string{
		"/menu-items/" + item.ID + "/duplicate",
		"/categories/" + category.ID + "/duplicate",
	} {
		rec := m.post(m.menuPath(path), map[string]any{})
		expectErrorCode(t, rec, api.CodeNotAuthenticated)
	}
}

// A dish is copied within its own restaurant; naming it under another one is
// not finding it.
func TestADishIsDuplicatedOnlyUnderItsOwnRestaurant(t *testing.T) {
	m := newMenuFixture(t)
	item := m.addItem(map[string]any{"name": "Döner", "price_cents": 950})
	other := m.createRestaurant("Pizzeria", m.cookies).ID

	rec := m.post("/restaurants/"+other+"/menu-items/"+item.ID+"/duplicate",
		map[string]any{}, m.cookies...)
	expectErrorCode(t, rec, api.CodeNotFound)
}
