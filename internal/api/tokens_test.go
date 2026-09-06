package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/joernott/doenerstag/internal/api"
	"github.com/joernott/doenerstag/internal/auth"
	"github.com/joernott/doenerstag/internal/db"
)

type tokenResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	Token      string `json:"token"`
	ExpiresAt  string `json:"expires_at"`
	LastUsedAt string `json:"last_used_at"`
	CreatedAt  string `json:"created_at"`
}

// createToken issues a token for the caller and returns the response.
func (f *apiFixture) createToken(name, owner string, cookies []*http.Cookie) tokenResponse {
	f.t.Helper()

	rec := f.post("/users/"+f.userID(owner)+"/tokens",
		map[string]string{"name": name}, cookies...)
	if rec.Code != http.StatusCreated {
		f.t.Fatalf("creating token %q: %d %s", name, rec.Code, rec.Body.String())
	}
	var body tokenResponse
	decode(f.t, rec, &body)
	return body
}

// bearer builds the Authorization header for a token.
func bearer(value string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + value}
}

func TestCreatingATokenReturnsTheValueOnce(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("anna")

	created := f.createToken("laptop", "anna", cookies)
	if created.Token == "" {
		t.Fatal("no token value was returned")
	}
	if !strings.HasPrefix(created.Token, created.Prefix) {
		t.Error("the prefix is not a prefix of the value")
	}

	// Listing must never return it again.
	list := f.get("/users/"+f.userID("anna")+"/tokens", cookies...)
	if list.Code != http.StatusOK {
		t.Fatalf("listing: %s", list.Body.String())
	}
	if strings.Contains(list.Body.String(), created.Token) {
		t.Error("the list returns the token value")
	}

	var listed struct {
		Tokens []tokenResponse `json:"tokens"`
	}
	decode(t, list, &listed)
	if len(listed.Tokens) != 1 {
		t.Fatalf("listed %d tokens, want 1", len(listed.Tokens))
	}
	if listed.Tokens[0].Prefix != created.Prefix {
		t.Error("the listed token is not the one created")
	}
	if listed.Tokens[0].Token != "" {
		t.Error("the listed token carries a value")
	}
}

func TestATokenAuthenticatesItsOwner(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("bertil")
	created := f.createToken("script", "bertil", cookies)

	who := f.probeWithToken(created.Token)
	if !who.Authenticated {
		t.Fatal("the token did not authenticate")
	}
	if who.Name != "bertil" {
		t.Errorf("authenticated as %q, want bertil", who.Name)
	}
	if !who.ViaToken {
		t.Error("a Bearer request is not reported as token-authenticated")
	}
	if who.SessionID != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("a token request has a session id: %s", who.SessionID)
	}
}

// A token inherits its owner's permissions, including administrator rights if
// the owner is root.
func TestATokenInheritsAdministratorRights(t *testing.T) {
	f := newAPIFixture(t)
	admin := f.loginAsAdmin("chief")
	created := f.createToken("admin-script", "chief", admin)

	who := f.probeWithToken(created.Token)
	if !who.IsAdmin {
		t.Error("the administrator's token does not carry administrator rights")
	}

	// And it reaches an administrator-only endpoint.
	rec := f.do(request{
		method: http.MethodGet, path: "/users",
		headers: bearer(created.Token),
	})
	if rec.Code != http.StatusOK {
		t.Errorf("the token could not list users: %d %s", rec.Code, rec.Body.String())
	}
}

// Bearer requests are exempt from CSRF: they carry no ambient browser
// credential, so there is nothing for a foreign site to abuse.
func TestATokenWriteNeedsNoCSRFHeader(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("carla")
	created := f.createToken("writer", "carla", cookies)

	rec := f.do(request{
		method: http.MethodPost, path: "/probe",
		headers: bearer(created.Token), omitCSRF: true,
	})
	if rec.Code != http.StatusOK {
		t.Errorf("a token write was refused: %d %s", rec.Code, rec.Body.String())
	}
}

// Revocation is immediate.
func TestRevokingATokenTakesEffectAtOnce(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("dora")
	created := f.createToken("revoked", "dora", cookies)

	if who := f.probeWithToken(created.Token); !who.Authenticated {
		t.Fatal("the token did not work before revocation")
	}

	rec := f.remove("/users/"+f.userID("dora")+"/tokens/"+created.ID, cookies...)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoking: %d %s", rec.Code, rec.Body.String())
	}

	after := f.do(request{
		method: http.MethodGet, path: "/probe",
		headers: bearer(created.Token),
	})
	expectError(t, after, http.StatusUnauthorized, api.CodeInvalidToken)
}

// A wrong token and a revoked one answer the same, so a caller learns nothing
// about which tokens are real.
func TestABadTokenIsRejected(t *testing.T) {
	f := newAPIFixture(t)

	generated, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"never issued":  "Bearer " + generated.Value,
		"not base64url": "Bearer " + strings.Repeat("!", 43),
		"too short":     "Bearer abc",
		"wrong scheme":  "Basic " + generated.Value,
		"scheme only":   "Bearer",
	}

	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			rec := f.do(request{
				method: http.MethodGet, path: "/probe",
				headers: map[string]string{"Authorization": header},
			})
			expectError(t, rec, http.StatusUnauthorized, api.CodeInvalidToken)
		})
	}
}

// An expired token stops working without anybody revoking it.
func TestAnExpiredTokenIsRejected(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("erik")

	expiry := f.now.Add(24 * time.Hour).UTC().Format(time.RFC3339)
	rec := f.post("/users/"+f.userID("erik")+"/tokens", map[string]string{
		"name": "expiring", "expires_at": expiry,
	}, cookies...)
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating: %s", rec.Body.String())
	}
	var created tokenResponse
	decode(t, rec, &created)
	if created.ExpiresAt == "" {
		t.Error("the expiry was not returned")
	}

	if who := f.probeWithToken(created.Token); !who.Authenticated {
		t.Fatal("the token did not work before expiry")
	}

	f.advance(25 * time.Hour)

	after := f.do(request{
		method: http.MethodGet, path: "/probe",
		headers: bearer(created.Token),
	})
	expectError(t, after, http.StatusUnauthorized, api.CodeInvalidToken)
}

// An expiry in the past is far more likely to be a typo in the year than a
// deliberate request for a token that never works.
func TestAnExpiryInThePastIsRefused(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("frieda")

	rec := f.post("/users/"+f.userID("frieda")+"/tokens", map[string]string{
		"name": "backdated", "expires_at": f.now.Add(-time.Hour).UTC().Format(time.RFC3339),
	}, cookies...)
	expectError(t, rec, http.StatusBadRequest, api.CodeInvalidField)

	malformed := f.post("/users/"+f.userID("frieda")+"/tokens", map[string]string{
		"name": "malformed", "expires_at": "next tuesday",
	}, cookies...)
	expectError(t, malformed, http.StatusBadRequest, api.CodeInvalidField)
}

// Token requests do not create or touch session rows, and are unaffected by the
// single-session rule: a script that runs nightly must not find itself logged
// out because somebody logged in from a browser.
func TestTokensAreIndependentOfSessions(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("gustav")
	created := f.createToken("nightly", "gustav", cookies)

	before := f.countSessions("gustav")

	if who := f.probeWithToken(created.Token); !who.Authenticated {
		t.Fatal("the token did not authenticate")
	}
	if after := f.countSessions("gustav"); after != before {
		t.Errorf("a token request changed the session count: %d then %d", before, after)
	}

	// A new login supersedes the browser session; the token keeps working.
	if rec := f.post("/auth/login", map[string]string{
		"name": "gustav", "password": validPassword,
	}); rec.Code != http.StatusOK {
		t.Fatalf("logging in again: %s", rec.Body.String())
	}
	if who := f.probeWithToken(created.Token); !who.Authenticated {
		t.Error("a new login invalidated the API token")
	}

	// And the idle timeout does not reach it either.
	f.advance(fixtureIdleTimeout * 4)
	if who := f.probeWithToken(created.Token); !who.Authenticated {
		t.Error("the idle timeout invalidated the API token")
	}
}

// Tokens are somebody's credentials, and only they and the administrator may
// see or change them.
func TestTokensArePrivateToTheirOwner(t *testing.T) {
	f := newAPIFixture(t)
	owner := f.register("hanna")
	intruder := f.register("ida")
	created := f.createToken("private", "hanna", owner)

	path := "/users/" + f.userID("hanna") + "/tokens"

	list := f.get(path, intruder...)
	expectError(t, list, http.StatusForbidden, api.CodeNotItemOwner)

	create := f.post(path, map[string]string{"name": "theirs"}, intruder...)
	expectError(t, create, http.StatusForbidden, api.CodeNotItemOwner)

	revoke := f.remove(path+"/"+created.ID, intruder...)
	expectError(t, revoke, http.StatusForbidden, api.CodeNotItemOwner)

	anonymous := f.get(path)
	expectError(t, anonymous, http.StatusUnauthorized, api.CodeNotAuthenticated)
}

// A token id belonging to somebody else must not be revocable through one's own
// URL, which would otherwise let any authenticated caller delete any token
// whose id they knew.
func TestATokenCannotBeRevokedThroughAnotherUsersPath(t *testing.T) {
	f := newAPIFixture(t)
	victim := f.register("jonas")
	attacker := f.register("klara")

	target := f.createToken("target", "jonas", victim)

	// The attacker owns this path, and supplies somebody else's token id.
	rec := f.remove("/users/"+f.userID("klara")+"/tokens/"+target.ID, attacker...)
	expectError(t, rec, http.StatusNotFound, api.CodeNotFound)

	// The token still works.
	if who := f.probeWithToken(target.Token); !who.Authenticated {
		t.Error("the token was revoked through another user's path")
	}
}

func TestTokenNamesMustBeUniquePerUser(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("lena")
	f.createToken("laptop", "lena", cookies)

	rec := f.post("/users/"+f.userID("lena")+"/tokens",
		map[string]string{"name": "laptop"}, cookies...)
	expectError(t, rec, http.StatusConflict, api.CodeNameExistsHere)
}

func TestATokenNeedsAName(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("malte")
	path := "/users/" + f.userID("malte") + "/tokens"

	for _, name := range []string{"", "   "} {
		rec := f.post(path, map[string]string{"name": name}, cookies...)
		expectError(t, rec, http.StatusBadRequest, api.CodeMissingField)
	}

	tooLong := f.post(path, map[string]string{"name": strings.Repeat("a", 65)}, cookies...)
	expectError(t, tooLong, http.StatusBadRequest, api.CodeInvalidField)
}

// The value is never stored, so it cannot be recovered from the database.
func TestTheTokenValueIsNotInTheDatabase(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("nils")
	created := f.createToken("secret", "nils", cookies)

	var found int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM api_token WHERE token_hash = $1 OR token_prefix = $1`,
		created.Token).Scan(&found); err != nil {
		t.Fatal(err)
	}
	if found != 0 {
		t.Error("the token value is stored in the database")
	}

	// What is stored is its hash, which is how lookup works.
	_, _, err := db.APITokenByHash(context.Background(), f.pool,
		auth.HashAPIToken(created.Token))
	if err != nil {
		t.Errorf("the token cannot be found by its hash: %v", err)
	}
}
