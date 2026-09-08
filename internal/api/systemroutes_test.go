package api_test

import (
	"net/http"
	"testing"
)

// The two endpoints an operator points a monitor at.
//
// system_test.go covers the version endpoint against a hand-built handler.
// These two need a real database to say anything, and neither had a test until
// sprint 14 counted which routes the suite reaches: both were registered,
// documented and never called.

// Health is what a monitor polls, so the answer has to be cheap and honest.
func TestHealthReportsAWorkingDatabase(t *testing.T) {
	f := newAPIFixture(t)

	rec := f.get("/health")
	if rec.Code != http.StatusOK {
		t.Fatalf("health: %d %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Status string `json:"status"`
	}
	decode(t, rec, &body)
	if body.Status != "ok" {
		t.Errorf("status is %q, want ok", body.Status)
	}
}

// Metrics counts what docs/04_api.md says it counts.
//
// The assertions are relative -- an order added moves orders_active by one --
// because the fixture's database is migrated and seeded with reference data,
// and pinning absolute numbers would make this test fail whenever a seed
// changes for unrelated reasons.
func TestMetricsCountsWhatItSaysItCounts(t *testing.T) {
	f := newAPIFixture(t)

	before := f.readMetrics()

	cook := f.register("cook")
	restaurant := f.createRestaurant("Döner Palast", cook)

	rec := f.post("/restaurants/"+restaurant.ID+"/menu-items", map[string]any{
		"name": "Döner Kebab", "external_id": "1", "price_cents": 650,
	}, cook...)
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating a menu item: %d %s", rec.Code, rec.Body.String())
	}

	after := f.readMetrics()

	if after.Users != before.Users+1 {
		t.Errorf("users went from %d to %d after one registration",
			before.Users, after.Users)
	}
	if after.Restaurants != before.Restaurants+1 {
		t.Errorf("restaurants went from %d to %d after one was created",
			before.Restaurants, after.Restaurants)
	}
	if after.MenuItems != before.MenuItems+1 {
		t.Errorf("menu items went from %d to %d after one was added",
			before.MenuItems, after.MenuItems)
	}

	// The pool numbers are the reason this endpoint exists at all: a monitor
	// that only saw row counts could not tell a busy server from a stuck one.
	if after.DBConnectionsMax <= 0 {
		t.Errorf("db_connections_max is %d", after.DBConnectionsMax)
	}
	if after.DBConnectionsOpen < after.DBConnectionsIdle {
		t.Errorf("%d connections are open and %d of them idle",
			after.DBConnectionsOpen, after.DBConnectionsIdle)
	}
}

// An expired order is counted as expired rather than as active.
func TestMetricsSeparatesActiveOrdersFromExpiredOnes(t *testing.T) {
	o := newOrderFixture(t)

	// The fixture's own order is two hours from its deadline.
	live := o.readMetrics()
	if live.OrdersActive < 1 {
		t.Fatalf("an order with a future deadline is not counted as active: %+v", live)
	}

	// The counter reads the database clock rather than the fixture's, so the
	// order has to be given a deadline in the past rather than the fixture
	// being moved forward.
	if _, err := o.pool.Exec(o.ctx(),
		"UPDATE food_order SET deadline_at = now() - interval '1 hour' WHERE id = $1",
		o.order.ID); err != nil {
		t.Fatalf("expiring the order: %v", err)
	}

	expired := o.readMetrics()
	if expired.OrdersActive != live.OrdersActive-1 {
		t.Errorf("active orders went from %d to %d after one expired",
			live.OrdersActive, expired.OrdersActive)
	}
	if expired.OrdersExpired != live.OrdersExpired+1 {
		t.Errorf("expired orders went from %d to %d",
			live.OrdersExpired, expired.OrdersExpired)
	}
}

// metricsBody mirrors the endpoint's response.
type metricsBody struct {
	Restaurants       int   `json:"restaurants"`
	MenuItems         int   `json:"menu_items"`
	OrdersActive      int   `json:"orders_active"`
	OrdersExpired     int   `json:"orders_expired"`
	Users             int   `json:"users"`
	DBConnectionsOpen int32 `json:"db_connections_open"`
	DBConnectionsIdle int32 `json:"db_connections_idle"`
	DBConnectionsMax  int32 `json:"db_connections_max"`
}

func (f *apiFixture) readMetrics() metricsBody {
	f.t.Helper()

	rec := f.get("/metrics")
	if rec.Code != http.StatusOK {
		f.t.Fatalf("metrics: %d %s", rec.Code, rec.Body.String())
	}
	var body metricsBody
	decode(f.t, rec, &body)
	return body
}
