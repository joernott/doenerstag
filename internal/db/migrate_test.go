package db_test

import (
	"context"
	"os"
	"testing"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/testdb"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testdb.Shutdown()
	os.Exit(code)
}

// The embedded migrations must be readable without a database, which is what
// lets the server check its schema expectation at startup.
func TestLatestVersionReadsTheEmbeddedMigrations(t *testing.T) {
	version, err := db.LatestVersion()
	if err != nil {
		t.Fatalf("LatestVersion: %v", err)
	}
	if version == 0 {
		t.Error("LatestVersion is 0, so no migrations are embedded")
	}
}

func TestMigrateUpFromEmpty(t *testing.T) {
	pool := testdb.Empty(t)
	dsn := testdb.DSNFor(t, currentDatabase(t, pool))

	migrator, err := db.NewMigrator(dsn, nil)
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })

	version, dirty, err := migrator.Version()
	if err != nil {
		t.Fatalf("Version on an empty database: %v", err)
	}
	if version != db.NoSchema || dirty {
		t.Errorf("empty database reports version %d dirty=%v, want %d false",
			version, dirty, db.NoSchema)
	}

	if err := migrator.Up(); err != nil {
		t.Fatalf("Up: %v", err)
	}

	latest, err := db.LatestVersion()
	if err != nil {
		t.Fatal(err)
	}

	version, dirty, err = migrator.Version()
	if err != nil {
		t.Fatalf("Version after Up: %v", err)
	}
	if version != latest {
		t.Errorf("after Up the version is %d, want %d", version, latest)
	}
	if dirty {
		t.Error("the database is dirty after a successful Up")
	}
}

// Applying an already-current schema must succeed and change nothing, because
// `doenerstag update` runs unconditionally.
func TestMigrateUpIsIdempotent(t *testing.T) {
	pool := testdb.Empty(t)
	dsn := testdb.DSNFor(t, currentDatabase(t, pool))

	migrator, err := db.NewMigrator(dsn, nil)
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })

	if err := migrator.Up(); err != nil {
		t.Fatalf("first Up: %v", err)
	}
	before, _, _ := migrator.Version()

	if err := migrator.Up(); err != nil {
		t.Fatalf("second Up: %v", err)
	}
	after, dirty, _ := migrator.Version()

	if before != after {
		t.Errorf("a second Up changed the version from %d to %d", before, after)
	}
	if dirty {
		t.Error("a second Up left the database dirty")
	}
}

// Every migration must roll back cleanly, or a failed update cannot be undone.
func TestMigrateDownToEmpty(t *testing.T) {
	pool := testdb.Empty(t)
	dsn := testdb.DSNFor(t, currentDatabase(t, pool))

	migrator, err := db.NewMigrator(dsn, nil)
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })

	if err := migrator.Up(); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := migrator.Down(); err != nil {
		t.Fatalf("Down: %v", err)
	}

	version, dirty, err := migrator.Version()
	if err != nil {
		t.Fatalf("Version after Down: %v", err)
	}
	if version != db.NoSchema || dirty {
		t.Errorf("after Down the version is %d dirty=%v, want %d false",
			version, dirty, db.NoSchema)
	}

	// Nothing of ours may survive a full rollback. schema_migrations is
	// golang-migrate's own bookkeeping and is expected to remain.
	remaining := tableNames(t, pool)
	for _, name := range remaining {
		if name != "schema_migrations" {
			t.Errorf("table %q survived a full rollback", name)
		}
	}
}

// Stepping one migration at a time up and back down again proves each pair is
// self-contained, rather than only the whole sequence being reversible.
func TestEachMigrationStepsUpAndDownIndependently(t *testing.T) {
	pool := testdb.Empty(t)
	dsn := testdb.DSNFor(t, currentDatabase(t, pool))

	migrator, err := db.NewMigrator(dsn, nil)
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })

	latest, err := db.LatestVersion()
	if err != nil {
		t.Fatal(err)
	}

	for range latest {
		before, _, _ := migrator.Version()

		if err := migrator.Steps(1); err != nil {
			t.Fatalf("stepping up from %d: %v", before, err)
		}
		applied, dirty, _ := migrator.Version()
		if dirty {
			t.Fatalf("migration %d left the database dirty", applied)
		}

		// Down and up again, so a broken down migration is caught next to the
		// up migration it belongs with.
		if err := migrator.Steps(-1); err != nil {
			t.Fatalf("stepping back down from %d: %v", applied, err)
		}
		rolledBack, dirty, _ := migrator.Version()
		if dirty {
			t.Fatalf("rolling back migration %d left the database dirty", applied)
		}
		if rolledBack != before {
			t.Fatalf("rolling back migration %d returned to version %d, want %d",
				applied, rolledBack, before)
		}

		if err := migrator.Steps(1); err != nil {
			t.Fatalf("re-applying migration %d: %v", applied, err)
		}
	}

	final, _, _ := migrator.Version()
	if final != latest {
		t.Errorf("finished at version %d, want %d", final, latest)
	}
}

func TestMigratedTemplateIsUsable(t *testing.T) {
	pool := testdb.Migrated(t)

	var version uint
	err := pool.QueryRow(context.Background(),
		"SELECT version FROM schema_migrations").Scan(&version)
	if err != nil {
		t.Fatalf("reading schema_migrations: %v", err)
	}

	latest, err := db.LatestVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version != latest {
		t.Errorf("the template is at version %d, want %d", version, latest)
	}
}

// The UTC guarantee is asserted rather than trusted, because a session in
// another zone would silently shift every timestamp the application reads.
func TestConnectionsRunInUTC(t *testing.T) {
	dsn := testdb.AdminDSN(t)

	opts, err := optionsFromDSN(dsn)
	if err != nil {
		t.Fatalf("parsing the test DSN: %v", err)
	}

	pool, err := db.Connect(context.Background(), opts, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(pool.Close)

	zone, err := db.SessionTimeZone(context.Background(), pool)
	if err != nil {
		t.Fatalf("SessionTimeZone: %v", err)
	}
	if zone != "UTC" {
		t.Errorf("session time zone is %q, want UTC", zone)
	}
}

func TestVerifyServerVersionAcceptsPostgres18(t *testing.T) {
	pool := testdb.Empty(t)

	if err := db.VerifyServerVersion(context.Background(), pool); err != nil {
		t.Errorf("the test server was rejected: %v", err)
	}
}
