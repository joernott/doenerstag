package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/joernott/doenerstag/internal/db"
)

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
	if longerThan(name, MaxTagNameLength) {
		WriteError(w, r, tooLong("name", "name", MaxTagNameLength))
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
