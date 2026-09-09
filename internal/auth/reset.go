package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ResetLifetime is how long a password reset link works.
const ResetLifetime = time.Hour

/*
Password reset tokens.

A reset token is the whole credential: whoever holds one can set the password of
the account it names. So the shape of it is the security design, and it is worth
saying why it is this shape rather than a row in a table or an entry in a map.

It is signed rather than stored. The obvious implementation -- generate a random
identifier, keep it in a map for an hour -- cannot work for the whole feature,
because `doenerstag user password` runs in its own process and exits. It has no
way to put anything into the running server's memory, and an administrator who
generates a reset link from the command line expects the server to honour it. A
signed token needs no shared storage: it carries who it is for and when it
stops working, and the signature is what makes those claims trustworthy.

What signing gives up is revocation, and that is bought back in the server's
memory rather than in the token: a token that has been used is remembered until
it would have expired anyway, so a link works once. A restart forgets that,
which means a link could be used twice across a restart, within its hour. That
is a smaller hole than it sounds -- the holder already had a working link -- and
it is the honest cost of a mechanism that the command line can take part in.

Domain separation matters here. The secret is the same one that signs sessions,
so the payload is prefixed with a purpose that a session token does not have. A
session token must never be usable as a reset token, and the prefix is what
makes that structural rather than a matter of the two formats happening to
differ.
*/
const resetPurpose = "doenerstag-password-reset-v1"

// resetSeparator divides the payload from its signature.
//
// A tilde rather than the obvious dot, and not for taste: a reset token is a
// path segment, and the SPA fallback treats a final segment containing a dot as
// a request for a file and answers 404 rather than serving the application. A
// dot here made every reset link in every mail land on a JSON error envelope.
// Tilde is outside the base64url alphabet, so it cannot occur inside either
// half, and it needs no escaping in a URL.
const resetSeparator = "~"

// Errors a reset token can fail with. They are distinguished because the caller
// tells a person something different for each: an expired link can be asked for
// again, a malformed one means the mail client mangled it.
var (
	ErrResetMalformed = errors.New("the reset link is not one of ours")
	ErrResetExpired   = errors.New("the reset link has expired")
)

// ResetSigner issues and checks password reset tokens.
type ResetSigner struct {
	secret []byte
}

// NewResetSigner builds a signer on the session signing secret.
func NewResetSigner(secret string) (*ResetSigner, error) {
	if len(secret) < SecretBytes {
		return nil, fmt.Errorf(
			"the session signing secret is %d characters; at least %d are required",
			len(secret), SecretBytes)
	}
	return &ResetSigner{secret: []byte(secret)}, nil
}

// IssueReset returns a token that lets the holder set the password of user for
// the next ResetLifetime.
func (s *ResetSigner) IssueReset(user uuid.UUID, now time.Time) string {
	payload := fmt.Sprintf("%s.%d", user, now.Add(ResetLifetime).Unix())
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return encoded + resetSeparator + s.sign(encoded)
}

// ParseReset checks a token and reports who it is for.
//
// It returns the token's identity as well, which is what the server remembers
// in order to make a link work once. The identity is a hash rather than the
// token: a server that keeps a list of valid reset tokens in memory is a server
// whose memory is worth stealing.
func (s *ResetSigner) ParseReset(token string, now time.Time) (user uuid.UUID, id string, err error) {
	encoded, signature, ok := strings.Cut(token, resetSeparator)
	if !ok {
		return uuid.Nil, "", ErrResetMalformed
	}

	// Constant time, because comparing signatures with == leaks where they
	// first differ, and that is enough to forge one a byte at a time.
	if !hmac.Equal([]byte(signature), []byte(s.sign(encoded))) {
		return uuid.Nil, "", ErrResetMalformed
	}

	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return uuid.Nil, "", ErrResetMalformed
	}
	idPart, expiryPart, ok := strings.Cut(string(raw), ".")
	if !ok {
		return uuid.Nil, "", ErrResetMalformed
	}
	parsed, err := uuid.Parse(idPart)
	if err != nil {
		return uuid.Nil, "", ErrResetMalformed
	}
	seconds, err := strconv.ParseInt(expiryPart, 10, 64)
	if err != nil {
		return uuid.Nil, "", ErrResetMalformed
	}

	// The expiry is checked after the signature, so a token nobody could have
	// produced is refused as forged rather than as stale.
	if !now.Before(time.Unix(seconds, 0)) {
		return uuid.Nil, "", ErrResetExpired
	}

	return parsed, tokenIdentity(token), nil
}

// ResetExpiry reports when a token stops working, for a caller that wants to
// say so. It does not validate the signature and must not be used to decide
// anything.
func ResetExpiry(issuedAt time.Time) time.Time { return issuedAt.Add(ResetLifetime) }

func (s *ResetSigner) sign(encoded string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(resetPurpose))
	mac.Write([]byte("|"))
	mac.Write([]byte(encoded))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// tokenIdentity names a token without being one.
func tokenIdentity(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
