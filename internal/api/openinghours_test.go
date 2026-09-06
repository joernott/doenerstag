package api_test

import (
	"net/http"
	"testing"

	"github.com/joernott/doenerstag/internal/api"
)

type openingHoursResponse struct {
	OpeningHours []struct {
		ID              string `json:"id"`
		DayOfWeek       int    `json:"day_of_week"`
		Start           string `json:"start"`
		End             string `json:"end"`
		CrossesMidnight bool   `json:"crosses_midnight"`
	} `json:"opening_hours"`
}

// The exit criterion names a lunch-break pattern, which is the case several
// rows per weekday exist for.
func TestALunchBreakIsTwoPeriodsOnOneDay(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("anna")
	restaurant := f.createRestaurant("Mittagspause", cookies)

	rec := f.do(request{
		method: http.MethodPut, path: "/restaurants/" + restaurant.ID + "/opening-hours",
		body: map[string]any{"opening_hours": []map[string]any{
			{"day_of_week": 1, "start": "11:00", "end": "14:00"},
			{"day_of_week": 1, "start": "17:00", "end": "22:00"},
			{"day_of_week": 2, "start": "11:00", "end": "22:00"},
		}},
		cookies: cookies,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	var body openingHoursResponse
	decode(t, rec, &body)
	if len(body.OpeningHours) != 3 {
		t.Fatalf("stored %d periods, want 3", len(body.OpeningHours))
	}

	// Ordered by weekday then start, which is the order a person would read
	// them out in.
	if body.OpeningHours[0].Start != "11:00" || body.OpeningHours[1].Start != "17:00" {
		t.Errorf("Monday's periods are out of order: %+v", body.OpeningHours[:2])
	}
	if body.OpeningHours[0].DayOfWeek != 1 || body.OpeningHours[2].DayOfWeek != 2 {
		t.Errorf("weekdays are out of order: %+v", body.OpeningHours)
	}
	for _, p := range body.OpeningHours {
		if p.CrossesMidnight {
			t.Errorf("%d %s-%s is reported as crossing midnight", p.DayOfWeek, p.Start, p.End)
		}
	}
}

// An end before a start means the period runs into the next day, which is how a
// Friday 22:00-02:00 is expressed. It is legal, and the response says so
// explicitly rather than leaving a client to work out that the times look
// backwards.
func TestAPeriodMayCrossMidnight(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("bertil")
	restaurant := f.createRestaurant("Nachtimbiss", cookies)

	rec := f.do(request{
		method: http.MethodPut, path: "/restaurants/" + restaurant.ID + "/opening-hours",
		body: map[string]any{"opening_hours": []map[string]any{
			{"day_of_week": 5, "start": "22:00", "end": "02:00"},
		}},
		cookies: cookies,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("a midnight-crossing period was rejected: %s", rec.Body.String())
	}

	var body openingHoursResponse
	decode(t, rec, &body)
	if len(body.OpeningHours) != 1 {
		t.Fatalf("stored %d periods", len(body.OpeningHours))
	}
	if !body.OpeningHours[0].CrossesMidnight {
		t.Error("the period is not reported as crossing midnight")
	}
}

// Equal times are a typo, not a restaurant that is never open, and 1008 is the
// documented code for exactly that.
func TestAZeroLengthPeriodIsRejected(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("carla")
	restaurant := f.createRestaurant("Zero", cookies)

	rec := f.do(request{
		method: http.MethodPut, path: "/restaurants/" + restaurant.ID + "/opening-hours",
		body: map[string]any{"opening_hours": []map[string]any{
			{"day_of_week": 3, "start": "12:00", "end": "12:00"},
		}},
		cookies: cookies,
	})
	expectError(t, rec, http.StatusBadRequest, api.CodeOpeningHoursZeroLength)
}

func TestOpeningHoursValidation(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("dora")
	restaurant := f.createRestaurant("Validated", cookies)
	path := "/restaurants/" + restaurant.ID + "/opening-hours"

	cases := map[string]map[string]any{
		"weekday zero":        {"day_of_week": 0, "start": "11:00", "end": "12:00"},
		"weekday eight":       {"day_of_week": 8, "start": "11:00", "end": "12:00"},
		"hour out of range":   {"day_of_week": 1, "start": "24:00", "end": "12:00"},
		"minute out of range": {"day_of_week": 1, "start": "11:60", "end": "12:00"},
		"not a time":          {"day_of_week": 1, "start": "lunchtime", "end": "12:00"},
		"single digit hour":   {"day_of_week": 1, "start": "9:00", "end": "12:00"},
		"with seconds":        {"day_of_week": 1, "start": "11:00:00", "end": "12:00"},
		"empty":               {"day_of_week": 1, "start": "", "end": "12:00"},
	}

	for name, period := range cases {
		t.Run(name, func(t *testing.T) {
			rec := f.do(request{
				method: http.MethodPut, path: path,
				body:    map[string]any{"opening_hours": []map[string]any{period}},
				cookies: cookies,
			})
			expectError(t, rec, http.StatusBadRequest, api.CodeInvalidField)
		})
	}
}

// The PUT replaces the whole set, which is what makes editing a weekly pattern
// straightforward: there is no ordering problem between removing Tuesday and
// adding its replacement.
func TestPuttingOpeningHoursReplacesTheWholeSet(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("erik")
	restaurant := f.createRestaurant("Replaced", cookies)
	path := "/restaurants/" + restaurant.ID + "/opening-hours"

	put := func(periods []map[string]any) openingHoursResponse {
		t.Helper()
		rec := f.do(request{
			method: http.MethodPut, path: path,
			body:    map[string]any{"opening_hours": periods},
			cookies: cookies,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("putting: %d %s", rec.Code, rec.Body.String())
		}
		var body openingHoursResponse
		decode(t, rec, &body)
		return body
	}

	first := put([]map[string]any{
		{"day_of_week": 1, "start": "11:00", "end": "14:00"},
		{"day_of_week": 2, "start": "11:00", "end": "14:00"},
	})
	if len(first.OpeningHours) != 2 {
		t.Fatalf("stored %d periods", len(first.OpeningHours))
	}

	second := put([]map[string]any{
		{"day_of_week": 3, "start": "09:00", "end": "17:00"},
	})
	if len(second.OpeningHours) != 1 {
		t.Fatalf("after replacing, %d periods remain", len(second.OpeningHours))
	}
	if second.OpeningHours[0].DayOfWeek != 3 {
		t.Errorf("the wrong period survived: %+v", second.OpeningHours[0])
	}

	// An empty set clears them, which means "no hours recorded" rather than
	// "never open".
	empty := put(nil)
	if len(empty.OpeningHours) != 0 {
		t.Errorf("an empty set left %d periods", len(empty.OpeningHours))
	}
}

// A rejected row must leave the previous week intact rather than half of it.
func TestARejectedPeriodLeavesTheOldHoursIntact(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("frieda")
	restaurant := f.createRestaurant("Atomic", cookies)
	path := "/restaurants/" + restaurant.ID + "/opening-hours"

	if rec := f.do(request{
		method: http.MethodPut, path: path,
		body: map[string]any{"opening_hours": []map[string]any{
			{"day_of_week": 1, "start": "11:00", "end": "14:00"},
		}},
		cookies: cookies,
	}); rec.Code != http.StatusOK {
		t.Fatalf("the first put failed: %s", rec.Body.String())
	}

	// The second period is invalid, so the whole request must be refused.
	bad := f.do(request{
		method: http.MethodPut, path: path,
		body: map[string]any{"opening_hours": []map[string]any{
			{"day_of_week": 2, "start": "11:00", "end": "14:00"},
			{"day_of_week": 9, "start": "11:00", "end": "14:00"},
		}},
		cookies: cookies,
	})
	if bad.Code < 400 {
		t.Fatalf("an invalid weekday was accepted: %s", bad.Body.String())
	}

	read := f.get(path)
	var body openingHoursResponse
	decode(t, read, &body)
	if len(body.OpeningHours) != 1 || body.OpeningHours[0].DayOfWeek != 1 {
		t.Errorf("the refused request changed the stored hours: %+v", body.OpeningHours)
	}
}

func TestOpeningHoursAreReadableAnonymouslyButNotWritable(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("gustav")
	restaurant := f.createRestaurant("Public Hours", cookies)
	path := "/restaurants/" + restaurant.ID + "/opening-hours"

	if rec := f.get(path); rec.Code != http.StatusOK {
		t.Errorf("an anonymous read answered %d", rec.Code)
	}

	rec := f.do(request{
		method: http.MethodPut, path: path,
		body: map[string]any{"opening_hours": []map[string]any{
			{"day_of_week": 1, "start": "11:00", "end": "14:00"},
		}},
	})
	expectError(t, rec, http.StatusUnauthorized, api.CodeNotAuthenticated)
}

// The detail endpoint carries the hours, so the restaurant page is one request.
func TestTheRestaurantDetailIncludesOpeningHours(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("hanna")
	restaurant := f.createRestaurant("Detailed", cookies)

	if rec := f.do(request{
		method: http.MethodPut, path: "/restaurants/" + restaurant.ID + "/opening-hours",
		body: map[string]any{"opening_hours": []map[string]any{
			{"day_of_week": 6, "start": "12:00", "end": "23:00"},
		}},
		cookies: cookies,
	}); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}

	rec := f.get("/restaurants/" + restaurant.ID)
	var body restaurantResponse
	decode(t, rec, &body)
	if len(body.OpeningHours) != 1 {
		t.Fatalf("the detail carries %d periods, want 1", len(body.OpeningHours))
	}
	if body.OpeningHours[0].Start != "12:00" {
		t.Errorf("the period reads %+v", body.OpeningHours[0])
	}
}
