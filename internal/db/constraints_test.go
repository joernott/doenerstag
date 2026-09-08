package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joernott/doenerstag/internal/testdb"
)

// fixture builds the minimum set of rows the order constraints need, and
// returns the ids that tests reference.
type fixture struct {
	pool       *pgxpool.Pool
	userID     string
	restaurant string
	menuItem   string
}

func newFixture(t *testing.T) fixture {
	t.Helper()

	pool := testdb.Migrated(t)
	ctx := context.Background()

	f := fixture{
		pool:       pool,
		userID:     "00000000-0000-7000-8000-0000000000a1",
		restaurant: "00000000-0000-7000-8000-0000000000b1",
		menuItem:   "00000000-0000-7000-8000-0000000000c1",
	}

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("building the fixture: %v\n%s", err, sql)
		}
	}

	exec(`INSERT INTO app_user (id, name, password_hash) VALUES ($1, 'anna', 'x')`, f.userID)
	exec(`INSERT INTO restaurant (id, name, currency_code) VALUES ($1, 'Döner Palast', 'EUR')`,
		f.restaurant)
	exec(`INSERT INTO menu_item (id, restaurant_id, name, price_cents)
	      VALUES ($1, $2, 'Döner Kebab', 650)`, f.menuItem, f.restaurant)

	return f
}

// The rule that stops an order closing after the food has already arrived. It
// is enforced in the database as well as the frontend because the API is
// documented for third-party use.
func TestOrderDeadlineMustPrecedeFulfilment(t *testing.T) {
	f := newFixture(t)

	fulfilment := time.Now().Add(2 * time.Hour)

	err := insertOrder(t, f, fulfilment.Add(time.Hour), fulfilment)
	if err == nil {
		t.Error("an order closing after the food arrives was accepted")
	}

	err = insertOrder(t, f, fulfilment, fulfilment)
	if err == nil {
		t.Error("an order with deadline equal to the fulfilment time was accepted")
	}

	err = insertOrder(t, f, fulfilment.Add(-time.Hour), fulfilment)
	if err != nil {
		t.Errorf("a valid order was rejected: %v", err)
	}
}

func insertOrder(t *testing.T, f fixture, deadline, fulfilment time.Time) error {
	t.Helper()
	return execErr(t, f.pool, `
		INSERT INTO food_order
			(id, creator_id, restaurant_id, fulfilment, fulfilment_at, deadline_at, currency_code)
		VALUES
			(gen_random_uuid(), $1, $2, 'pickup', $3, $4, 'EUR')`,
		f.userID, f.restaurant, fulfilment, deadline)
}

func TestOrderFulfilmentMustBePickupOrDelivery(t *testing.T) {
	f := newFixture(t)
	fulfilment := time.Now().Add(2 * time.Hour)

	for _, mode := range []string{"pickup", "delivery"} {
		err := execErr(t, f.pool, `
			INSERT INTO food_order
				(id, creator_id, restaurant_id, fulfilment, fulfilment_at, deadline_at, currency_code)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, 'EUR')`,
			f.userID, f.restaurant, mode, fulfilment, fulfilment.Add(-time.Hour))
		if err != nil {
			t.Errorf("fulfilment mode %q was rejected: %v", mode, err)
		}
	}

	err := execErr(t, f.pool, `
		INSERT INTO food_order
			(id, creator_id, restaurant_id, fulfilment, fulfilment_at, deadline_at, currency_code)
		VALUES (gen_random_uuid(), $1, $2, 'teleport', $3, $4, 'EUR')`,
		f.userID, f.restaurant, fulfilment, fulfilment.Add(-time.Hour))
	if err == nil {
		t.Error("an unknown fulfilment mode was accepted")
	}
}

// An order may exist with no items at all: it is created before anyone has
// added anything, and that is the normal case.
func TestAnOrderMayHaveNoItems(t *testing.T) {
	f := newFixture(t)
	fulfilment := time.Now().Add(2 * time.Hour)

	if err := insertOrder(t, f, fulfilment.Add(-time.Hour), fulfilment); err != nil {
		t.Errorf("an empty order was rejected: %v", err)
	}
}

func TestOrderItemQuantityMustBeAtLeastOne(t *testing.T) {
	f := newFixture(t)
	orderID := insertValidOrder(t, f)

	for _, quantity := range []int{0, -1} {
		err := execErr(t, f.pool, `
			INSERT INTO order_item
				(id, order_id, user_id, menu_item_id, quantity, item_name, unit_price_cents)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, 'Döner Kebab', 650)`,
			orderID, f.userID, f.menuItem, quantity)
		if err == nil {
			t.Errorf("quantity %d was accepted", quantity)
		}
	}

	err := execErr(t, f.pool, `
		INSERT INTO order_item
			(id, order_id, user_id, menu_item_id, quantity, item_name, unit_price_cents)
		VALUES (gen_random_uuid(), $1, $2, $3, 1, 'Döner Kebab', 650)`,
		orderID, f.userID, f.menuItem)
	if err != nil {
		t.Errorf("quantity 1 was rejected: %v", err)
	}
}

// The same person ordering the same dish twice, once with onions and once
// without, is two rows. There must be no unique constraint preventing it.
func TestTheSameItemMayBeOrderedTwiceByOnePerson(t *testing.T) {
	f := newFixture(t)
	orderID := insertValidOrder(t, f)

	for i := range 2 {
		err := execErr(t, f.pool, `
			INSERT INTO order_item
				(id, order_id, user_id, menu_item_id, quantity, item_name, unit_price_cents, note)
			VALUES (gen_random_uuid(), $1, $2, $3, 1, 'Döner Kebab', 650, $4)`,
			orderID, f.userID, f.menuItem, []string{"no onions", "extra onions"}[i])
		if err != nil {
			t.Fatalf("insert %d was rejected: %v", i+1, err)
		}
	}
}

func TestPricesMayNotBeNegative(t *testing.T) {
	f := newFixture(t)

	err := execErr(t, f.pool, `
		INSERT INTO menu_item (id, restaurant_id, name, price_cents)
		VALUES (gen_random_uuid(), $1, 'Free Lunch', -1)`, f.restaurant)
	if err == nil {
		t.Error("a negative menu item price was accepted")
	}

	// A modification delta may be negative: "without meat" can cost less.
	err = execErr(t, f.pool, `
		INSERT INTO menu_item_modification (id, menu_item_id, name, price_delta_cents)
		VALUES (gen_random_uuid(), $1, 'without meat', -100)`, f.menuItem)
	if err != nil {
		t.Errorf("a negative modification delta was rejected: %v", err)
	}
}

func TestOpeningHoursDayOfWeekRange(t *testing.T) {
	f := newFixture(t)

	for _, day := range []int{0, 8, -1} {
		err := execErr(t, f.pool, `
			INSERT INTO opening_hours (id, restaurant_id, day_of_week, start_time, end_time)
			VALUES (gen_random_uuid(), $1, $2, '11:00', '22:00')`, f.restaurant, day)
		if err == nil {
			t.Errorf("day_of_week %d was accepted; ISO-8601 runs 1 to 7", day)
		}
	}

	for day := 1; day <= 7; day++ {
		err := execErr(t, f.pool, `
			INSERT INTO opening_hours (id, restaurant_id, day_of_week, start_time, end_time)
			VALUES (gen_random_uuid(), $1, $2, '11:00', '22:00')`, f.restaurant, day)
		if err != nil {
			t.Errorf("day_of_week %d was rejected: %v", day, err)
		}
	}
}

// An end before the start means the period crosses midnight and must be
// allowed. Equal times are a typo and must not be.
func TestOpeningHoursMayCrossMidnightButNotBeZeroLength(t *testing.T) {
	f := newFixture(t)

	err := execErr(t, f.pool, `
		INSERT INTO opening_hours (id, restaurant_id, day_of_week, start_time, end_time)
		VALUES (gen_random_uuid(), $1, 5, '22:00', '02:00')`, f.restaurant)
	if err != nil {
		t.Errorf("a period crossing midnight was rejected: %v", err)
	}

	err = execErr(t, f.pool, `
		INSERT INTO opening_hours (id, restaurant_id, day_of_week, start_time, end_time)
		VALUES (gen_random_uuid(), $1, 6, '11:00', '11:00')`, f.restaurant)
	if err == nil {
		t.Error("a zero-length opening period was accepted")
	}
}

// Several periods on one weekday are how a lunch break is expressed.
func TestSeveralOpeningPeriodsPerDayAreAllowed(t *testing.T) {
	f := newFixture(t)

	for _, period := range [][2]string{{"11:00", "14:30"}, {"17:00", "22:00"}} {
		err := execErr(t, f.pool, `
			INSERT INTO opening_hours (id, restaurant_id, day_of_week, start_time, end_time)
			VALUES (gen_random_uuid(), $1, 2, $2, $3)`, f.restaurant, period[0], period[1])
		if err != nil {
			t.Errorf("period %v was rejected: %v", period, err)
		}
	}
}

// Uniqueness is per restaurant and among the living, so two restaurants may
// both have a "Drinks" category and a deleted one must not block its reuse.
func TestCategoryNamesAreUniquePerRestaurantAmongTheLiving(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	other := "00000000-0000-7000-8000-0000000000b2"
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO restaurant (id, name, currency_code) VALUES ($1, 'Pizza Place', 'EUR')`,
		other); err != nil {
		t.Fatalf("creating a second restaurant: %v", err)
	}

	insert := func(restaurant, name string) error {
		return execErr(t, f.pool,
			`INSERT INTO menu_category (id, restaurant_id, name) VALUES (gen_random_uuid(), $1, $2)`,
			restaurant, name)
	}

	if err := insert(f.restaurant, "Drinks"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := insert(other, "Drinks"); err != nil {
		t.Errorf("a second restaurant could not have its own Drinks category: %v", err)
	}
	if err := insert(f.restaurant, "drinks"); err == nil {
		t.Error("a duplicate category name differing only in case was accepted")
	}

	// Soft delete it, and the name becomes reusable.
	if _, err := f.pool.Exec(ctx,
		`UPDATE menu_category SET deleted_at = now() WHERE restaurant_id = $1`,
		f.restaurant); err != nil {
		t.Fatalf("soft deleting: %v", err)
	}
	if err := insert(f.restaurant, "Drinks"); err != nil {
		t.Errorf("a soft-deleted category still blocks its own name: %v", err)
	}
}

func TestMenuItemNamesAreUniquePerRestaurantAmongTheLiving(t *testing.T) {
	f := newFixture(t)

	err := execErr(t, f.pool,
		`INSERT INTO menu_item (id, restaurant_id, name, price_cents)
		 VALUES (gen_random_uuid(), $1, 'döner kebab', 700)`, f.restaurant)
	if err == nil {
		t.Error("a duplicate menu item name differing only in case was accepted")
	}
}

// Deleting a restaurant an order points at must be refused, or the order loses
// the thing it is an order for.
func TestRestaurantReferencedByAnOrderCannotBeDeleted(t *testing.T) {
	f := newFixture(t)
	insertValidOrder(t, f)

	err := execErr(t, f.pool, `DELETE FROM restaurant WHERE id = $1`, f.restaurant)
	if err == nil {
		t.Error("a restaurant with an order against it was deleted")
	}
}

// Deleting a menu item an order item references must be refused too. Menu items
// are soft-deleted instead, and cleanup removes them once nothing points at
// them.
func TestMenuItemReferencedByAnOrderItemCannotBeDeleted(t *testing.T) {
	f := newFixture(t)
	orderID := insertValidOrder(t, f)

	if err := execErr(t, f.pool, `
		INSERT INTO order_item
			(id, order_id, user_id, menu_item_id, quantity, item_name, unit_price_cents)
		VALUES (gen_random_uuid(), $1, $2, $3, 1, 'Döner Kebab', 650)`,
		orderID, f.userID, f.menuItem); err != nil {
		t.Fatalf("adding an order item: %v", err)
	}

	err := execErr(t, f.pool, `DELETE FROM menu_item WHERE id = $1`, f.menuItem)
	if err == nil {
		t.Error("a menu item referenced by an order item was deleted")
	}
}

// Deleting a user reassigns their order items to the placeholder rather than
// taking the rows with them, which is what keeps expired orders readable.
func TestDeletingAUserReassignsTheirOrderItems(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	orderID := insertValidOrder(t, f)

	if _, err := f.pool.Exec(ctx, `
		INSERT INTO order_item
			(id, order_id, user_id, menu_item_id, quantity, item_name, unit_price_cents)
		VALUES (gen_random_uuid(), $1, $2, $3, 1, 'Döner Kebab', 650)`,
		orderID, f.userID, f.menuItem); err != nil {
		t.Fatalf("adding an order item: %v", err)
	}

	if _, err := f.pool.Exec(ctx, `DELETE FROM app_user WHERE id = $1`, f.userID); err != nil {
		t.Fatalf("deleting the user: %v", err)
	}

	var owner string
	if err := f.pool.QueryRow(ctx,
		`SELECT user_id::text FROM order_item WHERE order_id = $1`, orderID).Scan(&owner); err != nil {
		t.Fatalf("the order item did not survive its owner: %v", err)
	}
	if owner != DeletedUserID {
		t.Errorf("the order item is owned by %s, want the placeholder %s", owner, DeletedUserID)
	}

	var creator string
	if err := f.pool.QueryRow(ctx,
		`SELECT creator_id::text FROM food_order WHERE id = $1`, orderID).Scan(&creator); err != nil {
		t.Fatalf("the order did not survive its creator: %v", err)
	}
	if creator != DeletedUserID {
		t.Errorf("the order was created by %s, want the placeholder %s", creator, DeletedUserID)
	}
}

// Deleting a session or a token must follow its user, unlike order history.
func TestDeletingAUserCascadesToSessionsAndTokens(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.pool.Exec(ctx, `
		INSERT INTO session (id, user_id, absolute_expires)
		VALUES (gen_random_uuid(), $1, now() + interval '7 days')`, f.userID); err != nil {
		t.Fatalf("creating a session: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO api_token (id, user_id, name, token_hash, token_prefix)
		VALUES (gen_random_uuid(), $1, 'ci', 'hash', 'abcd1234')`, f.userID); err != nil {
		t.Fatalf("creating a token: %v", err)
	}

	if _, err := f.pool.Exec(ctx, `DELETE FROM app_user WHERE id = $1`, f.userID); err != nil {
		t.Fatalf("deleting the user: %v", err)
	}

	for _, table := range []string{"session", "api_token"} {
		var count int
		if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%d rows survived in %s", count, table)
		}
	}
}

func TestOneSessionPerUser(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	insert := func() error {
		return execErr(t, f.pool, `
			INSERT INTO session (id, user_id, absolute_expires)
			VALUES (gen_random_uuid(), $1, now() + interval '7 days')`, f.userID)
	}

	if err := insert(); err != nil {
		t.Fatalf("first session: %v", err)
	}
	if err := insert(); err == nil {
		t.Error("a second concurrent session was accepted for one user")
	}

	// Replacing it is how logging in elsewhere works.
	if _, err := f.pool.Exec(ctx, `DELETE FROM session WHERE user_id = $1`, f.userID); err != nil {
		t.Fatalf("removing the session: %v", err)
	}
	if err := insert(); err != nil {
		t.Errorf("a replacement session was rejected: %v", err)
	}
}

func TestUserNamesAreUniqueCaseInsensitively(t *testing.T) {
	f := newFixture(t)

	err := execErr(t, f.pool,
		`INSERT INTO app_user (id, name, password_hash) VALUES (gen_random_uuid(), 'ANNA', 'x')`)
	if err == nil {
		t.Error("ANNA was accepted alongside anna")
	}
}

func TestImageMediaTypeIsRestricted(t *testing.T) {
	pool := testdb.Migrated(t)

	insert := func(mediaType string) error {
		return execErr(t, pool, `
			INSERT INTO image
				(id, media_type, width, height, byte_size, data,
				 thumb_media_type, thumb_width, thumb_height, thumb_data, sha256)
			VALUES (gen_random_uuid(), $1, 10, 10, 3, '\x010203',
			        $1, 5, 5, '\x010203', sha256('x'::bytea))`, mediaType)
	}

	for _, allowed := range []string{"image/jpeg", "image/png", "image/gif"} {
		if err := insert(allowed); err != nil {
			t.Errorf("media type %q was rejected: %v", allowed, err)
		}
	}
	for _, refused := range []string{"image/svg+xml", "image/webp", "application/pdf", "text/html"} {
		if err := insert(refused); err == nil {
			t.Errorf("media type %q was accepted", refused)
		}
	}
}

// byte_size must describe the bytes actually stored, or the metrics and the
// Content-Length header would disagree with the body.
func TestImageByteSizeMustMatchTheData(t *testing.T) {
	pool := testdb.Migrated(t)

	err := execErr(t, pool, `
		INSERT INTO image
			(id, media_type, width, height, byte_size, data,
			 thumb_media_type, thumb_width, thumb_height, thumb_data, sha256)
		VALUES (gen_random_uuid(), 'image/png', 10, 10, 999, '\x010203',
		        'image/png', 5, 5, '\x010203', sha256('x'::bytea))`)
	if err == nil {
		t.Error("byte_size disagreeing with the stored data was accepted")
	}
}

func TestContentPageKeyIsRestricted(t *testing.T) {
	pool := testdb.Migrated(t)

	err := execErr(t, pool,
		`INSERT INTO content_page (id, key, html) VALUES (gen_random_uuid(), 'about', '<p>x</p>')`)
	if err == nil {
		t.Error("an unknown content page key was accepted")
	}
}

func insertValidOrder(t *testing.T, f fixture) string {
	t.Helper()

	fulfilment := time.Now().Add(2 * time.Hour)
	var id string
	err := f.pool.QueryRow(context.Background(), `
		INSERT INTO food_order
			(id, creator_id, restaurant_id, fulfilment, fulfilment_at, deadline_at, currency_code)
		VALUES (gen_random_uuid(), $1, $2, 'pickup', $3, $4, 'EUR')
		RETURNING id::text`,
		f.userID, f.restaurant, fulfilment, fulfilment.Add(-time.Hour)).Scan(&id)
	if err != nil {
		t.Fatalf("creating an order: %v", err)
	}
	return id
}
