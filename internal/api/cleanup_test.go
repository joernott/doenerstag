package api_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/joernott/doenerstag/internal/api"
	"github.com/joernott/doenerstag/internal/db"
)

// The cleanup endpoint deletes, which is why it is the only system endpoint
// that asks who is calling. The other three expose counts and versions and are
// deliberately open.
func TestOnlyTheAdministratorMayRunTheCleanup(t *testing.T) {
	f := newAPIFixture(t)
	ordinary := f.register("tilda")

	anonymous := f.do(request{method: http.MethodPost, path: "/cleanup"})
	expectError(t, anonymous, http.StatusUnauthorized, api.CodeNotAuthenticated)

	refused := f.do(request{
		method: http.MethodPost, path: "/cleanup", cookies: ordinary,
	})
	expectError(t, refused, http.StatusForbidden, api.CodeAdminRequired)
}

// What it removes, and what it reports having removed. An order past the
// retention period goes; one inside it stays.
func TestTheCleanupRemovesExpiredOrdersAndSaysHowMany(t *testing.T) {
	f := newAPIFixture(t)
	admin := f.loginAsAdmin("ulla")
	f.register("volker")

	// One order well past any retention period, and one from an hour ago.
	old := f.seedOrder("volker", f.now.Add(-90*24*time.Hour))
	recent := f.seedOrder("volker", f.now.Add(-time.Hour))

	rec := f.do(request{
		method: http.MethodPost, path: "/cleanup", cookies: admin,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("the cleanup answered %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Removed map[string]int64 `json:"removed"`
		Total   int64            `json:"total"`
	}
	decode(t, rec, &body)

	if body.Removed["orders"] < 1 {
		t.Errorf("no orders were reported removed: %+v", body.Removed)
	}
	if body.Total < 1 {
		t.Errorf("the total is %d, want at least 1", body.Total)
	}

	// The old one is gone and the recent one is not, which is the whole point
	// of a retention period.
	if _, err := db.OrderByID(context.Background(), f.pool, old); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("the expired order survived: %v", err)
	}
	if _, err := db.OrderByID(context.Background(), f.pool, recent); err != nil {
		t.Errorf("an order inside the retention period was removed: %v", err)
	}
}
