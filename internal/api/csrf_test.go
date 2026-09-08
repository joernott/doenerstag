package api_test

import (
	"net/http"
	"testing"

	"github.com/joernott/doenerstag/internal/api"
)

// The central case: a cookie-authenticated write without the header is refused.
func TestAWriteWithoutTheCSRFHeaderIsRefused(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("anna")

	rec := f.do(request{
		method: http.MethodPost, path: "/probe",
		cookies: cookies, omitCSRF: true,
	})
	expectError(t, rec, http.StatusForbidden, api.CodeInvalidCSRF)
}

// And with it, the same request succeeds.
func TestAWriteWithTheCSRFHeaderSucceeds(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("bertil")

	rec := f.do(request{method: http.MethodPost, path: "/probe", cookies: cookies})
	if rec.Code != http.StatusOK {
		t.Errorf("status %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
}

// A wrong value is as bad as a missing one -- this is the case an attacker who
// can guess produces, and the reason the comparison exists at all.
func TestAWrongCSRFTokenIsRefused(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("carla")

	csrf := ""
	for _, c := range cookies {
		if c.Name == api.CSRFCookieName {
			csrf = c.Value
		}
	}
	if csrf == "" {
		t.Fatal("no CSRF cookie was issued")
	}

	cases := map[string]string{
		"empty":            "",
		"another token":    "IiAr2vJXcQ7-EXAMPLE-not-the-real-token-value",
		"one byte short":   csrf[:len(csrf)-1],
		"one byte extra":   csrf + "x",
		"differing at end": csrf[:len(csrf)-1] + flipLast(csrf),
	}

	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			rec := f.do(request{
				method: http.MethodPost, path: "/probe",
				cookies:  cookies,
				omitCSRF: true,
				headers:  map[string]string{api.CSRFHeaderName: value},
			})
			expectError(t, rec, http.StatusForbidden, api.CodeInvalidCSRF)
		})
	}
}

// Sending the header without the cookie must not pass. It is the pair that
// proves the request came from a page that could read this origin's cookies;
// either half alone proves nothing.
func TestTheHeaderAloneIsNotEnough(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("dora")

	var session *http.Cookie
	for _, c := range cookies {
		if c.Name == api.SessionCookieName {
			session = c
		}
	}

	rec := f.do(request{
		method: http.MethodPost, path: "/probe",
		cookies:  []*http.Cookie{session},
		omitCSRF: true,
		headers:  map[string]string{api.CSRFHeaderName: "anything at all"},
	})
	expectError(t, rec, http.StatusForbidden, api.CodeInvalidCSRF)
}

// Two empty strings must not compare equal, which is exactly the state a
// request with neither cookie nor header arrives in.
func TestNeitherCookieNorHeaderIsRefused(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("erik")

	var session *http.Cookie
	for _, c := range cookies {
		if c.Name == api.SessionCookieName {
			session = c
		}
	}

	rec := f.do(request{
		method: http.MethodPost, path: "/probe",
		cookies: []*http.Cookie{session}, omitCSRF: true,
	})
	expectError(t, rec, http.StatusForbidden, api.CodeInvalidCSRF)
}

// GET must never need the header: requiring one would break every ordinary
// navigation, and a safe method changes nothing to protect.
func TestSafeMethodsAreExempt(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("frieda")

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		rec := f.do(request{
			method: method, path: "/probe",
			cookies: cookies, omitCSRF: true,
		})
		if rec.Code == http.StatusForbidden {
			t.Errorf("%s was refused for want of a CSRF token", method)
		}
	}
}

// An anonymous write carries no ambient credential, so there is nothing for a
// foreign site to abuse. It is refused by authorization instead, with the
// accurate reason: nobody is logged in.
func TestAnAnonymousWriteIsNotACSRFFailure(t *testing.T) {
	f := newAPIFixture(t)

	rec := f.do(request{method: http.MethodPost, path: "/probe", omitCSRF: true})
	if rec.Code == http.StatusForbidden {
		var body errorBody
		decode(t, rec, &body)
		if api.Code(body.Error.Code) == api.CodeInvalidCSRF {
			t.Error("an anonymous request was refused for want of a CSRF token")
		}
	}
}

// Logging in must not require a token the caller cannot have yet.
func TestLoginAndRegisterDoNotNeedACSRFToken(t *testing.T) {
	f := newAPIFixture(t)

	registered := f.do(request{
		method: http.MethodPost, path: "/auth/register",
		body:     map[string]string{"name": "gustav", "password": validPassword},
		omitCSRF: true,
	})
	if registered.Code != http.StatusCreated {
		t.Errorf("registration needed a CSRF token: %d %s",
			registered.Code, registered.Body.String())
	}

	loggedIn := f.do(request{
		method: http.MethodPost, path: "/auth/login",
		body:     map[string]string{"name": "gustav", "password": validPassword},
		omitCSRF: true,
	})
	if loggedIn.Code != http.StatusOK {
		t.Errorf("login needed a CSRF token: %d %s", loggedIn.Code, loggedIn.Body.String())
	}
}

// A CSRF token from an earlier session must not work with a newer one: each
// login issues a fresh pair, and mixing them is the state a stale tab is in.
func TestACSRFTokenFromAnotherSessionIsRefused(t *testing.T) {
	f := newAPIFixture(t)
	first := f.register("hanna")

	second := f.post("/auth/login", map[string]string{
		"name": "hanna", "password": validPassword,
	})
	if second.Code != http.StatusOK {
		t.Fatalf("the second login failed: %s", second.Body.String())
	}

	oldCSRF := ""
	for _, c := range first {
		if c.Name == api.CSRFCookieName {
			oldCSRF = c.Value
		}
	}

	rec := f.do(request{
		method: http.MethodPost, path: "/probe",
		cookies:  second.Result().Cookies(),
		omitCSRF: true,
		headers:  map[string]string{api.CSRFHeaderName: oldCSRF},
	})
	expectError(t, rec, http.StatusForbidden, api.CodeInvalidCSRF)
}

// flipLast returns the last character of s changed, for building a value that
// differs from the original only at the end.
func flipLast(s string) string {
	last := s[len(s)-1]
	if last == 'a' {
		return "b"
	}
	return "a"
}
