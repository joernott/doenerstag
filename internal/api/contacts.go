package api

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/joernott/doenerstag/internal/db"
)

// validateContacts turns request bodies into database inputs.
//
// The contact type is checked against the seeded table here rather than left to
// the foreign key, so an unknown type names the field it came from.
func (h *RestaurantHandlers) validateContacts(
	r *http.Request, requests []contactRequest,
) ([]db.NewContact, *Error) {
	contacts := make([]db.NewContact, 0, len(requests))
	for _, in := range requests {
		contact, err := h.validateContact(r, in)
		if err != nil {
			return nil, err
		}
		contacts = append(contacts, contact)
	}
	return contacts, nil
}

func (h *RestaurantHandlers) validateContact(
	r *http.Request, in contactRequest,
) (db.NewContact, *Error) {
	typeID, err := uuid.Parse(strings.TrimSpace(in.ContactTypeID))
	if err != nil {
		return db.NewContact{}, &Error{
			Code:   CodeInvalidField,
			Field:  "contact_type_id",
			Detail: "this is not a valid identifier",
		}
	}

	if _, dbErr := db.ContactTypeByID(r.Context(), h.Pool, typeID); dbErr != nil {
		if errors.Is(dbErr, db.ErrNotFound) {
			return db.NewContact{}, &Error{
				Code:   CodeInvalidField,
				Field:  "contact_type_id",
				Detail: "no contact type has this id; see GET /contact-types",
			}
		}
		return db.NewContact{}, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr}
	}

	value := strings.TrimSpace(in.Value)
	if value == "" {
		return db.NewContact{}, &Error{Code: CodeMissingField, Field: "value"}
	}
	if utf8.RuneCountInString(value) > MaxContactValueLength {
		return db.NewContact{}, &Error{
			Code:   CodeInvalidField,
			Field:  "value",
			Detail: "the value may be at most 500 characters",
		}
	}

	label := strings.TrimSpace(in.Label)
	if utf8.RuneCountInString(label) > MaxContactLabelLength {
		return db.NewContact{}, &Error{
			Code:   CodeInvalidField,
			Field:  "label",
			Detail: "the label may be at most 100 characters",
		}
	}

	return db.NewContact{
		ContactTypeID: typeID,
		Value:         value,
		Label:         label,
		SortOrder:     in.SortOrder,
	}, nil
}

func (h *RestaurantHandlers) listContacts(w http.ResponseWriter, r *http.Request) {
	restaurant, err := h.lookup(r)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	contacts, dbErr := db.ListContacts(r.Context(), h.Pool, restaurant.ID)
	if dbErr != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	bodies := make([]contactBody, 0, len(contacts))
	for i := range contacts {
		bodies = append(bodies, publicContact(contacts[i]))
	}
	_ = WriteJSON(w, http.StatusOK, map[string]any{"contacts": bodies})
}

func (h *RestaurantHandlers) createContact(w http.ResponseWriter, r *http.Request) {
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

	var body contactRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	contact, err := h.validateContact(r, body)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	contact.RestaurantID = restaurant.ID

	created, dbErr := db.CreateContact(r.Context(), h.Pool, contact, principal.User.ID)
	if dbErr != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}
	_ = WriteJSON(w, http.StatusCreated, publicContact(created))
}

func (h *RestaurantHandlers) replaceContact(w http.ResponseWriter, r *http.Request) {
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

	existing, lookupErr := h.lookupContact(r, restaurant.ID)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}

	var body contactRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	contact, err := h.validateContact(r, body)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	contact.RestaurantID = restaurant.ID

	updated, dbErr := db.ReplaceContact(r.Context(), h.Pool, existing.ID, contact, principal.User.ID)
	if dbErr != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}
	_ = WriteJSON(w, http.StatusOK, publicContact(updated))
}

func (h *RestaurantHandlers) deleteContact(w http.ResponseWriter, r *http.Request) {
	restaurant, lookupErr := h.lookup(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	if _, authErr := RequireAuthenticated(r); authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	existing, lookupErr := h.lookupContact(r, restaurant.ID)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}

	err := db.DeleteContact(r.Context(), h.Pool, existing.ID)
	switch {
	case errors.Is(err, db.ErrLastContact):
		// F3.1: a restaurant needs at least one contact. Removing the last one
		// would leave a record that violates its own rule, so the API refuses
		// rather than letting the restaurant become uncontactable.
		WriteError(w, r, &Error{
			Code:   CodeRestaurantNeedsContact,
			Detail: "this is the restaurant's only contact; add another before removing it",
		})
		return
	case errors.Is(err, db.ErrNotFound):
		WriteError(w, r, &Error{Code: CodeNotFound})
		return
	case err != nil:
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// lookupContact resolves the :cid parameter and checks it belongs to this
// restaurant.
//
// The ownership check is what stops one restaurant's contact being edited or
// removed through another's URL. It answers 404 rather than something more
// specific: from the caller's side there is no such contact at this path.
func (h *RestaurantHandlers) lookupContact(r *http.Request, restaurantID uuid.UUID) (contactRef, *Error) {
	id, err := uuid.Parse(Param(r, "cid"))
	if err != nil {
		return contactRef{}, &Error{Code: CodeNotFound}
	}

	contact, dbErr := db.ContactByID(r.Context(), h.Pool, id)
	switch {
	case errors.Is(dbErr, db.ErrNotFound):
		return contactRef{}, &Error{Code: CodeNotFound}
	case dbErr != nil:
		return contactRef{}, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr}
	}
	if contact.RestaurantID != restaurantID {
		return contactRef{}, &Error{Code: CodeNotFound}
	}
	return contactRef{ID: contact.ID}, nil
}

// contactRef is the little that the lookup needs to hand back.
type contactRef struct{ ID uuid.UUID }

// listCurrencies and listContactTypes serve the seeded reference tables.
//
// Neither returns a display name: seeded reference data is identified by a
// stable code and named by the frontend's i18n catalog, so that adding a
// language does not mean a database migration. See docs/07_i18n.md.
func (h *RestaurantHandlers) listCurrencies(w http.ResponseWriter, r *http.Request) {
	currencies, err := db.ListCurrencies(r.Context(), h.Pool)
	if err != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	type currencyBody struct {
		Code          string `json:"code"`
		Symbol        string `json:"symbol"`
		MinorUnit     int    `json:"minor_unit"`
		MinorPerMajor int    `json:"minor_per_major"`
		SortOrder     int    `json:"sort_order"`
	}

	bodies := make([]currencyBody, 0, len(currencies))
	for _, c := range currencies {
		bodies = append(bodies, currencyBody{
			Code: c.Code, Symbol: c.Symbol,
			MinorUnit: c.MinorUnit, MinorPerMajor: c.MinorPerMajor,
			SortOrder: c.SortOrder,
		})
	}
	_ = WriteJSON(w, http.StatusOK, map[string]any{"currencies": bodies})
}

func (h *RestaurantHandlers) listContactTypes(w http.ResponseWriter, r *http.Request) {
	types, err := db.ListContactTypes(r.Context(), h.Pool)
	if err != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	type contactTypeBody struct {
		ID        string `json:"id"`
		Code      string `json:"code"`
		RenderAs  string `json:"render_as"`
		SortOrder int    `json:"sort_order"`
	}

	bodies := make([]contactTypeBody, 0, len(types))
	for _, t := range types {
		bodies = append(bodies, contactTypeBody{
			ID: t.ID.String(), Code: t.Code,
			RenderAs: t.RenderAs, SortOrder: t.SortOrder,
		})
	}
	_ = WriteJSON(w, http.StatusOK, map[string]any{"contact_types": bodies})
}
