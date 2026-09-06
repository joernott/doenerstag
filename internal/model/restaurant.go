package model

import (
	"time"

	"github.com/google/uuid"
)

// Restaurant is a place orders can be placed at.
//
// Any logged-in user may add and edit one; only the administrator may delete
// one, so that a misclick cannot remove a restaurant's whole menu.
type Restaurant struct {
	ID           uuid.UUID
	Name         string
	LogoImageID  *uuid.UUID
	CurrencyCode string

	// MinOrderValueCents and DeliveryFeeCents are optional, and nil is not the
	// same as zero: "no minimum" and "a minimum of nothing" would render
	// differently, and an order copies whichever it finds.
	MinOrderValueCents *int64
	DeliveryFeeCents   *int64

	Notes     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Contact is one way of reaching a restaurant.
//
// The pickup address is a contact whose type renders as an address; there is no
// separate address column.
type Contact struct {
	ID            uuid.UUID
	RestaurantID  uuid.UUID
	ContactTypeID uuid.UUID
	// ContactTypeCode is denormalised into the response so a client does not
	// have to join against /contact-types to render one entry.
	ContactTypeCode string
	RenderAs        string
	Value           string
	Label           string
	SortOrder       int
}

// OpeningPeriod is one opening interval on one weekday.
//
// Several per weekday are allowed, which is how a lunch break is expressed: a
// restaurant open 11:00-14:00 and 17:00-22:00 on Monday has two rows.
type OpeningPeriod struct {
	ID           uuid.UUID
	RestaurantID uuid.UUID

	// DayOfWeek is ISO-8601: 1 is Monday, 7 is Sunday.
	DayOfWeek int

	// Start and End are local wall-clock times, formatted "15:04".
	//
	// End earlier than Start means the period crosses midnight, as a Friday
	// 22:00-02:00 does. Equal times are refused: a zero-length opening period
	// is a typo, not a restaurant that is never open.
	Start string
	End   string
}

// CrossesMidnight reports whether the period runs into the next day.
func (p OpeningPeriod) CrossesMidnight() bool { return p.End < p.Start }

// Currency is a seeded ISO 4217 entry.
//
// It carries no display name: seeded reference data is identified by a stable
// code and named by the frontend's i18n catalog. See docs/07_i18n.md.
type Currency struct {
	Code      string
	Symbol    string
	MinorUnit int
	SortOrder int
}

// ContactType is a seeded kind of contact entry.
type ContactType struct {
	ID   uuid.UUID
	Code string
	// RenderAs tells the frontend how to present the value: tel, mailto, url,
	// address or text.
	RenderAs  string
	SortOrder int
}

// Image is a stored picture and its thumbnail.
//
// The bytes live in the database rather than on disk, so that pg_dump plus the
// configuration file is the whole backup. See
// docs/adr/0008-images-in-the-database.md.
type Image struct {
	ID        uuid.UUID
	MediaType string
	Width     int
	Height    int
	ByteSize  int

	ThumbMediaType string
	ThumbWidth     int
	ThumbHeight    int

	// SHA256 is of the stored image, after downscaling and re-encoding. It
	// deduplicates uploads and serves as the ETag.
	SHA256           []byte
	OriginalFilename string
	CreatedAt        time.Time
}
