package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/joernott/doenerstag/internal/auth"
	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/mail"
	"github.com/joernott/doenerstag/internal/model"
)

/*
Password reset.

Two endpoints and one rule that shapes both of them: nothing here may reveal
whether an account exists.

POST /auth/password-reset answers the same way whether the name it was given
belongs to somebody or not, takes the same time either way, and says only that a
mail has been sent if there was an address to send it to. The alternative -- a
helpful "no such user" -- turns this endpoint into a way to enumerate every
account on the installation, and an intranet tool's account list is a staff list.

POST /auth/password-reset/{token} sets the password. The token is signed rather
than stored, for the reasons in internal/auth/reset.go; what is stored, here in
memory, is which tokens have already been used, so that a link works once.
*/

// ResetHandlers serve the password reset endpoints.
type ResetHandlers struct {
	Pool   *pgxpool.Pool
	Signer *auth.ResetSigner

	// Sender delivers the message. Never nil: an installation with no mail
	// server gets one that refuses, which is a supported way to run this.
	Sender mail.Sender

	// BaseURL is the address a link in a message points at.
	BaseURL string

	// Used remembers the tokens that have been spent.
	Used *UsedResets

	Logger *zerolog.Logger

	Now func() time.Time
}

func (h *ResetHandlers) now() time.Time {
	return nowFrom(h.Now)
}

// Register adds the reset routes.
func (h *ResetHandlers) Register(r *Router) {
	r.HandleFunc(http.MethodPost, "/auth/password-reset", h.request)
	r.HandleFunc(http.MethodPost, "/auth/password-reset/:token", h.complete)
}

// UsedResets remembers which reset tokens have been spent.
//
// In memory, and deliberately: it holds nothing worth keeping across a restart
// and nothing anybody would want to read. Entries are kept only until the token
// they name would have expired anyway, so the map cannot grow without bound
// however many links are issued.
//
// A restart forgets it, which means a link could be used a second time within
// its hour. The alternative is a table, an index and a cleanup job for a fact
// that stops being interesting after sixty minutes.
type UsedResets struct {
	mu    sync.Mutex
	spent map[string]time.Time
}

// NewUsedResets returns an empty store.
func NewUsedResets() *UsedResets {
	return &UsedResets{spent: map[string]time.Time{}}
}

// Spend records a token and reports whether it was already used.
func (u *UsedResets) Spend(id string, now time.Time) (already bool) {
	u.mu.Lock()
	defer u.mu.Unlock()

	// Swept here rather than by a goroutine: this is the only thing that adds
	// to the map, so it is the only place it can grow, and a background sweeper
	// would be a goroutine to start, stop and test for no gain.
	for key, expires := range u.spent {
		if !now.Before(expires) {
			delete(u.spent, key)
		}
	}

	if _, ok := u.spent[id]; ok {
		return true
	}
	u.spent[id] = now.Add(auth.ResetLifetime)
	return false
}

// resetRequest is the body of POST /auth/password-reset.
type resetRequest struct {
	// Name is the login name or the e-mail address. One field rather than two,
	// because somebody who has forgotten their password should not also have to
	// remember which of the two they registered with.
	Name string `json:"name"`
}

// request issues a reset link and mails it.
func (h *ResetHandlers) request(w http.ResponseWriter, r *http.Request) {
	var body resetRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		WriteError(w, r, &Error{Code: CodeMissingField, Field: "name",
			Detail: "give the user name or the e-mail address of the account"})
		return
	}

	// Everything past this point answers the same way. The work is still done,
	// and the failures are still logged, but the response does not vary.
	users := h.lookupAccounts(r.Context(), name)
	for i := range users {
		h.issue(r.Context(), r, users[i])
	}
	if len(users) == 0 {
		h.log(r).Info().Str("asked_for", name).
			Msg("a password reset was asked for an account that does not exist")
	}

	_ = WriteJSON(w, http.StatusAccepted, map[string]string{
		"status": "accepted",
	})
}

// lookupAccounts finds the accounts a reset was asked for: the one with that name, or
// else every one with that e-mail address.
//
// An address may be shared on purpose -- an administrator account and a
// personal one with the same mailbox -- so every account that has it gets a
// link of its own, and each mail says which account its link is for (20.5).
func (h *ResetHandlers) lookupAccounts(ctx context.Context, name string) []model.User {
	user, err := db.UserByName(ctx, h.Pool, name)
	if err == nil {
		return []model.User{user}
	}
	if !errors.Is(err, db.ErrNotFound) {
		h.log(nil).Error().Err(err).Msg("looking up an account for a password reset")
		return nil
	}

	users, err := db.UsersByEmail(ctx, h.Pool, name)
	if err != nil {
		h.log(nil).Error().Err(err).Msg("looking up an account for a password reset")
		return nil
	}
	return users
}

// issue mints a token and sends the mail.
func (h *ResetHandlers) issue(ctx context.Context, r *http.Request, user model.User) {
	logger := h.log(r)

	if user.Email == "" {
		// Nothing to do, and nothing the caller may be told: saying "that
		// account has no address" is saying the account exists.
		logger.Info().Str("user", user.Name).
			Msg("a password reset was asked for an account with no e-mail address")
		return
	}

	token := h.Signer.IssueReset(user.ID, h.now())
	link := auth.ResetLink(h.BaseURL, token)

	// The link is not logged. A log line carrying a working reset link is a
	// credential in the log, readable by anybody who can read logs -- which
	// includes the operator this feature exists to be independent of.
	if err := h.Sender.Send(ctx, mail.Message{
		To:      user.Email,
		Subject: auth.ResetMailSubject,
		Body:    auth.ResetMailBody(user, link),
	}); err != nil {
		logger.Error().Err(err).Str("user", user.Name).
			Msg("sending a password reset message")
		return
	}

	logger.Info().Str("user", user.Name).Msg("a password reset link was sent")
}

// completeRequest is the body of POST /auth/password-reset/{token}.
type completeRequest struct {
	Password string `json:"password"`
}

// complete sets the new password.
func (h *ResetHandlers) complete(w http.ResponseWriter, r *http.Request) {
	token := Param(r, "token")

	var body completeRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	userID, id, err := h.Signer.ParseReset(token, h.now())
	switch {
	case errors.Is(err, auth.ErrResetExpired):
		WriteError(w, r, &Error{Code: CodeResetExpired})
		return
	case err != nil:
		WriteError(w, r, &Error{Code: CodeResetInvalid})
		return
	}

	// The password is checked before the token is spent. A link burnt by a
	// password that was rejected for being too short would be a link somebody
	// has to ask for again, having done nothing wrong.
	if err := auth.ValidateComplexity(body.Password); err != nil {
		WriteError(w, r, &Error{
			Code:   CodePasswordTooWeak,
			Field:  "password",
			Detail: err.Error(),
		})
		return
	}

	if h.Used.Spend(id, h.now()) {
		WriteError(w, r, &Error{Code: CodeResetInvalid})
		return
	}

	hash, err := auth.Hash(body.Password)
	if err != nil {
		WriteError(w, r, &Error{Code: CodeInternal, Cause: err})
		return
	}

	user, err := db.UpdateUser(r.Context(), h.Pool, userID, userID, db.UserUpdate{PasswordHash: &hash})
	if errors.Is(err, db.ErrNotFound) {
		// The account went away between the link being issued and used.
		WriteError(w, r, &Error{Code: CodeResetInvalid})
		return
	}
	if err != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	// Every session of that account ends. Somebody resetting a password may be
	// doing it because somebody else has been using theirs, and leaving the
	// other browser logged in would defeat the whole exercise.
	if err := db.DeleteSessionsForUser(r.Context(), h.Pool, userID); err != nil {
		h.log(r).Error().Err(err).Str("user", user.Name).
			Msg("ending the sessions of an account whose password was reset")
	}

	h.log(r).Info().Str("user", user.Name).Msg("a password was reset")
	_ = WriteJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}

func (h *ResetHandlers) log(r *http.Request) *zerolog.Logger {
	return requestLogger(h.Logger, r)
}
