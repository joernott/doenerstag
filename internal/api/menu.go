package api

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
)

// Field limits, matching the CHECK constraints in migration 000006.
const (
	MaxMenuNameLength        = 200
	MaxExternalIDLength      = 32
	MaxMenuDescriptionLength = 2000
	MaxTagCodeLength         = 64
	MaxTagNameLength         = 64
)

// externalIDShape matches the CHECK constraint on menu_item.external_id.
//
// Checked here as well so a bad value names its field, rather than arriving as
// a constraint violation the caller cannot act on.
var externalIDShape = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)

// tagCodeShape matches the CHECK constraint on tag.code.
var tagCodeShape = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)

// MenuHandlers serves categories, items, modifications and classification.
type MenuHandlers struct {
	Pool *pgxpool.Pool
}

// Register adds the menu routes.
//
// Creating and editing are open to any logged-in user, deleting to the
// administrator only -- the same asymmetry as restaurants, and for the same
// reason: adding menu data has to be trivial, and losing a menu to a misclick
// must not be.
func (h *MenuHandlers) Register(r *Router) {
	r.HandleFunc(http.MethodGet, "/restaurants/:id/categories", h.listCategories)
	r.HandleFunc(http.MethodPost, "/restaurants/:id/categories", h.createCategory)
	r.HandleFunc(http.MethodPatch, "/restaurants/:id/categories/:cid", h.patchCategory)
	r.HandleFunc(http.MethodDelete, "/restaurants/:id/categories/:cid", h.deleteCategory)

	r.HandleFunc(http.MethodGet, "/restaurants/:id/menu-items", h.listItems)
	r.HandleFunc(http.MethodPost, "/restaurants/:id/menu-items", h.createItem)
	r.HandleFunc(http.MethodGet, "/restaurants/:id/menu-items/:mid", h.getItem)
	r.HandleFunc(http.MethodPatch, "/restaurants/:id/menu-items/:mid", h.patchItem)
	r.HandleFunc(http.MethodDelete, "/restaurants/:id/menu-items/:mid", h.deleteItem)

	r.HandleFunc(http.MethodGet, "/restaurants/:id/menu-items/:mid/modifications", h.listModifications)
	r.HandleFunc(http.MethodPost, "/restaurants/:id/menu-items/:mid/modifications", h.createModification)
	r.HandleFunc(http.MethodPatch, "/restaurants/:id/menu-items/:mid/modifications/:k", h.patchModification)
	r.HandleFunc(http.MethodDelete, "/restaurants/:id/menu-items/:mid/modifications/:k", h.deleteModification)

	r.HandleFunc(http.MethodGet, "/tags", h.listTags)
	r.HandleFunc(http.MethodPost, "/tags", h.createTag)
	r.HandleFunc(http.MethodGet, "/allergens", h.listAllergens)
	r.HandleFunc(http.MethodGet, "/additives", h.listAdditives)
}

type categoryBody struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
}

func publicCategory(c model.MenuCategory) categoryBody {
	return categoryBody{ID: c.ID.String(), Name: c.Name, SortOrder: c.SortOrder}
}

type menuItemBody struct {
	ID           string  `json:"id"`
	RestaurantID string  `json:"restaurant_id"`
	CategoryID   *string `json:"category_id"`
	ExternalID   string  `json:"external_id"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	ImageID      *string `json:"image_id"`
	PriceCents   int64   `json:"price_cents"`
	Available    bool    `json:"available"`

	Tags      []tagBody       `json:"tags"`
	Allergens []referenceBody `json:"allergens"`
	Additives []referenceBody `json:"additives"`

	// Modifications is present only on a single-item read. Omitted from a list,
	// where it would multiply the response for data nothing on a list page
	// shows.
	Modifications []modificationBody `json:"modifications,omitempty"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type tagBody struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	// Name is meaningful only for user-created tags. The frontend translates a
	// seeded tag from its code and ignores this.
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
}

// referenceBody is the shape of an allergen or an additive: a code, a
// reference number, and no display name. See docs/07_i18n.md.
type referenceBody struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	Reference string `json:"reference"`
	SortOrder int    `json:"sort_order"`
}

type modificationBody struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	PriceDeltaCents int64  `json:"price_delta_cents"`
	SortOrder       int    `json:"sort_order"`
}

func publicModification(m model.Modification) modificationBody {
	return modificationBody{
		ID:              m.ID.String(),
		Name:            m.Name,
		PriceDeltaCents: m.PriceDeltaCents,
		SortOrder:       m.SortOrder,
	}
}

func publicMenuItem(m model.MenuItem) menuItemBody {
	body := menuItemBody{
		ID:           m.ID.String(),
		RestaurantID: m.RestaurantID.String(),
		ExternalID:   m.ExternalID,
		Name:         m.Name,
		Description:  m.Description,
		PriceCents:   m.PriceCents,
		Available:    m.Available,
		Tags:         make([]tagBody, 0, len(m.Tags)),
		Allergens:    make([]referenceBody, 0, len(m.Allergens)),
		Additives:    make([]referenceBody, 0, len(m.Additives)),
		CreatedAt:    m.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    m.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if m.CategoryID != nil {
		id := m.CategoryID.String()
		body.CategoryID = &id
	}
	if m.ImageID != nil {
		id := m.ImageID.String()
		body.ImageID = &id
	}

	for _, t := range m.Tags {
		body.Tags = append(body.Tags, tagBody{
			ID: t.ID.String(), Code: t.Code, Name: t.Name, SortOrder: t.SortOrder,
		})
	}
	for _, a := range m.Allergens {
		body.Allergens = append(body.Allergens, referenceBody{
			ID: a.ID.String(), Code: a.Code, Reference: a.Reference, SortOrder: a.SortOrder,
		})
	}
	for _, a := range m.Additives {
		body.Additives = append(body.Additives, referenceBody{
			ID: a.ID.String(), Code: a.Code, Reference: a.Reference, SortOrder: a.SortOrder,
		})
	}
	for _, mod := range m.Modifications {
		body.Modifications = append(body.Modifications, publicModification(mod))
	}
	return body
}

// restaurantID resolves the :id parameter, checking the restaurant lives.
//
// Every menu route hangs off a restaurant, so an item under a deleted or
// non-existent restaurant is a 404 at the path rather than an empty list.
func (h *MenuHandlers) restaurantID(r *http.Request) (uuid.UUID, *Error) {
	id, err := uuid.Parse(Param(r, "id"))
	if err != nil {
		return uuid.Nil, &Error{Code: CodeNotFound}
	}

	exists, dbErr := db.RestaurantExists(r.Context(), h.Pool, id)
	if dbErr != nil {
		return uuid.Nil, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr}
	}
	if !exists {
		return uuid.Nil, &Error{Code: CodeNotFound}
	}
	return id, nil
}

func (h *MenuHandlers) listCategories(w http.ResponseWriter, r *http.Request) {
	restaurantID, err := h.restaurantID(r)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	categories, dbErr := db.ListCategories(r.Context(), h.Pool, restaurantID)
	if dbErr != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	bodies := make([]categoryBody, 0, len(categories))
	for i := range categories {
		bodies = append(bodies, publicCategory(categories[i]))
	}
	_ = WriteJSON(w, http.StatusOK, map[string]any{"categories": bodies})
}

type categoryRequest struct {
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
}

func (h *MenuHandlers) createCategory(w http.ResponseWriter, r *http.Request) {
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

	var body categoryRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	name := strings.TrimSpace(body.Name)
	if err := validateMenuName(name); err != nil {
		WriteError(w, r, err)
		return
	}

	created, dbErr := db.CreateCategory(r.Context(), h.Pool,
		restaurantID, name, body.SortOrder, principal.User.ID)
	switch {
	case errors.Is(dbErr, db.ErrNameTaken):
		WriteError(w, r, &Error{Code: CodeNameExistsHere, Field: "name"})
		return
	case dbErr != nil:
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	_ = WriteJSON(w, http.StatusCreated, publicCategory(created))
}

type patchCategoryRequest struct {
	Name      *string `json:"name"`
	SortOrder *int    `json:"sort_order"`
}

func (h *MenuHandlers) patchCategory(w http.ResponseWriter, r *http.Request) {
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

	category, lookupErr := h.category(r, restaurantID)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}

	var body patchCategoryRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	var update db.CategoryUpdate
	if body.Name != nil {
		name := strings.TrimSpace(*body.Name)
		if err := validateMenuName(name); err != nil {
			WriteError(w, r, err)
			return
		}
		update.Name = &name
	}
	update.SortOrder = body.SortOrder

	updated, dbErr := db.UpdateCategory(r.Context(), h.Pool,
		category.ID, principal.User.ID, update)
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

	_ = WriteJSON(w, http.StatusOK, publicCategory(updated))
}

// deleteCategory soft-deletes a category. Administrator only (F4.5).
func (h *MenuHandlers) deleteCategory(w http.ResponseWriter, r *http.Request) {
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

	category, lookupErr := h.category(r, restaurantID)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}

	if err := db.SoftDeleteCategory(r.Context(), h.Pool, category.ID, principal.User.ID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			WriteError(w, r, &Error{Code: CodeNotFound})
			return
		}
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// category resolves :cid and checks it belongs to this restaurant.
func (h *MenuHandlers) category(r *http.Request, restaurantID uuid.UUID) (model.MenuCategory, *Error) {
	id, err := uuid.Parse(Param(r, "cid"))
	if err != nil {
		return model.MenuCategory{}, &Error{Code: CodeNotFound}
	}

	category, dbErr := db.CategoryByID(r.Context(), h.Pool, id)
	switch {
	case errors.Is(dbErr, db.ErrNotFound):
		return model.MenuCategory{}, &Error{Code: CodeNotFound}
	case dbErr != nil:
		return model.MenuCategory{}, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr}
	}
	if category.RestaurantID != restaurantID {
		return model.MenuCategory{}, &Error{Code: CodeNotFound}
	}
	return category, nil
}

func validateMenuName(name string) *Error {
	if name == "" {
		return &Error{Code: CodeMissingField, Field: "name"}
	}
	if utf8.RuneCountInString(name) > MaxMenuNameLength {
		return &Error{
			Code:   CodeInvalidField,
			Field:  "name",
			Detail: "the name may be at most 200 characters",
		}
	}
	return nil
}
