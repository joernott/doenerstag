package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
)

// CSRFTokenBytes is the entropy in a CSRF token, per
// docs/05_auth_and_permissions.md.
const CSRFTokenBytes = 32

// GenerateCSRFToken returns a new token, base64url-encoded.
//
// It is issued with the session and travels in a cookie the frontend can read,
// which is the readable half of the double-submit pair. Its only job is to be
// unguessable by a foreign origin: a cross-site request can cause the browser
// to send the cookie, but cannot read it to copy into the header.
func GenerateCSRFToken() (string, error) {
	raw := make([]byte, CSRFTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generating a CSRF token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// EqualCSRFToken compares a cookie value with a header value in constant time.
//
// Constant time because the comparison is against a secret the attacker is
// trying to produce. A byte-at-a-time comparison leaks how much of a guess was
// right, which over enough requests is enough to construct the whole value.
//
// Empty never matches, including empty against empty: a request with neither
// cookie nor header must fail the check rather than pass it by symmetry, which
// is exactly the case a missing session would produce.
func EqualCSRFToken(cookie, header string) bool {
	if cookie == "" || header == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie), []byte(header)) == 1
}
