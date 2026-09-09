// Package transfer carries restaurants between installations as a file.
//
// The shape of the document is the whole design here, and two decisions in it
// are worth stating before the types.
//
// **Reference data travels by code, never by id.** Tags, allergens, additives,
// currencies and contact types are seeded per installation, so their uuids
// differ between one server and the next. A file naming tag
// 018f0000-...-0001 would import cleanly onto the machine it came from and
// attach the wrong tags, or none, anywhere else. Codes -- "vegan", "A", "1",
// "EUR", "phone" -- mean the same thing everywhere, and an unknown one is worth
// refusing loudly.
//
// **Everything else travels by id.** The restaurant, its categories and its
// items keep the uuids they had, which is what makes "a restaurant with this id
// already exists" a question worth asking, and what lets an item point at its
// category without inventing a second naming scheme.
//
// Images are not carried. The logo and the item pictures are rows in the image
// table holding binary, and putting them in a YAML file would multiply its size
// by a hundred to move something a person can upload again in a minute. An
// imported restaurant has no logo, and 09_configuration.md says so.
package transfer

// Version is the document format this package writes.
//
// Present so that a file written today can be recognised, and refused with a
// sentence rather than a type error, by a version of this that has moved on.
const Version = 1

// Document is a file: one or more restaurants.
//
// Always a list, even for one restaurant, so that a file exported with --all
// and a file exported with a single -i have the same shape and the same reader.
type Document struct {
	Version     int          `json:"version" yaml:"version"`
	Restaurants []Restaurant `json:"restaurants" yaml:"restaurants"`
}

// Restaurant is everything about one restaurant that survives the journey.
type Restaurant struct {
	ID   string `json:"id" yaml:"id"`
	Name string `json:"name" yaml:"name"`

	// Currency is the ISO 4217 code, not the row id.
	Currency string `json:"currency" yaml:"currency"`

	// MinOrderValueCents and DeliveryFeeCents are pointers because nil and zero
	// differ: "no minimum" and "a minimum of nothing" are not the same, and an
	// order copies whichever it finds.
	MinOrderValueCents *int64 `json:"min_order_value_cents,omitempty" yaml:"min_order_value_cents,omitempty"`
	DeliveryFeeCents   *int64 `json:"delivery_fee_cents,omitempty" yaml:"delivery_fee_cents,omitempty"`

	Notes string `json:"notes,omitempty" yaml:"notes,omitempty"`

	Contacts     []Contact      `json:"contacts,omitempty" yaml:"contacts,omitempty"`
	OpeningHours []OpeningHours `json:"opening_hours,omitempty" yaml:"opening_hours,omitempty"`
	Categories   []Category     `json:"categories,omitempty" yaml:"categories,omitempty"`
	Items        []Item         `json:"items,omitempty" yaml:"items,omitempty"`
}

// Contact is one way of reaching the restaurant.
type Contact struct {
	// Type is the contact type code -- "phone", "email", "website" -- not its
	// row id.
	Type      string `json:"type" yaml:"type"`
	Value     string `json:"value" yaml:"value"`
	Label     string `json:"label,omitempty" yaml:"label,omitempty"`
	SortOrder int    `json:"sort_order" yaml:"sort_order"`
}

// OpeningHours is one period on one day.
type OpeningHours struct {
	// Day is ISO-8601: 1 is Monday, 7 is Sunday.
	Day int `json:"day" yaml:"day"`

	// Start and End are wall-clock "15:04". End before Start crosses midnight.
	Start string `json:"start" yaml:"start"`
	End   string `json:"end" yaml:"end"`
}

// Category is a heading on the menu.
type Category struct {
	ID        string `json:"id" yaml:"id"`
	Name      string `json:"name" yaml:"name"`
	SortOrder int    `json:"sort_order" yaml:"sort_order"`
}

// Item is one thing that can be ordered.
type Item struct {
	ID string `json:"id" yaml:"id"`

	// Category is the id of a category in the same document, or empty for an
	// item under no heading.
	Category string `json:"category,omitempty" yaml:"category,omitempty"`

	// ExternalID is the restaurant's own item number, "12" or "A3".
	ExternalID  string `json:"external_id,omitempty" yaml:"external_id,omitempty"`
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	PriceCents  int64  `json:"price_cents" yaml:"price_cents"`

	// Available false is temporarily sold out, which is not a deletion and
	// travels because it is part of what the menu currently says.
	Available bool `json:"available" yaml:"available"`

	// Tags, Allergens and Additives are codes.
	Tags      []string `json:"tags,omitempty" yaml:"tags,omitempty"`
	Allergens []string `json:"allergens,omitempty" yaml:"allergens,omitempty"`
	Additives []string `json:"additives,omitempty" yaml:"additives,omitempty"`

	Modifications []Modification `json:"modifications,omitempty" yaml:"modifications,omitempty"`
}

// Modification is an option on an item: extra cheese, no onions.
type Modification struct {
	Name            string `json:"name" yaml:"name"`
	PriceDeltaCents int64  `json:"price_delta_cents" yaml:"price_delta_cents"`
	SortOrder       int    `json:"sort_order" yaml:"sort_order"`
}
