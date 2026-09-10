package api

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
)

// MaxAvailabilityNameLength matches migration 12.
const MaxAvailabilityNameLength = 200

// clockTime is "HH:MM" on a 24-hour clock, which is what the schema's `time`
// column holds and what an <input type="time"> submits.
var clockTime = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

type availabilityBody struct {
	ID           string `json:"id"`
	RestaurantID string `json:"restaurant_id"`
	Name         string `json:"name"`

	// OnDate is "YYYY-MM-DD", or null for "any date".
	OnDate *string `json:"on_date"`
	// Weekdays is ISO numbering, 1 = Monday. Empty means every day.
	Weekdays []int `json:"weekdays"`
	// StartTime and EndTime are "HH:MM", or null for "any time". Both or
	// neither, which the schema enforces.
	StartTime *string `json:"start_time"`
	EndTime   *string `json:"end_time"`

	SortOrder int `json:"sort_order"`
}

func publicAvailability(f model.AvailabilityFilter) availabilityBody {
	body := availabilityBody{
		ID:           f.ID.String(),
		RestaurantID: f.RestaurantID.String(),
		Name:         f.Name,
		Weekdays:     f.Weekdays,
		SortOrder:    f.SortOrder,
	}
	if body.Weekdays == nil {
		body.Weekdays = []int{}
	}
	if f.OnDate != nil {
		day := f.OnDate.Format(time.DateOnly)
		body.OnDate = &day
	}
	if f.StartTime != nil {
		body.StartTime = clockString(*f.StartTime)
	}
	if f.EndTime != nil {
		body.EndTime = clockString(*f.EndTime)
	}
	return body
}

func clockString(minutes int) *string {
	value := fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
	return &value
}

// Register adds the availability routes to the menu handlers.
func (h *MenuHandlers) registerAvailability(r *Router) {
	r.HandleFunc(http.MethodGet, "/restaurants/:id/availability", h.listAvailability)
	r.HandleFunc(http.MethodPost, "/restaurants/:id/availability", h.createAvailability)
	r.HandleFunc(http.MethodPatch, "/restaurants/:id/availability/:fid", h.patchAvailability)
	r.HandleFunc(http.MethodDelete, "/restaurants/:id/availability/:fid", h.deleteAvailability)

	r.HandleFunc(http.MethodPut, "/restaurants/:id/categories/:cid/availability", h.setCategoryAvailability)
	r.HandleFunc(http.MethodPut, "/restaurants/:id/menu-items/:mid/availability", h.setItemAvailability)
}

// listAvailability returns a restaurant's filters. Public, like the menu: when
// a dish is served is part of what a menu says.
func (h *MenuHandlers) listAvailability(w http.ResponseWriter, r *http.Request) {
	restaurantID, err := requiredUUID(Param(r, "id"), "id")
	if err != nil {
		WriteError(w, r, &Error{Code: CodeNotFound})
		return
	}

	filters, dbErr := db.ListAvailabilityFilters(r.Context(), h.Pool, restaurantID)
	if dbErr != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	bodies := make([]availabilityBody, 0, len(filters))
	for _, f := range filters {
		bodies = append(bodies, publicAvailability(f))
	}
	_ = WriteJSON(w, http.StatusOK, map[string]any{"availability": bodies})
}

type availabilityRequest struct {
	Name      *string  `json:"name"`
	OnDate    *string  `json:"on_date"`
	Weekdays  *[]int   `json:"weekdays"`
	StartTime *string  `json:"start_time"`
	EndTime   *string  `json:"end_time"`
	SortOrder *int     `json:"sort_order"`
	raw       rawPatch `json:"-"`
}

// validated is what a request means once it has been checked: nil pointers for
// absent fields, and values that the schema will accept.
type validatedAvailability struct {
	name      string
	onDate    *time.Time
	weekdays  []int
	startTime *string
	endTime   *string
	sortOrder int
}

func (h *MenuHandlers) createAvailability(w http.ResponseWriter, r *http.Request) {
	restaurant, authErr := h.restaurantID(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	principal, err := RequireAuthenticated(r)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	var body availabilityRequest
	if !decodeJSONWithRaw(w, r, &body, &body.raw) {
		return
	}
	if body.Name == nil {
		WriteError(w, r, &Error{Code: CodeMissingField, Field: "name"})
		return
	}

	in, verr := validateAvailability(body, validatedAvailability{})
	if verr != nil {
		WriteError(w, r, verr)
		return
	}

	created, dbErr := db.CreateAvailabilityFilter(r.Context(), h.Pool, principal.User.ID,
		db.NewAvailabilityFilter{
			RestaurantID: restaurant,
			Name:         in.name,
			OnDate:       in.onDate,
			Weekdays:     in.weekdays,
			StartTime:    in.startTime,
			EndTime:      in.endTime,
			SortOrder:    in.sortOrder,
		})
	if dbErr != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	_ = WriteJSON(w, http.StatusCreated, publicAvailability(created))
}

func (h *MenuHandlers) patchAvailability(w http.ResponseWriter, r *http.Request) {
	filter, authErr := h.editableFilter(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	principal, err := RequireAuthenticated(r)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	var body availabilityRequest
	if !decodeJSONWithRaw(w, r, &body, &body.raw) {
		return
	}

	// Validated against the filter as it stands, so that a request setting only
	// a start time is refused when there is no end time to go with it.
	current := validatedAvailability{
		name:      filter.Name,
		onDate:    filter.OnDate,
		weekdays:  filter.Weekdays,
		sortOrder: filter.SortOrder,
	}
	if filter.StartTime != nil {
		current.startTime = clockString(*filter.StartTime)
	}
	if filter.EndTime != nil {
		current.endTime = clockString(*filter.EndTime)
	}

	in, verr := validateAvailability(body, current)
	if verr != nil {
		WriteError(w, r, verr)
		return
	}

	update := db.AvailabilityFilterUpdate{
		Name:      &in.name,
		OnDate:    &in.onDate,
		Weekdays:  &in.weekdays,
		StartTime: &in.startTime,
		EndTime:   &in.endTime,
		SortOrder: &in.sortOrder,
	}

	updated, dbErr := db.UpdateAvailabilityFilter(r.Context(), h.Pool, filter.ID,
		principal.User.ID, update)
	switch {
	case errors.Is(dbErr, db.ErrNotFound):
		WriteError(w, r, &Error{Code: CodeNotFound})
		return
	case dbErr != nil:
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	_ = WriteJSON(w, http.StatusOK, publicAvailability(updated))
}

// deleteAvailability removes a filter. Administrator only, like every other
// delete of menu structure: what it removes is shared by everything it was
// attached to, and the attachments go with it.
func (h *MenuHandlers) deleteAvailability(w http.ResponseWriter, r *http.Request) {
	if _, err := RequireAdmin(r); err != nil {
		WriteError(w, r, err)
		return
	}

	filter, authErr := h.editableFilter(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	if err := db.DeleteAvailabilityFilter(r.Context(), h.Pool, filter.ID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			WriteError(w, r, &Error{Code: CodeNotFound})
			return
		}
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// editableFilter resolves the filter in the path and checks it belongs to the
// restaurant in the path, so that a filter id from one restaurant cannot be
// edited through another's URL.
func (h *MenuHandlers) editableFilter(r *http.Request) (model.AvailabilityFilter, *Error) {
	restaurantID, authErr := h.restaurantID(r)
	if authErr != nil {
		return model.AvailabilityFilter{}, authErr
	}

	filterID, err := requiredUUID(Param(r, "fid"), "fid")
	if err != nil {
		return model.AvailabilityFilter{}, &Error{Code: CodeNotFound}
	}

	filter, dbErr := db.AvailabilityFilterByID(r.Context(), h.Pool, filterID)
	switch {
	case errors.Is(dbErr, db.ErrNotFound):
		return model.AvailabilityFilter{}, &Error{Code: CodeNotFound}
	case dbErr != nil:
		return model.AvailabilityFilter{}, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr}
	}

	if filter.RestaurantID != restaurantID {
		return model.AvailabilityFilter{}, &Error{Code: CodeNotFound}
	}
	return filter, nil
}

// validateAvailability checks a request against the rules the schema enforces,
// so that a mistake comes back as a named field rather than as a constraint
// violation reported as a database error.
//
// `current` is what the filter says now, so that a PATCH changing one half of
// the time window is judged against the other half rather than against nothing.
func validateAvailability(
	body availabilityRequest, current validatedAvailability,
) (validatedAvailability, *Error) {
	out := current

	if body.Name != nil {
		name := strings.TrimSpace(*body.Name)
		if err := validateMenuName(name); err != nil {
			return out, err
		}
		out.name = name
	}

	if body.raw.has("on_date") {
		out.onDate = nil
		if body.OnDate != nil && *body.OnDate != "" {
			day, err := time.Parse(time.DateOnly, *body.OnDate)
			if err != nil {
				return out, &Error{
					Code: CodeInvalidField, Field: "on_date",
					Detail: "expected a date as YYYY-MM-DD",
				}
			}
			out.onDate = &day
		}
	}

	if body.Weekdays != nil {
		days, err := validateWeekdays(*body.Weekdays)
		if err != nil {
			return out, err
		}
		out.weekdays = days
	}

	if body.raw.has("start_time") {
		value, err := validateClock(body.StartTime, "start_time")
		if err != nil {
			return out, err
		}
		out.startTime = value
	}
	if body.raw.has("end_time") {
		value, err := validateClock(body.EndTime, "end_time")
		if err != nil {
			return out, err
		}
		out.endTime = value
	}

	if body.SortOrder != nil {
		out.sortOrder = *body.SortOrder
	}

	// The three rules the schema states, checked here so that they come back as
	// a field rather than as 9001.
	if (out.startTime == nil) != (out.endTime == nil) {
		return out, &Error{
			Code: CodeInvalidField, Field: "start_time",
			Detail: "a time window needs both a start and an end, or neither",
		}
	}
	if out.startTime != nil && *out.startTime == *out.endTime {
		return out, &Error{
			Code: CodeInvalidField, Field: "end_time",
			Detail: "a window that starts and ends at the same time is empty",
		}
	}
	if out.onDate == nil && len(out.weekdays) == 0 && out.startTime == nil {
		return out, &Error{
			Code: CodeInvalidField, Field: "weekdays",
			Detail: "a filter must restrict something: a date, some weekdays or a time",
		}
	}

	return out, nil
}

func validateWeekdays(days []int) ([]int, *Error) {
	seen := make(map[int]bool, len(days))
	out := make([]int, 0, len(days))
	for _, day := range days {
		if day < 1 || day > 7 {
			return nil, &Error{
				Code: CodeInvalidField, Field: "weekdays",
				Detail: "days are 1 (Monday) to 7 (Sunday)",
			}
		}
		if seen[day] {
			continue
		}
		seen[day] = true
		out = append(out, day)
	}
	sort.Ints(out)
	return out, nil
}

func validateClock(value *string, field string) (*string, *Error) {
	if value == nil || *value == "" {
		return nil, nil
	}
	if !clockTime.MatchString(*value) {
		return nil, &Error{
			Code: CodeInvalidField, Field: field,
			Detail: "expected a time as HH:MM on a 24-hour clock",
		}
	}
	return value, nil
}

// availabilityAt reads the moment a menu is being asked about.
//
// Absent means "do not judge": the restaurant page shows the whole menu because
// it is the menu. The order page passes its order's fulfilment time, because
// what matters is whether the kitchen will make the dish when the food is
// fetched, not whether it would make it now.
func availabilityAt(r *http.Request) (*time.Time, *Error) {
	raw := r.URL.Query().Get("at")
	if raw == "" {
		return nil, nil
	}
	at, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, &Error{
			Code: CodeInvalidField, Field: "at",
			Detail: "expected an RFC 3339 timestamp",
		}
	}
	return &at, nil
}

// availableAt applies the rule, or reports true when no moment was asked about.
func availableAt(at *time.Time, category, item []model.AvailabilityFilter) bool {
	if at == nil {
		return true
	}
	return model.ItemAvailableAt(category, item, at.Local())
}

// filterBodies renders a set of filters for a menu payload.
func filterBodies(filters []model.AvailabilityFilter) []availabilityBody {
	out := make([]availabilityBody, 0, len(filters))
	for _, f := range filters {
		out = append(out, publicAvailability(f))
	}
	return out
}

// decorateItems attaches each item's filters and, when a moment was asked
// about, the verdict for it.
//
// The verdict is computed here rather than left to the client because the rule
// is one rule: the same code that refuses to add an unavailable item to an
// order decides what the order page shows. A second implementation in the
// frontend would be a second thing to keep in step, and the two would disagree
// on the day it mattered.
func (h *MenuHandlers) decorateItems(
	r *http.Request, restaurantID uuid.UUID, bodies []menuItemBody, items []model.MenuItem,
) *Error {
	at, err := availabilityAt(r)
	if err != nil {
		return err
	}

	byItem, dbErr := db.FiltersByMenuItem(r.Context(), h.Pool, restaurantID)
	if dbErr != nil {
		return &Error{Code: CodeDatabaseUnavailable, Cause: dbErr}
	}
	byCategory, dbErr := db.FiltersByCategory(r.Context(), h.Pool, restaurantID)
	if dbErr != nil {
		return &Error{Code: CodeDatabaseUnavailable, Cause: dbErr}
	}

	for i := range bodies {
		own := byItem[items[i].ID]
		bodies[i].Availability = filterBodies(own)

		if at == nil {
			continue
		}
		var category []model.AvailabilityFilter
		if items[i].CategoryID != nil {
			category = byCategory[*items[i].CategoryID]
		}
		verdict := availableAt(at, category, own)
		bodies[i].AvailableAt = &verdict
	}
	return nil
}

// ItemAvailableAt answers the same question for one item, for the order path
// that has to refuse an item the kitchen will not make.
func (h *MenuHandlers) itemAvailableAt(
	r *http.Request, item model.MenuItem, at time.Time,
) (bool, error) {
	own, err := db.FiltersForMenuItem(r.Context(), h.Pool, item.ID)
	if err != nil {
		return false, err
	}
	var category []model.AvailabilityFilter
	if item.CategoryID != nil {
		category, err = db.FiltersForCategory(r.Context(), h.Pool, *item.CategoryID)
		if err != nil {
			return false, err
		}
	}
	return model.ItemAvailableAt(category, own, at.Local()), nil
}

// setCategoryAvailability replaces the filters attached to a category.
//
// PUT rather than POST because it is a replacement: the editor shows the set of
// ticked boxes and submits the set it now has. Doing it any other way means a
// client that has to work out the difference, which is work the server can do
// in one statement.
func (h *MenuHandlers) setCategoryAvailability(w http.ResponseWriter, r *http.Request) {
	restaurantID, authErr := h.restaurantID(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}
	category, authErr := h.category(r, restaurantID)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}
	h.setAttachments(w, r, restaurantID, func(actor uuid.UUID, ids []uuid.UUID) error {
		return db.SetCategoryFilters(r.Context(), h.Pool, category.ID, actor, ids)
	})
}

// setItemAvailability is the same for a menu item.
func (h *MenuHandlers) setItemAvailability(w http.ResponseWriter, r *http.Request) {
	restaurantID, authErr := h.restaurantID(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}
	item, authErr := h.item(r, restaurantID)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}
	h.setAttachments(w, r, restaurantID, func(actor uuid.UUID, ids []uuid.UUID) error {
		return db.SetMenuItemFilters(r.Context(), h.Pool, item.ID, actor, ids)
	})
}

// setAttachments is the shared half: read the ids, check they are this
// restaurant's, write them, answer with what is now attached.
func (h *MenuHandlers) setAttachments(
	w http.ResponseWriter, r *http.Request, restaurantID uuid.UUID,
	write func(actor uuid.UUID, ids []uuid.UUID) error,
) {
	principal, authErr := RequireAuthenticated(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	var body struct {
		FilterIDs []string `json:"filter_ids"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	known, dbErr := db.ListAvailabilityFilters(r.Context(), h.Pool, restaurantID)
	if dbErr != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}
	byID := make(map[uuid.UUID]model.AvailabilityFilter, len(known))
	for _, f := range known {
		byID[f.ID] = f
	}

	ids := make([]uuid.UUID, 0, len(body.FilterIDs))
	attached := make([]model.AvailabilityFilter, 0, len(body.FilterIDs))
	seen := make(map[uuid.UUID]bool, len(body.FilterIDs))
	for _, raw := range body.FilterIDs {
		id, err := requiredUUID(raw, "filter_ids")
		if err != nil {
			WriteError(w, r, err)
			return
		}
		// A filter belonging to another restaurant is refused rather than
		// silently dropped: attaching it would be a rule from somebody else's
		// menu, and a request that half-worked is worse than one that did not.
		filter, ok := byID[id]
		if !ok {
			WriteError(w, r, &Error{
				Code: CodeInvalidField, Field: "filter_ids",
				Detail: "no availability filter of this restaurant has this id",
			})
			return
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
		attached = append(attached, filter)
	}

	if err := write(principal.User.ID, ids); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			WriteError(w, r, &Error{Code: CodeNotFound})
			return
		}
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	_ = WriteJSON(w, http.StatusOK, map[string]any{"availability": filterBodies(attached)})
}
