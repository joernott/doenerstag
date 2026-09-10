package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
)

// listItems serves the filtered menu.
func (h *MenuHandlers) listItems(w http.ResponseWriter, r *http.Request) {
	restaurantID, lookupErr := h.restaurantID(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}

	filter, err := parseMenuFilter(r.URL.Query())
	if err != nil {
		WriteError(w, r, err)
		return
	}

	items, dbErr := db.ListMenuItems(r.Context(), h.Pool, restaurantID, filter)
	if dbErr != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	bodies := make([]menuItemBody, 0, len(items))
	for i := range items {
		bodies = append(bodies, publicMenuItem(items[i]))
	}
	if err := h.decorateItems(r, restaurantID, bodies, items); err != nil {
		WriteError(w, r, err)
		return
	}
	_ = WriteJSON(w, http.StatusOK, map[string]any{"menu_items": bodies})
}

// parseMenuFilter reads the documented query parameters.
//
// Every one is repeatable. An unparseable category id is an error rather than
// being ignored: a filter that silently drops a term returns more than was
// asked for, which for an allergen exclusion is the dangerous direction.
func parseMenuFilter(query url.Values) (db.MenuItemFilter, *Error) {
	var filter db.MenuItemFilter

	for _, raw := range query["category"] {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			return filter, &Error{
				Code:   CodeInvalidField,
				Field:  "category",
				Detail: "this is not a valid category identifier",
			}
		}
		filter.CategoryIDs = append(filter.CategoryIDs, id)
	}

	filter.TagCodes = trimmedValues(query["tag"])
	filter.ExcludeAllergenCodes = trimmedValues(query["exclude_allergen"])
	filter.ExcludeAdditiveCodes = trimmedValues(query["exclude_additive"])

	if raw := query.Get("available"); raw != "" {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "true", "1":
			value := true
			filter.Available = &value
		case "false", "0":
			value := false
			filter.Available = &value
		default:
			return filter, &Error{
				Code:   CodeInvalidField,
				Field:  "available",
				Detail: "the value must be true or false",
			}
		}
	}

	return filter, nil
}

// trimmedValues drops empty entries, so ?tag= is the same as omitting it.
func trimmedValues(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (h *MenuHandlers) getItem(w http.ResponseWriter, r *http.Request) {
	restaurantID, lookupErr := h.restaurantID(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}

	item, lookupErr := h.item(r, restaurantID)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}

	detailed, dbErr := db.MenuItemWithDetail(r.Context(), h.Pool, item.ID)
	if dbErr != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}
	_ = WriteJSON(w, http.StatusOK, publicMenuItem(detailed))
}

// menuItemRequest is the body of POST and, with pointers, of PATCH.
type menuItemRequest struct {
	CategoryID  *string  `json:"category_id"`
	ExternalID  string   `json:"external_id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	ImageID     *string  `json:"image_id"`
	PriceCents  int64    `json:"price_cents"`
	Available   *bool    `json:"available"`
	TagIDs      []string `json:"tag_ids"`
	AllergenIDs []string `json:"allergen_ids"`
	AdditiveIDs []string `json:"additive_ids"`
}

func (h *MenuHandlers) createItem(w http.ResponseWriter, r *http.Request) {
	restaurantID, lookupErr := h.restaurantID(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	principal, authErr := RequireAuthenticated(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	var body menuItemRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	name := strings.TrimSpace(body.Name)
	if err := validateMenuName(name); err != nil {
		WriteError(w, r, err)
		return
	}

	externalID := strings.TrimSpace(body.ExternalID)
	if err := validateExternalID(externalID); err != nil {
		WriteError(w, r, err)
		return
	}
	if err := validateDescription(body.Description); err != nil {
		WriteError(w, r, err)
		return
	}
	if body.PriceCents < 0 {
		WriteError(w, r, &Error{
			Code:   CodeInvalidField,
			Field:  "price_cents",
			Detail: "the price may not be negative",
		})
		return
	}

	categoryID, err := h.parseCategory(r, restaurantID, body.CategoryID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	imageID, err := parseOptionalUUID(body.ImageID, "image_id")
	if err != nil {
		WriteError(w, r, err)
		return
	}

	available := true
	if body.Available != nil {
		available = *body.Available
	}

	created, dbErr := db.CreateMenuItem(r.Context(), h.Pool, db.NewMenuItem{
		RestaurantID: restaurantID,
		CategoryID:   categoryID,
		ExternalID:   externalID,
		Name:         name,
		Description:  strings.TrimSpace(body.Description),
		ImageID:      imageID,
		PriceCents:   body.PriceCents,
		Available:    available,
	}, principal.User.ID)
	switch {
	case errors.Is(dbErr, db.ErrNameTaken):
		WriteError(w, r, &Error{Code: CodeNameExistsHere, Field: "name"})
		return
	case errors.Is(dbErr, db.ErrNotFound):
		WriteError(w, r, &Error{
			Code:   CodeInvalidField,
			Detail: "a referenced record does not exist",
		})
		return
	case dbErr != nil:
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	if err := h.applyClassification(r, created.ID, body, principal.User.ID); err != nil {
		WriteError(w, r, err)
		return
	}

	h.writeItem(w, r, created.ID, http.StatusCreated)
}

type patchMenuItemRequest struct {
	CategoryID  *string  `json:"category_id"`
	ExternalID  *string  `json:"external_id"`
	Name        *string  `json:"name"`
	Description *string  `json:"description"`
	ImageID     *string  `json:"image_id"`
	PriceCents  *int64   `json:"price_cents"`
	Available   *bool    `json:"available"`
	TagIDs      []string `json:"tag_ids"`
	AllergenIDs []string `json:"allergen_ids"`
	AdditiveIDs []string `json:"additive_ids"`

	raw rawPatch `json:"-"`
}

func (h *MenuHandlers) patchItem(w http.ResponseWriter, r *http.Request) {
	restaurantID, lookupErr := h.restaurantID(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	principal, authErr := RequireAuthenticated(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	item, lookupErr := h.item(r, restaurantID)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}

	var body patchMenuItemRequest
	if !decodeJSONWithRaw(w, r, &body, &body.raw) {
		return
	}

	update, err := h.validateItemPatch(r, restaurantID, body)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	updated, dbErr := db.UpdateMenuItem(r.Context(), h.Pool, item.ID, principal.User.ID, update)
	switch {
	case errors.Is(dbErr, db.ErrNameTaken):
		WriteError(w, r, &Error{Code: CodeNameExistsHere, Field: "name"})
		return
	case errors.Is(dbErr, db.ErrNotFound):
		WriteError(w, r, &Error{Code: CodeNotFound})
		return
	case dbErr != nil:
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	// Classification is replaced only when its key was sent, so a PATCH that
	// changes the price does not clear an item's allergens.
	classification := menuItemRequest{
		TagIDs:      body.TagIDs,
		AllergenIDs: body.AllergenIDs,
		AdditiveIDs: body.AdditiveIDs,
	}
	if err := h.applySelectedClassification(r, updated.ID, classification,
		principal.User.ID, body.raw); err != nil {
		WriteError(w, r, err)
		return
	}

	h.writeItem(w, r, updated.ID, http.StatusOK)
}

func (h *MenuHandlers) validateItemPatch(
	r *http.Request, restaurantID uuid.UUID, body patchMenuItemRequest,
) (db.MenuItemUpdate, *Error) {
	var update db.MenuItemUpdate

	if body.Name != nil {
		name := strings.TrimSpace(*body.Name)
		if err := validateMenuName(name); err != nil {
			return update, err
		}
		update.Name = &name
	}

	if body.ExternalID != nil {
		externalID := strings.TrimSpace(*body.ExternalID)
		if err := validateExternalID(externalID); err != nil {
			return update, err
		}
		update.ExternalID = &externalID
	}

	if body.Description != nil {
		if err := validateDescription(*body.Description); err != nil {
			return update, err
		}
		description := strings.TrimSpace(*body.Description)
		update.Description = &description
	}

	if body.PriceCents != nil {
		if *body.PriceCents < 0 {
			return update, &Error{
				Code:   CodeInvalidField,
				Field:  "price_cents",
				Detail: "the price may not be negative",
			}
		}
		update.PriceCents = body.PriceCents
	}

	update.Available = body.Available

	if body.raw.has("category_id") {
		categoryID, err := h.parseCategory(r, restaurantID, body.CategoryID)
		if err != nil {
			return update, err
		}
		update.CategoryID = &categoryID
	}
	if body.raw.has("image_id") {
		imageID, err := parseOptionalUUID(body.ImageID, "image_id")
		if err != nil {
			return update, err
		}
		update.ImageID = &imageID
	}

	return update, nil
}

// deleteItem soft-deletes an item. Administrator only (F4.5).
func (h *MenuHandlers) deleteItem(w http.ResponseWriter, r *http.Request) {
	restaurantID, lookupErr := h.restaurantID(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	principal, authErr := RequireAdmin(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	item, lookupErr := h.item(r, restaurantID)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}

	if err := db.SoftDeleteMenuItem(r.Context(), h.Pool, item.ID, principal.User.ID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			WriteError(w, r, &Error{Code: CodeNotFound})
			return
		}
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// item resolves :mid and checks it belongs to this restaurant.
func (h *MenuHandlers) item(r *http.Request, restaurantID uuid.UUID) (model.MenuItem, *Error) {
	id, err := uuid.Parse(Param(r, "mid"))
	if err != nil {
		return model.MenuItem{}, &Error{Code: CodeNotFound}
	}

	item, dbErr := db.MenuItemByID(r.Context(), h.Pool, id)
	switch {
	case errors.Is(dbErr, db.ErrNotFound):
		return model.MenuItem{}, &Error{Code: CodeNotFound}
	case dbErr != nil:
		return model.MenuItem{}, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr}
	}
	if item.RestaurantID != restaurantID {
		return model.MenuItem{}, &Error{Code: CodeNotFound}
	}
	return item, nil
}

// writeItem answers with the item and everything hanging off it.
func (h *MenuHandlers) writeItem(w http.ResponseWriter, r *http.Request, id uuid.UUID, status int) {
	detailed, err := db.MenuItemWithDetail(r.Context(), h.Pool, id)
	if err != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}
	_ = WriteJSON(w, status, publicMenuItem(detailed))
}

// parseCategory resolves an optional category and checks it belongs to the
// same restaurant.
//
// Without that check an item could be filed under another restaurant's
// category, which would put it in the wrong menu and is not something the
// foreign key can prevent.
func (h *MenuHandlers) parseCategory(
	r *http.Request, restaurantID uuid.UUID, value *string,
) (*uuid.UUID, *Error) {
	id, err := parseOptionalUUID(value, "category_id")
	if err != nil || id == nil {
		return nil, err
	}

	category, dbErr := db.CategoryByID(r.Context(), h.Pool, *id)
	switch {
	case errors.Is(dbErr, db.ErrNotFound):
		return nil, &Error{
			Code:   CodeInvalidField,
			Field:  "category_id",
			Detail: "no category has this id",
		}
	case dbErr != nil:
		return nil, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr}
	}
	if category.RestaurantID != restaurantID {
		return nil, &Error{
			Code:   CodeInvalidField,
			Field:  "category_id",
			Detail: "that category belongs to a different restaurant",
		}
	}
	return id, nil
}

func validateExternalID(value string) *Error {
	if value == "" {
		return nil
	}
	if !externalIDShape.MatchString(value) {
		return &Error{
			Code:   CodeInvalidField,
			Field:  "external_id",
			Detail: "the item number may be up to 32 letters, digits, dots, dashes or underscores",
		}
	}
	return nil
}

func validateDescription(value string) *Error {
	if utf8.RuneCountInString(value) > MaxMenuDescriptionLength {
		return &Error{
			Code:   CodeInvalidField,
			Field:  "description",
			Detail: "the description is too long",
		}
	}
	return nil
}
