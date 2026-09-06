package api_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/joernott/doenerstag/internal/api"
)

// Failures against one name are counted, and the one past the limit is refused
// with 5000 and a Retry-After. The shipped limit is ten per fifteen minutes;
// see the note on fixturePerName for why the test uses a smaller one.
func TestLoginIsLimitedPerUserName(t *testing.T) {
	f := newAPIFixture(t)
	f.limitLogins()
	f.register("anna")

	for i := range fixturePerName {
		rec := f.post("/auth/login", map[string]string{
			"name": "anna", "password": "wrong",
		})
		expectError(t, rec, http.StatusUnauthorized, api.CodeInvalidLogin)
		if rec.Header().Get("Retry-After") != "" {
			t.Errorf("attempt %d was refused before the limit", i+1)
		}
	}

	rec := f.post("/auth/login", map[string]string{
		"name": "anna", "password": "wrong",
	})
	expectError(t, rec, http.StatusTooManyRequests, api.CodeTooManyLogins)

	retryAfter := rec.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("no Retry-After header")
	}
	seconds, err := strconv.Atoi(retryAfter)
	if err != nil || seconds < 1 {
		t.Errorf("Retry-After is %q, want a positive number of seconds", retryAfter)
	}
	if seconds > int(fixtureWindow.Seconds()) {
		t.Errorf("Retry-After is %d seconds, longer than the %s window",
			seconds, fixtureWindow)
	}
}

// The correct password is refused too once the limit is reached, or the limit
// would only slow down somebody who is already failing.
func TestTheLimitAppliesToTheRightPasswordToo(t *testing.T) {
	f := newAPIFixture(t)
	f.limitLogins()
	f.register("bertil")

	for range fixturePerName {
		f.post("/auth/login", map[string]string{"name": "bertil", "password": "wrong"})
	}

	rec := f.post("/auth/login", map[string]string{
		"name": "bertil", "password": validPassword,
	})
	expectError(t, rec, http.StatusTooManyRequests, api.CodeTooManyLogins)
}

// A successful login clears the name's counter: somebody who mistypes and then
// gets it right has shown they are who they say they are.
func TestASuccessfulLoginForgivesEarlierFailures(t *testing.T) {
	f := newAPIFixture(t)
	f.limitLogins()
	f.register("carla")

	for range fixturePerName - 1 {
		f.post("/auth/login", map[string]string{"name": "carla", "password": "wrong"})
	}

	if rec := f.post("/auth/login", map[string]string{
		"name": "carla", "password": validPassword,
	}); rec.Code != http.StatusOK {
		t.Fatalf("the correct password was refused: %s", rec.Body.String())
	}

	// The allowance is back: another full run of failures is tolerated.
	for i := range fixturePerName {
		rec := f.post("/auth/login", map[string]string{"name": "carla", "password": "wrong"})
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d after a success was refused; the counter was not cleared", i+1)
		}
	}
}

// One account being attacked must not lock everybody else out.
func TestTheNameLimitIsPerName(t *testing.T) {
	f := newAPIFixture(t)
	f.limitLogins()
	f.register("dora")
	f.register("erik")

	for range fixturePerName + 1 {
		f.post("/auth/login", map[string]string{"name": "dora", "password": "wrong"})
	}

	if rec := f.post("/auth/login", map[string]string{
		"name": "erik", "password": validPassword,
	}); rec.Code != http.StatusOK {
		t.Errorf("one account's lockout blocked another: %d %s", rec.Code, rec.Body.String())
	}
}

// The name is folded to lower case, matching the case-insensitive login.
// Otherwise alternating Anna, anna and ANNA would buy three times the attempts.
func TestTheNameLimitIgnoresCase(t *testing.T) {
	f := newAPIFixture(t)
	f.limitLogins()
	f.register("frieda")

	for i := range fixturePerName {
		name := "frieda"
		if i%2 == 0 {
			name = "FRIEDA"
		}
		f.post("/auth/login", map[string]string{"name": name, "password": "wrong"})
	}

	rec := f.post("/auth/login", map[string]string{
		"name": "Frieda", "password": "wrong",
	})
	expectError(t, rec, http.StatusTooManyRequests, api.CodeTooManyLogins)
}

// The per-address limit catches a script working through a list of names, which
// the per-name counter never sees because each name has only one attempt.
func TestLoginIsLimitedPerClientAddress(t *testing.T) {
	f := newAPIFixture(t)
	f.limitLogins()

	// Each attempt uses a different name, so no name counter comes near its
	// limit. All of them share the fixture's peer address.
	for i := range fixturePerAddress {
		rec := f.post("/auth/login", map[string]string{
			"name": "nobody" + strconv.Itoa(i), "password": "wrong",
		})
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d was refused before the address limit", i+1)
		}
	}

	rec := f.post("/auth/login", map[string]string{
		"name": "somebody-else", "password": "wrong",
	})
	expectError(t, rec, http.StatusTooManyRequests, api.CodeTooManyLogins)
}

// A shared address is the normal case here -- one office behind one NAT -- so a
// success must not clear the address counter that is protecting everybody on
// it.
func TestASuccessDoesNotClearTheAddressCounter(t *testing.T) {
	f := newAPIFixture(t)
	f.limitLogins()
	f.register("gustav")

	for i := range fixturePerAddress - 1 {
		f.post("/auth/login", map[string]string{
			"name": "nobody" + strconv.Itoa(i), "password": "wrong",
		})
	}

	if rec := f.post("/auth/login", map[string]string{
		"name": "gustav", "password": validPassword,
	}); rec.Code != http.StatusOK {
		t.Fatalf("a valid login was refused: %s", rec.Body.String())
	}

	// One more failure reaches the address limit, and the next is refused.
	f.post("/auth/login", map[string]string{"name": "nobody-more", "password": "wrong"})
	rec := f.post("/auth/login", map[string]string{"name": "nobody-again", "password": "wrong"})
	expectError(t, rec, http.StatusTooManyRequests, api.CodeTooManyLogins)
}

// The window elapses and the allowance returns.
func TestTheLimitLapsesAfterTheWindow(t *testing.T) {
	f := newAPIFixture(t)
	f.limitLogins()
	f.register("hanna")

	for range fixturePerName + 1 {
		f.post("/auth/login", map[string]string{"name": "hanna", "password": "wrong"})
	}
	if rec := f.post("/auth/login", map[string]string{
		"name": "hanna", "password": validPassword,
	}); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the limit was not in force: %d", rec.Code)
	}

	f.advance(fixtureWindow + time.Second)

	if rec := f.post("/auth/login", map[string]string{
		"name": "hanna", "password": validPassword,
	}); rec.Code != http.StatusOK {
		t.Errorf("the limit did not lapse after %s: %d %s",
			fixtureWindow, rec.Code, rec.Body.String())
	}
}

// Registration is not rate limited: docs/05_auth_and_permissions.md limits the
// login endpoint and says everything else is unprotected, which is appropriate
// for the deployment context.
func TestOnlyLoginIsLimited(t *testing.T) {
	f := newAPIFixture(t)
	f.limitLogins()

	for i := range fixturePerAddress + 5 {
		rec := f.post("/auth/register", map[string]string{
			"name": "person" + strconv.Itoa(i), "password": validPassword,
		})
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("registration %d was rate limited", i+1)
		}
	}
}

// The limiter is shared between requests, so it has to be safe to use from
// several at once. The race detector is what actually checks this; the test
// exists to give it something to look at.
func TestTheLimiterIsSafeForConcurrentUse(t *testing.T) {
	limiter := &api.LoginLimiter{
		PerName: 10, PerAddress: 60, Window: time.Minute,
	}

	done := make(chan struct{})
	for i := range 8 {
		go func(n int) {
			defer func() { done <- struct{}{} }()
			name := "user" + strconv.Itoa(n%3)
			for range 50 {
				limiter.Allow(name, "192.0.2.1")
				limiter.RecordFailure(name, "192.0.2.1")
				limiter.RecordSuccess(name)
			}
		}(i)
	}
	for range 8 {
		<-done
	}
}
