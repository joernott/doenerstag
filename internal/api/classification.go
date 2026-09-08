package api

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
)

// applyClassification sets every classification list on a newly created item.
func (h *MenuHandlers) applyClassification(
	r *http.Request, itemID uuid.UUID, body menuItemRequest, actor uuid.UUID,
) *Error {
	tagIDs, err := parseIDList(body.TagIDs, "tag_ids")
	if err != nil {
		return err
	}
	allergenIDs, err := parseIDList(body.AllergenIDs, "allergen_ids")
	if err != nil {
		return err
	}
	additiveIDs, err := parseIDList(body.AdditiveIDs, "additive_ids")
	if err != nil {
		return err
	}

	ctx := r.Context()
	if dbErr := db.SetMenuItemTags(ctx, h.Pool, itemID, tagIDs, actor); dbErr != nil {
		return classificationError(dbErr, "tag_ids")
	}
	if dbErr := db.SetMenuItemAllergens(ctx, h.Pool, itemID, allergenIDs, actor); dbErr != nil {
		return classificationError(dbErr, "allergen_ids")
	}
	if dbErr := db.SetMenuItemAdditives(ctx, h.Pool, itemID, additiveIDs, actor); dbErr != nil {
		return classificationError(dbErr, "additive_ids")
	}
	return nil
}

// applySelectedClassification replaces only the lists whose key was sent.
//
// A PATCH that changes the price must not clear an item's allergens, so an
// absent key means "leave it" and a present one -- including an empty array --
// means "this is now the whole set".
func (h *MenuHandlers) applySelectedClassification(
	r *http.Request, itemID uuid.UUID, body menuItemRequest, actor uuid.UUID, raw rawPatch,
) *Error {
	ctx := r.Context()

	if raw.has("tag_ids") {
		ids, err := parseIDList(body.TagIDs, "tag_ids")
		if err != nil {
			return err
		}
		if dbErr := db.SetMenuItemTags(ctx, h.Pool, itemID, ids, actor); dbErr != nil {
			return classificationError(dbErr, "tag_ids")
		}
	}
	if raw.has("allergen_ids") {
		ids, err := parseIDList(body.AllergenIDs, "allergen_ids")
		if err != nil {
			return err
		}
		if dbErr := db.SetMenuItemAllergens(ctx, h.Pool, itemID, ids, actor); dbErr != nil {
			return classificationError(dbErr, "allergen_ids")
		}
	}
	if raw.has("additive_ids") {
		ids, err := parseIDList(body.AdditiveIDs, "additive_ids")
		if err != nil {
			return err
		}
		if dbErr := db.SetMenuItemAdditives(ctx, h.Pool, itemID, ids, actor); dbErr != nil {
			return classificationError(dbErr, "additive_ids")
		}
	}
	return nil
}

// classificationError turns a failed link write into a response.
//
// A foreign key violation here means a tag or allergen id that does not exist,
// which is the caller's mistake and names its field rather than surfacing as a
// server error.
func classificationError(err error, field string) *Error {
	if db.IsForeignKeyViolation(err) {
		return &Error{
			Code:   CodeInvalidField,
			Field:  field,
			Detail: "one of these identifiers does not exist",
		}
	}
	return &Error{Code: CodeDatabaseUnavailable, Cause: err}
}

// parseIDList turns request strings into ids.
func parseIDList(values []string, field string) ([]uuid.UUID, *Error) {
	ids := make([]uuid.UUID, 0, len(values))
	for _, raw := range values {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			return nil, &Error{
				Code:   CodeInvalidField,
				Field:  field,
				Detail: "this is not a valid identifier",
			}
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (h *MenuHandlers) listModifications(w http.ResponseWriter, r *http.Request) {
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

	modifications, dbErr := db.ListModifications(r.Context(), h.Pool, item.ID)
	if dbErr != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	bodies := make([]modificationBody, 0, len(modifications))
	for i := range modifications {
		bodies = append(bodies, publicModification(modifications[i]))
	}
	_ = WriteJSON(w, http.StatusOK, map[string]any{"modifications": bodies})
}

type modificationRequest struct {
	Name            string `json:"name"`
	PriceDeltaCents int64  `json:"price_delta_cents"`
	SortOrder       int    `json:"sort_order"`
}

func (h *MenuHandlers) createModification(w http.ResponseWriter, r *http.Request) {
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

	var body modificationRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	name := strings.TrimSpace(body.Name)
	if err := validateMenuName(name); err != nil {
		WriteError(w, r, err)
		return
	}

	// No check that the delta is positive: "without meat" can reasonably cost
	// less, and the schema allows a negative delta for exactly that.
	created, dbErr := db.CreateModification(r.Context(), h.Pool, db.NewModification{
		MenuItemID:      item.ID,
		Name:            name,
		PriceDeltaCents: body.PriceDeltaCents,
		SortOrder:       body.SortOrder,
	}, principal.User.ID)
	switch {
	case errors.Is(dbErr, db.ErrNameTaken):
		WriteError(w, r, &Error{Code: CodeNameExistsHere, Field: "name"})
		return
	case dbErr != nil:
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	_ = WriteJSON(w, http.StatusCreated, publicModification(created))
}

type patchModificationRequest struct {
	Name            *string `json:"name"`
	PriceDeltaCents *int64  `json:"price_delta_cents"`
	SortOrder       *int    `json:"sort_order"`
}

func (h *MenuHandlers) patchModification(w http.ResponseWriter, r *http.Request) {
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
	modification, lookupErr := h.modification(r, item.ID)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}

	var body patchModificationRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	var update db.ModificationUpdate
	if body.Name != nil {
		name := strings.TrimSpace(*body.Name)
		if err := validateMenuName(name); err != nil {
			WriteError(w, r, err)
			return
		}
		update.Name = &name
	}
	update.PriceDeltaCents = body.PriceDeltaCents
	update.SortOrder = body.SortOrder

	updated, dbErr := db.UpdateModification(r.Context(), h.Pool,
		modification.ID, principal.User.ID, update)
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

	_ = WriteJSON(w, http.StatusOK, publicModification(updated))
}

// deleteModification soft-deletes one. Administrator only, matching the other
// menu deletions.
func (h *MenuHandlers) deleteModification(w http.ResponseWriter, r *http.Request) {
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
	modification, lookupErr := h.modification(r, item.ID)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}

	if err := db.SoftDeleteModification(r.Context(), h.Pool,
		modification.ID, principal.User.ID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			WriteError(w, r, &Error{Code: CodeNotFound})
			return
		}
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// modification resolves :k and checks it belongs to this item.
func (h *MenuHandlers) modification(r *http.Request, itemID uuid.UUID) (model.Modification, *Error) {
	id, err := uuid.Parse(Param(r, "k"))
	if err != nil {
		return model.Modification{}, &Error{Code: CodeNotFound}
	}

	modification, dbErr := db.ModificationByID(r.Context(), h.Pool, id)
	switch {
	case errors.Is(dbErr, db.ErrNotFound):
		return model.Modification{}, &Error{Code: CodeNotFound}
	case dbErr != nil:
		return model.Modification{}, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr}
	}
	if modification.MenuItemID != itemID {
		return model.Modification{}, &Error{Code: CodeNotFound}
	}
	return modification, nil
}

func (h *MenuHandlers) listTags(w http.ResponseWriter, r *http.Request) {
	tags, err := db.ListTags(r.Context(), h.Pool)
	if err != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	bodies := make([]tagBody, 0, len(tags))
	for _, t := range tags {
		bodies = append(bodies, tagBody{
			ID: t.ID.String(), Code: t.Code, Name: t.Name, SortOrder: t.SortOrder,
		})
	}
	_ = WriteJSON(w, http.StatusOK, map[string]any{"tags": bodies})
}

type tagRequest struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// createTag adds a free tag.
//
// Tags are the one reference table users may add to, because the point of them
// is to describe food in whatever terms a particular group uses. A seeded tag
// is translated from its code; a user-created one has no catalog entry, so the
// name is required and is what gets shown.
func (h *MenuHandlers) createTag(w http.ResponseWriter, r *http.Request) {
	principal, authErr := RequireAuthenticated(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	var body tagRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	code := strings.ToLower(strings.TrimSpace(body.Code))
	if code == "" {
		WriteError(w, r, &Error{Code: CodeMissingField, Field: "code"})
		return
	}
	if !tagCodeShape.MatchString(code) {
		WriteError(w, r, &Error{
			Code:   CodeInvalidField,
			Field:  "code",
			Detail: "a tag code may be up to 64 lower-case letters, digits, dashes or underscores",
		})
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		WriteError(w, r, &Error{Code: CodeMissingField, Field: "name"})
		return
	}
	if utf8.RuneCountInString(name) > MaxTagNameLength {
		WriteError(w, r, &Error{
			Code:   CodeInvalidField,
			Field:  "name",
			Detail: "the name may be at most 64 characters",
		})
		return
	}

	created, dbErr := db.CreateTag(r.Context(), h.Pool, code, name, principal.User.ID)
	switch {
	case errors.Is(dbErr, db.ErrNameTaken):
		WriteError(w, r, &Error{
			Code:   CodeNameExistsHere,
			Field:  "code",
			Detail: "a tag with this code already exists",
		})
		return
	case dbErr != nil:
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	_ = WriteJSON(w, http.StatusCreated, tagBody{
		ID: created.ID.String(), Code: created.Code,
		Name: created.Name, SortOrder: created.SortOrder,
	})
}

// listAllergens and listAdditives are read-only: both are regulated lists, and
// a user inventing an allergen would be worse than useless.
func (h *MenuHandlers) listAllergens(w http.ResponseWriter, r *http.Request) {
	allergens, err := db.ListAllergens(r.Context(), h.Pool)
	if err != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	bodies := make([]referenceBody, 0, len(allergens))
	for _, a := range allergens {
		bodies = append(bodies, referenceBody{
			ID: a.ID.String(), Code: a.Code, Reference: a.Reference, SortOrder: a.SortOrder,
		})
	}
	_ = WriteJSON(w, http.StatusOK, map[string]any{"allergens": bodies})
}

func (h *MenuHandlers) listAdditives(w http.ResponseWriter, r *http.Request) {
	additives, err := db.ListAdditives(r.Context(), h.Pool)
	if err != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	bodies := make([]referenceBody, 0, len(additives))
	for _, a := range additives {
		bodies = append(bodies, referenceBody{
			ID: a.ID.String(), Code: a.Code, Reference: a.Reference, SortOrder: a.SortOrder,
		})
	}
	_ = WriteJSON(w, http.StatusOK, map[string]any{"additives": bodies})
}
