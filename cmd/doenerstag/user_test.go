package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/testdb"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testdb.Shutdown()
	os.Exit(code)
}

// configFor writes a configuration file pointing at the test database, the way
// an operator's /etc/doenerstag/doenerstag.yaml points at theirs.
func configFor(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	c := pool.Config().ConnConfig
	content := fmt.Sprintf(
		"database:\n  server: %q\n  port: %d\n  name: %q\n  user: %q\n  password: %q\n  sslmode: disable\n",
		c.Host, c.Port, c.Database, c.User, c.Password)
	path := filepath.Join(t.TempDir(), "doenerstag.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// 20.4: `user delete` applies F2.6 like the browser does. It used to run a bare
// DELETE, which left the account's items in an open order standing, owned by
// the placeholder and still counted in the total.
func TestDeletingAUserFromTheCommandLineFollowsF26(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()

	creator, err := db.CreateUser(ctx, pool, db.NewUser{Name: "gastgeber", PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	leaving, err := db.CreateUser(ctx, pool, db.NewUser{Name: "geht", PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	restaurant, err := db.CreateRestaurant(ctx, pool,
		db.NewRestaurant{Name: "Pinar", CurrencyCode: "EUR"}, creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	dish, err := db.CreateMenuItem(ctx, pool, db.NewMenuItem{
		RestaurantID: restaurant.ID, Name: "Döner", PriceCents: 950, Available: true,
	}, creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	open, err := db.CreateOrder(ctx, pool, db.NewOrder{
		CreatorID: creator.ID, RestaurantID: restaurant.ID, Fulfilment: "pickup",
		FulfilmentAt: time.Now().Add(3 * time.Hour), DeadlineAt: time.Now().Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddOrderItem(ctx, pool, db.NewOrderItem{
		OrderID: open.ID, UserID: leaving.ID, MenuItemID: dish.ID, Quantity: 1,
	}); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if code := run([]string{"user", "delete", "--username", "geht", "--config", configFor(t, pool)},
		stdout, stderr); code != exitOK {
		t.Fatalf("user delete exited %d:\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	items, err := db.ListOrderItems(ctx, pool, open.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("the open order still holds %d item(s) of the deleted account; F2.6 removes them", len(items))
	}
}
