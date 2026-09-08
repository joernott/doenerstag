package auth

import (
	"crypto/rand"
	"encoding/base64"
	"math/big"
)

// GeneratedPasswordLength is how long a generated password is.
//
// Twenty rather than the ten the rules demand as a minimum: this password is
// read off a terminal and typed once, or pasted, and nobody has to remember it.
// The cost of the extra ten characters falls entirely on an attacker.
const GeneratedPasswordLength = 20

// The alphabets a generated password draws from, one per character class.
//
// Class 5 is deliberately absent. It is defined by exclusion -- any printable
// character the first four classes do not cover -- so it would admit characters
// that survive a terminal, a copy-paste and a keyboard layout only by luck. A
// password an administrator has to read out over the telephone is not the place
// to exercise the widest part of the specification. Three classes are required
// and four are used.
var (
	generatorUpper   = []rune("ABCDEFGHJKLMNPQRSTUVWXYZ")
	generatorLower   = []rune("abcdefghijkmnpqrstuvwxyz")
	generatorDigit   = []rune("23456789")
	generatorSpecial = []rune("<>|-_.:,;#!$%&?@")
)

// The characters left out of the alphabets above are the ones that look like
// each other: I, l and 1, and O, o and 0. Somebody transcribing a generated
// password from a screen should not have to guess which of three glyphs they
// are looking at.

// GeneratePassword returns a password that satisfies the complexity rules.
//
// It draws one character from each of the four alphabets first, so the result
// cannot fail the rules by chance, then fills the rest from all of them and
// shuffles. Filling first and hoping would pass almost always and fail rarely,
// which is the worst of both: an administrator would meet the failure once, in
// production, with no way to reproduce it.
func GeneratePassword() (string, error) {
	alphabets := [][]rune{generatorUpper, generatorLower, generatorDigit, generatorSpecial}

	var all []rune
	for _, alphabet := range alphabets {
		all = append(all, alphabet...)
	}

	out := make([]rune, 0, GeneratedPasswordLength)
	for _, alphabet := range alphabets {
		r, err := pick(alphabet)
		if err != nil {
			return "", err
		}
		out = append(out, r)
	}
	for len(out) < GeneratedPasswordLength {
		r, err := pick(all)
		if err != nil {
			return "", err
		}
		out = append(out, r)
	}

	// Without this the first four characters would always be upper, lower,
	// digit, special, in that order, which hands an attacker the shape of every
	// password this ever generates.
	if err := shuffle(out); err != nil {
		return "", err
	}

	password := string(out)

	// The rules are the authority, not this function's reasoning about them. If
	// the two ever disagree -- someone tightens ValidateComplexity, someone
	// edits an alphabet -- the failure belongs here, loudly, and not in an
	// account nobody can log into.
	if err := ValidateComplexity(password); err != nil {
		return "", err
	}
	return password, nil
}

// pick chooses one rune uniformly at random.
func pick(from []rune) (rune, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(from))))
	if err != nil {
		return 0, err
	}
	return from[n.Int64()], nil
}

// shuffle is Fisher-Yates over crypto/rand.
//
// math/rand would be seeded per process, which for a verb that runs once and
// exits means every installation generating the same sequence.
func shuffle(runes []rune) error {
	for i := len(runes) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return err
		}
		runes[i], runes[j.Int64()] = runes[j.Int64()], runes[i]
	}
	return nil
}

// GenerateResetToken returns the identifier that appears in a password reset
// link.
//
// It is the secret: anybody holding it can set the password of the account it
// belongs to, so it is generated the same way a session secret is and is long
// enough that guessing is not a strategy. base64url, so it survives being a
// path segment in a mail client that has reformatted the message.
func GenerateResetToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
