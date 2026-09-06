package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/joernott/doenerstag/internal/api"
	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
)

type impactResponse struct {
	ActiveOrders        int `json:"active_orders"`
	ActiveItems         int `json:"active_items"`
	ExpiredItems        int `json:"expired_items"`
	CreatedActiveOrders int `json:"created_active_orders"`
}

// F2.6 is about orders, and Sprint 8 is what gives orders an API. These tests
// insert the fixtures directly, as docs/14_implementation_plan.md says they
// will: "Its tests insert order fixtures directly; Sprint 8 re-verifies the
// behaviour against real orders."
func (f *apiFixture) seedOrder(creator string, deadline time.Time) uuid.UUID {
	f.t.Helper()
	ctx := context.Background()

	creatorID := uuid.MustParse(f.userID(creator))
	orderID := uuid.Must(uuid.NewV7())

	if _, err := f.pool.Exec(ctx, `
		INSERT INTO food_order (id, creator_id, restaurant_id, fulfilment,
		                        fulfilment_at, deadline_at, currency_code,
		                        created_by, updated_by)
		VALUES ($1, $2, $3, 'pickup', $4::timestamptz + interval '1 hour',
		        $4::timestamptz, 'EUR', $2, $2)`,
		orderID, creatorID, f.seedRestaurant(), deadline); err != nil {
		f.t.Fatalf("seeding an order: %v", err)
	}
	return orderID
}

// seedRestaurant creates one restaurant for the fixture to hang orders off,
// reusing it if it already exists.
func (f *apiFixture) seedRestaurant() uuid.UUID {
	f.t.Helper()
	ctx := context.Background()

	var id uuid.UUID
	err := f.pool.QueryRow(ctx,
		`SELECT id FROM restaurant WHERE name = 'Test Restaurant'`).Scan(&id)
	if err == nil {
		return id
	}

	id = uuid.Must(uuid.NewV7())
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO restaurant (id, name, currency_code) VALUES ($1, 'Test Restaurant', 'EUR')`,
		id); err != nil {
		f.t.Fatalf("seeding a restaurant: %v", err)
	}
	return id
}

// seedMenuItem creates a menu item for the fixture's restaurant.
func (f *apiFixture) seedMenuItem() uuid.UUID {
	f.t.Helper()
	ctx := context.Background()

	restaurant := f.seedRestaurant()

	var categoryID uuid.UUID
	err := f.pool.QueryRow(ctx,
		`SELECT id FROM menu_category WHERE restaurant_id = $1 LIMIT 1`, restaurant).
		Scan(&categoryID)
	if err != nil {
		categoryID = uuid.Must(uuid.NewV7())
		if _, err := f.pool.Exec(ctx, `
			INSERT INTO menu_category (id, restaurant_id, name, sort_order)
			VALUES ($1, $2, 'Test', 1)`, categoryID, restaurant); err != nil {
			f.t.Fatalf("seeding a menu category: %v", err)
		}
	}

	var itemID uuid.UUID
	err = f.pool.QueryRow(ctx,
		`SELECT id FROM menu_item WHERE category_id = $1 LIMIT 1`, categoryID).
		Scan(&itemID)
	if err == nil {
		return itemID
	}

	itemID = uuid.Must(uuid.NewV7())
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO menu_item (id, restaurant_id, category_id, name, price_cents)
		VALUES ($1, $2, $3, 'Döner', 650)`,
		itemID, restaurant, categoryID); err != nil {
		f.t.Fatalf("seeding a menu item: %v", err)
	}
	return itemID
}

// seedItem puts one item belonging to a user into an order.
func (f *apiFixture) seedItem(orderID uuid.UUID, owner string) uuid.UUID {
	f.t.Helper()

	ownerID := uuid.MustParse(f.userID(owner))
	itemID := uuid.Must(uuid.NewV7())

	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO order_item (id, order_id, user_id, menu_item_id, quantity,
		                        item_name, unit_price_cents, created_by, updated_by)
		VALUES ($1, $2, $3, $4, 1, 'Döner', 650, $3, $3)`,
		itemID, orderID, ownerID, f.seedMenuItem()); err != nil {
		f.t.Fatalf("seeding an order item: %v", err)
	}
	return itemID
}

func (f *apiFixture) itemOwner(itemID uuid.UUID) uuid.UUID {
	f.t.Helper()

	var owner uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		`SELECT user_id FROM order_item WHERE id = $1`, itemID).Scan(&owner); err != nil {
		f.t.Fatalf("reading the item's owner: %v", err)
	}
	return owner
}

func (f *apiFixture) itemExists(itemID uuid.UUID) bool {
	f.t.Helper()

	var exists bool
	if err := f.pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM order_item WHERE id = $1)`, itemID).
		Scan(&exists); err != nil {
		f.t.Fatalf("checking the item: %v", err)
	}
	return exists
}

// The impact is the only warning a user gets before an irreversible deletion,
// so it has to be right.
func TestDeletionImpactCountsWhatWouldChange(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("anna")
	f.register("other")

	active := f.seedOrder("other", f.now.Add(2*time.Hour))
	expired := f.seedOrder("other", f.now.Add(-2*time.Hour))
	mine := f.seedOrder("anna", f.now.Add(2*time.Hour))

	f.seedItem(active, "anna")
	f.seedItem(active, "other")
	f.seedItem(expired, "anna")
	f.seedItem(expired, "anna")

	rec := f.get("/users/"+f.userID("anna")+"/deletion-impact", cookies...)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	var impact impactResponse
	decode(t, rec, &impact)

	// Two active orders: the one they have an item in, and the one they made.
	if impact.ActiveOrders != 2 {
		t.Errorf("active orders %d, want 2", impact.ActiveOrders)
	}
	if impact.ActiveItems != 1 {
		t.Errorf("active items %d, want 1", impact.ActiveItems)
	}
	if impact.ExpiredItems != 2 {
		t.Errorf("expired items %d, want 2", impact.ExpiredItems)
	}
	if impact.CreatedActiveOrders != 1 {
		t.Errorf("created active orders %d, want 1", impact.CreatedActiveOrders)
	}

	// Reporting the impact must not change anything.
	if !f.itemExists(f.seedItem(mine, "anna")) {
		t.Error("the impact query removed something")
	}
}

// The three F2.6 rules, together, on one deletion.
func TestDeletingAnAccountAppliesTheF26Rules(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("bertil")
	f.register("other")

	active := f.seedOrder("other", f.now.Add(2*time.Hour))
	expired := f.seedOrder("other", f.now.Add(-2*time.Hour))
	created := f.seedOrder("bertil", f.now.Add(2*time.Hour))

	activeItem := f.seedItem(active, "bertil")
	expiredItem := f.seedItem(expired, "bertil")
	otherItem := f.seedItem(active, "other")

	rec := f.remove("/users/"+f.userID("bertil"), cookies...)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204: %s", rec.Code, rec.Body.String())
	}

	// Rule 1: items in expired orders pass to the placeholder, so the order
	// still reads correctly without naming anyone.
	if !f.itemExists(expiredItem) {
		t.Error("an item in an expired order was deleted rather than reassigned")
	} else if owner := f.itemOwner(expiredItem); owner != model.DeletedUserID {
		t.Errorf("the expired item belongs to %s, want the placeholder", owner)
	}

	// Rule 2: items in active orders are removed outright.
	if f.itemExists(activeItem) {
		t.Error("an item in an active order survived the deletion")
	}

	// Rule 3: created orders pass to the placeholder and survive.
	var creator uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		`SELECT creator_id FROM food_order WHERE id = $1`, created).Scan(&creator); err != nil {
		t.Fatalf("the created order did not survive: %v", err)
	}
	if creator != model.DeletedUserID {
		t.Errorf("the order's creator is %s, want the placeholder", creator)
	}

	// Nobody else's data was touched.
	if !f.itemExists(otherItem) {
		t.Error("another user's item was deleted")
	}

	// And the account is gone.
	if _, err := db.UserByName(context.Background(), f.pool, "bertil"); err == nil {
		t.Error("the account still exists")
	}
}

// Deleting your own account logs you out, and the session row goes with it.
func TestDeletingYourOwnAccountEndsTheSession(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("carla")

	rec := f.remove("/users/"+f.userID("carla"), cookies...)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	if cleared := cookie(rec, api.SessionCookieName); cleared == nil || cleared.MaxAge >= 0 {
		t.Error("the session cookie was not cleared")
	}

	after := f.do(request{method: http.MethodGet, path: "/probe", cookies: cookies})
	expectError(t, after, http.StatusUnauthorized, api.CodeSessionSuperseded)
}

func TestOneUserCannotDeleteAnother(t *testing.T) {
	f := newAPIFixture(t)
	f.register("dora")
	intruder := f.register("erik")

	rec := f.remove("/users/"+f.userID("dora"), intruder...)
	expectError(t, rec, http.StatusForbidden, api.CodeNotItemOwner)

	anonymous := f.remove("/users/" + f.userID("dora"))
	expectError(t, anonymous, http.StatusUnauthorized, api.CodeNotAuthenticated)

	if _, err := db.UserByName(context.Background(), f.pool, "dora"); err != nil {
		t.Error("the account was deleted anyway")
	}
}

func TestTheAdministratorCanDeleteAnAccount(t *testing.T) {
	f := newAPIFixture(t)
	f.register("frieda")
	admin := f.loginAsAdmin("chief")

	rec := f.remove("/users/"+f.userID("frieda"), admin...)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	// The administrator's own session must survive deleting somebody else.
	if who := f.whoami(admin...); !who.Authenticated {
		t.Error("deleting an account ended the administrator's session")
	}
}

// F2.8: the administrator cannot be deleted, by anyone including itself.
func TestTheAdministratorCannotBeDeleted(t *testing.T) {
	f := newAPIFixture(t)
	admin := f.loginAsAdmin("chief")

	rec := f.remove("/users/"+f.userID("chief"), admin...)
	expectError(t, rec, http.StatusForbidden, api.CodeAdminRequired)

	if _, err := db.UserByName(context.Background(), f.pool, "chief"); err != nil {
		t.Error("the administrator was deleted")
	}
}

func TestThePlaceholderCannotBeDeleted(t *testing.T) {
	f := newAPIFixture(t)
	admin := f.loginAsAdmin("chief")

	rec := f.remove("/users/"+model.DeletedUserID.String(), admin...)
	expectError(t, rec, http.StatusForbidden, api.CodePlaceholderReadOnly)

	if _, err := db.UserByID(context.Background(), f.pool, model.DeletedUserID); err != nil {
		t.Error("the placeholder was deleted")
	}
}

// The impact is nobody else's business.
func TestDeletionImpactIsPrivate(t *testing.T) {
	f := newAPIFixture(t)
	f.register("gustav")
	intruder := f.register("hanna")

	rec := f.get("/users/"+f.userID("gustav")+"/deletion-impact", intruder...)
	expectError(t, rec, http.StatusForbidden, api.CodeNotItemOwner)
}

// A user with nothing to lose sees zeroes rather than an error.
func TestDeletionImpactOfAnUnusedAccount(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("ida")

	rec := f.get("/users/"+f.userID("ida")+"/deletion-impact", cookies...)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	var impact impactResponse
	decode(t, rec, &impact)
	if impact != (impactResponse{}) {
		t.Errorf("an account with no orders reports %+v", impact)
	}
}

// The deletion is one transaction: a partial result -- items reassigned but the
// account still present, or the account gone and its items orphaned -- would be
// worse than either outcome.
func TestDeletionLeavesNoDanglingItems(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("jonas")
	f.register("other")

	expired := f.seedOrder("other", f.now.Add(-2*time.Hour))
	for range 3 {
		f.seedItem(expired, "jonas")
	}

	if rec := f.remove("/users/"+f.userID("jonas"), cookies...); rec.Code != http.StatusNoContent {
		t.Fatalf("deleting: %s", rec.Body.String())
	}

	var orphans int
	if err := f.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM order_item i
		LEFT JOIN app_user u ON u.id = i.user_id
		WHERE u.id IS NULL`).Scan(&orphans); err != nil {
		t.Fatal(err)
	}
	if orphans != 0 {
		t.Errorf("%d order items point at an account that no longer exists", orphans)
	}
}
