package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/joernott/doenerstag/internal/db"
)

// dbError is the standard translation of a failure from the db package.
//
// The row asked for is missing: 4000. A row the request named does not exist
// -- an image, a currency, a category -- which is the caller's mistake and not
// the server's: 1002. An order's restaurant changed after it has items: 4002.
// Anything else: the database is unavailable. A handler with a case of its
// own, a name already taken say, writes that case first and leaves the rest
// to this, so the one case that differs is the one a reader sees.
func dbError(err error) *Error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, db.ErrNotFound):
		return &Error{Code: CodeNotFound}
	case errors.Is(err, db.ErrUnknownReference):
		return &Error{Code: CodeInvalidField, Detail: "a referenced record does not exist"}
	case errors.Is(err, db.ErrRestaurantLocked):
		return &Error{Code: CodeRestaurantLocked}
	default:
		return &Error{Code: CodeDatabaseUnavailable, Cause: err}
	}
}

// loadParam reads the row a path parameter names.
//
// An id that does not parse and an id nothing has are the same answer, 4000:
// "not a UUID" tells a caller nothing they can use, and is one more way of
// telling which ids exist. Every db.XByID has this signature, so a lookup is
// one line plus whatever check its parent needs -- that a menu item is this
// restaurant's, say -- which is the line worth reading.
func loadParam[T any](
	r *http.Request, q db.Querier, param string,
	load func(context.Context, db.Querier, uuid.UUID) (T, error),
) (T, *Error) {
	var zero T
	id, err := uuid.Parse(Param(r, param))
	if err != nil {
		return zero, &Error{Code: CodeNotFound}
	}
	row, dbErr := load(r.Context(), q, id)
	if dbErr != nil {
		return zero, dbError(dbErr)
	}
	return row, nil
}
