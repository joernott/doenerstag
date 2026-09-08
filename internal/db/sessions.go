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

// sessionColumns is the shared projection, as for users.
const sessionColumns = `id, user_id, issued_at, last_seen_at, absolute_expires,
	coalesce(remote_addr, ''), coalesce(user_agent, '')`

func scanSession(row pgx.Row) (model.Session, error) {
	var s model.Session
	err := row.Scan(&s.ID, &s.UserID, &s.IssuedAt, &s.LastSeenAt,
		&s.AbsoluteExpires, &s.RemoteAddr, &s.UserAgent)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Session{}, ErrNotFound
		}
		return model.Session{}, err
	}
	return s, nil
}

// NewSession is what a successful login supplies.
type NewSession struct {
	UserID     uuid.UUID
	Lifetime   time.Duration
	RemoteAddr string
	UserAgent  string

	// IssuedAt is when the session starts. The zero value means now.
	//
	// The application supplies the time rather than the statement calling
	// now(), because the idle timeout compares last_seen_at against the
	// application's clock: a row written by the database clock and judged by
	// the application's is two clocks deciding one question, which is right
	// only for as long as they agree. One clock also makes the timeouts
	// testable without sleeping through them.
	IssuedAt time.Time
}

// at returns the supplied time, or now when it is zero.
func at(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now()
	}
	return t
}

// ReplaceSession makes a new session the user's only one.
//
// docs/05_auth_and_permissions.md gives session a unique constraint on user_id
// and says a login replaces any existing row, so the previously logged-in
// browser is rejected on its next request.
//
// One statement rather than a delete followed by an insert, because two
// statements leave a window in which the user has no session at all: a request
// arriving in it would be told its session was superseded, and the person would
// be bounced to the login screen by their own successful login.
//
// ON CONFLICT rather than a data-modifying CTE. A CTE looks like it should
// work, but every sub-statement in a WITH runs against the same snapshot, so
// the INSERT cannot see the DELETE and the unique constraint rejects it. The
// upsert takes over the existing row instead, giving it the new session's
// identifier -- nothing references session.id, so re-keying the row is free.
func ReplaceSession(ctx context.Context, q Querier, in NewSession) (model.Session, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return model.Session{}, fmt.Errorf("generating a session id: %w", err)
	}

	issued := at(in.IssuedAt)

	row := q.QueryRow(ctx, `
		INSERT INTO session (id, user_id, issued_at, last_seen_at, absolute_expires,
		                     remote_addr, user_agent, created_by, updated_by)
		-- $3 is cast at every use: without it PostgreSQL sees the same parameter
		-- as a timestamp in two places and as the left operand of an interval
		-- addition in a third, and refuses with "inconsistent types deduced".
		VALUES ($1, $2, $3::timestamptz, $3::timestamptz,
		        $3::timestamptz + $4::interval,
		        nullif($5, ''), nullif($6, ''), $2, $2)
		ON CONFLICT (user_id) DO UPDATE SET
			id               = EXCLUDED.id,
			issued_at        = EXCLUDED.issued_at,
			last_seen_at     = EXCLUDED.last_seen_at,
			absolute_expires = EXCLUDED.absolute_expires,
			remote_addr      = EXCLUDED.remote_addr,
			user_agent       = EXCLUDED.user_agent,
			updated_by       = EXCLUDED.updated_by
		RETURNING `+sessionColumns,
		id, in.UserID, issued, in.Lifetime.String(), in.RemoteAddr, in.UserAgent)

	return scanSession(row)
}

// SessionByID reads a session by the id carried in the token's jti claim.
func SessionByID(ctx context.Context, q Querier, id uuid.UUID) (model.Session, error) {
	return scanSession(q.QueryRow(ctx,
		`SELECT `+sessionColumns+` FROM session WHERE id = $1`, id))
}

// SessionWithUser reads a session and its account in one round trip.
//
// Every authenticated request needs both -- the session to check the timeouts,
// the user because is_admin is re-read from the database on each request rather
// than trusted from the token's advisory adm claim. Two queries per request
// would double the database traffic of the whole API for no benefit.
func SessionWithUser(ctx context.Context, q Querier, id uuid.UUID) (model.Session, model.User, error) {
	var (
		s model.Session
		u model.User
	)
	err := q.QueryRow(ctx, `
		SELECT s.id, s.user_id, s.issued_at, s.last_seen_at, s.absolute_expires,
		       coalesce(s.remote_addr, ''), coalesce(s.user_agent, ''),
		       u.id, u.name, coalesce(u.display_name, ''), coalesce(u.email, ''),
		       u.is_admin, u.last_login_at, u.created_at, u.updated_at
		FROM session s
		JOIN app_user u ON u.id = s.user_id
		WHERE s.id = $1`, id).
		Scan(&s.ID, &s.UserID, &s.IssuedAt, &s.LastSeenAt, &s.AbsoluteExpires,
			&s.RemoteAddr, &s.UserAgent,
			&u.ID, &u.Name, &u.DisplayName, &u.Email, &u.IsAdmin,
			&u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Session{}, model.User{}, ErrNotFound
		}
		return model.Session{}, model.User{}, err
	}
	return s, u, nil
}

// TouchSession advances last_seen_at, which is what the idle timeout measures.
//
// The write is skipped when the stored value is newer than throttle, so a burst
// of requests costs one UPDATE rather than one per request. The condition is in
// the WHERE clause rather than in Go so that two concurrent requests cannot
// both decide to write.
//
// now comes from the caller, for the reason given on NewSession.IssuedAt: the
// value written here is the one the idle check reads back, so both must come
// from the same clock.
func TouchSession(ctx context.Context, q Querier, id uuid.UUID, now time.Time, throttle time.Duration) error {
	stamp := at(now)
	_, err := q.Exec(ctx, `
		UPDATE session SET last_seen_at = $2::timestamptz
		WHERE id = $1 AND last_seen_at < $2::timestamptz - $3::interval`,
		id, stamp, throttle.String())
	return err
}

// DeleteSession ends one session. Logging out an already-deleted session is not
// an error: the desired state is "no session", and it holds either way.
func DeleteSession(ctx context.Context, q Querier, id uuid.UUID) error {
	_, err := q.Exec(ctx, `DELETE FROM session WHERE id = $1`, id)
	return err
}

// DeleteExpiredSessions removes rows past their absolute lifetime.
//
// Sessions are also rejected on use, so this is housekeeping rather than a
// security control: without it the table would grow by one abandoned row per
// login forever. The cleanup verb calls it.
func DeleteExpiredSessions(ctx context.Context, q Querier, now time.Time) (int64, error) {
	tag, err := q.Exec(ctx,
		`DELETE FROM session WHERE absolute_expires <= $1`, at(now))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// DeleteSessionsForUser ends every session of one account.
//
// Used when a password is reset. Somebody resetting a password may be doing it
// precisely because somebody else has been using theirs, and a reset that left
// the other browser logged in would achieve nothing.
//
// Removing no rows is success: an account with no session is the state this
// asks for.
func DeleteSessionsForUser(ctx context.Context, q Querier, user uuid.UUID) error {
	_, err := q.Exec(ctx, `DELETE FROM session WHERE user_id = $1`, user)
	return err
}
