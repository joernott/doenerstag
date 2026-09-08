package api

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
)

// Field limits, matching the CHECK constraints in migration 000005.
const (
	MaxRestaurantNameLength = 200
	MaxContactValueLength   = 500
	MaxContactLabelLength   = 100
)

// RestaurantHandlers serves restaurants, their contacts and their opening
// hours.
type RestaurantHandlers struct {
	Pool *pgxpool.Pool
}

// Register adds the restaurant routes.
//
// Creating and editing are open to any logged-in user so the database can be
// crowdsourced; only deleting is restricted to the administrator, so that a
// misclick cannot remove a restaurant's whole menu. That asymmetry is
// deliberate and is stated in docs/05_auth_and_permissions.md.
func (h *RestaurantHandlers) Register(r *Router) {
	r.HandleFunc(http.MethodGet, "/restaurants", h.list)
	r.HandleFunc(http.MethodPost, "/restaurants", h.create)
	r.HandleFunc(http.MethodGet, "/restaurants/:id", h.get)
	r.HandleFunc(http.MethodPatch, "/restaurants/:id", h.patch)
	r.HandleFunc(http.MethodDelete, "/restaurants/:id", h.remove)

	r.HandleFunc(http.MethodGet, "/restaurants/:id/contacts", h.listContacts)
	r.HandleFunc(http.MethodPost, "/restaurants/:id/contacts", h.createContact)
	r.HandleFunc(http.MethodPut, "/restaurants/:id/contacts/:cid", h.replaceContact)
	r.HandleFunc(http.MethodDelete, "/restaurants/:id/contacts/:cid", h.deleteContact)

	r.HandleFunc(http.MethodGet, "/restaurants/:id/opening-hours", h.listOpeningHours)
	r.HandleFunc(http.MethodPut, "/restaurants/:id/opening-hours", h.replaceOpeningHours)

	r.HandleFunc(http.MethodGet, "/currencies", h.listCurrencies)
	r.HandleFunc(http.MethodGet, "/contact-types", h.listContactTypes)
}

// restaurantBody is the list shape: the restaurant and its logo id, no
// subresources.
type restaurantBody struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	LogoImageID        *string `json:"logo_image_id"`
	CurrencyCode       string  `json:"currency_code"`
	MinOrderValueCents *int64  `json:"min_order_value_cents"`
	DeliveryFeeCents   *int64  `json:"delivery_fee_cents"`
	Notes              string  `json:"notes"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

func publicRestaurant(r model.Restaurant) restaurantBody {
	body := restaurantBody{
		ID:                 r.ID.String(),
		Name:               r.Name,
		CurrencyCode:       r.CurrencyCode,
		MinOrderValueCents: r.MinOrderValueCents,
		DeliveryFeeCents:   r.DeliveryFeeCents,
		Notes:              r.Notes,
		CreatedAt:          r.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:          r.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if r.LogoImageID != nil {
		id := r.LogoImageID.String()
		body.LogoImageID = &id
	}
	return body
}

// restaurantDetailBody is GET /restaurants/{id}: the restaurant with its
// contacts and opening hours, so the detail page is one request.
type restaurantDetailBody struct {
	restaurantBody
	Contacts     []contactBody       `json:"contacts"`
	OpeningHours []openingPeriodBody `json:"opening_hours"`
}

type contactBody struct {
	ID              string `json:"id"`
	ContactTypeID   string `json:"contact_type_id"`
	ContactTypeCode string `json:"contact_type_code"`
	RenderAs        string `json:"render_as"`
	Value           string `json:"value"`
	Label           string `json:"label"`
	SortOrder       int    `json:"sort_order"`
}

func publicContact(c model.Contact) contactBody {
	return contactBody{
		ID:              c.ID.String(),
		ContactTypeID:   c.ContactTypeID.String(),
		ContactTypeCode: c.ContactTypeCode,
		RenderAs:        c.RenderAs,
		Value:           c.Value,
		Label:           c.Label,
		SortOrder:       c.SortOrder,
	}
}

type openingPeriodBody struct {
	ID        string `json:"id"`
	DayOfWeek int    `json:"day_of_week"`
	Start     string `json:"start"`
	End       string `json:"end"`
	// CrossesMidnight is derived rather than stored, so a client does not have
	// to know that an end before a start is meaningful rather than a mistake.
	CrossesMidnight bool `json:"crosses_midnight"`
}

func publicOpeningPeriod(p model.OpeningPeriod) openingPeriodBody {
	return openingPeriodBody{
		ID:              p.ID.String(),
		DayOfWeek:       p.DayOfWeek,
		Start:           p.Start,
		End:             p.End,
		CrossesMidnight: p.CrossesMidnight(),
	}
}

func (h *RestaurantHandlers) list(w http.ResponseWriter, r *http.Request) {
	restaurants, err := db.ListRestaurants(r.Context(), h.Pool)
	if err != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	bodies := make([]restaurantBody, 0, len(restaurants))
	for i := range restaurants {
		bodies = append(bodies, publicRestaurant(restaurants[i]))
	}
	_ = WriteJSON(w, http.StatusOK, map[string]any{"restaurants": bodies})
}

func (h *RestaurantHandlers) get(w http.ResponseWriter, r *http.Request) {
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
	periods, dbErr := db.ListOpeningHours(r.Context(), h.Pool, restaurant.ID)
	if dbErr != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	body := restaurantDetailBody{
		restaurantBody: publicRestaurant(restaurant),
		Contacts:       make([]contactBody, 0, len(contacts)),
		OpeningHours:   make([]openingPeriodBody, 0, len(periods)),
	}
	for i := range contacts {
		body.Contacts = append(body.Contacts, publicContact(contacts[i]))
	}
	for i := range periods {
		body.OpeningHours = append(body.OpeningHours, publicOpeningPeriod(periods[i]))
	}
	_ = WriteJSON(w, http.StatusOK, body)
}

// createRestaurantRequest is the body of POST /restaurants.
//
// A restaurant needs at least one contact (F3.1), so the contacts come with the
// creation rather than in a follow-up call. Creating one and then failing to
// add a contact would otherwise leave a restaurant that violates its own rule.
type createRestaurantRequest struct {
	Name               string           `json:"name"`
	CurrencyCode       string           `json:"currency_code"`
	LogoImageID        *string          `json:"logo_image_id"`
	MinOrderValueCents *int64           `json:"min_order_value_cents"`
	DeliveryFeeCents   *int64           `json:"delivery_fee_cents"`
	Notes              string           `json:"notes"`
	Contacts           []contactRequest `json:"contacts"`
}

type contactRequest struct {
	ContactTypeID string `json:"contact_type_id"`
	Value         string `json:"value"`
	Label         string `json:"label"`
	SortOrder     int    `json:"sort_order"`
}

func (h *RestaurantHandlers) create(w http.ResponseWriter, r *http.Request) {
	principal, authErr := RequireAuthenticated(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	var body createRestaurantRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	name := strings.TrimSpace(body.Name)
	if err := validateRestaurantName(name); err != nil {
		WriteError(w, r, err)
		return
	}

	currency := strings.ToUpper(strings.TrimSpace(body.CurrencyCode))
	if err := h.validateCurrency(r, currency); err != nil {
		WriteError(w, r, err)
		return
	}

	logoID, err := parseOptionalUUID(body.LogoImageID, "logo_image_id")
	if err != nil {
		WriteError(w, r, err)
		return
	}

	if err := validateMoney(body.MinOrderValueCents, "min_order_value_cents"); err != nil {
		WriteError(w, r, err)
		return
	}
	if err := validateMoney(body.DeliveryFeeCents, "delivery_fee_cents"); err != nil {
		WriteError(w, r, err)
		return
	}

	// F3.1: at least one contact, checked before anything is written.
	if len(body.Contacts) == 0 {
		WriteError(w, r, &Error{Code: CodeRestaurantNeedsContact, Field: "contacts"})
		return
	}
	contacts, err := h.validateContacts(r, body.Contacts)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	created, dbErr := db.CreateRestaurantWithContacts(r.Context(), h.Pool, db.NewRestaurant{
		Name:               name,
		CurrencyCode:       currency,
		LogoImageID:        logoID,
		MinOrderValueCents: body.MinOrderValueCents,
		DeliveryFeeCents:   body.DeliveryFeeCents,
		Notes:              strings.TrimSpace(body.Notes),
	}, contacts, principal.User.ID)
	switch {
	case errors.Is(dbErr, db.ErrNameTaken):
		WriteError(w, r, &Error{Code: CodeNameExistsHere, Field: "name"})
		return
	case errors.Is(dbErr, db.ErrNotFound):
		// A logo image or contact type that does not exist.
		WriteError(w, r, &Error{Code: CodeInvalidField, Detail: "a referenced record does not exist"})
		return
	case dbErr != nil:
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	h.writeDetail(w, r, created)
}

// patchRestaurantRequest carries the fields PATCH may change.
type patchRestaurantRequest struct {
	Name               *string  `json:"name"`
	CurrencyCode       *string  `json:"currency_code"`
	LogoImageID        *string  `json:"logo_image_id"`
	MinOrderValueCents *int64   `json:"min_order_value_cents"`
	DeliveryFeeCents   *int64   `json:"delivery_fee_cents"`
	Notes              *string  `json:"notes"`
	raw                rawPatch `json:"-"`
}

// rawPatch records which keys were present, so that an explicit null can be
// told from an absent field. encoding/json collapses both to a nil pointer,
// and for the three nullable columns here the difference is "leave it" versus
// "clear it".
type rawPatch map[string]any

func (p rawPatch) has(key string) bool {
	_, present := p[key]
	return present
}

func (h *RestaurantHandlers) patch(w http.ResponseWriter, r *http.Request) {
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

	var body patchRestaurantRequest
	if !decodeJSONWithRaw(w, r, &body, &body.raw) {
		return
	}

	update, err := h.validatePatch(r, body)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	updated, dbErr := db.UpdateRestaurant(r.Context(), h.Pool, restaurant.ID,
		principal.User.ID, update)
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

	h.writeDetail(w, r, updated)
}

func (h *RestaurantHandlers) validatePatch(
	r *http.Request, body patchRestaurantRequest,
) (db.RestaurantUpdate, *Error) {
	var update db.RestaurantUpdate

	if body.Name != nil {
		name := strings.TrimSpace(*body.Name)
		if err := validateRestaurantName(name); err != nil {
			return update, err
		}
		update.Name = &name
	}

	if body.CurrencyCode != nil {
		currency := strings.ToUpper(strings.TrimSpace(*body.CurrencyCode))
		if err := h.validateCurrency(r, currency); err != nil {
			return update, err
		}
		update.CurrencyCode = &currency
	}

	if body.raw.has("logo_image_id") {
		id, err := parseOptionalUUID(body.LogoImageID, "logo_image_id")
		if err != nil {
			return update, err
		}
		update.LogoImageID = &id
	}

	if body.raw.has("min_order_value_cents") {
		if err := validateMoney(body.MinOrderValueCents, "min_order_value_cents"); err != nil {
			return update, err
		}
		update.MinOrderValueCents = &body.MinOrderValueCents
	}
	if body.raw.has("delivery_fee_cents") {
		if err := validateMoney(body.DeliveryFeeCents, "delivery_fee_cents"); err != nil {
			return update, err
		}
		update.DeliveryFeeCents = &body.DeliveryFeeCents
	}

	if body.Notes != nil {
		notes := strings.TrimSpace(*body.Notes)
		update.Notes = &notes
	}

	return update, nil
}

// remove soft-deletes a restaurant. Administrator only (F3.5).
func (h *RestaurantHandlers) remove(w http.ResponseWriter, r *http.Request) {
	principal, authErr := RequireAdmin(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	id, parseErr := uuid.Parse(Param(r, "id"))
	if parseErr != nil {
		WriteError(w, r, &Error{Code: CodeNotFound})
		return
	}

	err := db.SoftDeleteRestaurant(r.Context(), h.Pool, id, principal.User.ID)
	switch {
	case errors.Is(err, db.ErrStillReferenced):
		// F3.5's second half: an order points at it, and deleting would make
		// that order unreadable.
		WriteError(w, r, &Error{Code: CodeRestaurantInUse})
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

// lookup resolves the :id path parameter to a living restaurant.
func (h *RestaurantHandlers) lookup(r *http.Request) (model.Restaurant, *Error) {
	id, err := uuid.Parse(Param(r, "id"))
	if err != nil {
		return model.Restaurant{}, &Error{Code: CodeNotFound}
	}

	restaurant, dbErr := db.RestaurantByID(r.Context(), h.Pool, id)
	switch {
	case errors.Is(dbErr, db.ErrNotFound):
		return model.Restaurant{}, &Error{Code: CodeNotFound}
	case dbErr != nil:
		return model.Restaurant{}, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr}
	}
	return restaurant, nil
}

// writeDetail answers with the full restaurant, so a create or update returns
// the same shape a GET does.
func (h *RestaurantHandlers) writeDetail(w http.ResponseWriter, r *http.Request, restaurant model.Restaurant) {
	contacts, err := db.ListContacts(r.Context(), h.Pool, restaurant.ID)
	if err != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}
	periods, err := db.ListOpeningHours(r.Context(), h.Pool, restaurant.ID)
	if err != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	body := restaurantDetailBody{
		restaurantBody: publicRestaurant(restaurant),
		Contacts:       make([]contactBody, 0, len(contacts)),
		OpeningHours:   make([]openingPeriodBody, 0, len(periods)),
	}
	for i := range contacts {
		body.Contacts = append(body.Contacts, publicContact(contacts[i]))
	}
	for i := range periods {
		body.OpeningHours = append(body.OpeningHours, publicOpeningPeriod(periods[i]))
	}

	status := http.StatusOK
	if r.Method == http.MethodPost {
		status = http.StatusCreated
	}
	_ = WriteJSON(w, status, body)
}

func validateRestaurantName(name string) *Error {
	if name == "" {
		return &Error{Code: CodeMissingField, Field: "name"}
	}
	if utf8.RuneCountInString(name) > MaxRestaurantNameLength {
		return &Error{
			Code:   CodeInvalidField,
			Field:  "name",
			Detail: "the name may be at most 200 characters",
		}
	}
	return nil
}

// validateCurrency checks the code against the seeded table.
//
// Answering 1013 rather than letting the foreign key fail: the documented code
// names the problem, and a constraint violation surfacing as 9001 would tell
// the caller only that something went wrong on the server.
func (h *RestaurantHandlers) validateCurrency(r *http.Request, code string) *Error {
	if code == "" {
		return &Error{Code: CodeMissingField, Field: "currency_code"}
	}

	known, err := db.CurrencyExists(r.Context(), h.Pool, code)
	if err != nil {
		return &Error{Code: CodeDatabaseUnavailable, Cause: err}
	}
	if !known {
		return &Error{Code: CodeUnknownCurrency, Field: "currency_code"}
	}
	return nil
}

// validateMoney refuses a negative amount, matching the CHECK constraints.
func validateMoney(cents *int64, field string) *Error {
	if cents != nil && *cents < 0 {
		return &Error{
			Code:   CodeInvalidField,
			Field:  field,
			Detail: "the amount may not be negative",
		}
	}
	return nil
}

// parseOptionalUUID reads a nullable id field.
func parseOptionalUUID(value *string, field string) (*uuid.UUID, *Error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(*value))
	if err != nil {
		return nil, &Error{
			Code:   CodeInvalidField,
			Field:  field,
			Detail: "this is not a valid identifier",
		}
	}
	return &id, nil
}
