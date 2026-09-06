package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// APITokenBytes is the entropy in an API token, per
// docs/05_auth_and_permissions.md.
const APITokenBytes = 32

// APITokenPrefixLength is how much of a token is stored alongside its hash, so
// a user can tell two of their tokens apart in a list.
//
// Eight base64url characters are six bytes of the token. That is far too little
// to guess the rest from, and enough that a user with several tokens can see
// which one a script is using.
const APITokenPrefixLength = 8

// ErrMalformedToken is returned for a value that cannot be a token at all, so
// that the caller can answer without a database round trip.
var ErrMalformedToken = errors.New("auth: malformed API token")

// GeneratedAPIToken is the result of creating a token: the value, which is
// shown to the user exactly once, and the two things that are stored.
type GeneratedAPIToken struct {
	// Value is the plaintext token. It is never persisted and never logged.
	Value string
	// Hash is the SHA-256 of Value, hex-encoded.
	Hash string
	// Prefix is the first APITokenPrefixLength characters of Value.
	Prefix string
}

// GenerateAPIToken creates a token.
//
// SHA-256 rather than Argon2id, deliberately. Argon2id exists to make guessing
// a human-chosen password expensive; this value is 32 bytes from crypto/rand,
// so there is nothing to guess and a slow hash would only add latency to every
// authenticated request. The reasoning does not carry over to passwords, which
// is why they use Argon2id and this does not.
func GenerateAPIToken() (GeneratedAPIToken, error) {
	raw := make([]byte, APITokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return GeneratedAPIToken{}, fmt.Errorf("generating an API token: %w", err)
	}

	value := base64.RawURLEncoding.EncodeToString(raw)
	return GeneratedAPIToken{
		Value:  value,
		Hash:   HashAPIToken(value),
		Prefix: value[:APITokenPrefixLength],
	}, nil
}

// HashAPIToken is the one-way function the stored hash is made with. Lookup
// hashes the presented value and searches for it, so the plaintext never
// reaches the database.
func HashAPIToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// ParseBearer extracts the token from an Authorization header value.
//
// The scheme is matched case-insensitively, as RFC 7235 requires. An empty
// header returns ErrMalformedToken rather than an empty token, so that a caller
// cannot accidentally look up the hash of the empty string.
func ParseBearer(header string) (string, error) {
	const scheme = "bearer "
	if len(header) <= len(scheme) || !strings.EqualFold(header[:len(scheme)], scheme) {
		return "", ErrMalformedToken
	}

	value := strings.TrimSpace(header[len(scheme):])
	if value == "" {
		return "", ErrMalformedToken
	}
	// A token is base64url of 32 bytes: 43 characters, no padding. Rejecting
	// anything else here keeps obviously wrong input from reaching the database
	// as a lookup.
	if len(value) != base64.RawURLEncoding.EncodedLen(APITokenBytes) {
		return "", ErrMalformedToken
	}
	if _, err := base64.RawURLEncoding.DecodeString(value); err != nil {
		return "", ErrMalformedToken
	}
	return value, nil
}
