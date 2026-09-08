package api

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
)

// timeOfDay matches "HH:MM" in 24-hour form.
//
// A regexp rather than time.Parse, because time.Parse("15:04", "7:30") is
// rejected while time.Parse accepts things like "24:00" through normalisation
// in other layouts. Being explicit about the accepted shape is shorter than
// explaining which of the standard library's leniencies apply.
var timeOfDay = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

func (h *RestaurantHandlers) listOpeningHours(w http.ResponseWriter, r *http.Request) {
	restaurant, err := h.lookup(r)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	periods, dbErr := db.ListOpeningHours(r.Context(), h.Pool, restaurant.ID)
	if dbErr != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	bodies := make([]openingPeriodBody, 0, len(periods))
	for i := range periods {
		bodies = append(bodies, publicOpeningPeriod(periods[i]))
	}
	_ = WriteJSON(w, http.StatusOK, map[string]any{"opening_hours": bodies})
}

// openingHoursRequest is the body of PUT /restaurants/{id}/opening-hours.
//
// The whole week at once. Opening hours are edited as a pattern, not one
// interval at a time, and replacing the set avoids any ordering question
// between deleting a Tuesday period and inserting its replacement.
type openingHoursRequest struct {
	OpeningHours []openingPeriodRequest `json:"opening_hours"`
}

type openingPeriodRequest struct {
	DayOfWeek int    `json:"day_of_week"`
	Start     string `json:"start"`
	End       string `json:"end"`
}

// MaxOpeningPeriods bounds one submission.
//
// Seven days with a handful of periods each is the realistic maximum; the
// limit exists so that one request cannot ask the server to insert an
// unbounded number of rows.
const MaxOpeningPeriods = 100

func (h *RestaurantHandlers) replaceOpeningHours(w http.ResponseWriter, r *http.Request) {
	restaurant, lookupErr := h.lookup(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	principal, authErr := RequireAuthenticated(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	var body openingHoursRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	periods, err := validateOpeningHours(body.OpeningHours)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	stored, dbErr := db.ReplaceOpeningHours(r.Context(), h.Pool,
		restaurant.ID, periods, principal.User.ID)
	if dbErr != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	bodies := make([]openingPeriodBody, 0, len(stored))
	for i := range stored {
		bodies = append(bodies, publicOpeningPeriod(stored[i]))
	}
	_ = WriteJSON(w, http.StatusOK, map[string]any{"opening_hours": bodies})
}

// validateOpeningHours checks a whole submitted week.
//
// An empty set is allowed and means "no opening hours recorded", which is not
// the same as a restaurant that is never open -- plenty of entries will simply
// never have had their hours filled in.
func validateOpeningHours(requests []openingPeriodRequest) ([]model.OpeningPeriod, *Error) {
	if len(requests) > MaxOpeningPeriods {
		return nil, &Error{
			Code:   CodeInvalidField,
			Field:  "opening_hours",
			Detail: fmt.Sprintf("at most %d periods may be submitted", MaxOpeningPeriods),
		}
	}

	periods := make([]model.OpeningPeriod, 0, len(requests))
	for i, in := range requests {
		if in.DayOfWeek < 1 || in.DayOfWeek > 7 {
			return nil, &Error{
				Code:   CodeInvalidField,
				Field:  fmt.Sprintf("opening_hours[%d].day_of_week", i),
				Detail: "the weekday must be 1 (Monday) through 7 (Sunday)",
			}
		}

		start := strings.TrimSpace(in.Start)
		end := strings.TrimSpace(in.End)
		for field, value := range map[string]string{"start": start, "end": end} {
			if !timeOfDay.MatchString(value) {
				return nil, &Error{
					Code:   CodeInvalidField,
					Field:  fmt.Sprintf("opening_hours[%d].%s", i, field),
					Detail: "the time must be HH:MM in 24-hour form",
				}
			}
		}

		// Equal times are a typo, not a restaurant that is never open. An end
		// before a start is legal and means the period crosses midnight, as a
		// Friday 22:00-02:00 does -- so only equality is refused, and 1008 is
		// the documented code for exactly this.
		if start == end {
			return nil, &Error{
				Code:   CodeOpeningHoursZeroLength,
				Field:  fmt.Sprintf("opening_hours[%d]", i),
				Detail: "the start and end times are the same",
			}
		}

		periods = append(periods, model.OpeningPeriod{
			DayOfWeek: in.DayOfWeek,
			Start:     start,
			End:       end,
		})
	}
	return periods, nil
}
