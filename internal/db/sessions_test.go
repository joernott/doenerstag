package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/testdb"
)

func TestSessionRoundTrip(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()
	u := newUser(t, pool, "Olga")

	created, err := db.ReplaceSession(ctx, pool, db.NewSession{
		UserID:     u.ID,
		Lifetime:   7 * 24 * time.Hour,
		RemoteAddr: "192.0.2.10",
		UserAgent:  "test/1.0",
	})
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	if created.UserID != u.ID {
		t.Error("the session belongs to the wrong account")
	}
	if !created.AbsoluteExpires.After(created.IssuedAt) {
		t.Error("the session expires before it was issued")
	}

	read, err := db.SessionByID(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if read.RemoteAddr != "192.0.2.10" || read.UserAgent != "test/1.0" {
		t.Errorf("read back %+v", read)
	}
}

// The single-session rule from docs/05_auth_and_permissions.md: logging in
// again replaces the row, and the first browser is rejected on its next
// request.
func TestASecondLoginReplacesTheFirstSession(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()
	u := newUser(t, pool, "Paula")

	first, err := db.ReplaceSession(ctx, pool, db.NewSession{
		UserID: u.ID, Lifetime: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.ReplaceSession(ctx, pool, db.NewSession{
		UserID: u.ID, Lifetime: time.Hour,
	})
	if err != nil {
		t.Fatalf("the second login failed: %v", err)
	}

	if first.ID == second.ID {
		t.Fatal("the second login reused the first session id")
	}
	if _, err := db.SessionByID(ctx, pool, first.ID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("the first session survived: %v", err)
	}
	if _, err := db.SessionByID(ctx, pool, second.ID); err != nil {
		t.Errorf("the second session is not usable: %v", err)
	}

	// And the constraint holds: exactly one row for the user.
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM session WHERE user_id = $1`, u.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("the user has %d sessions, want exactly 1", count)
	}
}

// Two users logging in do not disturb each other, which the delete-and-insert
// would break if its WHERE clause were ever dropped.
func TestOneUsersLoginDoesNotEndAnothers(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()
	first := newUser(t, pool, "Quirin")
	second := newUser(t, pool, "Rosa")

	kept, err := db.ReplaceSession(ctx, pool, db.NewSession{UserID: first.ID, Lifetime: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReplaceSession(ctx, pool, db.NewSession{UserID: second.ID, Lifetime: time.Hour}); err != nil {
		t.Fatal(err)
	}

	if _, err := db.SessionByID(ctx, pool, kept.ID); err != nil {
		t.Errorf("another user's login ended this session: %v", err)
	}
}

// Every authenticated request needs the session and the account together,
// because is_admin is re-read rather than trusted from the token.
func TestSessionWithUserReturnsBoth(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()
	u := newUser(t, pool, "Sven")

	created, err := db.ReplaceSession(ctx, pool, db.NewSession{UserID: u.ID, Lifetime: time.Hour})
	if err != nil {
		t.Fatal(err)
	}

	session, user, err := db.SessionWithUser(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if session.ID != created.ID {
		t.Error("the wrong session came back")
	}
	if user.ID != u.ID || user.Name != "Sven" {
		t.Errorf("the wrong account came back: %+v", user)
	}
	if user.IsAdmin {
		t.Error("an ordinary account is reported as an administrator")
	}
}

func TestSessionWithUserIsNotFoundAfterLogout(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()
	u := newUser(t, pool, "Tanja")

	created, err := db.ReplaceSession(ctx, pool, db.NewSession{UserID: u.ID, Lifetime: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteSession(ctx, pool, created.ID); err != nil {
		t.Fatal(err)
	}

	if _, _, err := db.SessionWithUser(ctx, pool, created.ID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
	// Logging out twice is not an error: the desired state is "no session".
	if err := db.DeleteSession(ctx, pool, created.ID); err != nil {
		t.Errorf("logging out twice failed: %v", err)
	}
}

// last_seen_at is what the idle timeout measures, and the throttle is what
// keeps a burst of requests from being a burst of writes.
func TestTouchIsThrottled(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()
	u := newUser(t, pool, "Udo")

	created, err := db.ReplaceSession(ctx, pool, db.NewSession{UserID: u.ID, Lifetime: time.Hour})
	if err != nil {
		t.Fatal(err)
	}

	// A minute's throttle on a session created moments ago: no write.
	if err := db.TouchSession(ctx, pool, created.ID, time.Now(), time.Minute); err != nil {
		t.Fatal(err)
	}
	unchanged, err := db.SessionByID(ctx, pool, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !unchanged.LastSeenAt.Equal(created.LastSeenAt) {
		t.Error("a throttled touch wrote anyway")
	}

	// A zero throttle always writes, which is how the timestamp advances once
	// the throttle has elapsed.
	if err := db.TouchSession(ctx, pool, created.ID, time.Now(), 0); err != nil {
		t.Fatal(err)
	}
	touched, err := db.SessionByID(ctx, pool, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !touched.LastSeenAt.After(created.LastSeenAt) {
		t.Errorf("last_seen_at did not advance: %v then %v",
			created.LastSeenAt, touched.LastSeenAt)
	}
}

// Expiry is enforced on use; this is the housekeeping that stops the table
// growing by one abandoned row per login forever.
func TestExpiredSessionsAreCollected(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()
	live := newUser(t, pool, "Vera")
	stale := newUser(t, pool, "Willi")

	kept, err := db.ReplaceSession(ctx, pool, db.NewSession{UserID: live.ID, Lifetime: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	dead, err := db.ReplaceSession(ctx, pool, db.NewSession{UserID: stale.ID, Lifetime: time.Hour})
	if err != nil {
		t.Fatal(err)
	}

	// Age it directly: the check constraint forbids inserting a row that has
	// already expired, so this is the only way to get one.
	if _, err := pool.Exec(ctx, `
		UPDATE session SET issued_at = now() - interval '2 hours',
		                   absolute_expires = now() - interval '1 hour'
		WHERE id = $1`, dead.ID); err != nil {
		t.Fatal(err)
	}

	removed, err := db.DeleteExpiredSessions(ctx, pool, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("collected %d sessions, want 1", removed)
	}
	if _, err := db.SessionByID(ctx, pool, kept.ID); err != nil {
		t.Errorf("a live session was collected: %v", err)
	}
}

// Deleting an account must take its session with it, or a deleted user's
// browser would keep a token pointing at a row that outlived them.
func TestDeletingAUserRemovesTheirSession(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()
	u := newUser(t, pool, "Xenia")

	created, err := db.ReplaceSession(ctx, pool, db.NewSession{UserID: u.ID, Lifetime: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteUser(ctx, pool, u.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.SessionByID(ctx, pool, created.ID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("the session outlived its account: %v", err)
	}
}
