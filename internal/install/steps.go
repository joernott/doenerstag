package install

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joernott/doenerstag/internal/auth"
	"github.com/joernott/doenerstag/internal/htmlsafe"
)

// AdministratorName is the one account with is_admin set. It cannot be renamed
// or deleted; see docs/05_auth_and_permissions.md.
const AdministratorName = "root"

// JWTSecretBytes is the length of the generated session signing key.
//
// The server refuses to start with anything shorter, so this is the floor
// rather than a preference.
const JWTSecretBytes = 32

// EnsureAdministrator creates the root account, or resets its password.
//
// Re-running install is how a lost root password is recovered, so an existing
// administrator has its password replaced rather than the step being skipped.
func EnsureAdministrator(ctx context.Context, pool *pgxpool.Pool, password string) error {
	if err := auth.ValidateComplexity(password); err != nil {
		return fmt.Errorf("the administrator password is not acceptable: %w", err)
	}

	hash, err := auth.Hash(password)
	if err != nil {
		return fmt.Errorf("hashing the administrator password: %w", err)
	}

	var existing string
	err = pool.QueryRow(ctx,
		`SELECT id::text FROM app_user WHERE lower(name) = lower($1)`,
		AdministratorName).Scan(&existing)

	switch {
	case err == nil:
		_, err = pool.Exec(ctx, `
			UPDATE app_user
			SET password_hash = $2, is_admin = true, display_name = 'Administrator'
			WHERE id = $1::uuid`, existing, hash)
		if err != nil {
			return fmt.Errorf("resetting the administrator password: %w", err)
		}
		return nil

	case isNoRows(err):
		id, err := newID()
		if err != nil {
			return err
		}
		_, err = pool.Exec(ctx, `
			INSERT INTO app_user (id, name, display_name, password_hash, is_admin)
			VALUES ($1::uuid, $2, 'Administrator', $3, true)`,
			id, AdministratorName, hash)
		if err != nil {
			return fmt.Errorf("creating the administrator: %w", err)
		}
		return nil

	default:
		return fmt.Errorf("looking for an existing administrator: %w", err)
	}
}

// GenerateJWTSecret returns a new base64url-encoded signing key.
func GenerateJWTSecret() (string, error) {
	raw := make([]byte, JWTSecretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generating the session signing secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// JWTSecretIsUsable reports whether an existing secret is long enough to keep.
//
// A secret that survives a re-run keeps everyone logged in, which is why
// install reuses one rather than rotating on every run. One that is too short
// or missing has to be replaced regardless.
func JWTSecretIsUsable(secret string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(secret)
	if err == nil {
		return len(decoded) >= JWTSecretBytes
	}
	// A secret an operator typed by hand is not necessarily base64. Judge it by
	// its raw length instead.
	return len(secret) >= JWTSecretBytes
}

// ContentPageKeys are the two pages the operator supplies.
var ContentPageKeys = []string{"imprint", "legal_notes"}

// LoadContentPage reads an HTML snippet from a file, sanitises it and stores it.
//
// An empty path leaves whatever is already there, which is the placeholder the
// migration seeded on a fresh database and the operator's own text on a re-run.
func LoadContentPage(ctx context.Context, pool *pgxpool.Pool, key, path string) error {
	if path == "" {
		return nil
	}

	raw, err := os.ReadFile(path) //nolint:gosec // operator-supplied path, which is the point
	if err != nil {
		return fmt.Errorf("reading the %s snippet from %s: %w", key, path, err)
	}

	sanitised := SanitiseHTML(string(raw))
	if strings.TrimSpace(sanitised) == "" {
		return fmt.Errorf("the %s snippet at %s is empty once sanitised", key, path)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO content_page (id, key, html)
		VALUES ($1::uuid, $2, $3)
		ON CONFLICT (key) DO UPDATE SET html = EXCLUDED.html`,
		mustID(), key, sanitised)
	if err != nil {
		return fmt.Errorf("storing the %s page: %w", key, err)
	}
	return nil
}

// SanitiseHTML strips anything an administrator-supplied snippet has no
// business containing.
//
// Only the administrator can set these pages, so this is not the first line of
// defence — the CSP in docs/05_auth_and_permissions.md is — but a stored
// snippet is rendered with innerHTML in the one place the frontend does that,
// and an allow-list is cheaper than trusting the account.
//
// The policy lives in internal/htmlsafe because the API replaces these pages as
// well, and one policy in two places would eventually be two policies with the
// weaker one as the way in.
func SanitiseHTML(html string) string {
	return htmlsafe.Sanitise(html)
}

// SemanticVersion is an application version split into its parts.
type SemanticVersion struct {
	Major, Minor, Patch int
}

// ParseSemanticVersion reads "1.2.3", tolerating a leading v and any
// pre-release or build suffix.
//
// A development build reports 0.0.0-dev, which parses to zeroes rather than
// failing: recording that an unversioned binary did the install is more useful
// than refusing to record anything.
func ParseSemanticVersion(s string) (SemanticVersion, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(s), "v")
	// Drop a pre-release or build suffix: 1.2.3-rc1+build5.
	if i := strings.IndexAny(trimmed, "-+"); i >= 0 {
		trimmed = trimmed[:i]
	}

	parts := strings.Split(trimmed, ".")
	if len(parts) != 3 {
		return SemanticVersion{}, fmt.Errorf(
			"version %q is not major.minor.patch", s)
	}

	var v SemanticVersion
	for i, target := range []*int{&v.Major, &v.Minor, &v.Patch} {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return SemanticVersion{}, fmt.Errorf(
				"version %q has a non-numeric component %q", s, parts[i])
		}
		*target = n
	}
	return v, nil
}

// RecordVersion appends the app_version row that install and update write.
//
// One row per run, so the table is a history of what was applied and when. The
// version verb and the version page report the newest.
func RecordVersion(ctx context.Context, pool *pgxpool.Pool, v SemanticVersion, schemaVersion uint) error {
	if schemaVersion == 0 {
		return fmt.Errorf("refusing to record a schema version of 0: nothing was migrated")
	}

	id, err := newID()
	if err != nil {
		return err
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO app_version (id, major, minor, patch, schema_version)
		VALUES ($1::uuid, $2, $3, $4, $5)`,
		id, v.Major, v.Minor, v.Patch, int64(schemaVersion)) //nolint:gosec // a migration count cannot overflow int64
	if err != nil {
		return fmt.Errorf("recording the application version: %w", err)
	}
	return nil
}

// InstalledVersion is one row of the app_version history.
type InstalledVersion struct {
	SemanticVersion
	SchemaVersion int64
	AppliedAt     string
}

// String renders the version the way the version verb prints it.
func (v InstalledVersion) String() string {
	return fmt.Sprintf("doenerstag %d.%d.%d (schema %d, applied %s)",
		v.Major, v.Minor, v.Patch, v.SchemaVersion, v.AppliedAt)
}

// ReadVersion returns the newest app_version row.
func ReadVersion(ctx context.Context, pool *pgxpool.Pool) (InstalledVersion, error) {
	var v InstalledVersion
	err := pool.QueryRow(ctx, `
		SELECT major, minor, patch, schema_version,
		       to_char(applied_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		FROM app_version
		ORDER BY applied_at DESC, created_at DESC
		LIMIT 1`).Scan(&v.Major, &v.Minor, &v.Patch, &v.SchemaVersion, &v.AppliedAt)
	if err != nil {
		if isNoRows(err) {
			return InstalledVersion{}, fmt.Errorf(
				"the database records no installed version; run doenerstag install")
		}
		return InstalledVersion{}, fmt.Errorf("reading the installed version: %w", err)
	}
	return v, nil
}

// newID returns a UUIDv7, which is what every primary key in this schema uses.
func newID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generating an identifier: %w", err)
	}
	return id.String(), nil
}

// mustID is newID for the call sites where a failure is not worth threading an
// error through. UUIDv7 generation only fails if the system entropy source
// does, at which point nothing else works either.
func mustID() string {
	id, err := newID()
	if err != nil {
		panic(err)
	}
	return id
}

func isNoRows(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no rows in result set")
}
