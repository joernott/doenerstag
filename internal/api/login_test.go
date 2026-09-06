package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/joernott/doenerstag/internal/api"
	"github.com/joernott/doenerstag/internal/auth"
	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
)

// register creates an account through the API and returns its cookies, which is
// how every test that needs a logged-in caller gets one.
func (f *apiFixture) register(name string) []*http.Cookie {
	f.t.Helper()

	rec := f.post("/auth/register", map[string]string{
		"name": name, "password": validPassword,
	})
	if rec.Code != http.StatusCreated {
		f.t.Fatalf("registering %q: %s", name, rec.Body.String())
	}
	return rec.Result().Cookies()
}

func TestLoginIssuesASession(t *testing.T) {
	f := newAPIFixture(t)
	f.register("anna")

	rec := f.post("/auth/login", map[string]string{
		"name": "anna", "password": validPassword,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	var body sessionResponse
	decode(t, rec, &body)
	if body.User == nil || body.User.Name != "anna" {
		t.Fatalf("returned %+v", body.User)
	}

	session := cookie(rec, api.SessionCookieName)
	csrf := cookie(rec, api.CSRFCookieName)
	if session == nil || csrf == nil {
		t.Fatal("login did not set both cookies")
	}

	signer, err := auth.NewSigner(fixtureSecret)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := signer.Parse(session.Value)
	if err != nil {
		t.Fatalf("the token does not verify: %v", err)
	}
	if claims.UserID.String() != body.User.ID {
		t.Error("the token names a different account than the response")
	}
}

// The login name is matched case-insensitively, matching the unique index.
func TestLoginIsCaseInsensitiveOnTheName(t *testing.T) {
	f := newAPIFixture(t)
	f.register("bertil")

	rec := f.post("/auth/login", map[string]string{
		"name": "BERTIL", "password": validPassword,
	})
	if rec.Code != http.StatusOK {
		t.Errorf("a differently-cased name failed: %s", rec.Body.String())
	}
}

// The password is not case-insensitive, and a near miss is still a miss.
func TestLoginRejectsAWrongPassword(t *testing.T) {
	f := newAPIFixture(t)
	f.register("carla")

	for _, password := range []string{
		"wrong-password-entirely",
		strings.ToLower(validPassword),
		validPassword + " ",
		"",
	} {
		rec := f.post("/auth/login", map[string]string{
			"name": "carla", "password": password,
		})
		expectError(t, rec, http.StatusUnauthorized, api.CodeInvalidLogin)
		if cookie(rec, api.SessionCookieName) != nil {
			t.Errorf("a failed login with %q set a session cookie", password)
		}
	}
}

// Every failure answers the same code, so that the login form cannot be used to
// find out who has an account.
func TestLoginDoesNotRevealWhetherTheAccountExists(t *testing.T) {
	f := newAPIFixture(t)
	f.register("dora")

	wrongPassword := f.post("/auth/login", map[string]string{
		"name": "dora", "password": "not the password",
	})
	noSuchUser := f.post("/auth/login", map[string]string{
		"name": "nobody-at-all", "password": "not the password",
	})

	if wrongPassword.Code != noSuchUser.Code {
		t.Errorf("statuses differ: %d and %d", wrongPassword.Code, noSuchUser.Code)
	}
	if wrongPassword.Body.String() == "" || noSuchUser.Body.String() == "" {
		t.Fatal("one of the responses was empty")
	}

	// The bodies differ only in the request id, so compare the envelopes with
	// it removed.
	var a, b errorBody
	decode(t, wrongPassword, &a)
	decode(t, noSuchUser, &b)
	if a.Error.Code != b.Error.Code || a.Error.Message != b.Error.Message {
		t.Errorf("the responses differ: %+v and %+v", a.Error, b.Error)
	}
	if a.Error.Field != b.Error.Field {
		t.Errorf("one response names a field and the other does not: %q, %q",
			a.Error.Field, b.Error.Field)
	}
}

// The single-session rule from docs/05_auth_and_permissions.md: a second login
// replaces the first, and the first browser is rejected on its next request.
func TestASecondLoginSupersedesTheFirst(t *testing.T) {
	f := newAPIFixture(t)
	f.register("erik")

	first := f.post("/auth/login", map[string]string{
		"name": "erik", "password": validPassword,
	})
	second := f.post("/auth/login", map[string]string{
		"name": "erik", "password": validPassword,
	})
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("a login failed: %d, %d", first.Code, second.Code)
	}

	signer, err := auth.NewSigner(fixtureSecret)
	if err != nil {
		t.Fatal(err)
	}
	firstClaims, err := signer.Parse(cookie(first, api.SessionCookieName).Value)
	if err != nil {
		t.Fatal(err)
	}
	secondClaims, err := signer.Parse(cookie(second, api.SessionCookieName).Value)
	if err != nil {
		t.Fatal(err)
	}

	if firstClaims.SessionID == secondClaims.SessionID {
		t.Fatal("the second login reused the first session")
	}

	ctx := context.Background()
	if _, err := db.SessionByID(ctx, f.pool, firstClaims.SessionID); err == nil {
		t.Error("the first session row survived the second login")
	}
	if _, err := db.SessionByID(ctx, f.pool, secondClaims.SessionID); err != nil {
		t.Errorf("the second session is not usable: %v", err)
	}
}

// Each login gets its own CSRF token: reusing one across sessions would mean a
// token stayed valid after the session it was issued with had gone.
func TestEachLoginIssuesAFreshCSRFToken(t *testing.T) {
	f := newAPIFixture(t)
	f.register("frieda")

	first := f.post("/auth/login", map[string]string{
		"name": "frieda", "password": validPassword,
	})
	second := f.post("/auth/login", map[string]string{
		"name": "frieda", "password": validPassword,
	})

	a := cookie(first, api.CSRFCookieName)
	b := cookie(second, api.CSRFCookieName)
	if a == nil || b == nil {
		t.Fatal("a login did not set a CSRF cookie")
	}
	if a.Value == b.Value {
		t.Error("two logins issued the same CSRF token")
	}
}

func TestLoginStampsTheLoginTime(t *testing.T) {
	f := newAPIFixture(t)
	f.register("gustav")

	ctx := context.Background()
	before, err := db.UserByName(ctx, f.pool, "gustav")
	if err != nil {
		t.Fatal(err)
	}
	if before.LastLoginAt != nil {
		t.Error("registering already stamped a login time")
	}

	if rec := f.post("/auth/login", map[string]string{
		"name": "gustav", "password": validPassword,
	}); rec.Code != http.StatusOK {
		t.Fatalf("login failed: %s", rec.Body.String())
	}

	after, err := db.UserByName(ctx, f.pool, "gustav")
	if err != nil {
		t.Fatal(err)
	}
	if after.LastLoginAt == nil {
		t.Fatal("the login time was not recorded")
	}
	if !after.LastLoginAt.Equal(f.now.UTC()) && after.LastLoginAt.Unix() != f.now.Unix() {
		t.Errorf("last login is %v, want the fixture clock %v", after.LastLoginAt, f.now)
	}
}

// The deleted-user placeholder holds a hash that is not a PHC string, so no
// password can verify against it. It must not be possible to log in as it.
func TestThePlaceholderAccountCannotLogIn(t *testing.T) {
	f := newAPIFixture(t)

	placeholder, err := db.UserByID(context.Background(), f.pool, model.DeletedUserID)
	if err != nil {
		t.Fatal(err)
	}

	for _, password := range []string{"*", "", "deleted", validPassword} {
		rec := f.post("/auth/login", map[string]string{
			"name": placeholder.Name, "password": password,
		})
		expectError(t, rec, http.StatusUnauthorized, api.CodeInvalidLogin)
	}
}

func TestLoginRejectsAMissingName(t *testing.T) {
	f := newAPIFixture(t)

	for _, body := range []map[string]string{
		{"password": validPassword},
		{"name": "", "password": validPassword},
		{"name": "   ", "password": validPassword},
	} {
		rec := f.post("/auth/login", body)
		expectError(t, rec, http.StatusUnauthorized, api.CodeInvalidLogin)
	}
}

// The response must not carry the credential back, in any form.
func TestLoginNeverEchoesTheCredential(t *testing.T) {
	f := newAPIFixture(t)
	f.register("hanna")

	rec := f.post("/auth/login", map[string]string{
		"name": "hanna", "password": validPassword,
	})
	if strings.Contains(rec.Body.String(), validPassword) {
		t.Error("the response contains the password")
	}
	if strings.Contains(rec.Body.String(), "$argon2id$") {
		t.Error("the response contains the password hash")
	}

	// And neither does a failure.
	failed := f.post("/auth/login", map[string]string{
		"name": "hanna", "password": "the wrong one entirely",
	})
	if strings.Contains(failed.Body.String(), "the wrong one entirely") {
		t.Error("the failure echoes the attempted password")
	}
}
