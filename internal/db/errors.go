package db

// The errors this package answers with, in one place, so a caller looking for
// what a function can return finds the whole vocabulary here (20.23). Each one
// means one thing; the api package maps them to responses in dbError.

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNotFound is returned when a row does not exist.
//
// One sentinel for the whole package rather than one per table: every caller
// turns it into the same 4000, and distinguishing "no such user" from "no such
// order" at this layer would only invite an error message that tells an
// anonymous caller which identifiers are real.
var ErrNotFound = errors.New("db: not found")

// ErrUnknownReference is returned when a write names a row that does not
// exist: a logo image, a currency, a category, a contact type, an account.
//
// Not ErrNotFound, which means the row being read or changed is missing. The
// two were one sentinel until 20.8, and a PATCH naming an unknown image was
// answered "404 restaurant not found" about a restaurant that was right there.
var ErrUnknownReference = errors.New("db: a referenced row does not exist")

// ErrNameTaken is returned when a unique name constraint rejects a write.
var ErrNameTaken = errors.New("db: name already taken")

// ErrStillReferenced is returned when a row cannot be removed because
// something else points at it.
var ErrStillReferenced = errors.New("db: still referenced")

// ErrLastContact is returned when removing a contact would leave a restaurant
// with none, which F3.1 forbids.
var ErrLastContact = errors.New("db: a restaurant needs at least one contact")

// ErrRestaurantLocked is returned when an order's restaurant is changed while
// the order has items (F5.8).
var ErrRestaurantLocked = errors.New("db: the order has items, so its restaurant is fixed")

// ErrWrongRestaurant is returned when a menu item does not belong to the
// order's restaurant.
var ErrWrongRestaurant = errors.New("db: menu item belongs to another restaurant")

// ErrItemUnavailable is returned when a menu item is marked sold out.
var ErrItemUnavailable = errors.New("db: menu item is unavailable")

// ErrItemNotServedThen is returned when a menu item exists and is not sold out,
// but its availability filters say the kitchen does not make it at the order's
// fulfilment time. A different problem with a different fix: come back on
// Friday, rather than ask whether they have any left.
var ErrItemNotServedThen = errors.New("db: menu item is not served at that time")

// ErrModificationWrongItem is returned when a selected modification does not
// belong to the menu item being ordered.
var ErrModificationWrongItem = errors.New("db: modification does not belong to this menu item")

// isUniqueViolation reports whether err is PostgreSQL's 23505.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// isForeignKeyViolation reports whether err is PostgreSQL's 23503.
//
// It means a referenced row does not exist -- an unknown currency code, or an
// image id that was never uploaded -- which the API answers as 4000 rather than
// as a server error.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
