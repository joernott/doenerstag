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

func TestLogoutEndsTheSession(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("anna")

	signer, err := auth.NewSigner(fixtureSecret)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := signer.Parse(f.sessionCookie(cookies).Value)
	if err != nil {
		t.Fatal(err)
	}

	rec := f.do(request{method: http.MethodPost, path: "/auth/logout", cookies: cookies})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	var body sessionResponse
	decode(t, rec, &body)
	if body.User != nil {
		t.Errorf("logout returned a user: %+v", body.User)
	}

	// The row is gone.
	if _, err := db.SessionByID(context.Background(), f.pool, claims.SessionID); err == nil {
		t.Error("the session row survived logout")
	}

	// And the same cookie no longer authenticates. It is anonymity rather than
	// an error, and /auth/session is where the reason is reported.
	f.expectEnded(api.CodeSessionSuperseded, cookies...)
}

// Both cookies must be expired, with attributes matching the ones they were set
// with: a browser keys a cookie on name, domain and path, so a deletion that
// differs would leave the original in place and the logout would do nothing.
func TestLogoutClearsBothCookies(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("bertil")

	rec := f.do(request{method: http.MethodPost, path: "/auth/logout", cookies: cookies})

	for _, name := range []string{api.SessionCookieName, api.CSRFCookieName} {
		cleared := cookie(rec, name)
		if cleared == nil {
			t.Errorf("%s was not cleared", name)
			continue
		}
		if cleared.Value != "" {
			t.Errorf("%s still has a value: %q", name, cleared.Value)
		}
		if cleared.MaxAge >= 0 {
			t.Errorf("%s has Max-Age %d, want a negative value", name, cleared.MaxAge)
		}
		if cleared.Path != "/" {
			t.Errorf("%s is cleared with Path %q, but was set with /", name, cleared.Path)
		}
		if cleared.SameSite != http.SameSiteStrictMode {
			t.Errorf("%s is cleared with SameSite %v, but was set with Strict",
				name, cleared.SameSite)
		}
		if !cleared.Secure {
			t.Errorf("%s is cleared without Secure, but was set with it", name)
		}
	}
}

// Logout is a request for a state, and that state holds whether or not there
// was a session to end.
func TestLogoutWithoutASessionSucceeds(t *testing.T) {
	f := newAPIFixture(t)

	rec := f.do(request{method: http.MethodPost, path: "/auth/logout"})
	if rec.Code != http.StatusOK {
		t.Errorf("status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if cookie(rec, api.SessionCookieName) == nil {
		t.Error("the cookies were not cleared for a caller with no session")
	}
}

// Logging out twice is not an error either.
func TestLogoutIsIdempotent(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("carla")

	first := f.do(request{method: http.MethodPost, path: "/auth/logout", cookies: cookies})
	if first.Code != http.StatusOK {
		t.Fatalf("the first logout failed: %s", first.Body.String())
	}

	// The second carries a cookie whose row is gone. That is anonymity, not an
	// error, so the handler is reached and answers 200 -- logout is a request
	// for a state, and the state holds. It used to be refused by the
	// authentication middleware, which meant the one action that clears a dead
	// cookie was the one action a dead cookie prevented.
	second := f.do(request{method: http.MethodPost, path: "/auth/logout", cookies: cookies})
	if second.Code != http.StatusOK {
		t.Fatalf("the second logout answered %d, want 200: %s", second.Code, second.Body.String())
	}
	if cleared := cookie(second, api.SessionCookieName); cleared == nil || cleared.MaxAge >= 0 {
		t.Error("the second logout did not clear the dead cookie")
	}
}

// Only the caller's own session ends.
func TestLogoutDoesNotEndAnotherUsersSession(t *testing.T) {
	f := newAPIFixture(t)
	mine := f.register("dora")
	theirs := f.register("erik")

	if rec := f.do(request{
		method: http.MethodPost, path: "/auth/logout", cookies: mine,
	}); rec.Code != http.StatusOK {
		t.Fatalf("logout failed: %s", rec.Body.String())
	}

	if who := f.whoami(theirs...); !who.Authenticated || who.Name != "erik" {
		t.Error("logging one user out ended another user's session")
	}
}

func TestSessionEndpointReportsAnonymous(t *testing.T) {
	f := newAPIFixture(t)

	rec := f.do(request{method: http.MethodGet, path: "/auth/session"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var body sessionResponse
	decode(t, rec, &body)
	if body.User != nil {
		t.Errorf("an anonymous caller was reported as %+v", body.User)
	}

	// The JSON must actually contain "user": null, because that is what
	// docs/04_api.md promises and what the frontend branches on.
	if got := rec.Body.String(); !strings.Contains(got, `"user":null`) {
		t.Errorf("the body does not carry an explicit null user: %s", got)
	}
}

func TestSessionEndpointReportsTheCaller(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("frieda")

	rec := f.do(request{method: http.MethodGet, path: "/auth/session", cookies: cookies})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var body sessionResponse
	decode(t, rec, &body)
	if body.User == nil {
		t.Fatal("a logged-in caller was reported as anonymous")
	}
	if body.User.Name != "frieda" {
		t.Errorf("reported as %q", body.User.Name)
	}
	if body.ExpiresAt == "" {
		t.Error("no expiry was reported, so the frontend cannot warn before it lapses")
	}
	if body.User.IsAdmin {
		t.Error("an ordinary account is reported as an administrator")
	}
}

// After logging out, the endpoint the frontend calls on load must say so.
func TestSessionEndpointAfterLogout(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("gustav")

	if rec := f.do(request{
		method: http.MethodPost, path: "/auth/logout", cookies: cookies,
	}); rec.Code != http.StatusOK {
		t.Fatalf("logout failed: %s", rec.Body.String())
	}

	// The browser has been told to drop the cookies, so the next call carries
	// none.
	rec := f.do(request{method: http.MethodGet, path: "/auth/session"})
	var body sessionResponse
	decode(t, rec, &body)
	if body.User != nil {
		t.Error("the session endpoint still reports a user after logout")
	}
}
