package api_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/joernott/doenerstag/internal/api"
	"github.com/joernott/doenerstag/internal/auth"
)

// registerWithEmail makes an account that can actually be sent a reset link.
func registerWithEmail(f *apiFixture, name, email string) {
	f.t.Helper()

	rec := f.post("/auth/register", map[string]string{
		"name": name, "display_name": name, "email": email, "password": validPassword,
	})
	if rec.Code != http.StatusCreated {
		f.t.Fatalf("registering %s: %s", name, rec.Body.String())
	}
}

// The whole flow, end to end, through the link that was actually mailed rather
// than a token the test minted for itself.
func TestAForgottenPasswordCanBeResetFromTheLinkThatWasSent(t *testing.T) {
	f := newAPIFixture(t)
	registerWithEmail(f, "hanna", "hanna@example.invalid")

	rec := f.post("/auth/password-reset", map[string]string{"name": "hanna"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("asking for a reset: %d %s", rec.Code, rec.Body.String())
	}

	link := f.mail.resetLink(t)
	if !strings.HasPrefix(link, "https://doener.test/reset-password/") {
		t.Errorf("the link does not point at this installation: %s", link)
	}

	const newPassword = "Ein-Ganz-Neues-Passwort9"
	done := f.post("/auth/password-reset/"+f.mail.resetToken(t),
		map[string]string{"password": newPassword})
	if done.Code != http.StatusOK {
		t.Fatalf("completing the reset: %d %s", done.Code, done.Body.String())
	}

	// The new password works.
	login := f.post("/auth/login", map[string]string{"name": "hanna", "password": newPassword})
	if login.Code != http.StatusOK {
		t.Errorf("the new password does not log in: %d %s", login.Code, login.Body.String())
	}

	// And the old one does not.
	old := f.post("/auth/login", map[string]string{"name": "hanna", "password": validPassword})
	if old.Code == http.StatusOK {
		t.Error("the old password still logs in after a reset")
	}
}

// The address is accepted as well as the name: somebody who has forgotten their
// password should not also have to remember which of the two they registered
// with.
func TestAResetCanBeAskedForByEmailAddress(t *testing.T) {
	f := newAPIFixture(t)
	registerWithEmail(f, "ingo", "Ingo@Example.invalid")

	// Different case, because an address is case-insensitive and telling
	// somebody otherwise is telling them their account does not exist.
	rec := f.post("/auth/password-reset", map[string]string{"name": "ingo@example.INVALID"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("asking for a reset: %d %s", rec.Code, rec.Body.String())
	}
	if got := len(f.mail.messages()); got != 1 {
		t.Fatalf("%d messages were sent, want 1", got)
	}
}

// The property this endpoint exists to protect. An intranet tool's account list
// is a staff list, and an endpoint that answers differently for a name that
// exists is a way to read it out one guess at a time.
func TestAResetRequestSaysNothingAboutWhetherAnAccountExists(t *testing.T) {
	f := newAPIFixture(t)
	registerWithEmail(f, "jonas", "jonas@example.invalid")

	existing := f.post("/auth/password-reset", map[string]string{"name": "jonas"})
	fake := f.post("/auth/password-reset", map[string]string{"name": "nobody-by-that-name"})

	if existing.Code != fake.Code {
		t.Errorf("an existing account answered %d and a missing one %d",
			existing.Code, fake.Code)
	}
	if existing.Body.String() != fake.Body.String() {
		t.Errorf("the two answers differ:\n existing: %s\n missing : %s",
			existing.Body.String(), fake.Body.String())
	}
}

// An account with no address is the same case again: saying "that account has
// no e-mail address" is saying the account exists.
func TestAnAccountWithNoAddressAnswersTheSameWay(t *testing.T) {
	f := newAPIFixture(t)
	f.register("kai") // no e-mail address

	withoutAddress := f.post("/auth/password-reset", map[string]string{"name": "kai"})
	missing := f.post("/auth/password-reset", map[string]string{"name": "no-such-person"})

	if withoutAddress.Code != missing.Code ||
		withoutAddress.Body.String() != missing.Body.String() {
		t.Errorf("an addressless account is distinguishable from a missing one:\n %s\n %s",
			withoutAddress.Body.String(), missing.Body.String())
	}
	if got := len(f.mail.messages()); got != 0 {
		t.Errorf("%d messages were sent for an account with no address", got)
	}
}

// A link works once. It is the whole reason the server keeps anything in memory
// at all.
func TestAResetLinkWorksOnce(t *testing.T) {
	f := newAPIFixture(t)
	registerWithEmail(f, "lena", "lena@example.invalid")

	if rec := f.post("/auth/password-reset", map[string]string{"name": "lena"}); rec.Code != http.StatusAccepted {
		t.Fatal(rec.Body.String())
	}
	token := f.mail.resetToken(t)

	first := f.post("/auth/password-reset/"+token, map[string]string{"password": "Erstes-Neues-Passwort7"})
	if first.Code != http.StatusOK {
		t.Fatalf("the first use failed: %s", first.Body.String())
	}

	second := f.post("/auth/password-reset/"+token, map[string]string{"password": "Zweites-Neues-Passwort8"})
	expectError(t, second, http.StatusBadRequest, api.CodeResetInvalid)

	// And the second password was not set.
	login := f.post("/auth/login", map[string]string{"name": "lena", "password": "Zweites-Neues-Passwort8"})
	if login.Code == http.StatusOK {
		t.Error("the second password was set despite the link being spent")
	}
}

// A link that is rejected for a weak password is not spent. Burning it would
// mean asking for another one, having done nothing wrong.
func TestAWeakPasswordDoesNotBurnTheLink(t *testing.T) {
	f := newAPIFixture(t)
	registerWithEmail(f, "mila", "mila@example.invalid")

	if rec := f.post("/auth/password-reset", map[string]string{"name": "mila"}); rec.Code != http.StatusAccepted {
		t.Fatal(rec.Body.String())
	}
	token := f.mail.resetToken(t)

	weak := f.post("/auth/password-reset/"+token, map[string]string{"password": "short"})
	expectError(t, weak, http.StatusBadRequest, api.CodePasswordTooWeak)

	good := f.post("/auth/password-reset/"+token, map[string]string{"password": "Ein-Gutes-Passwort4"})
	if good.Code != http.StatusOK {
		t.Errorf("the link was spent by a rejected password: %s", good.Body.String())
	}
}

func TestAnExpiredResetLinkSaysSo(t *testing.T) {
	f := newAPIFixture(t)
	registerWithEmail(f, "nora", "nora@example.invalid")

	if rec := f.post("/auth/password-reset", map[string]string{"name": "nora"}); rec.Code != http.StatusAccepted {
		t.Fatal(rec.Body.String())
	}
	token := f.mail.resetToken(t)

	f.advance(auth.ResetLifetime + time.Minute)

	rec := f.post("/auth/password-reset/"+token, map[string]string{"password": "Noch-Ein-Passwort5"})
	// Expired rather than invalid: one can be asked for again, the other means
	// the mail client mangled the link, and a person reacts differently.
	expectError(t, rec, http.StatusBadRequest, api.CodeResetExpired)
}

func TestAForgedResetLinkIsRefused(t *testing.T) {
	f := newAPIFixture(t)
	registerWithEmail(f, "olaf", "olaf@example.invalid")

	if rec := f.post("/auth/password-reset", map[string]string{"name": "olaf"}); rec.Code != http.StatusAccepted {
		t.Fatal(rec.Body.String())
	}
	token := f.mail.resetToken(t)

	for _, forged := range []string{
		token[:len(token)-1] + "X", // a signature one character out
		"not-a-token",
		strings.SplitN(token, "~", 2)[0], // the payload with no signature at all
	} {
		rec := f.post("/auth/password-reset/"+forged,
			map[string]string{"password": "Ein-Anderes-Passwort6"})
		if rec.Code == http.StatusOK {
			t.Errorf("a forged link was accepted: %s", forged)
		}
	}
}

// A session token must never work as a reset link. Both are signed with the
// same secret, and the purpose prefix is what keeps them apart.
func TestASessionTokenIsNotAResetLink(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("pia")

	rec := f.post("/auth/password-reset/"+f.sessionCookie(cookies).Value,
		map[string]string{"password": "Ein-Neues-Passwort3"})
	if rec.Code == http.StatusOK {
		t.Fatal("a session cookie was accepted as a password reset link")
	}
}

// Resetting a password ends every session of that account. Somebody may be
// resetting precisely because someone else has been using theirs.
func TestAResetEndsEveryExistingSession(t *testing.T) {
	f := newAPIFixture(t)
	registerWithEmail(f, "quirin", "quirin@example.invalid")

	login := f.post("/auth/login", map[string]string{"name": "quirin", "password": validPassword})
	if login.Code != http.StatusOK {
		t.Fatal(login.Body.String())
	}
	cookies := login.Result().Cookies()
	if who := f.whoami(cookies...); !who.Authenticated {
		t.Fatal("the fixture did not end up logged in")
	}

	if rec := f.post("/auth/password-reset", map[string]string{"name": "quirin"}); rec.Code != http.StatusAccepted {
		t.Fatal(rec.Body.String())
	}
	done := f.post("/auth/password-reset/"+f.mail.resetToken(t),
		map[string]string{"password": "Wieder-Ein-Passwort2"})
	if done.Code != http.StatusOK {
		t.Fatal(done.Body.String())
	}

	if who := f.whoami(cookies...); who.Authenticated {
		t.Error("the session that existed before the reset still works")
	}
}

// A working link must not be written to the log: a log line carrying one is a
// credential readable by anybody who can read logs.
func TestTheResetLinkIsNotLogged(t *testing.T) {
	f := newAPIFixture(t)
	registerWithEmail(f, "rosa", "rosa@example.invalid")

	logged := f.captureLog(func() {
		if rec := f.post("/auth/password-reset", map[string]string{"name": "rosa"}); rec.Code != http.StatusAccepted {
			t.Fatal(rec.Body.String())
		}
	})

	token := f.mail.resetToken(t)
	if strings.Contains(logged, token) {
		t.Errorf("the reset token was written to the log:\n%s", logged)
	}
}
