package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// SecretBytes is the length of jwt_secret, generated at install.
const SecretBytes = 32

// Errors a token can fail with.
//
// They are distinct because the API answers them with different codes: a
// signature that does not verify is 2000, while a token that verified but has
// expired is 2002, and telling the two apart is what lets the frontend send
// somebody back to the login screen with a reason.
var (
	ErrTokenInvalid = errors.New("auth: token invalid")
	ErrTokenExpired = errors.New("auth: token expired")
	ErrNoSecret     = errors.New("auth: no signing secret is configured")
)

// Claims is the payload of a session token, per
// docs/adr/0004-jwt-with-server-side-sessions.md.
type Claims struct {
	// UserID is sub.
	UserID uuid.UUID
	// SessionID is jti, the primary key of the session row.
	SessionID uuid.UUID
	// IssuedAt is iat.
	IssuedAt time.Time
	// ExpiresAt is exp: iat plus the absolute timeout.
	ExpiresAt time.Time
	// IsAdmin is adm.
	//
	// Advisory only. The server re-reads is_admin from the database on every
	// request, because a token issued before an account changed would otherwise
	// carry a stale answer to the most consequential question the API asks. It
	// exists so the frontend can render the right navigation without a round
	// trip, and for nothing else.
	IsAdmin bool
}

// Signer issues and verifies session tokens.
//
// HS256 with a shared secret rather than an asymmetric algorithm: there is one
// server, it both issues and verifies, and a second key pair would add key
// management without adding a party that needs it.
type Signer struct {
	secret []byte
}

// NewSigner takes the configured jwt_secret.
//
// A short or absent secret is refused rather than padded: a token signed with
// a weak key is worse than no session at all, and install generates a proper
// one, so an undersized secret here means a hand-edited configuration file.
func NewSigner(secret string) (*Signer, error) {
	if secret == "" {
		return nil, ErrNoSecret
	}
	if len(secret) < SecretBytes {
		return nil, fmt.Errorf(
			"auth: the signing secret is %d bytes, which is shorter than the %d "+
				"install generates; regenerate it rather than shortening it",
			len(secret), SecretBytes)
	}
	return &Signer{secret: []byte(secret)}, nil
}

// Issue signs a token for a session.
func (s *Signer) Issue(c Claims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": c.UserID.String(),
		"jti": c.SessionID.String(),
		"iat": c.IssuedAt.Unix(),
		"exp": c.ExpiresAt.Unix(),
		"adm": c.IsAdmin,
	})

	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("signing the session token: %w", err)
	}
	return signed, nil
}

// Parse verifies a token's signature and expiry and returns its claims.
//
// The signing method is pinned to HS256. Accepting whatever the token's own
// header asks for is the classic JWT vulnerability: a token declaring "none",
// or declaring RS256 so that the HMAC secret is used as an RSA public key,
// would otherwise verify.
func (s *Signer) Parse(raw string) (Claims, error) {
	token, err := jwt.Parse(raw,
		func(*jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	switch {
	case errors.Is(err, jwt.ErrTokenExpired):
		return Claims{}, ErrTokenExpired
	case err != nil:
		return Claims{}, fmt.Errorf("%w: %s", ErrTokenInvalid, err)
	case !token.Valid:
		return Claims{}, ErrTokenInvalid
	}

	mapped, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return Claims{}, ErrTokenInvalid
	}

	var c Claims
	if c.UserID, err = uuidClaim(mapped, "sub"); err != nil {
		return Claims{}, err
	}
	if c.SessionID, err = uuidClaim(mapped, "jti"); err != nil {
		return Claims{}, err
	}
	if c.IssuedAt, err = timeClaim(mapped, "iat"); err != nil {
		return Claims{}, err
	}
	if c.ExpiresAt, err = timeClaim(mapped, "exp"); err != nil {
		return Claims{}, err
	}
	// A missing adm is false, which is the safe direction: the claim is
	// advisory and the database decides.
	c.IsAdmin, _ = mapped["adm"].(bool)

	return c, nil
}

func uuidClaim(m jwt.MapClaims, name string) (uuid.UUID, error) {
	raw, ok := m[name].(string)
	if !ok {
		return uuid.Nil, fmt.Errorf("%w: %s is missing", ErrTokenInvalid, name)
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: %s is not a uuid", ErrTokenInvalid, name)
	}
	return id, nil
}

// timeClaim reads a numeric date.
//
// JSON numbers decode as float64, and a Unix second past 2^53 is not a concern,
// but a claim that is not a number at all is: it would otherwise silently
// become the zero time, which for exp would mean "expired in 1970" and for iat
// would make an idle timeout nonsensical.
func timeClaim(m jwt.MapClaims, name string) (time.Time, error) {
	seconds, ok := m[name].(float64)
	if !ok {
		return time.Time{}, fmt.Errorf("%w: %s is not a number", ErrTokenInvalid, name)
	}
	return time.Unix(int64(seconds), 0).UTC(), nil
}
