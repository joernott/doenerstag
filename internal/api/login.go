package api

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/joernott/doenerstag/internal/auth"
	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
)

// loginRequest is the body of POST /auth/login.
type loginRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

// login verifies a password and starts a session.
//
// Every failure answers 2001, "invalid user name or password", whatever went
// wrong. Distinguishing "no such user" from "wrong password" would turn the
// login form into a way to enumerate who has an account, which on an intranet
// tool is a list of colleagues.
func (h *AuthHandlers) login(w http.ResponseWriter, r *http.Request) {
	var body loginRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" || body.Password == "" {
		// Still a 2001 rather than a validation error: an empty field is a
		// failed login attempt, and answering it differently would be another
		// way to learn something about the account.
		WriteError(w, r, &Error{Code: CodeInvalidLogin})
		return
	}

	user, hash, err := db.PasswordHashByName(r.Context(), h.Pool, name)
	switch {
	case errors.Is(err, db.ErrNotFound):
		// Verify against a hash that cannot match, so that a request for a
		// name that does not exist costs the same as one that does. Without
		// this, the response time alone answers "does this person have an
		// account", and Argon2id's whole point is that it is slow enough to
		// measure.
		equaliseLoginTiming()
		WriteError(w, r, &Error{Code: CodeInvalidLogin})
		return
	case err != nil:
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	if auth.IsUnusable(hash) {
		// The deleted-user placeholder, and anything else deliberately locked
		// out. Verify would reject it anyway; saying so here keeps the reason
		// visible rather than looking like a coincidence.
		WriteError(w, r, &Error{Code: CodeInvalidLogin})
		return
	}

	if err := auth.Verify(body.Password, hash); err != nil {
		WriteError(w, r, &Error{Code: CodeInvalidLogin, Cause: err})
		return
	}

	// A hash made with weaker parameters than the current ones is upgraded now,
	// while the plaintext is in hand. This is the only moment it can be done.
	h.rehashIfNeeded(r, user, hash, body.Password)

	session, err := h.startSession(r, w, user)
	if err != nil {
		WriteError(w, r, &Error{Code: CodeInternal, Cause: err})
		return
	}

	// Bookkeeping, after the session exists. A login that worked must not be
	// reported as a failure because a timestamp could not be written.
	if err := h.recordLogin(r.Context(), user); err != nil {
		h.log(r).Warn().Err(err).
			Str("user", user.Name).Msg("could not record the login time")
	}

	public := publicUser(user)
	_ = WriteJSON(w, http.StatusOK, sessionBody{
		User:      &public,
		ExpiresAt: session.AbsoluteExpires.UTC().Format(time.RFC3339),
	})
}

// rehashIfNeeded upgrades a stored hash to the current parameters.
//
// Failure is logged and ignored: the password was correct, so refusing the
// login because an optimisation could not be applied would be the wrong answer
// to a successful authentication.
func (h *AuthHandlers) rehashIfNeeded(r *http.Request, user model.User, stored, password string) {
	if !auth.NeedsRehash(stored) {
		return
	}

	upgraded, err := auth.HashWithoutValidation(password)
	if err != nil {
		h.log(r).Warn().Err(err).Msg("could not re-hash the password")
		return
	}
	if _, err := db.UpdateUser(r.Context(), h.Pool, user.ID, user.ID,
		db.UserUpdate{PasswordHash: &upgraded}); err != nil {
		h.log(r).Warn().Err(err).Msg("could not store the upgraded hash")
		return
	}
	h.log(r).Info().Str("user", user.Name).
		Msg("password hash upgraded to the current parameters")
}

// dummyHash is a real Argon2id hash of a value nothing will ever be, computed
// once. Verifying against it costs what verifying a genuine hash costs, which
// is the point.
var dummyHash = sync.OnceValue(func() string {
	hash, err := auth.HashWithoutValidation(
		"a value no account has, used only to spend the same time")
	if err != nil {
		// Nothing useful to do here, and returning an empty string is safe:
		// Verify rejects it immediately, which loses the timing equalisation
		// but never lets anybody in.
		return ""
	}
	return hash
})

// equaliseLoginTiming spends roughly what a real verification spends.
func equaliseLoginTiming() {
	_ = auth.Verify("any password at all", dummyHash())
}
