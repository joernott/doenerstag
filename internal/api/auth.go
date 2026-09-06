package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/joernott/doenerstag/internal/auth"
	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/logging"
	"github.com/joernott/doenerstag/internal/model"
)

// Registration limits from docs/05_auth_and_permissions.md. Counted in code
// points rather than bytes, so a name in a non-Latin script is not shorter than
// it looks.
const (
	MinUserNameLength    = 3
	MaxUserNameLength    = 64
	MaxDisplayNameLength = 64
	MaxEmailLength       = 254
)

// UserAgentLogLimit bounds what is stored from a User-Agent header.
//
// The header is attacker-controlled and unbounded, and it lands in a database
// column. Truncating keeps a request from writing a megabyte per login.
const UserAgentLogLimit = 512

// AuthHandlers serves registration, login, logout and session information.
type AuthHandlers struct {
	Pool   *pgxpool.Pool
	Signer *auth.Signer

	// AbsoluteTimeout is the session lifetime, and the cookie's Max-Age.
	AbsoluteTimeout time.Duration

	// Secure controls the Secure attribute on the cookies. False under
	// --no-https only.
	Secure bool

	// Logger records the things that are worth knowing but must not fail a
	// request: a login that worked but whose bookkeeping did not. Nil is
	// allowed and discards, so a test does not have to supply one.
	Logger *zerolog.Logger

	// Now is the clock, injectable so a test can age a session without
	// sleeping through an idle timeout.
	Now func() time.Time
}

func (h *AuthHandlers) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// log returns a logger already carrying the request's correlation ID, so that a
// line written here can be joined to the request line the middleware writes.
func (h *AuthHandlers) log(r *http.Request) *zerolog.Logger {
	if h.Logger == nil {
		discard := zerolog.Nop()
		return &discard
	}
	logger := h.Logger.With().
		Str(logging.FieldRequestID, RequestIDFrom(r.Context())).
		Logger()
	return &logger
}

// Register adds the authentication routes.
func (h *AuthHandlers) Register(r *Router) {
	r.HandleFunc(http.MethodPost, "/auth/register", h.register)
	r.HandleFunc(http.MethodPost, "/auth/login", h.login)
}

// registerRequest is the body of POST /auth/register.
type registerRequest struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Password    string `json:"password"`
}

// userBody is the public shape of an account.
//
// It is what every endpoint returning a user sends, and it deliberately has no
// field for the password hash, so no future edit can add one by filling in a
// struct literal.
type userBody struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
}

func publicUser(u model.User) userBody {
	return userBody{
		ID:          u.ID.String(),
		Name:        u.Name,
		DisplayName: u.Label(),
		IsAdmin:     u.IsAdmin,
	}
}

// sessionBody is what register, login and GET /auth/session return.
type sessionBody struct {
	User *userBody `json:"user"`
	// ExpiresAt is the session's absolute expiry, so the frontend can warn
	// before it lapses rather than discovering it on the next click.
	ExpiresAt string `json:"expires_at,omitempty"`
}

// register creates an account and logs it straight in.
//
// docs/04_api.md says the response returns the new user and starts a session:
// making somebody type their password twice in a row, once to register and once
// to log in, is friction with no security benefit.
func (h *AuthHandlers) register(w http.ResponseWriter, r *http.Request) {
	var body registerRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	name := strings.TrimSpace(body.Name)
	displayName := strings.TrimSpace(body.DisplayName)
	email := strings.TrimSpace(body.Email)

	if err := validateUserName(name); err != nil {
		WriteError(w, r, err)
		return
	}
	if err := validateDisplayName(displayName); err != nil {
		WriteError(w, r, err)
		return
	}
	if err := validateEmail(email); err != nil {
		WriteError(w, r, err)
		return
	}

	// The complexity rules are enforced here as well as in the frontend,
	// because a password set through the API is subject to identical rules.
	// The error names how many rules were met and never echoes the password.
	if err := auth.ValidateComplexity(body.Password); err != nil {
		WriteError(w, r, &Error{
			Code:   CodePasswordTooWeak,
			Field:  "password",
			Detail: err.Error(),
		})
		return
	}

	hash, err := auth.Hash(body.Password)
	if err != nil {
		WriteError(w, r, &Error{Code: CodeInternal, Cause: err})
		return
	}

	user, err := db.CreateUser(r.Context(), h.Pool, db.NewUser{
		Name:         name,
		DisplayName:  displayName,
		Email:        email,
		PasswordHash: hash,
	})
	switch {
	case errors.Is(err, db.ErrNameTaken):
		WriteError(w, r, &Error{Code: CodeUserNameTaken, Field: "name"})
		return
	case err != nil:
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	session, err := h.startSession(r, w, user)
	if err != nil {
		WriteError(w, r, &Error{Code: CodeInternal, Cause: err})
		return
	}

	public := publicUser(user)
	_ = WriteJSON(w, http.StatusCreated, sessionBody{
		User:      &public,
		ExpiresAt: session.AbsoluteExpires.UTC().Format(time.RFC3339),
	})
}

// startSession creates the session row, signs a token and sets both cookies.
//
// Shared by register and login, so that an account created through
// registration is in exactly the same state as one that logged in -- rather
// than in a nearly-identical state that drifts the first time one of the two
// paths is changed.
func (h *AuthHandlers) startSession(r *http.Request, w http.ResponseWriter, user model.User) (model.Session, error) {
	session, err := db.ReplaceSession(r.Context(), h.Pool, db.NewSession{
		UserID:     user.ID,
		Lifetime:   h.AbsoluteTimeout,
		RemoteAddr: clientAddress(r),
		UserAgent:  truncate(r.UserAgent(), UserAgentLogLimit),
	})
	if err != nil {
		return model.Session{}, err
	}

	token, err := h.Signer.Issue(auth.Claims{
		UserID:    user.ID,
		SessionID: session.ID,
		IssuedAt:  session.IssuedAt,
		ExpiresAt: session.AbsoluteExpires,
		IsAdmin:   user.IsAdmin,
	})
	if err != nil {
		return model.Session{}, err
	}

	csrf, err := auth.GenerateCSRFToken()
	if err != nil {
		return model.Session{}, err
	}

	setSessionCookies(w, token, csrf, h.AbsoluteTimeout, h.Secure)
	return session, nil
}

// recordLogin stamps last_login_at without failing the request if it cannot.
//
// A login that worked should not be reported as a failure because a bookkeeping
// column could not be written; the session already exists at this point, so
// returning an error would leave the caller logged in but told otherwise.
func (h *AuthHandlers) recordLogin(ctx context.Context, user model.User) error {
	return db.RecordLogin(ctx, h.Pool, user.ID, h.now())
}

// validateUserName applies the length and content rules.
//
// Length is in code points, matching the CHECK constraint, which uses
// char_length. A control character is rejected because a name is displayed to
// other people and rendered into log lines.
func validateUserName(name string) *Error {
	if name == "" {
		return &Error{Code: CodeMissingField, Field: "name"}
	}
	length := utf8.RuneCountInString(name)
	if length < MinUserNameLength || length > MaxUserNameLength {
		return &Error{
			Code:   CodeInvalidField,
			Field:  "name",
			Detail: "the user name must be between 3 and 64 characters",
		}
	}
	if strings.ContainsFunc(name, isControl) {
		return &Error{
			Code:   CodeInvalidField,
			Field:  "name",
			Detail: "the user name may not contain control characters",
		}
	}
	return nil
}

func validateDisplayName(name string) *Error {
	if name == "" {
		// Optional: an absent display name falls back to the user name.
		return nil
	}
	if utf8.RuneCountInString(name) > MaxDisplayNameLength {
		return &Error{
			Code:   CodeInvalidField,
			Field:  "display_name",
			Detail: "the display name may be at most 64 characters",
		}
	}
	if strings.ContainsFunc(name, isControl) {
		return &Error{
			Code:   CodeInvalidField,
			Field:  "display_name",
			Detail: "the display name may not contain control characters",
		}
	}
	return nil
}

// validateEmail format-checks an address.
//
// Deliberately loose, matching the CHECK constraint. The address is optional,
// never verified and never used to send anything, so a strict grammar would
// reject valid addresses for no benefit.
func validateEmail(address string) *Error {
	if address == "" {
		return nil
	}
	if len(address) > MaxEmailLength {
		return &Error{
			Code:   CodeInvalidField,
			Field:  "email",
			Detail: "the e-mail address is too long",
		}
	}
	if _, err := mail.ParseAddress(address); err != nil {
		return &Error{
			Code:   CodeInvalidField,
			Field:  "email",
			Detail: "this does not look like an e-mail address",
		}
	}
	return nil
}

func isControl(r rune) bool {
	return r < 0x20 || r == 0x7f
}

// truncate cuts a string to at most n bytes without splitting a rune.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

// clientAddress is the peer address, without the port.
//
// No X-Forwarded-For handling. docs/10_operations.md puts a reverse proxy in
// front only for TLS termination on port 443, and trusting a header that any
// client can send would let one caller attribute their failed logins to
// somebody else's address -- which, with per-address rate limiting, is a way to
// lock a colleague out.
func clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
