package model

import (
	"time"

	"github.com/google/uuid"
)

// AvailabilityFilter is a named rule about when food can be had.
//
// One row, one rule, reusable across a menu: "Mittagsmenü", "Fri-Sun after 5".
// See docs/03_data_model.md for how several of them combine; this type is about
// what one of them means.
type AvailabilityFilter struct {
	ID           uuid.UUID
	RestaurantID uuid.UUID
	Name         string

	// OnDate limits the filter to one calendar date. Several dates are several
	// filters.
	OnDate *time.Time

	// Weekdays is ISO numbering, 1 = Monday. Empty means every day.
	Weekdays []int

	// StartTime and EndTime are minutes since midnight, or nil for "any time".
	// Both or neither. End before start crosses midnight.
	StartTime *int
	EndTime   *int

	SortOrder int
}

// Matches reports whether this filter permits the given moment.
//
// The parts are ANDed: a filter naming weekdays and a time means those days and
// only during those hours. A part left unset has no opinion, so a filter with
// only a time applies on every day.
//
// The moment is compared in its own location, which is the caller's business:
// an order's fulfilment time is a real instant, and "Friday between five and
// ten" means five and ten where the restaurant is. Callers pass a time already
// in the right zone.
func (f AvailabilityFilter) Matches(at time.Time) bool {
	if f.OnDate != nil {
		wantY, wantM, wantD := f.OnDate.Date()
		gotY, gotM, gotD := at.Date()
		if wantY != gotY || wantM != gotM || wantD != gotD {
			return false
		}
	}

	if len(f.Weekdays) > 0 && !containsDay(f.Weekdays, isoWeekday(at)) {
		return false
	}

	if f.StartTime != nil && f.EndTime != nil {
		if !withinDayWindow(*f.StartTime, *f.EndTime, minutesOfDay(at)) {
			return false
		}
	}

	return true
}

// AvailableAt reports whether an element with these filters permits the moment.
//
// No filters means always: a menu that has never heard of this behaves exactly
// as it did. Otherwise the filters are alternatives -- a Monday filter and a
// Friday filter make a dish available on both -- which is why this is an "any"
// rather than an "all".
func AvailableAt(filters []AvailabilityFilter, at time.Time) bool {
	if len(filters) == 0 {
		return true
	}
	for _, f := range filters {
		if f.Matches(at) {
			return true
		}
	}
	return false
}

// ItemAvailableAt applies the asymmetric rule: within an element the filters are
// alternatives, but a category's rules and an item's must both be satisfied.
//
// The consequence is deliberate and looks like a bug from the outside: a
// Monday-only category holding a Wednesday-only item hides that item for good.
// That is what "this category is only served on Mondays" has to mean.
func ItemAvailableAt(category, item []AvailabilityFilter, at time.Time) bool {
	return AvailableAt(category, at) && AvailableAt(item, at)
}

// isoWeekday returns 1 for Monday through 7 for Sunday, which is what the
// schema stores and what the rest of this application uses. Go's own Weekday
// numbers Sunday as 0.
func isoWeekday(at time.Time) int {
	if day := int(at.Weekday()); day != 0 {
		return day
	}
	return 7
}

func minutesOfDay(at time.Time) int {
	return at.Hour()*60 + at.Minute()
}

func containsDay(days []int, day int) bool {
	for _, d := range days {
		if d == day {
			return true
		}
	}
	return false
}

// withinDayWindow tests a minute against a window that may cross midnight.
//
// A window from 22:00 to 02:00 is two intervals on the clock face and one
// period in the kitchen, which is the same judgement opening_hours makes. The
// start is inclusive and the end is exclusive: a kitchen that stops at ten
// stops at ten.
func withinDayWindow(start, end, minute int) bool {
	if start < end {
		return minute >= start && minute < end
	}
	return minute >= start || minute < end
}
