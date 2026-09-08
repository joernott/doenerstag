package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/testdb"
)

// cleanupFixture builds a database with something for every step to remove.
type cleanupFixture struct {
	t    *testing.T
	pool *pgxpool.Pool
	now  time.Time

	user       uuid.UUID
	restaurant uuid.UUID
	category   uuid.UUID
	menuItem   uuid.UUID
	oldOrder   uuid.UUID
	newOrder   uuid.UUID
}

func newCleanupFixture(t *testing.T) *cleanupFixture {
	t.Helper()

	pool := testdb.Migrated(t)
	f := &cleanupFixture{t: t, pool: pool, now: time.Now()}
	ctx := context.Background()

	f.user = newUser(t, pool, "Räumer").ID

	f.restaurant = uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(ctx, `
		INSERT INTO restaurant (id, name, currency_code) VALUES ($1, 'Aufräumen', 'EUR')`,
		f.restaurant); err != nil {
		t.Fatal(err)
	}

	f.category = uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(ctx, `
		INSERT INTO menu_category (id, restaurant_id, name) VALUES ($1, $2, 'Alles')`,
		f.category, f.restaurant); err != nil {
		t.Fatal(err)
	}

	f.menuItem = uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(ctx, `
		INSERT INTO menu_item (id, restaurant_id, category_id, name, price_cents)
		VALUES ($1, $2, $3, 'Döner', 650)`,
		f.menuItem, f.restaurant, f.category); err != nil {
		t.Fatal(err)
	}

	// One order well past the retention window, one comfortably inside it.
	f.oldOrder = f.addOrder(-30 * 24 * time.Hour)
	f.newOrder = f.addOrder(-time.Hour)
	return f
}

func (f *cleanupFixture) addOrder(deadlineOffset time.Duration) uuid.UUID {
	f.t.Helper()

	id := uuid.Must(uuid.NewV7())
	deadline := f.now.Add(deadlineOffset)
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO food_order (id, creator_id, restaurant_id, fulfilment,
		                        fulfilment_at, deadline_at, currency_code)
		VALUES ($1, $2, $3, 'pickup', $4::timestamptz + interval '1 hour',
		        $4::timestamptz, 'EUR')`,
		id, f.user, f.restaurant, deadline); err != nil {
		f.t.Fatalf("seeding an order: %v", err)
	}
	return id
}

func (f *cleanupFixture) addItem(orderID uuid.UUID) uuid.UUID {
	f.t.Helper()

	id := uuid.Must(uuid.NewV7())
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO order_item (id, order_id, user_id, menu_item_id, quantity,
		                        item_name, unit_price_cents)
		VALUES ($1, $2, $3, $4, 1, 'Döner', 650)`,
		id, orderID, f.user, f.menuItem); err != nil {
		f.t.Fatalf("seeding an order item: %v", err)
	}
	return id
}

func (f *cleanupFixture) count(table, where string, args ...any) int {
	f.t.Helper()

	var n int
	if err := f.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM "+table+" WHERE "+where, args...).Scan(&n); err != nil {
		f.t.Fatalf("counting %s: %v", table, err)
	}
	return n
}

// Step 1: orders past the retention window go, and their items with them.
func TestCleanupRemovesOldOrders(t *testing.T) {
	f := newCleanupFixture(t)
	oldItem := f.addItem(f.oldOrder)
	newItem := f.addItem(f.newOrder)

	report, err := db.Cleanup(context.Background(), f.pool, 14*24*time.Hour, f.now, false)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	if got := stepCount(report, "orders"); got != 1 {
		t.Errorf("removed %d orders, want 1", got)
	}
	if f.count("food_order", "id = $1", f.oldOrder) != 0 {
		t.Error("the old order survived")
	}
	if f.count("food_order", "id = $1", f.newOrder) != 1 {
		t.Error("the recent order was removed")
	}

	// The cascade took the items.
	if f.count("order_item", "id = $1", oldItem) != 0 {
		t.Error("the old order's item survived")
	}
	if f.count("order_item", "id = $1", newItem) != 1 {
		t.Error("the recent order's item was removed")
	}
}

// Steps 2 to 5: soft-deleted rows go once nothing references them, and not
// before. This is the ordering the specification fixes, and the reason for it.
func TestCleanupReleasesSoftDeletedMenuDataInOrder(t *testing.T) {
	f := newCleanupFixture(t)
	ctx := context.Background()

	// An image the menu item uses, and one nothing uses.
	used := f.addImage()
	orphan := f.addImage()
	if _, err := f.pool.Exec(ctx,
		`UPDATE menu_item SET image_id = $2 WHERE id = $1`, f.menuItem, used); err != nil {
		t.Fatal(err)
	}

	// A modification, selected by an item in the old order.
	modification := uuid.Must(uuid.NewV7())
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO menu_item_modification (id, menu_item_id, name, deleted_at)
		VALUES ($1, $2, 'ohne Zwiebeln', now())`, modification, f.menuItem); err != nil {
		t.Fatal(err)
	}
	item := f.addItem(f.oldOrder)
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO order_item_modification (id, order_item_id, modification_id, name)
		VALUES ($1, $2, $3, 'ohne Zwiebeln')`,
		uuid.Must(uuid.NewV7()), item, modification); err != nil {
		t.Fatal(err)
	}

	// Soft-delete the whole chain.
	for _, stmt := range []string{
		`UPDATE menu_item SET deleted_at = now()`,
		`UPDATE menu_category SET deleted_at = now()`,
		`UPDATE restaurant SET deleted_at = now()`,
	} {
		if _, err := f.pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}

	// A first pass with a long retention keeps the old order, so nothing
	// downstream can be released yet.
	first, err := db.Cleanup(ctx, f.pool, 365*24*time.Hour, f.now, false)
	if err != nil {
		t.Fatal(err)
	}
	if stepCount(first, "menu items") != 0 {
		t.Error("a menu item was removed while an order still referenced it")
	}
	if stepCount(first, "menu item modifications") != 0 {
		t.Error("a modification was removed while an order item still referenced it")
	}
	// The unreferenced image goes immediately: nothing was pinning it.
	if stepCount(first, "images") != 1 {
		t.Errorf("removed %d images, want the one orphan", stepCount(first, "images"))
	}
	if f.count("image", "id = $1", used) != 1 {
		t.Error("the image the menu item uses was removed")
	}
	if f.count("image", "id = $1", orphan) != 0 {
		t.Error("the orphan image survived")
	}

	// Now with the real retention: the order goes, and everything it was
	// holding follows in the same run, because the steps run in dependency
	// order.
	second, err := db.Cleanup(ctx, f.pool, 14*24*time.Hour, f.now, false)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []struct {
		object string
		count  int64
	}{
		{"orders", 1},
		{"menu item modifications", 1},
		{"menu items", 1},
		{"menu categories", 1},
		{"images", 1},
	} {
		if got := stepCount(second, want.object); got != want.count {
			t.Errorf("step %q removed %d, want %d", want.object, got, want.count)
		}
	}

	// One run cleared the chain from the modification down to the image,
	// because the steps run in dependency order.
	if f.count("image", "id = $1", used) != 0 {
		t.Error("the image survived its menu item")
	}

	// The restaurant is still here, and correctly so: the fixture's recent
	// order references it, and that order is inside the retention window.
	// Removing a restaurant an order points at would make the order unreadable,
	// which is the same rule F3.5 enforces on the API.
	if stepCount(second, "restaurants") != 0 {
		t.Error("a restaurant was removed while a live order referenced it")
	}
	if f.count("restaurant", "id = $1", f.restaurant) != 1 {
		t.Fatal("the restaurant went despite a live order referencing it")
	}

	// Once that order ages out, the restaurant follows.
	third, err := db.Cleanup(ctx, f.pool, time.Minute, f.now, false)
	if err != nil {
		t.Fatal(err)
	}
	if stepCount(third, "orders") != 1 {
		t.Errorf("the recent order was not removed: %+v", third.Steps)
	}
	if stepCount(third, "restaurants") != 1 {
		t.Errorf("the restaurant was not released: %+v", third.Steps)
	}
	if f.count("restaurant", "id = $1", f.restaurant) != 0 {
		t.Error("the restaurant survived after nothing referenced it")
	}
}

func (f *cleanupFixture) addImage() uuid.UUID {
	f.t.Helper()

	id := uuid.Must(uuid.NewV7())

	// The column requires exactly 32 bytes, so a bare UUID's 16 will not do.
	// Doubling it keeps each fixture image's hash distinct, which is what
	// matters here -- deduplication is not under test.
	sum := append(append([]byte{}, id[:]...), id[:]...)

	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO image (id, media_type, width, height, byte_size, data,
		                   thumb_media_type, thumb_width, thumb_height, thumb_data, sha256)
		VALUES ($1, 'image/png', 1, 1, 4, '\x00010203'::bytea,
		        'image/png', 1, 1, '\x00010203'::bytea, $2::bytea)`,
		id, sum); err != nil {
		f.t.Fatalf("seeding an image: %v", err)
	}
	return id
}

// Step 6: expired sessions and tokens.
func TestCleanupRemovesExpiredSessionsAndTokens(t *testing.T) {
	f := newCleanupFixture(t)
	ctx := context.Background()

	live, err := db.ReplaceSession(ctx, f.pool, db.NewSession{
		UserID: f.user, Lifetime: time.Hour, IssuedAt: f.now,
	})
	if err != nil {
		t.Fatal(err)
	}

	// A second user, so the one-session-per-user constraint allows a stale row
	// to exist alongside the live one.
	stale := newUser(t, f.pool, "Vergessen")
	dead, err := db.ReplaceSession(ctx, f.pool, db.NewSession{
		UserID: stale.ID, Lifetime: time.Hour, IssuedAt: f.now.Add(-48 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	past := f.now.Add(-time.Hour)
	expired := uuid.Must(uuid.NewV7())
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO api_token (id, user_id, name, token_hash, token_prefix, expires_at)
		VALUES ($1, $2, 'alt', 'hash-expired', 'abcdefgh', $3)`,
		expired, f.user, past); err != nil {
		t.Fatal(err)
	}
	kept := uuid.Must(uuid.NewV7())
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO api_token (id, user_id, name, token_hash, token_prefix)
		VALUES ($1, $2, 'ohne Ablauf', 'hash-kept', 'ijklmnop')`,
		kept, f.user); err != nil {
		t.Fatal(err)
	}

	report, err := db.Cleanup(ctx, f.pool, 14*24*time.Hour, f.now, false)
	if err != nil {
		t.Fatal(err)
	}

	if stepCount(report, "sessions") != 1 {
		t.Errorf("removed %d sessions, want 1", stepCount(report, "sessions"))
	}
	if f.count("session", "id = $1", live.ID) != 1 {
		t.Error("the live session was removed")
	}
	if f.count("session", "id = $1", dead.ID) != 0 {
		t.Error("the expired session survived")
	}

	if stepCount(report, "api tokens") != 1 {
		t.Errorf("removed %d tokens, want 1", stepCount(report, "api tokens"))
	}
	if f.count("api_token", "id = $1", expired) != 0 {
		t.Error("the expired token survived")
	}
	if f.count("api_token", "id = $1", kept) != 1 {
		t.Error("a token with no expiry was removed")
	}
}

// The exit criterion: a dry run reports what a real run would remove, and a
// real run removes exactly that.
func TestADryRunReportsWhatARealRunRemoves(t *testing.T) {
	f := newCleanupFixture(t)
	ctx := context.Background()
	f.addItem(f.oldOrder)
	f.addImage()

	dry, err := db.Cleanup(ctx, f.pool, 14*24*time.Hour, f.now, true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !dry.DryRun {
		t.Error("the report does not say it was a dry run")
	}
	if dry.Total() == 0 {
		t.Fatal("the dry run found nothing to remove")
	}

	// Nothing changed.
	if f.count("food_order", "id = $1", f.oldOrder) != 1 {
		t.Fatal("the dry run deleted an order")
	}
	before := f.count("image", "TRUE")

	actual, err := db.Cleanup(ctx, f.pool, 14*24*time.Hour, f.now, false)
	if err != nil {
		t.Fatalf("real run: %v", err)
	}

	if len(dry.Steps) != len(actual.Steps) {
		t.Fatalf("the dry run reported %d steps and the real one %d",
			len(dry.Steps), len(actual.Steps))
	}
	for i := range dry.Steps {
		if dry.Steps[i].Object != actual.Steps[i].Object {
			t.Errorf("step %d is %q in the dry run and %q in the real one",
				i, dry.Steps[i].Object, actual.Steps[i].Object)
		}
		if dry.Steps[i].Count != actual.Steps[i].Count {
			t.Errorf("step %q: the dry run said %d, the real run removed %d",
				dry.Steps[i].Object, dry.Steps[i].Count, actual.Steps[i].Count)
		}
	}

	if after := f.count("image", "TRUE"); before-after != int(stepCount(actual, "images")) {
		t.Errorf("the image count fell by %d but the report says %d",
			before-after, stepCount(actual, "images"))
	}
}

// Running twice must be safe: the verb goes in a crontab, and a second run
// after a partial failure must not be a problem.
func TestCleanupIsIdempotent(t *testing.T) {
	f := newCleanupFixture(t)
	ctx := context.Background()
	f.addItem(f.oldOrder)

	first, err := db.Cleanup(ctx, f.pool, 14*24*time.Hour, f.now, false)
	if err != nil {
		t.Fatal(err)
	}
	if first.Total() == 0 {
		t.Fatal("the first run removed nothing")
	}

	second, err := db.Cleanup(ctx, f.pool, 14*24*time.Hour, f.now, false)
	if err != nil {
		t.Fatalf("the second run failed: %v", err)
	}
	if second.Total() != 0 {
		t.Errorf("the second run removed %d more rows: %+v", second.Total(), second.Steps)
	}
}

// A live installation with nothing to remove is the common case, and must not
// be an error.
func TestCleanupOnACleanDatabase(t *testing.T) {
	pool := testdb.Migrated(t)

	report, err := db.Cleanup(context.Background(), pool, 14*24*time.Hour, time.Now(), false)
	if err != nil {
		t.Fatalf("cleanup on a fresh database: %v", err)
	}
	if report.Total() != 0 {
		t.Errorf("removed %d rows from a fresh database: %+v", report.Total(), report.Steps)
	}
	// Every step still ran and reported, so the log is complete.
	if len(report.Steps) != 8 {
		t.Errorf("%d steps ran, want the eight the specification lists", len(report.Steps))
	}
}

func stepCount(report db.CleanupReport, object string) int64 {
	for _, step := range report.Steps {
		if step.Object == object {
			return step.Count
		}
	}
	return -1
}
