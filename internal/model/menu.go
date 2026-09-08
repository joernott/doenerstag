package model

import (
	"time"

	"github.com/google/uuid"
)

// MenuCategory groups a restaurant's items.
//
// Categories carry a sort order because their sequence is a genuine editorial
// choice -- starters before mains. Menu items deliberately do not; see
// MenuItem.
type MenuCategory struct {
	ID           uuid.UUID
	RestaurantID uuid.UUID
	Name         string
	SortOrder    int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// MenuItem is one orderable thing.
//
// There is no sort order here on purpose. F4.6 orders items by the
// restaurant's own item number and then by name, which is how the printed menu
// is laid out, so a manual ordering column would have nothing to do.
type MenuItem struct {
	ID           uuid.UUID
	RestaurantID uuid.UUID

	// CategoryID is nil for an item in no category, which is the shape a
	// restaurant with an unstructured menu takes.
	CategoryID *uuid.UUID

	// ExternalID is the restaurant's own item number, "12" or "A3". Empty when
	// the restaurant does not number its menu.
	ExternalID string

	Name        string
	Description string
	ImageID     *uuid.UUID
	PriceCents  int64

	// Available false means temporarily sold out: shown, but not orderable.
	// It is not a soft delete, and the two are answered differently -- an
	// unavailable item still appears on the menu.
	Available bool

	CreatedAt time.Time
	UpdatedAt time.Time

	// Tags, Allergens and Additives are filled in by the queries that ask for
	// them. A list of items leaves them empty rather than issuing a query per
	// row.
	Tags      []Tag
	Allergens []Allergen
	Additives []Additive

	// Modifications are filled in for a single item read, not for a list.
	Modifications []Modification
}

// Modification is a predefined, clickable change to an item.
//
// A flat list, freely combinable, rendered as checkboxes: no groups, no
// required options, no mutually exclusive sets. See
// docs/adr/0007-flat-modification-list.md. Free-text modifications live in
// order_item.note instead.
type Modification struct {
	ID              uuid.UUID
	MenuItemID      uuid.UUID
	Name            string
	PriceDeltaCents int64
	SortOrder       int
}

// Tag is a free descriptive label.
//
// Unlike the other reference tables, users may add rows at runtime. A seeded
// tag is named by the frontend's i18n catalog from its code; a user-created one
// carries the name they typed, because no catalog entry exists for it.
type Tag struct {
	ID        uuid.UUID
	Code      string
	Name      string
	SortOrder int
}

// Allergen is one of the fourteen EU Annex II allergens.
//
// Seeded and read-only. Reference is the Annex II item number.
type Allergen struct {
	ID        uuid.UUID
	Code      string
	Reference string
	SortOrder int
}

// Additive is a declarable additive.
//
// Reference is the conventional German menu number, a display hint rather than
// an identifier: the numbering varies between restaurants.
type Additive struct {
	ID        uuid.UUID
	Code      string
	Reference string
	SortOrder int
}
