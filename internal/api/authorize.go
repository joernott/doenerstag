package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
)

// The authorization helpers from the permission matrix in
// docs/05_auth_and_permissions.md.
//
// Each returns an *Error or nil, rather than writing a response, so a handler
// composes them and writes once. They are deliberately small and deliberately
// separate: "authenticated", "owner", "creator", "participant" and "admin" are
// five different questions with five different answers, and a single
// can-this-user-do-this function taking a string would hide which one was
// being asked.
//
// Anonymous is always 2000 rather than 3000. Telling somebody who is not
// logged in that they lack permission invites them to ask for permission, when
// what they need to do is log in.

// RequireAuthenticated is the base case: any logged-in user.
func RequireAuthenticated(r *http.Request) (*Principal, *Error) {
	principal := PrincipalFrom(r.Context())
	if principal == nil {
		return nil, &Error{Code: CodeNotAuthenticated}
	}
	return principal, nil
}

// RequireAdmin allows only root.
//
// docs/05_auth_and_permissions.md is emphatic that exactly one administrator
// exists and no other account can be granted is_admin, so this is a check on
// one row's flag rather than a role system.
func RequireAdmin(r *http.Request) (*Principal, *Error) {
	principal, err := RequireAuthenticated(r)
	if err != nil {
		return nil, err
	}
	if !principal.IsAdmin() {
		return nil, &Error{Code: CodeAdminRequired}
	}
	return principal, nil
}

// RequireOwner allows the named user, or the administrator.
//
// "Wherever the owner column is ticked, the administrator can act as well" is
// the matrix's own rule, so it is applied here once rather than remembered at
// each call site.
func RequireOwner(r *http.Request, ownerID uuid.UUID) (*Principal, *Error) {
	principal, err := RequireAuthenticated(r)
	if err != nil {
		return nil, err
	}
	if !principal.Is(ownerID) && !principal.IsAdmin() {
		// 3002 is the item-owner code and reads oddly for a profile, but the
		// documented set has no general "not yours" number and inventing one
		// would break the promise that a code's meaning never changes. This is
		// the closest documented fit.
		return nil, &Error{Code: CodeNotItemOwner}
	}
	return principal, nil
}

// RequireCreator allows the creator of an order, or the administrator.
func RequireCreator(r *http.Request, creatorID uuid.UUID) (*Principal, *Error) {
	principal, err := RequireAuthenticated(r)
	if err != nil {
		return nil, err
	}
	if !principal.Is(creatorID) && !principal.IsAdmin() {
		return nil, &Error{Code: CodeNotOrderCreator}
	}
	return principal, nil
}

// RequireParticipant allows the creator of an order, anybody holding an item in
// it, or the administrator.
//
// Participation is derived, never stored: adding an item makes you a
// participant immediately, and removing your last one stops you being one. The
// query is the definition, which is why this helper takes a database handle
// where the others do not.
func RequireParticipant(
	ctx context.Context, r *http.Request, q db.Querier, orderID uuid.UUID,
) (*Principal, *Error) {
	principal, err := RequireAuthenticated(r)
	if err != nil {
		return nil, err
	}
	if principal.IsAdmin() {
		return principal, nil
	}

	participant, dbErr := db.IsParticipant(ctx, q, orderID, principal.User.ID)
	switch {
	case errors.Is(dbErr, db.ErrNotFound):
		return nil, &Error{Code: CodeNotFound}
	case dbErr != nil:
		return nil, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr}
	case !participant:
		return nil, &Error{Code: CodeNotParticipant}
	}
	return principal, nil
}

// RequireNotPlaceholder refuses any modification of the deleted-user row.
//
// The placeholder owns the order items of deleted accounts. It has no password
// that can verify, so it cannot be reached by logging in -- but an
// administrator can address it by id, and editing or renaming it would corrupt
// the history it exists to preserve.
func RequireNotPlaceholder(id uuid.UUID) *Error {
	if id == model.DeletedUserID {
		return &Error{Code: CodePlaceholderReadOnly}
	}
	return nil
}

// LookupUser resolves a path parameter to an account.
//
// A malformed id and an id that does not exist both answer 4000. They are the
// same fact from the caller's side -- there is no such user -- and telling them
// apart would say which identifiers are well-formed, which is a small hint
// worth not giving.
func LookupUser(r *http.Request, pool *pgxpool.Pool, param string) (model.User, *Error) {
	id, err := uuid.Parse(Param(r, param))
	if err != nil {
		return model.User{}, &Error{Code: CodeNotFound}
	}

	user, dbErr := db.UserByID(r.Context(), pool, id)
	switch {
	case errors.Is(dbErr, db.ErrNotFound):
		return model.User{}, &Error{Code: CodeNotFound}
	case dbErr != nil:
		return model.User{}, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr}
	}
	return user, nil
}
