package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/joernott/doenerstag/internal/db"
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
// An unknown reference here means a tag or allergen id that does not exist,
// which is the caller's mistake and names its field rather than surfacing as a
// server error.
func classificationError(err error, field string) *Error {
	if errors.Is(err, db.ErrUnknownReference) {
		return &Error{
			Code:   CodeInvalidField,
			Field:  field,
			Detail: "one of these identifiers does not exist",
		}
	}
	return &Error{Code: CodeDatabaseUnavailable, Cause: err}
}
