package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
)

func (h *MenuHandlers) listModifications(w http.ResponseWriter, r *http.Request) {
	restaurantID, lookupErr := h.restaurantID(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	item, lookupErr := h.lookupMenuItem(r, restaurantID)
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
	item, lookupErr := h.lookupMenuItem(r, restaurantID)
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
		WriteError(w, r, dbError(dbErr))
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
	item, lookupErr := h.lookupMenuItem(r, restaurantID)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	modification, lookupErr := h.lookupModification(r, item.ID)
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
	case dbErr != nil:
		WriteError(w, r, dbError(dbErr))
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
	item, lookupErr := h.lookupMenuItem(r, restaurantID)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	modification, lookupErr := h.lookupModification(r, item.ID)
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

// lookupModification resolves :k and checks it belongs to this item.
func (h *MenuHandlers) lookupModification(r *http.Request, itemID uuid.UUID) (model.Modification, *Error) {
	modification, err := loadParam(r, h.Pool, "k", db.ModificationByID)
	if err != nil {
		return model.Modification{}, err
	}
	if modification.MenuItemID != itemID {
		return model.Modification{}, &Error{Code: CodeNotFound}
	}
	return modification, nil
}
