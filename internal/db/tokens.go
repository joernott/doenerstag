package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/joernott/doenerstag/internal/model"
)

const tokenColumns = `id, user_id, name, token_prefix, expires_at, last_used_at, created_at`

func scanToken(row pgx.Row) (model.APIToken, error) {
	var t model.APIToken
	err := row.Scan(&t.ID, &t.UserID, &t.Name, &t.Prefix,
		&t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.APIToken{}, ErrNotFound
		}
		return model.APIToken{}, err
	}
	return t, nil
}

// NewAPIToken is what token creation supplies.
//
// Hash is the SHA-256 of the token; the token itself never reaches this layer,
// which is what makes "stored only as a hash" a property of the design rather
// than a discipline.
type NewAPIToken struct {
	UserID    uuid.UUID
	Name      string
	Hash      string
	Prefix    string
	ExpiresAt *time.Time
}

// CreateAPIToken inserts a token and returns its metadata.
func CreateAPIToken(ctx context.Context, q Querier, in NewAPIToken) (model.APIToken, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return model.APIToken{}, fmt.Errorf("generating a token id: %w", err)
	}

	row := q.QueryRow(ctx, `
		INSERT INTO api_token (id, user_id, name, token_hash, token_prefix, expires_at,
		                       created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $2, $2)
		RETURNING `+tokenColumns,
		id, in.UserID, in.Name, in.Hash, in.Prefix, in.ExpiresAt)

	t, err := scanToken(row)
	if isUniqueViolation(err) {
		// api_token_name_unique_per_user. The hash colliding is the other
		// unique constraint and would mean two identical 32-byte random
		// values, which is not a case worth a distinct error.
		return model.APIToken{}, ErrNameTaken
	}
	return t, err
}

// APITokenByHash looks a token up for authentication.
//
// The lookup is by hash, so the plaintext token is never compared against
// anything in the database and never appears in a query log. It returns the
// owner too, for the same reason SessionWithUser does.
func APITokenByHash(ctx context.Context, q Querier, hash string) (model.APIToken, model.User, error) {
	var (
		t model.APIToken
		u model.User
	)
	err := q.QueryRow(ctx, `
		SELECT t.id, t.user_id, t.name, t.token_prefix, t.expires_at, t.last_used_at, t.created_at,
		       u.id, u.name, coalesce(u.display_name, ''), coalesce(u.email, ''),
		       u.is_admin, u.last_login_at, u.created_at, u.updated_at
		FROM api_token t
		JOIN app_user u ON u.id = t.user_id
		WHERE t.token_hash = $1`, hash).
		Scan(&t.ID, &t.UserID, &t.Name, &t.Prefix, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt,
			&u.ID, &u.Name, &u.DisplayName, &u.Email, &u.IsAdmin,
			&u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.APIToken{}, model.User{}, ErrNotFound
		}
		return model.APIToken{}, model.User{}, err
	}
	return t, u, nil
}

// ListAPITokens returns a user's tokens, newest first. The value is not stored
// and so cannot be returned.
func ListAPITokens(ctx context.Context, q Querier, userID uuid.UUID) ([]model.APIToken, error) {
	rows, err := q.Query(ctx,
		`SELECT `+tokenColumns+` FROM api_token WHERE user_id = $1 ORDER BY created_at DESC`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tokens := make([]model.APIToken, 0, 8)
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// APITokenByID reads one token's metadata, for the revoke path to check
// ownership before deleting.
func APITokenByID(ctx context.Context, q Querier, id uuid.UUID) (model.APIToken, error) {
	return scanToken(q.QueryRow(ctx,
		`SELECT `+tokenColumns+` FROM api_token WHERE id = $1`, id))
}

// TouchAPIToken advances last_used_at under the same throttle as sessions.
func TouchAPIToken(ctx context.Context, q Querier, id uuid.UUID, throttle time.Duration) error {
	_, err := q.Exec(ctx, `
		UPDATE api_token SET last_used_at = now()
		WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - $2::interval)`,
		id, throttle.String())
	return err
}

// DeleteAPIToken revokes a token. Revocation is immediate: the next request
// carrying it finds no row.
func DeleteAPIToken(ctx context.Context, q Querier, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `DELETE FROM api_token WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteExpiredAPITokens removes tokens past their expiry date, for the cleanup
// verb. As with sessions, expiry is enforced on use as well.
func DeleteExpiredAPITokens(ctx context.Context, q Querier) (int64, error) {
	tag, err := q.Exec(ctx,
		`DELETE FROM api_token WHERE expires_at IS NOT NULL AND expires_at <= now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
