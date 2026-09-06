package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/joernott/doenerstag/internal/api"
	"github.com/joernott/doenerstag/internal/auth"
	"github.com/joernott/doenerstag/internal/db"
)

func TestRegisterCreatesAnAccountAndStartsASession(t *testing.T) {
	f := newAPIFixture(t)

	rec := f.post("/auth/register", map[string]string{
		"name":         "anna",
		"display_name": "Anna B.",
		"email":        "anna@example.invalid",
		"password":     validPassword,
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}

	var body sessionResponse
	decode(t, rec, &body)
	if body.User == nil {
		t.Fatal("the response has no user")
	}
	if body.User.Name != "anna" || body.User.DisplayName != "Anna B." {
		t.Errorf("returned %+v", body.User)
	}
	if body.User.IsAdmin {
		t.Error("a self-registered account is an administrator")
	}
	if body.ExpiresAt == "" {
		t.Error("the response does not say when the session expires")
	}

	// docs/04_api.md: registration starts a session, so both cookies are set.
	session := cookie(rec, api.SessionCookieName)
	if session == nil {
		t.Fatal("no session cookie was set")
	}
	if !session.HttpOnly {
		t.Error("the session cookie is readable by script")
	}
	if !session.Secure {
		t.Error("the session cookie is not marked Secure")
	}
	if session.SameSite != http.SameSiteStrictMode {
		t.Errorf("SameSite is %v, want Strict", session.SameSite)
	}
	if session.Path != "/" {
		t.Errorf("Path is %q, want /", session.Path)
	}

	csrf := cookie(rec, api.CSRFCookieName)
	if csrf == nil {
		t.Fatal("no CSRF cookie was set")
	}
	if csrf.HttpOnly {
		t.Error("the CSRF cookie is HttpOnly, so the frontend cannot read it")
	}
	if csrf.Value == "" {
		t.Error("the CSRF cookie is empty")
	}
	if csrf.MaxAge != session.MaxAge {
		t.Errorf("the CSRF cookie outlives its session: %d vs %d",
			csrf.MaxAge, session.MaxAge)
	}

	// The token in the cookie must be a token this server issued, naming this
	// account and a session that exists.
	signer, err := auth.NewSigner(fixtureSecret)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := signer.Parse(session.Value)
	if err != nil {
		t.Fatalf("the session cookie does not verify: %v", err)
	}
	if claims.UserID.String() != body.User.ID {
		t.Error("the token names a different account than the response")
	}
	if _, err := db.SessionByID(context.Background(), f.pool, claims.SessionID); err != nil {
		t.Errorf("the token's session does not exist: %v", err)
	}
	if claims.IsAdmin {
		t.Error("the token claims administrator rights")
	}
}

// The password must not come back in any form: not the plaintext, not the hash.
func TestRegisterNeverEchoesTheCredential(t *testing.T) {
	f := newAPIFixture(t)

	rec := f.post("/auth/register", map[string]string{
		"name": "bertil", "password": validPassword,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if strings.Contains(body, validPassword) {
		t.Error("the response contains the password")
	}
	if strings.Contains(body, "$argon2id$") {
		t.Error("the response contains the password hash")
	}

	// And the hash must actually be stored, hashed.
	_, stored, err := db.PasswordHashByName(context.Background(), f.pool, "bertil")
	if err != nil {
		t.Fatal(err)
	}
	if stored == validPassword {
		t.Fatal("the password is stored in plaintext")
	}
	if err := auth.Verify(validPassword, stored); err != nil {
		t.Errorf("the stored hash does not verify the password: %v", err)
	}
}

// The display name is optional and falls back to the user name.
func TestRegisterWithoutADisplayName(t *testing.T) {
	f := newAPIFixture(t)

	rec := f.post("/auth/register", map[string]string{
		"name": "carla", "password": validPassword,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	var body sessionResponse
	decode(t, rec, &body)
	if body.User.DisplayName != "carla" {
		t.Errorf("display name is %q, want the user name as the fallback",
			body.User.DisplayName)
	}
}

func TestRegisterRejectsATakenName(t *testing.T) {
	f := newAPIFixture(t)

	first := f.post("/auth/register", map[string]string{
		"name": "dora", "password": validPassword,
	})
	if first.Code != http.StatusCreated {
		t.Fatalf("the first registration failed: %s", first.Body.String())
	}

	// Differently cased, because "Dora" and "dora" are one person registering
	// twice.
	second := f.post("/auth/register", map[string]string{
		"name": "DORA", "password": validPassword,
	})
	expectError(t, second, http.StatusBadRequest, api.CodeUserNameTaken)
}

// The complexity rules are enforced independently of the frontend, because a
// password set through the API is subject to identical rules.
func TestRegisterEnforcesPasswordComplexity(t *testing.T) {
	f := newAPIFixture(t)

	cases := map[string]string{
		"too short":        "Ab3-x",
		"only two classes": "lowercaseonly",
		"only one class":   "aaaaaaaaaaaaaa",
	}

	for name, password := range cases {
		t.Run(name, func(t *testing.T) {
			rec := f.post("/auth/register", map[string]string{
				"name": "user" + strings.ReplaceAll(name, " ", ""), "password": password,
			})
			expectError(t, rec, http.StatusBadRequest, api.CodePasswordTooWeak)

			var body errorBody
			decode(t, rec, &body)
			if strings.Contains(rec.Body.String(), password) {
				t.Error("the error echoes the rejected password")
			}
			if body.Error.Field != "password" {
				t.Errorf("field is %q, want password", body.Error.Field)
			}
		})
	}
}

// A passphrase must pass without contortion, which is the claim
// docs/05_auth_and_permissions.md makes for the three-of-five rule: "passphrases
// pass easily on rules 1, 2 and 5 or 4".
//
// Note what that sentence does and does not promise. Space satisfies no rule,
// so an all-ASCII passphrase in sentence case meets only two classes and is
// refused. The third class comes from punctuation or from a language-specific
// character -- which any German passphrase of reasonable length has anyway.
// Both halves are asserted, because the accepting case alone would read as a
// promise the rule does not keep.
func TestRegisterAcceptsAPassphrase(t *testing.T) {
	f := newAPIFixture(t)

	accepted := map[string]string{
		"a German passphrase":         "Korrektes Pferd Batterie Klämmer",
		"a passphrase with a comma":   "Korrektes Pferd, Batterie Klammer",
		"a passphrase with a numeral": "Korrektes Pferd 4 Batterien",
	}
	for label, password := range accepted {
		t.Run(label, func(t *testing.T) {
			rec := f.post("/auth/register", map[string]string{
				"name": "user-" + strings.ReplaceAll(label, " ", "-"), "password": password,
			})
			if rec.Code != http.StatusCreated {
				t.Errorf("rejected: %s", rec.Body.String())
			}
		})
	}

	// Upper and lower case only: two classes, and the space adds nothing.
	rec := f.post("/auth/register", map[string]string{
		"name": "erik", "password": "Korrektes Pferd Batterie Klammer",
	})
	expectError(t, rec, http.StatusBadRequest, api.CodePasswordTooWeak)
}

func TestRegisterValidatesTheUserName(t *testing.T) {
	f := newAPIFixture(t)

	cases := map[string]struct {
		name string
		code api.Code
	}{
		"missing":    {"", api.CodeMissingField},
		"too short":  {"ab", api.CodeInvalidField},
		"too long":   {strings.Repeat("a", 65), api.CodeInvalidField},
		"whitespace": {"   ", api.CodeMissingField},
		"newline":    {"anna\nbertil", api.CodeInvalidField},
		"tab":        {"anna\tbertil", api.CodeInvalidField},
	}

	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			rec := f.post("/auth/register", map[string]string{
				"name": tc.name, "password": validPassword,
			})
			expectError(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}

// 64 characters is the limit, and it is counted in code points: a name in a
// non-Latin script is not shorter than it looks.
func TestUserNameLengthIsCountedInCodePoints(t *testing.T) {
	f := newAPIFixture(t)

	// 64 code points, but well over 64 bytes.
	name := strings.Repeat("ü", 64)
	rec := f.post("/auth/register", map[string]string{
		"name": name, "password": validPassword,
	})
	if rec.Code != http.StatusCreated {
		t.Errorf("a 64-character name was rejected: %s", rec.Body.String())
	}

	tooLong := strings.Repeat("ü", 65)
	rec = f.post("/auth/register", map[string]string{
		"name": tooLong, "password": validPassword,
	})
	expectError(t, rec, http.StatusBadRequest, api.CodeInvalidField)
}

func TestRegisterValidatesTheEmail(t *testing.T) {
	f := newAPIFixture(t)

	rec := f.post("/auth/register", map[string]string{
		"name": "frieda", "email": "not an address", "password": validPassword,
	})
	expectError(t, rec, http.StatusBadRequest, api.CodeInvalidField)

	var body errorBody
	decode(t, rec, &body)
	if body.Error.Field != "email" {
		t.Errorf("field is %q, want email", body.Error.Field)
	}
}

// The address is optional, and the check is deliberately loose: it is never
// verified and never used to send anything.
func TestRegisterAcceptsAnUnusualButValidEmail(t *testing.T) {
	f := newAPIFixture(t)

	rec := f.post("/auth/register", map[string]string{
		"name": "gustav", "email": "g+kebab@sub.domain.invalid", "password": validPassword,
	})
	if rec.Code != http.StatusCreated {
		t.Errorf("a valid address was rejected: %s", rec.Body.String())
	}
}

func TestRegisterRejectsAMalformedBody(t *testing.T) {
	f := newAPIFixture(t)

	cases := []struct {
		name string
		body string
		code api.Code
	}{
		{"not JSON", "this is not JSON", api.CodeMalformedJSON},
		{"empty", "", api.CodeMalformedJSON},
		{"two objects", `{"name":"a"}{"name":"b"}`, api.CodeMalformedJSON},
		{"wrong type", `{"name": 42, "password": "x"}`, api.CodeInvalidField},
		{"unknown field", `{"diplay_name": "typo", "password": "x"}`, api.CodeInvalidField},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := f.postRaw("/auth/register", tc.body)
			expectError(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}

// A client that sends "diplay_name" has made a mistake; ignoring it silently
// would mean their change appears to succeed and does nothing.
func TestUnknownFieldNamesTheField(t *testing.T) {
	f := newAPIFixture(t)

	rec := f.postRaw("/auth/register",
		`{"name":"hans","diplay_name":"typo","password":"`+validPassword+`"}`)

	var body errorBody
	decode(t, rec, &body)
	if body.Error.Field != "diplay_name" {
		t.Errorf("field is %q, want the misspelled field name", body.Error.Field)
	}
}
