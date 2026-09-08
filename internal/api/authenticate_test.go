package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/joernott/doenerstag/internal/api"
	"github.com/joernott/doenerstag/internal/auth"
	"github.com/joernott/doenerstag/internal/db"
)

// A request with no credentials is anonymous, not an error: most read endpoints
// are public.
func TestNoCredentialsIsAnonymous(t *testing.T) {
	f := newAPIFixture(t)

	who := f.whoami()
	if who.Authenticated {
		t.Error("a request with no cookie was authenticated")
	}
}

func TestAValidSessionIdentifiesTheCaller(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("anna")

	who := f.whoami(cookies...)
	if !who.Authenticated {
		t.Fatal("a freshly issued session did not authenticate")
	}
	if who.Name != "anna" {
		t.Errorf("identified as %q, want anna", who.Name)
	}
	if who.ViaToken {
		t.Error("a cookie request is reported as token-authenticated")
	}
	if who.IsAdmin {
		t.Error("an ordinary account is reported as an administrator")
	}
	if who.SessionID == "" {
		t.Error("no session id was attached")
	}
}

// Rule 1: the signature. A token this server did not sign is not one of ours,
// which is 2000 rather than 2002 -- the frontend must not tell somebody their
// session timed out when they were never logged in.
func TestAForeignTokenIsNotAuthenticated(t *testing.T) {
	f := newAPIFixture(t)
	f.register("bertil")

	other, err := auth.NewSigner("fedcba9876543210fedcba9876543210")
	if err != nil {
		t.Fatal(err)
	}
	forged, err := other.Issue(auth.Claims{
		UserID:    uuid.MustParse("018f0000-0000-7000-8000-000000000001"),
		SessionID: uuid.MustParse("018f0000-0000-7000-8000-000000000002"),
		IssuedAt:  f.now,
		ExpiresAt: f.now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := f.do(request{
		method: http.MethodGet, path: "/probe",
		cookies: []*http.Cookie{{Name: api.SessionCookieName, Value: forged}},
	})
	expectError(t, rec, http.StatusUnauthorized, api.CodeNotAuthenticated)
}

func TestGarbageInTheCookieIsNotAuthenticated(t *testing.T) {
	f := newAPIFixture(t)

	for _, value := range []string{"not-a-token", "a.b.c", "...."} {
		rec := f.do(request{
			method: http.MethodGet, path: "/probe",
			cookies: []*http.Cookie{{Name: api.SessionCookieName, Value: value}},
		})
		expectError(t, rec, http.StatusUnauthorized, api.CodeNotAuthenticated)
	}
}

// Rule 2: a missing session row means the session was superseded by a newer
// login or ended by logging out. 2003, so the frontend can say which.
func TestASupersededSessionIsRejected(t *testing.T) {
	f := newAPIFixture(t)
	first := f.register("carla")

	// A second login replaces the row the first cookie points at.
	if rec := f.post("/auth/login", map[string]string{
		"name": "carla", "password": validPassword,
	}); rec.Code != http.StatusOK {
		t.Fatalf("the second login failed: %s", rec.Body.String())
	}

	rec := f.do(request{method: http.MethodGet, path: "/probe", cookies: first})
	expectError(t, rec, http.StatusUnauthorized, api.CodeSessionSuperseded)
}

// Rule 3: the absolute lifetime.
//
// The session has to be kept active to reach it, which is the whole point of
// having an absolute timeout as well as an idle one: an idle timeout alone
// would let somebody who clicks something once an hour stay logged in for
// ever. Simply advancing the clock by seven days would test the idle timeout
// again, from a test named after the other one.
func TestAnActiveSessionStillEndsAtItsAbsoluteLifetime(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("dora")

	step := fixtureIdleTimeout / 2
	elapsed := time.Duration(0)

	for elapsed+step < fixtureAbsoluteTimeout {
		f.advance(step)
		elapsed += step
		if who := f.whoami(cookies...); !who.Authenticated {
			t.Fatalf("the session lapsed after %s despite being used every %s",
				elapsed, step)
		}
	}

	// One more step carries it past the absolute expiry, and no amount of
	// activity saves it.
	f.advance(step)
	rec := f.do(request{method: http.MethodGet, path: "/probe", cookies: cookies})
	expectError(t, rec, http.StatusUnauthorized, api.CodeSessionExpired)
}

// Rule 4: the idle timeout, and the row goes with it. Leaving the row would let
// the same token keep answering 2002 forever instead of 2003, and would hold
// the user's one session slot against them.
func TestAnIdleSessionLapsesAndItsRowIsRemoved(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("erik")

	signer, err := auth.NewSigner(fixtureSecret)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := signer.Parse(f.sessionCookie(cookies).Value)
	if err != nil {
		t.Fatal(err)
	}

	f.advance(fixtureIdleTimeout + time.Minute)
	rec := f.do(request{method: http.MethodGet, path: "/probe", cookies: cookies})
	expectError(t, rec, http.StatusUnauthorized, api.CodeSessionExpired)

	if _, err := db.SessionByID(context.Background(), f.pool, claims.SessionID); err == nil {
		t.Error("the lapsed session row was left behind")
	}

	// And the next request on the same cookie is now "superseded", because
	// there is no row at all.
	again := f.do(request{method: http.MethodGet, path: "/probe", cookies: cookies})
	expectError(t, again, http.StatusUnauthorized, api.CodeSessionSuperseded)
}

// Activity resets the idle clock, which is what makes it an idle timeout rather
// than a second absolute one.
func TestActivityKeepsASessionAlive(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("frieda")

	// Four steps of two thirds of the idle timeout each: cumulatively well past
	// it, but never idle for that long at a stretch.
	for i := range 4 {
		f.advance(fixtureIdleTimeout * 2 / 3)
		if who := f.whoami(cookies...); !who.Authenticated {
			t.Fatalf("the session lapsed at step %d despite continuous use", i)
		}
	}
}

// Rule 5: last_seen_at is written, but not on every request.
func TestLastSeenIsThrottled(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("gustav")
	ctx := context.Background()

	signer, err := auth.NewSigner(fixtureSecret)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := signer.Parse(f.sessionCookie(cookies).Value)
	if err != nil {
		t.Fatal(err)
	}

	before, err := db.SessionByID(ctx, f.pool, claims.SessionID)
	if err != nil {
		t.Fatal(err)
	}

	// A burst inside the throttle window must not move it.
	for range 5 {
		f.whoami(cookies...)
	}
	after, err := db.SessionByID(ctx, f.pool, claims.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.LastSeenAt.Equal(before.LastSeenAt) {
		t.Error("a burst of requests wrote last_seen_at more than once")
	}

	// The throttle is measured against the database clock, not the fixture's,
	// so waiting it out here would mean sleeping for a minute. That the write
	// happens once the window has passed is covered by the db package's
	// TestTouchIsThrottled, which can set the throttle to zero.
}

// is_admin comes from the database, not from the token's advisory adm claim.
// A token that claims administrator rights must not get them.
func TestTheAdmClaimIsNotTrusted(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("hanna")

	signer, err := auth.NewSigner(fixtureSecret)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := signer.Parse(f.sessionCookie(cookies).Value)
	if err != nil {
		t.Fatal(err)
	}

	// A properly signed token for the same real session, claiming adm.
	claims.IsAdmin = true
	forged, err := signer.Issue(claims)
	if err != nil {
		t.Fatal(err)
	}

	who := f.whoami(&http.Cookie{Name: api.SessionCookieName, Value: forged})
	if !who.Authenticated {
		t.Fatal("the re-signed token did not authenticate")
	}
	if who.IsAdmin {
		t.Error("a token claiming adm was granted administrator rights")
	}
}

// Deleting the account ends the session, because the row cascades.
func TestASessionDiesWithItsAccount(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("ida")
	ctx := context.Background()

	user, err := db.UserByName(ctx, f.pool, "ida")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteUser(ctx, f.pool, user.ID); err != nil {
		t.Fatal(err)
	}

	rec := f.do(request{method: http.MethodGet, path: "/probe", cookies: cookies})
	expectError(t, rec, http.StatusUnauthorized, api.CodeSessionSuperseded)
}

// The middleware runs on every path, so a public endpoint must still answer for
// an anonymous caller.
func TestAnonymousCallersReachPublicRoutes(t *testing.T) {
	f := newAPIFixture(t)

	rec := f.do(request{method: http.MethodGet, path: "/probe"})
	if rec.Code != http.StatusOK {
		t.Errorf("an anonymous request was refused: %d %s", rec.Code, rec.Body.String())
	}
}
