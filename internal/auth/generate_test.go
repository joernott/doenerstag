package auth_test

import (
	"strings"
	"testing"

	"github.com/joernott/doenerstag/internal/auth"
)

// The property that matters: a generated password is one the application will
// accept. Anything else it does is a detail, but a generated password that
// fails validation creates an account nobody can log into and an administrator
// who has no idea why.
//
// Run enough times to catch a rule that is satisfied only most of the time.
func TestAGeneratedPasswordPassesTheRules(t *testing.T) {
	for range 500 {
		password, err := auth.GeneratePassword()
		if err != nil {
			t.Fatalf("generating: %v", err)
		}
		if err := auth.ValidateComplexity(password); err != nil {
			t.Fatalf("generated %q, which the rules refuse: %v", password, err)
		}
	}
}

func TestAGeneratedPasswordIsTheStatedLength(t *testing.T) {
	password, err := auth.GeneratePassword()
	if err != nil {
		t.Fatal(err)
	}
	if got := len([]rune(password)); got != auth.GeneratedPasswordLength {
		t.Errorf("generated %d characters, want %d", got, auth.GeneratedPasswordLength)
	}
}

// Four classes, not the three the rules require. Meeting the minimum exactly
// would mean a password that stops being valid the moment anybody tightens the
// rules by one.
func TestAGeneratedPasswordUsesFourClasses(t *testing.T) {
	for range 100 {
		password, err := auth.GeneratePassword()
		if err != nil {
			t.Fatal(err)
		}
		if got := len(auth.SatisfiedClasses(password)); got != 4 {
			t.Fatalf("%q satisfies %d classes, want 4", password, got)
		}
	}
}

// Nothing that has to be read off a screen and typed should contain a glyph
// that looks like another glyph.
func TestAGeneratedPasswordAvoidsAmbiguousCharacters(t *testing.T) {
	const ambiguous = "IlO01o"

	for range 200 {
		password, err := auth.GeneratePassword()
		if err != nil {
			t.Fatal(err)
		}
		if i := strings.IndexAny(password, ambiguous); i >= 0 {
			t.Fatalf("%q contains %q, which is easy to transcribe wrongly",
				password, password[i])
		}
	}
}

// The first four characters used to be one from each alphabet in a fixed order.
// Shuffling is what stops the shape of every password being public knowledge,
// and a shuffle that silently did nothing would leave no other trace.
func TestGeneratedPasswordsAreShuffled(t *testing.T) {
	// If the order were fixed, every password would start with an upper-case
	// letter. Over this many draws, that happening by chance is not worth
	// worrying about.
	sawSomethingElse := false
	for range 100 {
		password, err := auth.GeneratePassword()
		if err != nil {
			t.Fatal(err)
		}
		first := rune(password[0])
		if first < 'A' || first > 'Z' {
			sawSomethingElse = true
			break
		}
	}
	if !sawSomethingElse {
		t.Error("every password began with an upper-case letter; the shuffle is not shuffling")
	}
}

func TestGeneratedPasswordsDiffer(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		password, err := auth.GeneratePassword()
		if err != nil {
			t.Fatal(err)
		}
		if seen[password] {
			t.Fatalf("generated %q twice", password)
		}
		seen[password] = true
	}
}

// The reset token is the secret in a password reset link: anybody holding it
// can set the password of the account it belongs to.
func TestResetTokensAreLongUniqueAndURLSafe(t *testing.T) {
	seen := map[string]bool{}
	for range 500 {
		token, err := auth.GenerateResetToken()
		if err != nil {
			t.Fatal(err)
		}
		if seen[token] {
			t.Fatalf("generated the reset token %q twice", token)
		}
		seen[token] = true

		// 32 bytes as base64url without padding.
		if len(token) != 43 {
			t.Fatalf("token %q is %d characters, want 43", token, len(token))
		}
		// It goes in a URL path, and a mail client will have had its way with
		// the message before anybody clicks it.
		if strings.ContainsAny(token, "+/=?&#% ") {
			t.Fatalf("token %q contains a character that does not survive a URL", token)
		}
	}
}
