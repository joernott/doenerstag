package api_test

import (
	"net/http"
	"testing"

	"github.com/joernott/doenerstag/internal/api"
)

type availabilityResp struct {
	ID           string  `json:"id"`
	RestaurantID string  `json:"restaurant_id"`
	Name         string  `json:"name"`
	OnDate       *string `json:"on_date"`
	Weekdays     []int   `json:"weekdays"`
	StartTime    *string `json:"start_time"`
	EndTime      *string `json:"end_time"`
	SortOrder    int     `json:"sort_order"`
}

// createFilter defines one filter and returns it.
func (m *menuFixture) createFilter(body map[string]any) availabilityResp {
	m.t.Helper()

	rec := m.post("/restaurants/"+m.restaurant+"/availability", body, m.cookies...)
	if rec.Code != http.StatusCreated {
		m.t.Fatalf("creating a filter: %d %s", rec.Code, rec.Body.String())
	}
	var created availabilityResp
	decode(m.t, rec, &created)
	return created
}

func (m *menuFixture) listFilters() []availabilityResp {
	m.t.Helper()

	rec := m.get("/restaurants/" + m.restaurant + "/availability")
	if rec.Code != http.StatusOK {
		m.t.Fatalf("listing filters: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Availability []availabilityResp `json:"availability"`
	}
	decode(m.t, rec, &body)
	return body.Availability
}

// A filter is defined, listed, changed and removed.
func TestAvailabilityFiltersRoundTrip(t *testing.T) {
	m := newMenuFixture(t)

	created := m.createFilter(map[string]any{
		"name":       "Fri-Sun after 5",
		"weekdays":   []int{5, 6, 7},
		"start_time": "17:00",
		"end_time":   "22:00",
	})
	if created.Name != "Fri-Sun after 5" || len(created.Weekdays) != 3 {
		t.Fatalf("created %+v", created)
	}
	if created.StartTime == nil || *created.StartTime != "17:00" {
		t.Errorf("start time is %v", created.StartTime)
	}

	// Listed publicly: when a dish is served is part of what a menu says.
	listed := m.listFilters()
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("listed %+v", listed)
	}

	patched := m.patch("/restaurants/"+m.restaurant+"/availability/"+created.ID,
		map[string]any{"name": "Weekend evenings", "weekdays": []int{6, 7}}, m.cookies...)
	if patched.Code != http.StatusOK {
		t.Fatalf("patching: %d %s", patched.Code, patched.Body.String())
	}
	var after availabilityResp
	decode(t, patched, &after)
	if after.Name != "Weekend evenings" || len(after.Weekdays) != 2 {
		t.Errorf("patched to %+v", after)
	}
	// The half that was not mentioned is left alone.
	if after.StartTime == nil || *after.StartTime != "17:00" {
		t.Errorf("the start time was lost: %v", after.StartTime)
	}

	// Deleting is the administrator's, like every other delete of menu
	// structure.
	mine := m.remove("/restaurants/"+m.restaurant+"/availability/"+created.ID, m.cookies...)
	expectErrorCode(t, mine, api.CodeAdminRequired)

	gone := m.remove("/restaurants/"+m.restaurant+"/availability/"+created.ID, m.admin...)
	if gone.Code != http.StatusNoContent {
		t.Fatalf("deleting: %d %s", gone.Code, gone.Body.String())
	}
	if left := m.listFilters(); len(left) != 0 {
		t.Errorf("%d filters survived the delete", len(left))
	}
}

// The rules the schema enforces come back as a named field rather than as a
// database error.
func TestAFilterMustRestrictSomething(t *testing.T) {
	m := newMenuFixture(t)

	for _, tc := range []struct {
		why  string
		body map[string]any
	}{
		{"nothing at all", map[string]any{"name": "everything"}},
		{"half a window", map[string]any{"name": "half", "start_time": "17:00"}},
		{"an empty window", map[string]any{
			"name": "empty", "start_time": "17:00", "end_time": "17:00",
		}},
		{"a day that is not a day", map[string]any{"name": "eight", "weekdays": []int{8}}},
		{"a time that is not a time", map[string]any{
			"name": "noon-ish", "start_time": "25:00", "end_time": "26:00",
		}},
		{"a date that is not a date", map[string]any{"name": "when", "on_date": "the 5th"}},
	} {
		t.Run(tc.why, func(t *testing.T) {
			rec := m.post("/restaurants/"+m.restaurant+"/availability", tc.body, m.cookies...)
			if rec.Code < 400 {
				t.Fatalf("accepted: %d %s", rec.Code, rec.Body.String())
			}
			if rec.Code >= 500 {
				t.Errorf("reported as a server error: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// A filter from another restaurant cannot be attached, and cannot be edited
// through this restaurant's URL either.
func TestAFilterBelongsToItsRestaurant(t *testing.T) {
	m := newMenuFixture(t)
	mine := m.createFilter(map[string]any{"name": "Mondays", "weekdays": []int{1}})

	other := newMenuFixture(t)
	theirs := other.createFilter(map[string]any{"name": "Fridays", "weekdays": []int{5}})

	// Editing through the wrong restaurant is a 404: from here that filter does
	// not exist.
	wrong := m.patch("/restaurants/"+m.restaurant+"/availability/"+theirs.ID,
		map[string]any{"name": "mine now"}, m.cookies...)
	expectErrorCode(t, wrong, api.CodeNotFound)

	// Attaching one is refused rather than silently dropped.
	category := m.addCategory("Nudeln", 10).ID
	rec := m.put("/restaurants/"+m.restaurant+"/categories/"+category+"/availability",
		map[string]any{"filter_ids": []string{mine.ID, theirs.ID}}, m.cookies...)
	expectErrorCode(t, rec, api.CodeInvalidField)

	// And nothing was attached: a request that half-worked is worse than one
	// that did not.
	categories := m.readCategories()
	for _, c := range categories {
		if c.ID == category && len(c.Availability) != 0 {
			t.Errorf("the refused request attached %d filter(s)", len(c.Availability))
		}
	}
}

// The whole point: the order page sees only what the kitchen will make, and the
// restaurant page sees the menu.
func TestTheMenuIsFilteredOnlyWhenAMomentIsAskedAbout(t *testing.T) {
	m := newMenuFixture(t)

	friday := m.createFilter(map[string]any{
		"name": "Fri-Sun after 5", "weekdays": []int{5, 6, 7},
		"start_time": "17:00", "end_time": "22:00",
	})

	pasta := m.addItem(map[string]any{"name": "Spaghetti", "price_cents": 900})
	if rec := m.put("/restaurants/"+m.restaurant+"/menu-items/"+pasta.ID+"/availability",
		map[string]any{"filter_ids": []string{friday.ID}}, m.cookies...); rec.Code != http.StatusOK {
		t.Fatalf("attaching: %d %s", rec.Code, rec.Body.String())
	}

	// No moment: the whole menu, and the item says what its rule is.
	all := m.readItems("")
	found := false
	for _, item := range all {
		if item.ID != pasta.ID {
			continue
		}
		found = true
		if item.AvailableAt != nil {
			t.Errorf("a verdict was given for no moment: %v", *item.AvailableAt)
		}
		if len(item.Availability) != 1 || item.Availability[0].ID != friday.ID {
			t.Errorf("the item does not carry its filter: %+v", item.Availability)
		}
	}
	if !found {
		t.Fatal("the item is missing from the unfiltered menu")
	}

	// A Friday evening, and a Tuesday lunchtime.
	for _, tc := range []struct {
		when  string
		avail bool
	}{
		{"2026-09-11T18:00:00Z", true},
		{"2026-09-08T12:00:00Z", false},
	} {
		items := m.readItems(tc.when)
		for _, item := range items {
			if item.ID != pasta.ID {
				continue
			}
			if item.AvailableAt == nil {
				t.Fatalf("no verdict for %s", tc.when)
			}
			if *item.AvailableAt != tc.avail {
				t.Errorf("%s: available %v, want %v", tc.when, *item.AvailableAt, tc.avail)
			}
		}
	}
}

// And it is enforced, not merely displayed: an order for a Tuesday cannot hold
// a dish the kitchen only makes at the weekend.
func TestAnItemCannotBeOrderedWhenItIsNotServed(t *testing.T) {
	o := newOrderFixture(t)

	// The fixture's order is a few hours from now. Restrict the dish to a
	// weekday the order is not for.
	other := (int(o.now.Weekday())+3)%7 + 1
	filter := o.createFilter(map[string]any{
		"name": "some other day", "weekdays": []int{other},
	})
	if rec := o.put("/restaurants/"+o.restaurant+"/menu-items/"+o.doener.ID+"/availability",
		map[string]any{"filter_ids": []string{filter.ID}}, o.cookies...); rec.Code != http.StatusOK {
		t.Fatalf("attaching: %d %s", rec.Code, rec.Body.String())
	}

	hungry := o.register("hungrig")
	rec := o.post("/orders/"+o.order.ID+"/items",
		map[string]any{"menu_item_id": o.doener.ID, "quantity": 1}, hungry...)
	expectErrorCode(t, rec, api.CodeItemNotServedThen)
}

// A category's filters bind the items inside it.
func TestACategorysFiltersReachItsItems(t *testing.T) {
	m := newMenuFixture(t)

	monday := m.createFilter(map[string]any{"name": "Mondays", "weekdays": []int{1}})
	category := m.addCategory("Montagsgerichte", 10).ID
	if rec := m.put("/restaurants/"+m.restaurant+"/categories/"+category+"/availability",
		map[string]any{"filter_ids": []string{monday.ID}}, m.cookies...); rec.Code != http.StatusOK {
		t.Fatalf("attaching: %d %s", rec.Code, rec.Body.String())
	}

	item := m.addItem(map[string]any{
		"name": "Montagseintopf", "price_cents": 500, "category_id": category,
	})

	// A Monday and a Tuesday, with no filter on the item itself.
	for _, tc := range []struct {
		when  string
		avail bool
	}{
		{"2026-09-07T12:00:00Z", true},
		{"2026-09-08T12:00:00Z", false},
	} {
		for _, got := range m.readItems(tc.when) {
			if got.ID != item.ID {
				continue
			}
			if got.AvailableAt == nil || *got.AvailableAt != tc.avail {
				t.Errorf("%s: %v, want %v", tc.when, got.AvailableAt, tc.avail)
			}
		}
	}

	// The category carries its filter for an editor to show.
	for _, c := range m.readCategories() {
		if c.ID == category && len(c.Availability) != 1 {
			t.Errorf("the category does not carry its filter: %+v", c.Availability)
		}
	}
}

// A moment that is not a timestamp is a named field, not a 500.
func TestAnUnparseableMomentIsRefused(t *testing.T) {
	m := newMenuFixture(t)

	rec := m.get("/restaurants/" + m.restaurant + "/menu-items?at=lunchtime")
	expectErrorCode(t, rec, api.CodeInvalidField)
}
