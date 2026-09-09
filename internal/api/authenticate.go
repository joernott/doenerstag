package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joernott/doenerstag/internal/auth"
	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
)

// TouchThrottle is how stale last_seen_at must be before a request writes it.
//
// docs/05_auth_and_permissions.md fixes it at 60 seconds. Without it, every
// request would be a write, which for a page that fetches four endpoints means
// four UPDATEs to record one glance at the screen.
const TouchThrottle = 60 * time.Second

// Principal is who is making a request.
//
// A request with no credentials has no principal, which is not an error: most
// read endpoints are public, and it is the authorization helpers that decide
// whether a given route tolerates an anonymous caller.
type Principal struct {
	User model.User

	// SessionID is the session row behind a cookie-authenticated request, and
	// the zero UUID for one authenticated by API token.
	SessionID uuid.UUID

	// ViaToken reports how the caller authenticated.
	//
	// CSRF protection turns on this: a Bearer token carries no ambient browser
	// credential, so there is nothing for a foreign site to abuse and the check
	// would only make scripted access harder for no gain.
	ViaToken bool
}

// IsAdmin reports whether the caller is the administrator.
//
// Read from the database row rather than from the token's adm claim, which is
// advisory. A token issued before an account changed would otherwise carry a
// stale answer to the most consequential question the API asks.
func (p *Principal) IsAdmin() bool { return p != nil && p.User.IsAdmin }

// Is reports whether the caller is the named user.
func (p *Principal) Is(id uuid.UUID) bool { return p != nil && p.User.ID == id }

const principalKey contextKey = 100

// PrincipalFrom returns the caller, or nil when the request is anonymous.
func PrincipalFrom(ctx context.Context) *Principal {
	principal, _ := ctx.Value(principalKey).(*Principal)
	return principal
}

// Authenticator validates credentials and attaches the caller to the request.
type Authenticator struct {
	Pool   *pgxpool.Pool
	Signer *auth.Signer

	// Secure controls the Secure attribute on the cookies this clears and
	// sets. False under --no-https only, matching AuthHandlers.
	Secure bool

	// IdleTimeout is how long a session may go untouched before it lapses.
	IdleTimeout time.Duration

	// Now is the clock, injectable for tests that need to age a session.
	Now func() time.Time
}

func (a *Authenticator) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

const sessionEndedKey contextKey = 101

// SessionEndedFrom returns the reason a request's session cookie was refused,
// or nil when there was no cookie or it worked.
//
// GET /auth/session is the one place that reads it: the frontend asks who it is
// on every page load, and that is the natural moment to say "your session ended
// and here is why" rather than leaving somebody staring at a logged-out page.
func SessionEndedFrom(ctx context.Context) *Error {
	ended, _ := ctx.Value(sessionEndedKey).(*Error)
	return ended
}

// Middleware identifies the caller, or leaves the request anonymous.
//
// Presenting no credential is anonymity. Presenting one that does not work
// depends on which credential it was, and the difference matters more than it
// looks:
//
// A Bearer token is explicit. A caller that names a token has asked for it to
// be used, and quietly downgrading them to anonymous would turn a clear 401
// into a confusing 403 or an empty list. That still fails.
//
// A session cookie is ambient. The browser attaches it to everything -- the
// page, the stylesheet, the script, every public endpoint -- without anyone
// choosing to. Refusing the request meant that one stale cookie made the whole
// application unreachable: navigating to / produced a JSON error envelope, and
// since the same cookie rode along on every subsequent request there was no way
// out of it short of clearing cookies by hand. So a cookie that does not work
// now means anonymous, and the reason is recorded for GET /auth/session to
// report.
//
// The intent behind the old behaviour was right -- somebody logged out by a
// session that lapsed overnight should be told, not silently downgraded -- and
// it is kept. It just cannot be the transport layer that says so, because the
// transport layer has no way to say it to a human.
func (a *Authenticator) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, err := a.authenticate(r)
			if err != nil {
				if r.Header.Get("Authorization") != "" {
					WriteError(w, r, err)
					return
				}
				// The dead cookie goes now, so the browser stops sending it, and
				// the reason is written down in its place. It has to be recorded
				// here rather than worked out again later: this request also
				// deletes the row of a session past its timeout, so a later
				// lookup would find nothing and conclude "superseded by a newer
				// login" -- confusing news for somebody whose session merely
				// lapsed overnight.
				clearSessionCookies(w, a.Secure)
				setSessionEndedCookie(w, err.Code, a.Secure)
				r = r.WithContext(context.WithValue(r.Context(), sessionEndedKey, err))
				next.ServeHTTP(w, r)
				return
			}

			if principal != nil {
				SetUserName(r.Context(), principal.User.Name)
				r = r.WithContext(context.WithValue(r.Context(), principalKey, principal))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// authenticate resolves the request's credentials.
//
// The Authorization header wins over the cookie. A request carrying both is
// unusual -- a script running in a browser that happens to have a session --
// and taking the explicit credential over the ambient one is the choice that
// cannot surprise: the caller asked for the token to be used.
func (a *Authenticator) authenticate(r *http.Request) (*Principal, *Error) {
	if header := r.Header.Get("Authorization"); header != "" {
		return a.fromBearer(r, header)
	}
	if raw := cookieValue(r, SessionCookieName); raw != "" {
		return a.fromSession(r, raw)
	}
	return nil, nil
}

// fromSession runs the five checks from docs/05_auth_and_permissions.md.
func (a *Authenticator) fromSession(r *http.Request, raw string) (*Principal, *Error) {
	// 1. Verify the signature and exp.
	claims, err := a.Signer.Parse(raw)
	switch {
	case errors.Is(err, auth.ErrTokenExpired):
		return nil, &Error{Code: CodeSessionExpired}
	case err != nil:
		// A token that does not verify is not a lapsed session; it is not one
		// of ours. 2000 rather than 2002, so the frontend does not tell
		// somebody their session timed out when they were never logged in.
		return nil, &Error{Code: CodeNotAuthenticated, Cause: err}
	}

	// 2. Load the session row. A missing row means the session was superseded
	//    by a newer login or ended by logging out.
	session, user, dbErr := db.SessionWithUser(r.Context(), a.Pool, claims.SessionID)
	switch {
	case errors.Is(dbErr, db.ErrNotFound):
		return nil, &Error{Code: CodeSessionSuperseded}
	case dbErr != nil:
		return nil, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr}
	}

	now := a.now()

	// 3. The absolute lifetime. exp should have caught this already; the row is
	//    checked as well because the two could disagree -- a token minted
	//    against a different absolute-timeout setting, say -- and the row is
	//    the authority.
	if session.Expired(now) {
		a.discard(r, session.ID)
		return nil, &Error{Code: CodeSessionExpired}
	}

	// 4. The idle timeout. The row goes with it: leaving it would let the same
	//    token keep answering 2002 forever instead of 2003, and would hold the
	//    user's one session slot against them.
	if session.Idle(now, a.IdleTimeout) {
		a.discard(r, session.ID)
		return nil, &Error{Code: CodeSessionExpired}
	}

	// 5. Record the visit, subject to the throttle.
	if err := db.TouchSession(r.Context(), a.Pool, session.ID, now, TouchThrottle); err != nil {
		// Not fatal to the request. The session is valid; failing to note that
		// it was used would be a strange reason to reject a caller who is
		// properly logged in. The next request tries again.
		_ = err
	}

	return &Principal{User: user, SessionID: session.ID}, nil
}

// fromBearer authenticates an API token.
//
// Token requests do not create or touch session rows and are unaffected by the
// single-session rule and the idle timeout, per
// docs/05_auth_and_permissions.md. A token is for a script, and a script that
// runs nightly should not find itself logged out.
func (a *Authenticator) fromBearer(r *http.Request, header string) (*Principal, *Error) {
	value, err := auth.ParseBearer(header)
	if err != nil {
		return nil, &Error{Code: CodeInvalidToken}
	}

	token, user, dbErr := db.APITokenByHash(r.Context(), a.Pool, auth.HashAPIToken(value))
	switch {
	case errors.Is(dbErr, db.ErrNotFound):
		// Revoked, or never existed. The same answer for both: a caller with a
		// wrong token learns nothing about which tokens are real.
		return nil, &Error{Code: CodeInvalidToken}
	case dbErr != nil:
		return nil, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr}
	}

	if token.Expired(a.now()) {
		return nil, &Error{Code: CodeInvalidToken}
	}

	if err := db.TouchAPIToken(r.Context(), a.Pool, token.ID, a.now(), TouchThrottle); err != nil {
		_ = err // As for sessions: worth recording, not worth failing over.
	}

	return &Principal{User: user, ViaToken: true}, nil
}

// discard removes a session that has just been found unusable.
//
// Errors are ignored: the caller is being rejected either way, and the row will
// be collected by the cleanup verb if this did not manage it.
func (a *Authenticator) discard(r *http.Request, id uuid.UUID) {
	_ = db.DeleteSession(r.Context(), a.Pool, id)
}
