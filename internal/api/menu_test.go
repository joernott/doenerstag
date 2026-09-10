package api_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/joernott/doenerstag/internal/api"
)

type categoryResponse struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	SortOrder    int                `json:"sort_order"`
	Availability []availabilityResp `json:"availability"`
}

type menuItemResponse struct {
	ID           string  `json:"id"`
	RestaurantID string  `json:"restaurant_id"`
	CategoryID   *string `json:"category_id"`
	ExternalID   string  `json:"external_id"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	ImageID      *string `json:"image_id"`
	PriceCents   int64   `json:"price_cents"`
	Available    bool    `json:"available"`

	Availability []availabilityResp `json:"availability"`
	AvailableAt  *bool              `json:"available_at"`

	Tags []struct {
		ID   string `json:"id"`
		Code string `json:"code"`
		Name string `json:"name"`
	} `json:"tags"`
	Allergens []struct {
		ID        string `json:"id"`
		Code      string `json:"code"`
		Reference string `json:"reference"`
	} `json:"allergens"`
	Additives []struct {
		ID        string `json:"id"`
		Code      string `json:"code"`
		Reference string `json:"reference"`
	} `json:"additives"`
	Modifications []struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		PriceDeltaCents int64  `json:"price_delta_cents"`
		SortOrder       int    `json:"sort_order"`
	} `json:"modifications"`
}

type menuListResponse struct {
	MenuItems []menuItemResponse `json:"menu_items"`
}

// menuFixture is a restaurant with a menu to work on.
type menuFixture struct {
	*apiFixture
	cookies    []*http.Cookie
	admin      []*http.Cookie
	restaurant string
}

func newMenuFixture(t *testing.T) *menuFixture {
	t.Helper()

	f := newAPIFixture(t)
	m := &menuFixture{apiFixture: f}
	m.cookies = f.register("cook")
	m.admin = f.loginAsAdmin("chief")
	m.restaurant = f.createRestaurant("Döner Palast", m.cookies).ID
	return m
}

func (m *menuFixture) menuPath(suffix string) string {
	return "/restaurants/" + m.restaurant + suffix
}

// addCategory creates one and returns it.
func (m *menuFixture) addCategory(name string, sortOrder int) categoryResponse {
	m.t.Helper()

	rec := m.post(m.menuPath("/categories"),
		map[string]any{"name": name, "sort_order": sortOrder}, m.cookies...)
	if rec.Code != http.StatusCreated {
		m.t.Fatalf("creating category %q: %d %s", name, rec.Code, rec.Body.String())
	}
	var body categoryResponse
	decode(m.t, rec, &body)
	return body
}

// addItem creates a menu item with the given fields.
func (m *menuFixture) addItem(fields map[string]any) menuItemResponse {
	m.t.Helper()

	rec := m.post(m.menuPath("/menu-items"), fields, m.cookies...)
	if rec.Code != http.StatusCreated {
		m.t.Fatalf("creating item %v: %d %s", fields["name"], rec.Code, rec.Body.String())
	}
	var body menuItemResponse
	decode(m.t, rec, &body)
	return body
}

// listItems fetches the menu with an optional query string.
func (m *menuFixture) listItems(query string) []menuItemResponse {
	m.t.Helper()

	rec := m.get(m.menuPath("/menu-items") + query)
	if rec.Code != http.StatusOK {
		m.t.Fatalf("listing%s: %d %s", query, rec.Code, rec.Body.String())
	}
	var body menuListResponse
	decode(m.t, rec, &body)
	return body.MenuItems
}

// names is the item names in order, which is what the ordering tests compare.
func names(items []menuItemResponse) []string {
	out := make([]string, 0, len(items))
	for i := range items {
		out = append(out, items[i].Name)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// F4.6, the rule the sprint's exit criteria call "ordered as a printed menu
// would be".
//
// A purely numeric item number sorts numerically, so 2 comes before 10 rather
// than after it as text would. A mixed number sorts as text, after every plain
// number. Items with no number sort last, by name.
func TestMenuItemsAreOrderedLikeAPrintedMenu(t *testing.T) {
	m := newMenuFixture(t)

	// Deliberately inserted out of order, and with the numbers chosen so that
	// text sorting and numeric sorting disagree.
	for _, item := range []struct {
		externalID string
		name       string
	}{
		{"10", "Lahmacun"},
		{"2", "Döner Kebab"},
		{"", "Tagesgericht"},
		{"A3", "Ayran"},
		{"1", "Falafel"},
		{"", "Suppe"},
		{"20", "Pide"},
		{"A1", "Cola"},
		{"3", "Dürüm"},
	} {
		m.addItem(map[string]any{
			"name": item.name, "external_id": item.externalID, "price_cents": 500,
		})
	}

	want := []string{
		// Numeric ids, numerically: 1, 2, 3, 10, 20.
		"Falafel", "Döner Kebab", "Dürüm", "Lahmacun", "Pide",
		// Then mixed ids as text: A1, A3.
		"Cola", "Ayran",
		// Then the unnumbered, by name.
		"Suppe", "Tagesgericht",
	}

	got := names(m.listItems(""))
	if !equal(got, want) {
		t.Errorf("the menu is ordered\n  %v\nwant\n  %v", got, want)
	}
}

// The same rule applies within a category, not only in the flat list.
func TestTheOrderingRuleAppliesWithinACategory(t *testing.T) {
	m := newMenuFixture(t)
	mains := m.addCategory("Hauptgerichte", 1)

	for _, item := range []struct{ externalID, name string }{
		{"12", "Zwölf"},
		{"3", "Drei"},
		{"7", "Sieben"},
	} {
		m.addItem(map[string]any{
			"name": item.name, "external_id": item.externalID,
			"price_cents": 500, "category_id": mains.ID,
		})
	}

	got := names(m.listItems("?category=" + mains.ID))
	want := []string{"Drei", "Sieben", "Zwölf"}
	if !equal(got, want) {
		t.Errorf("within a category the order is %v, want %v", got, want)
	}
}

// Two items with the same number fall back to the name.
func TestItemsWithTheSameNumberSortByName(t *testing.T) {
	m := newMenuFixture(t)

	for _, name := range []string{"Zander", "Aal", "Makrele"} {
		m.addItem(map[string]any{"name": name, "external_id": "5", "price_cents": 500})
	}

	got := names(m.listItems(""))
	want := []string{"Aal", "Makrele", "Zander"}
	if !equal(got, want) {
		t.Errorf("items sharing a number are ordered %v, want %v", got, want)
	}
}

func TestCreatingAndReadingAMenuItem(t *testing.T) {
	m := newMenuFixture(t)
	starters := m.addCategory("Vorspeisen", 1)

	created := m.addItem(map[string]any{
		"name": "Hummus", "external_id": "4", "price_cents": 450,
		"description": "Mit Fladenbrot", "category_id": starters.ID,
	})
	if created.PriceCents != 450 || created.ExternalID != "4" {
		t.Errorf("created %+v", created)
	}
	if created.CategoryID == nil || *created.CategoryID != starters.ID {
		t.Errorf("the category is %v", created.CategoryID)
	}
	if !created.Available {
		t.Error("a new item is not available by default")
	}

	// Anonymously, because a menu is public.
	rec := m.get(m.menuPath("/menu-items/" + created.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("reading: %d %s", rec.Code, rec.Body.String())
	}
	var read menuItemResponse
	decode(t, rec, &read)
	if read.Name != "Hummus" || read.Description != "Mit Fladenbrot" {
		t.Errorf("read back %+v", read)
	}
}

// available false is "temporarily sold out": still on the menu, not orderable.
// It is not a soft delete, and the two must not be confused.
func TestAnUnavailableItemStaysOnTheMenu(t *testing.T) {
	m := newMenuFixture(t)
	item := m.addItem(map[string]any{"name": "Ausverkauft", "price_cents": 500})

	rec := m.patch(m.menuPath("/menu-items/"+item.ID),
		map[string]any{"available": false}, m.cookies...)
	if rec.Code != http.StatusOK {
		t.Fatalf("marking unavailable: %s", rec.Body.String())
	}

	all := m.listItems("")
	if len(all) != 1 {
		t.Fatalf("the unavailable item vanished from the menu: %d items", len(all))
	}
	if all[0].Available {
		t.Error("the item is still marked available")
	}

	// And the filter can exclude it when a client wants only orderable food.
	orderable := m.listItems("?available=true")
	if len(orderable) != 0 {
		t.Errorf("available=true returned %d items", len(orderable))
	}
	unavailable := m.listItems("?available=false")
	if len(unavailable) != 1 {
		t.Errorf("available=false returned %d items", len(unavailable))
	}
}

// F4.5: only the administrator deletes menu data.
func TestOnlyTheAdministratorDeletesMenuData(t *testing.T) {
	m := newMenuFixture(t)
	category := m.addCategory("Zu löschen", 1)
	item := m.addItem(map[string]any{"name": "Zu löschen", "price_cents": 500})

	for _, path := range []string{
		m.menuPath("/categories/" + category.ID),
		m.menuPath("/menu-items/" + item.ID),
	} {
		anonymous := m.remove(path)
		expectError(t, anonymous, http.StatusUnauthorized, api.CodeNotAuthenticated)

		asUser := m.remove(path, m.cookies...)
		expectError(t, asUser, http.StatusForbidden, api.CodeAdminRequired)

		if rec := m.remove(path, m.admin...); rec.Code != http.StatusNoContent {
			t.Errorf("%s: the administrator could not delete: %d %s",
				path, rec.Code, rec.Body.String())
		}
	}
}

// 7.8: a soft-deleted item is hidden from every normal query.
func TestASoftDeletedItemIsHiddenEverywhere(t *testing.T) {
	m := newMenuFixture(t)
	item := m.addItem(map[string]any{"name": "Verschwunden", "price_cents": 500})
	other := m.addItem(map[string]any{"name": "Geblieben", "price_cents": 500})

	if rec := m.remove(m.menuPath("/menu-items/"+item.ID), m.admin...); rec.Code != http.StatusNoContent {
		t.Fatalf("deleting: %s", rec.Body.String())
	}

	// Gone from the list.
	remaining := m.listItems("")
	if len(remaining) != 1 || remaining[0].ID != other.ID {
		t.Errorf("the list is %v", names(remaining))
	}

	// Gone from the direct read.
	if rec := m.get(m.menuPath("/menu-items/" + item.ID)); rec.Code != http.StatusNotFound {
		t.Errorf("the deleted item still reads: %d", rec.Code)
	}

	// Gone from every write path, too: editing something invisible must not
	// work.
	if rec := m.patch(m.menuPath("/menu-items/"+item.ID),
		map[string]any{"name": "Wiederbelebt"}, m.cookies...); rec.Code != http.StatusNotFound {
		t.Errorf("a deleted item could be edited: %d %s", rec.Code, rec.Body.String())
	}
	if rec := m.remove(m.menuPath("/menu-items/"+item.ID), m.admin...); rec.Code != http.StatusNotFound {
		t.Errorf("a deleted item could be deleted again: %d", rec.Code)
	}

	// And its name is free again, because the unique index is partial.
	revived := m.addItem(map[string]any{"name": "Verschwunden", "price_cents": 500})
	if revived.ID == item.ID {
		t.Error("the new item reused the deleted row")
	}
}

// Deleting a category must not delete the food in it.
func TestDeletingACategoryReleasesItsItems(t *testing.T) {
	m := newMenuFixture(t)
	category := m.addCategory("Verschwindet", 1)
	item := m.addItem(map[string]any{
		"name": "Bleibt", "price_cents": 500, "category_id": category.ID,
	})

	if rec := m.remove(m.menuPath("/categories/"+category.ID), m.admin...); rec.Code != http.StatusNoContent {
		t.Fatalf("deleting the category: %s", rec.Body.String())
	}

	items := m.listItems("")
	if len(items) != 1 {
		t.Fatalf("the item went with its category: %d items remain", len(items))
	}
	if items[0].ID != item.ID {
		t.Error("the wrong item survived")
	}
	if items[0].CategoryID != nil {
		t.Errorf("the item still points at the deleted category: %v", items[0].CategoryID)
	}
}

func TestCategoriesAreOrderedEditorially(t *testing.T) {
	m := newMenuFixture(t)
	m.addCategory("Nachspeisen", 30)
	m.addCategory("Vorspeisen", 10)
	m.addCategory("Hauptgerichte", 20)

	rec := m.get(m.menuPath("/categories"))
	var body struct {
		Categories []categoryResponse `json:"categories"`
	}
	decode(t, rec, &body)

	got := make([]string, 0, len(body.Categories))
	for _, c := range body.Categories {
		got = append(got, c.Name)
	}
	want := []string{"Vorspeisen", "Hauptgerichte", "Nachspeisen"}
	if !equal(got, want) {
		t.Errorf("categories are ordered %v, want %v", got, want)
	}
}

// An item cannot be filed under another restaurant's category, which the
// foreign key alone would allow.
func TestAnItemCannotJoinAnotherRestaurantsCategory(t *testing.T) {
	m := newMenuFixture(t)
	other := m.createRestaurant("Anderes Lokal", m.cookies)

	foreign := m.post("/restaurants/"+other.ID+"/categories",
		map[string]any{"name": "Fremd"}, m.cookies...)
	var category categoryResponse
	decode(t, foreign, &category)

	rec := m.post(m.menuPath("/menu-items"), map[string]any{
		"name": "Falsch einsortiert", "price_cents": 500, "category_id": category.ID,
	}, m.cookies...)
	expectError(t, rec, http.StatusBadRequest, api.CodeInvalidField)
}

func TestMenuItemValidation(t *testing.T) {
	m := newMenuFixture(t)

	cases := map[string]struct {
		body map[string]any
		code api.Code
	}{
		"no name":         {map[string]any{"price_cents": 500}, api.CodeMissingField},
		"blank name":      {map[string]any{"name": "   ", "price_cents": 500}, api.CodeMissingField},
		"negative price":  {map[string]any{"name": "Minus", "price_cents": -1}, api.CodeInvalidField},
		"bad external id": {map[string]any{"name": "Falsch", "price_cents": 1, "external_id": "a b"}, api.CodeInvalidField},
		"external id too long": {map[string]any{
			"name": "Lang", "price_cents": 1,
			"external_id": "123456789012345678901234567890123",
		}, api.CodeInvalidField},
		"unknown image": {map[string]any{
			"name": "Bild", "price_cents": 1,
			"image_id": "018f0000-0000-7000-8000-00000000dead",
		}, api.CodeInvalidField},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := m.post(m.menuPath("/menu-items"), tc.body, m.cookies...)
			expectError(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}

func TestMenuNamesAreUniquePerRestaurant(t *testing.T) {
	m := newMenuFixture(t)
	m.addItem(map[string]any{"name": "Döner", "price_cents": 500})

	rec := m.post(m.menuPath("/menu-items"),
		map[string]any{"name": "DÖNER", "price_cents": 500}, m.cookies...)
	expectError(t, rec, http.StatusConflict, api.CodeNameExistsHere)

	// But another restaurant may have the same item.
	other := m.createRestaurant("Zweites Lokal", m.cookies)
	if rec := m.post("/restaurants/"+other.ID+"/menu-items",
		map[string]any{"name": "Döner", "price_cents": 500}, m.cookies...); rec.Code != http.StatusCreated {
		t.Errorf("another restaurant could not reuse the name: %s", rec.Body.String())
	}
}

// Menus are public to read and open to any logged-in user to edit.
func TestMenuReadIsPublicAndWriteNeedsALogin(t *testing.T) {
	m := newMenuFixture(t)
	item := m.addItem(map[string]any{"name": "Öffentlich", "price_cents": 500})

	for _, path := range []string{
		m.menuPath("/categories"),
		m.menuPath("/menu-items"),
		m.menuPath("/menu-items/" + item.ID),
		m.menuPath("/menu-items/" + item.ID + "/modifications"),
	} {
		if rec := m.get(path); rec.Code != http.StatusOK {
			t.Errorf("%s answered %d for an anonymous caller", path, rec.Code)
		}
	}

	anonymous := m.post(m.menuPath("/menu-items"),
		map[string]any{"name": "Heimlich", "price_cents": 500})
	expectError(t, anonymous, http.StatusUnauthorized, api.CodeNotAuthenticated)
}

// Menu routes hang off a restaurant, so a deleted one takes its menu's URLs
// with it.
func TestTheMenuOfAnUnknownRestaurantIsNotFound(t *testing.T) {
	m := newMenuFixture(t)

	for _, id := range []string{"018f0000-0000-7000-8000-00000000dead", "not-a-uuid"} {
		if rec := m.get("/restaurants/" + id + "/menu-items"); rec.Code != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", id, rec.Code)
		}
	}
}

// Renaming a category and moving it in the list.
//
// The sort order is what the reorder buttons in the menu editor change, one
// step at a time, and it is the field that decides what a printed menu looks
// like. The endpoint had no test at all until sprint 14 counted which routes
// the suite reaches.
func TestPatchingACategoryRenamesAndReorders(t *testing.T) {
	m := newMenuFixture(t)

	first := m.addCategory("Vom Grill", 1)
	second := m.addCategory("Getränke", 2)

	// A rename leaves the position alone.
	rec := m.patch(m.menuPath("/categories/"+first.ID),
		map[string]any{"name": "Vom Drehspieß"}, m.cookies...)
	if rec.Code != http.StatusOK {
		t.Fatalf("renaming: %d %s", rec.Code, rec.Body.String())
	}
	var renamed categoryResponse
	decode(t, rec, &renamed)
	if renamed.Name != "Vom Drehspieß" {
		t.Errorf("the name is %q", renamed.Name)
	}
	if renamed.SortOrder != 1 {
		t.Errorf("renaming moved the category to position %d", renamed.SortOrder)
	}

	// And a move leaves the name alone. Swapping the two is what the reorder
	// buttons do.
	rec = m.patch(m.menuPath("/categories/"+first.ID),
		map[string]any{"sort_order": 3}, m.cookies...)
	if rec.Code != http.StatusOK {
		t.Fatalf("reordering: %d %s", rec.Code, rec.Body.String())
	}
	decode(t, rec, &renamed)
	if renamed.Name != "Vom Drehspieß" {
		t.Errorf("reordering changed the name to %q", renamed.Name)
	}
	if renamed.SortOrder != 3 {
		t.Errorf("the category is at position %d, want 3", renamed.SortOrder)
	}

	// The list agrees, which is the point of the field.
	rec = m.get(m.menuPath("/categories"))
	if rec.Code != http.StatusOK {
		t.Fatalf("listing: %d %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Categories []categoryResponse `json:"categories"`
	}
	decode(t, rec, &list)
	if len(list.Categories) != 2 {
		t.Fatalf("the restaurant has %d categories", len(list.Categories))
	}
	if list.Categories[0].ID != second.ID {
		t.Errorf("the list still leads with %q", list.Categories[0].Name)
	}
}

// A category belonging to another restaurant is not found, rather than being
// patched through the wrong parent.
func TestPatchingACategoryOfAnotherRestaurantIsNotFound(t *testing.T) {
	m := newMenuFixture(t)
	category := m.addCategory("Vom Grill", 1)
	other := m.createRestaurant("Zweite Wahl", m.cookies).ID

	rec := m.patch("/restaurants/"+other+"/categories/"+category.ID,
		map[string]any{"name": "Untergeschoben"}, m.cookies...)
	expectErrorCode(t, rec, api.CodeNotFound)
}

// readItems lists the menu, optionally as at a moment.
func (m *menuFixture) readItems(at string) []menuItemResponse {
	m.t.Helper()

	path := m.menuPath("/menu-items")
	if at != "" {
		path += "?at=" + url.QueryEscape(at)
	}
	rec := m.get(path)
	if rec.Code != http.StatusOK {
		m.t.Fatalf("listing the menu: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		MenuItems []menuItemResponse `json:"menu_items"`
	}
	decode(m.t, rec, &body)
	return body.MenuItems
}

// readCategories lists the categories with their attached filters.
func (m *menuFixture) readCategories() []categoryResponse {
	m.t.Helper()

	rec := m.get(m.menuPath("/categories"))
	if rec.Code != http.StatusOK {
		m.t.Fatalf("listing categories: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Categories []categoryResponse `json:"categories"`
	}
	decode(m.t, rec, &body)
	return body.Categories
}
