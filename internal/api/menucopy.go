package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/joernott/doenerstag/internal/db"
)

// MaxCopyLabelLength bounds the word a copy's name is given, "copy" or
// "Kopie". It is a word, not a name; the bound keeps the suffix from eating a
// 200-character name whole.
const MaxCopyLabelLength = 32

// duplicateRequest is the body of both duplicate routes. Label is the word
// for "copy" in the language of whoever is copying, because the server has
// no language of its own to name things in (docs/07). Empty means "copy".
type duplicateRequest struct {
	Label string `json:"label"`
}

// copyLabel reads and checks the body of a duplicate request.
func copyLabel(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body duplicateRequest
	if !decodeJSON(w, r, &body) {
		return "", false
	}
	label := strings.TrimSpace(body.Label)
	if label == "" {
		label = "copy"
	}
	if longerThan(label, MaxCopyLabelLength) {
		WriteError(w, r, tooLong("label", "label", MaxCopyLabelLength))
		return "", false
	}
	return label, true
}

// duplicateItem copies a dish with everything it carries (21.1). Open to
// the same people as creating one.
func (h *MenuHandlers) duplicateItem(w http.ResponseWriter, r *http.Request) {
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
	label, ok := copyLabel(w, r)
	if !ok {
		return
	}

	copyID, err := db.DuplicateMenuItem(r.Context(), h.Pool, item.ID, label, principal.User.ID)
	if err != nil {
		WriteError(w, r, duplicateError(err))
		return
	}
	h.writeItem(w, r, copyID, http.StatusCreated)
}

// duplicateCategory copies a category and every dish in it (21.2).
func (h *MenuHandlers) duplicateCategory(w http.ResponseWriter, r *http.Request) {
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

	category, lookupErr := h.lookupCategory(r, restaurantID)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	label, ok := copyLabel(w, r)
	if !ok {
		return
	}

	copyID, err := db.DuplicateCategory(r.Context(), h.Pool, category.ID, label, principal.User.ID)
	if err != nil {
		WriteError(w, r, duplicateError(err))
		return
	}

	created, err := db.CategoryByID(r.Context(), h.Pool, copyID)
	if err != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}
	_ = WriteJSON(w, http.StatusCreated, publicCategory(created, h.categoryFilters(r, copyID)))
}

// duplicateError maps a failed copy. A name taken here is a copy made at the
// same moment by somebody else, since CopyName has already stepped past every
// name it could see; trying again finds the next free one.
func duplicateError(err error) *Error {
	if errors.Is(err, db.ErrNameTaken) {
		return &Error{Code: CodeNameExistsHere, Field: "name"}
	}
	return dbError(err)
}
