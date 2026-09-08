package auth_test

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/joernott/doenerstag/internal/auth"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func newTestSigner(t *testing.T) *auth.Signer {
	t.Helper()
	s, err := auth.NewSigner(testSecret)
	if err != nil {
		t.Fatalf("creating a signer: %v", err)
	}
	return s
}

func sampleClaims() auth.Claims {
	now := time.Now().Truncate(time.Second).UTC()
	return auth.Claims{
		UserID:    uuid.MustParse("018f0000-0000-7000-8000-000000000001"),
		SessionID: uuid.MustParse("018f0000-0000-7000-8000-000000000002"),
		IssuedAt:  now,
		ExpiresAt: now.Add(7 * 24 * time.Hour),
		IsAdmin:   true,
	}
}

func TestIssuedTokenParsesBackToItsClaims(t *testing.T) {
	s := newTestSigner(t)
	want := sampleClaims()

	raw, err := s.Issue(want)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}

	got, err := s.Parse(raw)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if got.UserID != want.UserID || got.SessionID != want.SessionID {
		t.Errorf("identifiers did not survive: got %+v, want %+v", got, want)
	}
	if !got.IssuedAt.Equal(want.IssuedAt) || !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("times did not survive: got %v/%v, want %v/%v",
			got.IssuedAt, got.ExpiresAt, want.IssuedAt, want.ExpiresAt)
	}
	if got.IsAdmin != want.IsAdmin {
		t.Errorf("adm is %v, want %v", got.IsAdmin, want.IsAdmin)
	}
}

// A token signed with a different secret must not verify. This is the whole
// point of signing it.
func TestAnotherSecretDoesNotVerify(t *testing.T) {
	raw, err := newTestSigner(t).Issue(sampleClaims())
	if err != nil {
		t.Fatal(err)
	}

	other, err := auth.NewSigner("fedcba9876543210fedcba9876543210")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Parse(raw); !errors.Is(err, auth.ErrTokenInvalid) {
		t.Errorf("a foreign token gave %v, want ErrTokenInvalid", err)
	}
}

// Rotating jwt_secret must log everyone out, which is the documented recovery
// path in docs/adr/0004-jwt-with-server-side-sessions.md. It is the same
// mechanism as the test above, asserted as the behaviour operators are told to
// rely on.
func TestRotatingTheSecretInvalidatesEveryToken(t *testing.T) {
	before, err := newTestSigner(t).Issue(sampleClaims())
	if err != nil {
		t.Fatal(err)
	}

	rotated := make([]byte, auth.SecretBytes)
	if _, err := rand.Read(rotated); err != nil {
		t.Fatal(err)
	}
	after, err := auth.NewSigner(string(rotated))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := after.Parse(before); err == nil {
		t.Error("a token issued before the rotation still verifies")
	}
}

// The classic JWT attack: a token whose header says the algorithm is "none",
// carrying whatever claims the attacker likes. Accepting the token's own
// statement about how to verify it is what makes this work, so the parser pins
// the method instead.
func TestUnsignedTokenIsRejected(t *testing.T) {
	claims := jwt.MapClaims{
		"sub": uuid.Nil.String(),
		"jti": uuid.Nil.String(),
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
		"adm": true,
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := newTestSigner(t).Parse(raw); !errors.Is(err, auth.ErrTokenInvalid) {
		t.Errorf("an alg=none token gave %v, want ErrTokenInvalid", err)
	}
}

// The other half of the same attack: declaring an asymmetric algorithm so that
// the HMAC secret is treated as a public key. Pinning HS256 covers both.
func TestForeignAlgorithmIsRejected(t *testing.T) {
	for _, alg := range []string{"HS384", "HS512"} {
		method := jwt.GetSigningMethod(alg)
		raw, err := jwt.NewWithClaims(method, jwt.MapClaims{
			"sub": uuid.Nil.String(), "jti": uuid.Nil.String(),
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
		}).SignedString([]byte(testSecret))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := newTestSigner(t).Parse(raw); !errors.Is(err, auth.ErrTokenInvalid) {
			t.Errorf("an %s token gave %v, want ErrTokenInvalid", alg, err)
		}
	}
}

// An expired token is distinguishable from an invalid one, because the API
// answers them 2002 and 2000 respectively.
func TestExpiredTokenIsReportedAsExpired(t *testing.T) {
	s := newTestSigner(t)
	claims := sampleClaims()
	claims.IssuedAt = time.Now().Add(-8 * 24 * time.Hour)
	claims.ExpiresAt = time.Now().Add(-time.Minute)

	raw, err := s.Issue(claims)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Parse(raw); !errors.Is(err, auth.ErrTokenExpired) {
		t.Errorf("an expired token gave %v, want ErrTokenExpired", err)
	}
}

// A token with no exp at all must not be treated as one that never expires.
func TestTokenWithoutExpiryIsRejected(t *testing.T) {
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": uuid.Nil.String(),
		"jti": uuid.Nil.String(),
		"iat": time.Now().Unix(),
	}).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newTestSigner(t).Parse(raw); err == nil {
		t.Error("a token with no exp was accepted")
	}
}

// Claims that verify but are not the shape the application expects must fail
// rather than becoming zero values: a sub that is not a uuid would otherwise
// authenticate the request as the nil user.
func TestMalformedClaimsAreRejected(t *testing.T) {
	cases := map[string]jwt.MapClaims{
		"sub is not a uuid": {"sub": "nobody", "jti": uuid.Nil.String(),
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix()},
		"jti is missing": {"sub": uuid.Nil.String(),
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix()},
		"iat is a string": {"sub": uuid.Nil.String(), "jti": uuid.Nil.String(),
			"iat": "yesterday", "exp": time.Now().Add(time.Hour).Unix()},
	}

	for name, claims := range cases {
		t.Run(name, func(t *testing.T) {
			raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).
				SignedString([]byte(testSecret))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := newTestSigner(t).Parse(raw); !errors.Is(err, auth.ErrTokenInvalid) {
				t.Errorf("got %v, want ErrTokenInvalid", err)
			}
		})
	}
}

// Tampering with the payload must invalidate the signature. Asserted directly
// rather than trusted, because the whole session model rests on it.
func TestEditingTheClaimsBreaksTheSignature(t *testing.T) {
	s := newTestSigner(t)
	claims := sampleClaims()
	claims.IsAdmin = false

	raw, err := s.Issue(claims)
	if err != nil {
		t.Fatal(err)
	}

	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		t.Fatalf("a JWT should have three parts, got %d", len(parts))
	}

	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	decoded["adm"] = true
	edited, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}

	forged := parts[0] + "." + base64.RawURLEncoding.EncodeToString(edited) + "." + parts[2]
	if _, err := s.Parse(forged); !errors.Is(err, auth.ErrTokenInvalid) {
		t.Errorf("a token with an edited adm claim gave %v, want ErrTokenInvalid", err)
	}
}

// install generates 32 bytes. A configuration file edited by hand to something
// shorter must be refused rather than quietly accepted.
func TestShortSecretIsRefused(t *testing.T) {
	if _, err := auth.NewSigner(""); !errors.Is(err, auth.ErrNoSecret) {
		t.Errorf("an empty secret gave %v, want ErrNoSecret", err)
	}
	if _, err := auth.NewSigner("short"); err == nil {
		t.Error("a five-byte signing secret was accepted")
	}
}
