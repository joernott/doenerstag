package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/joernott/doenerstag/internal/auth"
	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/testdb"
)

func TestAPITokenRoundTrip(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()
	u := newUser(t, pool, "Yannick")

	generated, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}

	created, err := db.CreateAPIToken(ctx, pool, db.NewAPIToken{
		UserID: u.ID, Name: "laptop",
		Hash: generated.Hash, Prefix: generated.Prefix,
	})
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	if created.Prefix != generated.Prefix {
		t.Error("the stored prefix is not the token's")
	}
	if created.ExpiresAt != nil {
		t.Error("a token created without an expiry has one")
	}

	token, owner, err := db.APITokenByHash(ctx, pool, generated.Hash)
	if err != nil {
		t.Fatalf("looking up by hash: %v", err)
	}
	if token.ID != created.ID || owner.ID != u.ID {
		t.Error("the lookup returned the wrong token or owner")
	}
}

// The token value must not be recoverable from the database. This is the
// property the whole "shown once" design rests on, so it is asserted against
// the stored row rather than against the Go struct.
func TestTheTokenValueIsNeverStored(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()
	u := newUser(t, pool, "Zoe")

	generated, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateAPIToken(ctx, pool, db.NewAPIToken{
		UserID: u.ID, Name: "script", Hash: generated.Hash, Prefix: generated.Prefix,
	}); err != nil {
		t.Fatal(err)
	}

	var found int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM api_token
		WHERE token_hash = $1 OR name = $1 OR token_prefix = $1`,
		generated.Value).Scan(&found); err != nil {
		t.Fatal(err)
	}
	if found != 0 {
		t.Error("the plaintext token value appears in the api_token row")
	}
}

func TestUnknownHashIsNotFound(t *testing.T) {
	pool := testdb.Migrated(t)

	_, _, err := db.APITokenByHash(context.Background(), pool,
		auth.HashAPIToken("a token that was never created"))
	if !errors.Is(err, db.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestTokenNamesAreUniquePerUser(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()
	first := newUser(t, pool, "Ada")
	second := newUser(t, pool, "Bob")

	generated, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateAPIToken(ctx, pool, db.NewAPIToken{
		UserID: first.ID, Name: "laptop", Hash: generated.Hash, Prefix: generated.Prefix,
	}); err != nil {
		t.Fatal(err)
	}

	again, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateAPIToken(ctx, pool, db.NewAPIToken{
		UserID: first.ID, Name: "laptop", Hash: again.Hash, Prefix: again.Prefix,
	}); !errors.Is(err, db.ErrNameTaken) {
		t.Errorf("a duplicate name for one user gave %v, want ErrNameTaken", err)
	}

	// The same name for a different user is fine: the constraint is per user.
	other, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateAPIToken(ctx, pool, db.NewAPIToken{
		UserID: second.ID, Name: "laptop", Hash: other.Hash, Prefix: other.Prefix,
	}); err != nil {
		t.Errorf("another user could not reuse the name: %v", err)
	}
}

func TestListTokensReturnsOnlyTheOwners(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()
	mine := newUser(t, pool, "Cleo")
	theirs := newUser(t, pool, "Dan")

	for _, name := range []string{"one", "two"} {
		generated, err := auth.GenerateAPIToken()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.CreateAPIToken(ctx, pool, db.NewAPIToken{
			UserID: mine.ID, Name: name, Hash: generated.Hash, Prefix: generated.Prefix,
		}); err != nil {
			t.Fatal(err)
		}
	}
	generated, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateAPIToken(ctx, pool, db.NewAPIToken{
		UserID: theirs.ID, Name: "hidden", Hash: generated.Hash, Prefix: generated.Prefix,
	}); err != nil {
		t.Fatal(err)
	}

	tokens, err := db.ListAPITokens(ctx, pool, mine.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 {
		t.Fatalf("listed %d tokens, want 2", len(tokens))
	}
	for _, token := range tokens {
		if token.UserID != mine.ID {
			t.Error("the list contains another user's token")
		}
	}
}

func TestTouchTokenIsThrottled(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()
	u := newUser(t, pool, "Edda")

	generated, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateAPIToken(ctx, pool, db.NewAPIToken{
		UserID: u.ID, Name: "cron", Hash: generated.Hash, Prefix: generated.Prefix,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.LastUsedAt != nil {
		t.Error("a new token has already been used")
	}

	// A never-used token is touched whatever the throttle: there is no earlier
	// write to be within it.
	if err := db.TouchAPIToken(ctx, pool, created.ID, time.Now(), time.Hour); err != nil {
		t.Fatal(err)
	}
	first, err := db.APITokenByID(ctx, pool, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.LastUsedAt == nil {
		t.Fatal("the first use was not recorded")
	}

	// Immediately afterwards, the throttle suppresses the write.
	if err := db.TouchAPIToken(ctx, pool, created.ID, time.Now(), time.Hour); err != nil {
		t.Fatal(err)
	}
	second, err := db.APITokenByID(ctx, pool, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !second.LastUsedAt.Equal(*first.LastUsedAt) {
		t.Error("a throttled touch wrote anyway")
	}
}

// Revocation has to be immediate: the next request carrying the token must
// find nothing.
func TestRevokedTokenNoLongerAuthenticates(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()
	u := newUser(t, pool, "Falk")

	generated, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateAPIToken(ctx, pool, db.NewAPIToken{
		UserID: u.ID, Name: "revoked", Hash: generated.Hash, Prefix: generated.Prefix,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := db.DeleteAPIToken(ctx, pool, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.APITokenByHash(ctx, pool, generated.Hash); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("a revoked token still authenticates: %v", err)
	}
	if err := db.DeleteAPIToken(ctx, pool, created.ID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("revoking twice gave %v, want ErrNotFound", err)
	}
}

func TestExpiredTokensAreCollected(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()
	u := newUser(t, pool, "Gerd")

	past := time.Now().Add(-time.Hour)
	expired, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateAPIToken(ctx, pool, db.NewAPIToken{
		UserID: u.ID, Name: "old", Hash: expired.Hash, Prefix: expired.Prefix,
		ExpiresAt: &past,
	}); err != nil {
		t.Fatal(err)
	}

	live, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	kept, err := db.CreateAPIToken(ctx, pool, db.NewAPIToken{
		UserID: u.ID, Name: "current", Hash: live.Hash, Prefix: live.Prefix,
	})
	if err != nil {
		t.Fatal(err)
	}

	removed, err := db.DeleteExpiredAPITokens(ctx, pool, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("collected %d tokens, want 1", removed)
	}
	if _, err := db.APITokenByID(ctx, pool, kept.ID); err != nil {
		t.Errorf("a token with no expiry was collected: %v", err)
	}
}

func TestDeletingAUserRemovesTheirTokens(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()
	u := newUser(t, pool, "Hanna")

	generated, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateAPIToken(ctx, pool, db.NewAPIToken{
		UserID: u.ID, Name: "gone", Hash: generated.Hash, Prefix: generated.Prefix,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := db.DeleteUser(ctx, pool, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.APITokenByID(ctx, pool, created.ID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("a deleted user's token survived: %v", err)
	}
}
