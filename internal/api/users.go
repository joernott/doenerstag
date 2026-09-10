package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joernott/doenerstag/internal/auth"
	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
)

// UserHandlers serves the account and API token endpoints.
type UserHandlers struct {
	Pool *pgxpool.Pool

	// Secure controls the Secure attribute when clearing cookies after a user
	// deletes their own account. False under --no-https only.
	Secure bool

	// Now is the clock, for token expiry.
	Now func() time.Time
}

func (h *UserHandlers) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// Register adds the account routes.
func (h *UserHandlers) Register(r *Router) {
	r.HandleFunc(http.MethodGet, "/users", h.list)
	r.HandleFunc(http.MethodGet, "/users/:id", h.get)
	r.HandleFunc(http.MethodPatch, "/users/:id", h.patch)
	r.HandleFunc(http.MethodDelete, "/users/:id", h.deleteUser)
	r.HandleFunc(http.MethodGet, "/users/:id/deletion-impact", h.deletionImpact)

	r.HandleFunc(http.MethodGet, "/users/:id/tokens", h.listTokens)
	r.HandleFunc(http.MethodPost, "/users/:id/tokens", h.createToken)
	r.HandleFunc(http.MethodDelete, "/users/:id/tokens/:tid", h.revokeToken)
}

// adminUserBody is the fuller shape the administrator's list returns.
//
// The e-mail address and the login time are here and not in userBody, because
// docs/04_api.md marks GET /users/{id} public and describes it as "id, name,
// display name". A single struct with omitempty would put the decision about
// who sees an address into whether a field happened to be filled in.
type adminUserBody struct {
	userBody
	Email       string `json:"email"`
	LastLoginAt string `json:"last_login_at,omitempty"`
	CreatedAt   string `json:"created_at"`
}

func adminUser(u model.User) adminUserBody {
	body := adminUserBody{
		userBody:  publicUser(u),
		Email:     u.Email,
		CreatedAt: u.CreatedAt.UTC().Format(time.RFC3339),
	}
	if u.LastLoginAt != nil {
		body.LastLoginAt = u.LastLoginAt.UTC().Format(time.RFC3339)
	}
	return body
}

// list returns every account, in the shape the caller is entitled to.
//
// Two tiers, the same way GET /orders/{id} has two: an administrator gets the
// administrative shape, with e-mail addresses and login times, and anybody else
// who is signed in gets the public profile -- id, name, display name -- which is
// what GET /users/{id} already serves to anyone at all.
//
// The second tier exists because an order names two people besides its creator,
// and picking somebody from a list is the only way to name an account rather
// than type a name at it. What it adds over the per-user endpoint is
// enumeration: a signed-in caller can learn who has an account here rather than
// only ask about one they can already name. That is a real difference and the
// reason this needs a session at all, and it is a much smaller one than
// handing out the addresses and last-seen times the administrative shape
// carries.
func (h *UserHandlers) list(w http.ResponseWriter, r *http.Request) {
	principal, err := RequireAuthenticated(r)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	users, dbErr := db.ListUsers(r.Context(), h.Pool)
	if dbErr != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	if !principal.User.IsAdmin {
		bodies := make([]userBody, 0, len(users))
		for i := range users {
			bodies = append(bodies, publicUser(users[i]))
		}
		_ = WriteJSON(w, http.StatusOK, map[string]any{"users": bodies})
		return
	}

	bodies := make([]adminUserBody, 0, len(users))
	for i := range users {
		bodies = append(bodies, adminUser(users[i]))
	}
	_ = WriteJSON(w, http.StatusOK, map[string]any{"users": bodies})
}

// get returns a public profile.
//
// Public, and public means public: id, name and display name, and nothing else.
// The e-mail address is not in this response for anybody, including the
// administrator, because a field that appears for some callers and not others
// is a field that will eventually appear for the wrong one.
func (h *UserHandlers) get(w http.ResponseWriter, r *http.Request) {
	user, err := LookupUser(r, h.Pool, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	_ = WriteJSON(w, http.StatusOK, publicUser(user))
}

// patchRequest is the body of PATCH /users/{id}.
//
// Pointers, so that absent and empty are different: omitting display_name
// leaves it alone, and sending "" clears it.
type patchRequest struct {
	Name        *string `json:"name"`
	DisplayName *string `json:"display_name"`
	Email       *string `json:"email"`
	Password    *string `json:"password"`

	// CurrentPassword confirms a password change.
	CurrentPassword *string `json:"current_password"`
}

// patch changes an account. Owner or administrator.
func (h *UserHandlers) patch(w http.ResponseWriter, r *http.Request) {
	user, lookupErr := LookupUser(r, h.Pool, "id")
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}

	principal, authErr := RequireOwner(r, user.ID)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}
	if err := RequireNotPlaceholder(user.ID); err != nil {
		WriteError(w, r, err)
		return
	}

	var body patchRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	update, err := h.validatePatch(r, user, principal, body)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	updated, dbErr := db.UpdateUser(r.Context(), h.Pool, user.ID, principal.User.ID, update)
	switch {
	case errors.Is(dbErr, db.ErrNameTaken):
		WriteError(w, r, &Error{Code: CodeUserNameTaken, Field: "name"})
		return
	case dbErr != nil:
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	_ = WriteJSON(w, http.StatusOK, publicUser(updated))
}

// validatePatch turns a request body into an update, applying the same rules
// registration does.
func (h *UserHandlers) validatePatch(
	r *http.Request, user model.User, principal *Principal, body patchRequest,
) (db.UserUpdate, *Error) {
	var update db.UserUpdate

	if body.Name != nil {
		name := strings.TrimSpace(*body.Name)
		if err := validateUserName(name); err != nil {
			return update, err
		}
		// root cannot be renamed: docs/05_auth_and_permissions.md says the one
		// administrator account cannot be renamed or deleted, and the whole
		// installation refers to it by name.
		if user.IsAdmin && !strings.EqualFold(name, model.AdministratorName) {
			return update, &Error{
				Code:   CodeInvalidField,
				Field:  "name",
				Detail: "the administrator account cannot be renamed",
			}
		}
		update.Name = &name
	}

	if body.DisplayName != nil {
		displayName := strings.TrimSpace(*body.DisplayName)
		if err := validateDisplayName(displayName); err != nil {
			return update, err
		}
		update.DisplayName = &displayName
	}

	if body.Email != nil {
		email := strings.TrimSpace(*body.Email)
		if err := validateEmail(email); err != nil {
			return update, err
		}
		update.Email = &email
	}

	if body.Password != nil {
		hash, err := h.newPasswordHash(r, user, principal, body)
		if err != nil {
			return update, err
		}
		update.PasswordHash = &hash
	}

	return update, nil
}

// newPasswordHash validates and hashes a password change.
//
// Changing your own password requires the current one. It is the check that
// stops a borrowed unlocked browser becoming a permanent account takeover: the
// session alone is enough to read and to order lunch, and should not be enough
// to lock the owner out of their own account.
//
// The administrator resetting somebody else's password does not supply it,
// because they do not have it -- that is the recovery path for a forgotten
// password, and requiring the old one would defeat the purpose.
func (h *UserHandlers) newPasswordHash(
	r *http.Request, user model.User, principal *Principal, body patchRequest,
) (string, *Error) {
	if principal.Is(user.ID) {
		if body.CurrentPassword == nil || *body.CurrentPassword == "" {
			return "", &Error{
				Code:   CodeMissingField,
				Field:  "current_password",
				Detail: "changing your own password requires the current one",
			}
		}

		stored, dbErr := db.PasswordHashByID(r.Context(), h.Pool, user.ID)
		if dbErr != nil {
			return "", &Error{Code: CodeDatabaseUnavailable, Cause: dbErr}
		}
		if err := auth.Verify(*body.CurrentPassword, stored); err != nil {
			return "", &Error{
				Code:   CodeInvalidLogin,
				Field:  "current_password",
				Detail: "the current password is not correct",
			}
		}
	}

	if err := auth.ValidateComplexity(*body.Password); err != nil {
		return "", &Error{
			Code:   CodePasswordTooWeak,
			Field:  "password",
			Detail: err.Error(),
		}
	}

	hash, hashErr := auth.Hash(*body.Password)
	if hashErr != nil {
		return "", &Error{Code: CodeInternal, Cause: hashErr}
	}
	return hash, nil
}

// tokenBody is a token's metadata. It has no field for the value, which is
// shown once at creation and never stored.
type tokenBody struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	CreatedAt  string `json:"created_at"`
	ExpiresAt  string `json:"expires_at,omitempty"`
	LastUsedAt string `json:"last_used_at,omitempty"`
}

func publicToken(t model.APIToken) tokenBody {
	body := tokenBody{
		ID:        t.ID.String(),
		Name:      t.Name,
		Prefix:    t.Prefix,
		CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339),
	}
	if t.ExpiresAt != nil {
		body.ExpiresAt = t.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if t.LastUsedAt != nil {
		body.LastUsedAt = t.LastUsedAt.UTC().Format(time.RFC3339)
	}
	return body
}

// listTokens returns a user's tokens. Owner or administrator.
func (h *UserHandlers) listTokens(w http.ResponseWriter, r *http.Request) {
	user, lookupErr := LookupUser(r, h.Pool, "id")
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	if _, err := RequireOwner(r, user.ID); err != nil {
		WriteError(w, r, err)
		return
	}

	tokens, dbErr := db.ListAPITokens(r.Context(), h.Pool, user.ID)
	if dbErr != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	bodies := make([]tokenBody, 0, len(tokens))
	for _, t := range tokens {
		bodies = append(bodies, publicToken(t))
	}
	_ = WriteJSON(w, http.StatusOK, map[string]any{"tokens": bodies})
}

type createTokenRequest struct {
	Name      string `json:"name"`
	ExpiresAt string `json:"expires_at"`
}

// createTokenResponse is the one response that carries a token value.
type createTokenResponse struct {
	tokenBody
	// Token is the value, returned exactly once. It is not stored, so this
	// response is the only chance to copy it.
	Token string `json:"token"`
}

// createToken issues a token. Owner or administrator.
func (h *UserHandlers) createToken(w http.ResponseWriter, r *http.Request) {
	user, lookupErr := LookupUser(r, h.Pool, "id")
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	if _, err := RequireOwner(r, user.ID); err != nil {
		WriteError(w, r, err)
		return
	}
	if err := RequireNotPlaceholder(user.ID); err != nil {
		WriteError(w, r, err)
		return
	}

	var body createTokenRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		WriteError(w, r, &Error{Code: CodeMissingField, Field: "name"})
		return
	}
	if len([]rune(name)) > MaxTokenNameLength {
		WriteError(w, r, &Error{
			Code:   CodeInvalidField,
			Field:  "name",
			Detail: "the token name may be at most 64 characters",
		})
		return
	}

	expiresAt, err := h.parseExpiry(body.ExpiresAt)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	generated, genErr := auth.GenerateAPIToken()
	if genErr != nil {
		WriteError(w, r, &Error{Code: CodeInternal, Cause: genErr})
		return
	}

	created, dbErr := db.CreateAPIToken(r.Context(), h.Pool, db.NewAPIToken{
		UserID:    user.ID,
		Name:      name,
		Hash:      generated.Hash,
		Prefix:    generated.Prefix,
		ExpiresAt: expiresAt,
	})
	switch {
	case errors.Is(dbErr, db.ErrNameTaken):
		WriteError(w, r, &Error{
			Code:   CodeNameExistsHere,
			Field:  "name",
			Detail: "this account already has a token with that name",
		})
		return
	case dbErr != nil:
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	_ = WriteJSON(w, http.StatusCreated, createTokenResponse{
		tokenBody: publicToken(created),
		Token:     generated.Value,
	})
}

// MaxTokenNameLength matches the CHECK constraint on api_token.name.
const MaxTokenNameLength = 64

// parseExpiry reads the optional expiry date.
//
// An expiry in the past is refused rather than accepted and immediately
// useless: it is far more likely to be a typo in the year than a deliberate
// request for a token that never works.
func (h *UserHandlers) parseExpiry(value string) (*time.Time, *Error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, &Error{
			Code:   CodeInvalidField,
			Field:  "expires_at",
			Detail: "the expiry must be an RFC 3339 timestamp",
		}
	}
	if !at.After(h.now()) {
		return nil, &Error{
			Code:   CodeInvalidField,
			Field:  "expires_at",
			Detail: "the expiry is in the past",
		}
	}
	return &at, nil
}

// revokeToken deletes a token. Owner or administrator.
//
// The token is checked to belong to the user in the path, so that one user's
// token id cannot be revoked through another user's URL -- which would
// otherwise let any authenticated caller delete any token whose id they knew.
func (h *UserHandlers) revokeToken(w http.ResponseWriter, r *http.Request) {
	user, lookupErr := LookupUser(r, h.Pool, "id")
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	if _, err := RequireOwner(r, user.ID); err != nil {
		WriteError(w, r, err)
		return
	}

	tokenID, parseErr := uuid.Parse(Param(r, "tid"))
	if parseErr != nil {
		WriteError(w, r, &Error{Code: CodeNotFound})
		return
	}

	token, dbErr := db.APITokenByID(r.Context(), h.Pool, tokenID)
	switch {
	case errors.Is(dbErr, db.ErrNotFound):
		WriteError(w, r, &Error{Code: CodeNotFound})
		return
	case dbErr != nil:
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}
	if token.UserID != user.ID {
		// 4000, not 3002: from the caller's side this token does not exist at
		// this path, and saying "not yours" would confirm that the id is real.
		WriteError(w, r, &Error{Code: CodeNotFound})
		return
	}

	if err := db.DeleteAPIToken(r.Context(), h.Pool, tokenID); err != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deletionImpactBody is what the user is shown before they confirm.
type deletionImpactBody struct {
	ActiveOrders        int `json:"active_orders"`
	ActiveItems         int `json:"active_items"`
	ExpiredItems        int `json:"expired_items"`
	CreatedActiveOrders int `json:"created_active_orders"`
}

// deletionImpact reports what deleting the account would do.
//
// F2.6 requires the user to be shown how many active orders they still
// participate in and given the chance to cancel. This is that number, and it is
// the only warning: the deletion itself is immediate and irreversible.
func (h *UserHandlers) deletionImpact(w http.ResponseWriter, r *http.Request) {
	user, lookupErr := LookupUser(r, h.Pool, "id")
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	if _, err := RequireOwner(r, user.ID); err != nil {
		WriteError(w, r, err)
		return
	}

	impact, dbErr := db.ImpactOfDeleting(r.Context(), h.Pool, user.ID, h.now())
	if dbErr != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	_ = WriteJSON(w, http.StatusOK, deletionImpactBody{
		ActiveOrders:        impact.ActiveOrders,
		ActiveItems:         impact.ActiveItems,
		ExpiredItems:        impact.ExpiredItems,
		CreatedActiveOrders: impact.CreatedActiveOrders,
	})
}

// deleteUser removes an account and applies the F2.6 rules.
func (h *UserHandlers) deleteUser(w http.ResponseWriter, r *http.Request) {
	user, lookupErr := LookupUser(r, h.Pool, "id")
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}

	principal, authErr := RequireOwner(r, user.ID)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}
	if err := RequireNotPlaceholder(user.ID); err != nil {
		WriteError(w, r, err)
		return
	}
	if user.IsAdmin {
		// The database trigger refuses this too. Answering here means the
		// caller gets the documented code rather than a constraint violation
		// surfacing as 9001.
		WriteError(w, r, &Error{
			Code:   CodeAdminRequired,
			Detail: "the administrator account cannot be deleted",
		})
		return
	}

	if err := db.DeleteAccount(r.Context(), h.Pool, user.ID, h.now()); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			WriteError(w, r, &Error{Code: CodeNotFound})
			return
		}
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	// Deleting your own account logs you out: the session row went with it, so
	// the cookies now point at nothing. Clearing them saves the browser a 2003
	// on its next request.
	if principal.Is(user.ID) && !principal.ViaToken {
		clearSessionCookies(w, h.Secure)
	}
	w.WriteHeader(http.StatusNoContent)
}
