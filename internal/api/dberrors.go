package api

import (
	"errors"

	"github.com/joernott/doenerstag/internal/db"
)

// dbError is the standard translation of a failure from the db package.
//
// The row asked for is missing: 4000. A row the request named does not exist
// -- an image, a currency, a category -- which is the caller's mistake and not
// the server's: 1002. Anything else: the database is unavailable. A handler
// with a case of its own, a name already taken say, writes that case first and
// leaves the rest to this, so the one case that differs is the one a reader
// sees.
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
