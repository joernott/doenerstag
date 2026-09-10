package model

import (
	"testing"
	"time"
)

func at(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parsing %q: %v", value, err)
	}
	return parsed
}

func minutes(h, m int) *int {
	v := h*60 + m
	return &v
}

func date(t *testing.T, value string) *time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		t.Fatalf("parsing %q: %v", value, err)
	}
	return &parsed
}

// The example from the report: pasta from Friday to Sunday, five until ten.
func TestAFilterAndsItsOwnParts(t *testing.T) {
	pasta := AvailabilityFilter{
		Name:      "Fri-Sun after 5",
		Weekdays:  []int{5, 6, 7},
		StartTime: minutes(17, 0),
		EndTime:   minutes(22, 0),
	}

	for _, tc := range []struct {
		when  string
		match bool
		why   string
	}{
		{"2026-09-11T18:00:00Z", true, "Friday evening, the case it was written for"},
		{"2026-09-13T21:59:00Z", true, "Sunday, one minute before closing"},
		{"2026-09-11T17:00:00Z", true, "the start is inclusive"},
		{"2026-09-11T22:00:00Z", false, "the end is not: a kitchen that stops at ten stops at ten"},
		{"2026-09-11T16:59:00Z", false, "Friday, one minute too early"},
		{"2026-09-08T18:00:00Z", false, "Tuesday evening: right time, wrong day"},
		{"2026-09-13T12:00:00Z", false, "Sunday lunch: right day, wrong time"},
	} {
		t.Run(tc.why, func(t *testing.T) {
			if got := pasta.Matches(at(t, tc.when)); got != tc.match {
				t.Errorf("%s matched %v, want %v", tc.when, got, tc.match)
			}
		})
	}
}

// A part left unset has no opinion.
func TestAnUnsetPartRestrictsNothing(t *testing.T) {
	lunch := AvailabilityFilter{
		Name:      "Mittagsmenü",
		StartTime: minutes(11, 30),
		EndTime:   minutes(14, 0),
	}

	// Every day, because no weekday was named.
	for day := 0; day < 7; day++ {
		when := at(t, "2026-09-07T12:00:00Z").AddDate(0, 0, day)
		if !lunch.Matches(when) {
			t.Errorf("%s did not match a filter that names no weekday", when.Weekday())
		}
	}

	christmas := AvailabilityFilter{Name: "Weihnachtsessen", OnDate: date(t, "2026-12-24")}
	if !christmas.Matches(at(t, "2026-12-24T03:00:00Z")) {
		t.Error("a filter with only a date did not match at an odd hour of that date")
	}
	if christmas.Matches(at(t, "2026-12-25T12:00:00Z")) {
		t.Error("a filter with only a date matched the next day")
	}
}

// Sunday is 7, not 0. Go numbers it 0 and the schema numbers it 7, and getting
// that wrong would make every Sunday rule silently wrong.
func TestSundayIsSeven(t *testing.T) {
	sunday := AvailabilityFilter{Name: "Sundays", Weekdays: []int{7}}

	if !sunday.Matches(at(t, "2026-09-13T12:00:00Z")) {
		t.Error("a Sunday filter did not match a Sunday")
	}
	if sunday.Matches(at(t, "2026-09-07T12:00:00Z")) {
		t.Error("a Sunday filter matched a Monday")
	}
}

// A window that crosses midnight is one period, not two.
func TestAWindowMayCrossMidnight(t *testing.T) {
	late := AvailabilityFilter{
		Name:      "after the pub",
		StartTime: minutes(22, 0),
		EndTime:   minutes(2, 0),
	}

	for _, tc := range []struct {
		when  string
		match bool
	}{
		{"2026-09-11T23:30:00Z", true},
		{"2026-09-11T01:00:00Z", true},
		{"2026-09-11T22:00:00Z", true},
		{"2026-09-11T02:00:00Z", false},
		{"2026-09-11T12:00:00Z", false},
	} {
		if got := late.Matches(at(t, tc.when)); got != tc.match {
			t.Errorf("%s matched %v, want %v", tc.when, got, tc.match)
		}
	}
}

// Several filters on one element are alternatives.
func TestFiltersOnOneElementAreAlternatives(t *testing.T) {
	monday := AvailabilityFilter{Name: "Mondays", Weekdays: []int{1}}
	friday := AvailabilityFilter{Name: "Fridays", Weekdays: []int{5}}
	both := []AvailabilityFilter{monday, friday}

	if !AvailableAt(both, at(t, "2026-09-07T12:00:00Z")) {
		t.Error("Monday was refused by a set containing a Monday filter")
	}
	if !AvailableAt(both, at(t, "2026-09-11T12:00:00Z")) {
		t.Error("Friday was refused by a set containing a Friday filter")
	}
	if AvailableAt(both, at(t, "2026-09-09T12:00:00Z")) {
		t.Error("Wednesday was allowed by Monday and Friday filters")
	}
}

// Nothing attached means always available, so a menu that predates all of this
// behaves exactly as it did.
func TestNoFiltersMeansAlways(t *testing.T) {
	if !AvailableAt(nil, at(t, "2026-09-09T03:00:00Z")) {
		t.Error("an element with no filters was unavailable")
	}
	if !ItemAvailableAt(nil, nil, at(t, "2026-09-09T03:00:00Z")) {
		t.Error("an item with no filters anywhere was unavailable")
	}
}

// A category's rules and an item's must both hold.
func TestACategoryAndAnItemBothHaveToAgree(t *testing.T) {
	mondayOnly := []AvailabilityFilter{{Name: "Mondays", Weekdays: []int{1}}}
	afterFive := []AvailabilityFilter{{
		Name: "after 5", StartTime: minutes(17, 0), EndTime: minutes(23, 59),
	}}

	if !ItemAvailableAt(mondayOnly, afterFive, at(t, "2026-09-07T18:00:00Z")) {
		t.Error("Monday evening failed a Monday category holding an evening item")
	}
	if ItemAvailableAt(mondayOnly, afterFive, at(t, "2026-09-07T12:00:00Z")) {
		t.Error("Monday lunchtime passed an item restricted to the evening")
	}
	if ItemAvailableAt(mondayOnly, afterFive, at(t, "2026-09-11T18:00:00Z")) {
		t.Error("Friday evening passed a category restricted to Mondays")
	}
}

// The consequence that looks like a defect and is not, stated as a test so that
// nobody quietly "fixes" it into an OR.
func TestAnItemCanBeHiddenForeverByItsCategory(t *testing.T) {
	mondayOnly := []AvailabilityFilter{{Name: "Mondays", Weekdays: []int{1}}}
	wednesdayOnly := []AvailabilityFilter{{Name: "Wednesdays", Weekdays: []int{3}}}

	start := at(t, "2026-09-07T00:00:00Z")
	for minute := 0; minute < 7*24*60; minute += 17 {
		when := start.Add(time.Duration(minute) * time.Minute)
		if ItemAvailableAt(mondayOnly, wednesdayOnly, when) {
			t.Fatalf("a Wednesday item in a Monday category was available at %s", when)
		}
	}
}
