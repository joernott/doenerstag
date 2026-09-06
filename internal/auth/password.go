// Package auth owns passwords, sessions and API tokens.
//
// Passwords are hashed server-side with Argon2id. The reasoning, including why
// hashing in the browser was rejected, is in
// docs/adr/0005-server-side-argon2id.md.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/text/unicode/norm"
)

// Argon2id parameters, per docs/05_auth_and_permissions.md.
//
// They travel with the hash in the PHC string, so raising them later does not
// invalidate existing passwords: a login against an outdated hash re-hashes it
// with the current values.
//
// Memory is the parameter to watch: argonMemory KiB is allocated per concurrent
// hash, so it, not time, is what would bite first if the work factor were
// raised. At the expected scale it never matters.
const (
	argonMemory      uint32 = 64 * 1024 // KiB
	argonIterations  uint32 = 3
	argonParallelism uint8  = 2
	argonSaltLength  int    = 16
	argonKeyLength   uint32 = 32
)

// argonVersion is the Argon2 version the PHC string records. The x/crypto
// implementation is 0x13, decimal 19.
const argonVersion = argon2.Version

// Errors reported by Verify.
var (
	// ErrMismatch means the password does not match the hash. It is the only
	// error a caller should ever show a user, and even then only as the
	// generic "invalid user name or password".
	ErrMismatch = errors.New("password does not match")

	// ErrMalformedHash means the stored value is not a hash this package can
	// read, which is a data problem rather than a wrong password.
	ErrMalformedHash = errors.New("malformed password hash")
)

// Params are the cost parameters of one hash.
type Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	KeyLength   uint32
}

// CurrentParams are the parameters new hashes are created with.
func CurrentParams() Params {
	return Params{
		Memory:      argonMemory,
		Iterations:  argonIterations,
		Parallelism: argonParallelism,
		KeyLength:   argonKeyLength,
	}
}

// Hash derives an Argon2id hash and returns it as a PHC string:
//
//	$argon2id$v=19$m=65536,t=3,p=2$<salt>$<key>
//
// The password is normalised to NFC first, so that a password typed on a
// machine that produces decomposed characters verifies on one that does not.
func Hash(password string) (string, error) {
	if err := ValidateComplexity(password); err != nil {
		return "", err
	}
	return hashWith(password, CurrentParams())
}

// HashWithoutValidation derives a hash without checking complexity.
//
// It exists for the deleted-user placeholder and for tests that need a hash of
// an arbitrary string. Anything accepting a password from a person must use
// Hash.
func HashWithoutValidation(password string) (string, error) {
	return hashWith(password, CurrentParams())
}

func hashWith(password string, p Params) (string, error) {
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generating a password salt: %w", err)
	}

	key := argon2.IDKey(
		[]byte(Normalise(password)), salt,
		p.Iterations, p.Memory, p.Parallelism, p.KeyLength)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argonVersion, p.Memory, p.Iterations, p.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// Verify reports whether password matches the PHC-encoded hash.
//
// It returns ErrMismatch for a wrong password and ErrMalformedHash for a stored
// value it cannot parse. The comparison is constant time.
func Verify(password, encoded string) error {
	params, salt, want, err := parsePHC(encoded)
	if err != nil {
		return err
	}

	got := argon2.IDKey(
		[]byte(Normalise(password)), salt,
		params.Iterations, params.Memory, params.Parallelism, params.KeyLength)

	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrMismatch
	}
	return nil
}

// NeedsRehash reports whether a stored hash was made with parameters weaker
// than the current ones.
//
// A successful login against such a hash should re-hash the password and update
// the row, which is how the cost is raised over time without a migration or a
// password reset.
func NeedsRehash(encoded string) bool {
	params, _, _, err := parsePHC(encoded)
	if err != nil {
		// Unreadable is worth replacing on the next successful login.
		return true
	}
	current := CurrentParams()
	return params.Memory < current.Memory ||
		params.Iterations < current.Iterations ||
		params.Parallelism < current.Parallelism ||
		params.KeyLength < current.KeyLength
}

// UnusablePasswordHash is stored for accounts that must never be logged into,
// currently the deleted-user placeholder.
//
// It is not a PHC string, so Verify rejects it before doing any work and no
// password can match it.
const UnusablePasswordHash = "*"

// IsUnusable reports whether a stored hash can never match any password.
func IsUnusable(encoded string) bool {
	return encoded == UnusablePasswordHash || !strings.HasPrefix(encoded, "$argon2id$")
}

// Normalise applies NFC normalisation.
//
// Passwords are normalised before hashing and before verifying, so a password
// containing an accented character verifies regardless of whether the client
// sent it precomposed or decomposed. Doing it in one place is what makes the
// two consistent.
func Normalise(password string) string {
	return norm.NFC.String(password)
}

func parsePHC(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	// "", "argon2id", "v=19", "m=..,t=..,p=..", salt, key
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Params{}, nil, nil, ErrMalformedHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return Params{}, nil, nil, ErrMalformedHash
	}
	if version != argonVersion {
		return Params{}, nil, nil, fmt.Errorf(
			"%w: unsupported Argon2 version %d", ErrMalformedHash, version)
	}

	var p Params
	_, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d",
		&p.Memory, &p.Iterations, &p.Parallelism)
	if err != nil {
		return Params{}, nil, nil, ErrMalformedHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, ErrMalformedHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return Params{}, nil, nil, ErrMalformedHash
	}
	if len(salt) == 0 || len(key) == 0 {
		return Params{}, nil, nil, ErrMalformedHash
	}

	p.KeyLength = uint32(len(key)) //nolint:gosec // a hash length cannot overflow
	return p, salt, key, nil
}
