// Package testdb provides real PostgreSQL 18 databases to the tests.
//
// There is no in-memory substitute. The schema uses PostgreSQL-specific types,
// partial unique indexes, triggers and constraint behaviour, so testing against
// a different engine would test the wrong thing.
//
// A database comes from one of two places, in this order:
//
//  1. testcontainers-go, when Docker is reachable.
//  2. DOENER_TEST_DATABASE_URL, pointing at a database the developer provides.
//
// When neither is available the tests skip with a message saying why, so that
// `go test ./...` never fails merely because Docker is not installed. See
// docs/12_testing.md.
package testdb

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/joernott/doenerstag/internal/db"
)

// DatabaseURLEnv names the environment variable holding a connection string to
// use instead of starting a container.
const DatabaseURLEnv = "DOENER_TEST_DATABASE_URL"

// The image the container source starts. Pinned to the only supported major
// version; see docs/08_technologies.md.
const postgresImage = "postgres:18-alpine"

// templateName is migrated once per test binary. Every database a test asks
// for is then created from it, which turns a full migration run into a fast
// file copy inside the server.
const templateName = "doenerstag_tmpl"

// startTimeout bounds container start plus the first migration.
const startTimeout = 3 * time.Minute

type harness struct {
	adminDSN string // connection string to the maintenance database
	skip     string // non-empty when no database is available
	cleanup  func()

	templateOnce sync.Once
	templateErr  error
}

var (
	shared     *harness
	sharedOnce sync.Once
)

// get returns the process-wide harness, starting a container on first use.
func get() *harness {
	sharedOnce.Do(func() {
		shared = start()
	})
	return shared
}

func start() *harness {
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()

	// Docker first, as specified. It gives a disposable server of exactly the
	// right version, which a developer's local install may not be.
	container, dsn, dockerErr := startContainer(ctx)
	if dockerErr == nil {
		return &harness{
			adminDSN: dsn,
			cleanup: func() {
				_ = testcontainers.TerminateContainer(container)
			},
		}
	}

	if url := os.Getenv(DatabaseURLEnv); url != "" {
		return &harness{adminDSN: url, cleanup: func() {}}
	}

	return &harness{
		cleanup: func() {},
		skip: fmt.Sprintf(
			"no test database available: Docker could not start a container (%v) "+
				"and %s is not set. Run contrib/setup_dev_pipeline.sh, or set %s to a "+
				"PostgreSQL 18 connection string.",
			dockerErr, DatabaseURLEnv, DatabaseURLEnv),
	}
}

func startContainer(ctx context.Context) (*tcpostgres.PostgresContainer, string, error) {
	container, err := tcpostgres.Run(ctx, postgresImage,
		tcpostgres.WithDatabase("postgres"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(2*time.Minute),
		),
	)
	if err != nil {
		return nil, "", err
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = testcontainers.TerminateContainer(container)
		return nil, "", err
	}
	return container, dsn, nil
}

// Shutdown stops the shared container. A package with database tests calls it
// from TestMain; forgetting to leaves a container running until Ryuk reaps it.
func Shutdown() {
	if shared != nil && shared.cleanup != nil {
		shared.cleanup()
	}
}

// Empty returns a pool to a fresh, empty database with no schema at all.
//
// The database is dropped when the test finishes. Migration tests use this;
// everything else wants Migrated.
func Empty(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return newDatabase(t, "")
}

// Migrated returns a pool to a fresh database with every migration applied.
//
// It is created from a template that was migrated once for the whole test
// binary, so the per-test cost is a file copy rather than a migration run.
func Migrated(t *testing.T) *pgxpool.Pool {
	t.Helper()

	h := get()
	h.requireAvailable(t)

	h.templateOnce.Do(func() {
		h.templateErr = h.buildTemplate()
	})
	if h.templateErr != nil {
		t.Fatalf("preparing the migrated template database: %v", h.templateErr)
	}

	return newDatabase(t, templateName)
}

// AdminDSN is a connection string to the maintenance database, for the rare
// test that needs to create or drop databases itself.
func AdminDSN(t *testing.T) string {
	t.Helper()
	h := get()
	h.requireAvailable(t)
	return h.adminDSN
}

// DSNFor builds a connection string for a named database on the test server.
func DSNFor(t *testing.T, database string) string {
	t.Helper()
	return replaceDatabase(AdminDSN(t), database)
}

func (h *harness) requireAvailable(t *testing.T) {
	t.Helper()
	if h.skip != "" {
		t.Skip(h.skip)
	}
}

// buildTemplate creates the template database and migrates it once.
//
// The connection is closed afterwards: CREATE DATABASE ... TEMPLATE refuses to
// run while anything else is connected to the template.
func (h *harness) buildTemplate() error {
	ctx := context.Background()

	if err := h.createDatabase(ctx, templateName, ""); err != nil {
		return err
	}

	migrator, err := db.NewMigrator(replaceDatabase(h.adminDSN, templateName), nil)
	if err != nil {
		return err
	}
	if err := migrator.Up(); err != nil {
		_ = migrator.Close()
		return err
	}
	return migrator.Close()
}

func (h *harness) createDatabase(ctx context.Context, name, template string) error {
	admin, err := pgxpool.New(ctx, h.adminDSN)
	if err != nil {
		return fmt.Errorf("connecting to the maintenance database: %w", err)
	}
	defer admin.Close()

	stmt := fmt.Sprintf("CREATE DATABASE %s", quoteIdentifier(name))
	if template != "" {
		stmt += fmt.Sprintf(" TEMPLATE %s", quoteIdentifier(template))
	}

	if _, err := admin.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("creating database %s: %w", name, err)
	}
	return nil
}

func (h *harness) dropDatabase(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, h.adminDSN)
	if err != nil {
		return
	}
	defer admin.Close()

	// WITH (FORCE) disconnects anything still attached, so a leaked pool in a
	// failing test does not leave an undroppable database behind.
	_, _ = admin.Exec(ctx,
		fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", quoteIdentifier(name)))
}

// newDatabase creates a uniquely named database and returns a pool to it.
func newDatabase(t *testing.T, template string) *pgxpool.Pool {
	t.Helper()

	h := get()
	h.requireAvailable(t)

	ctx := context.Background()
	name := databaseNameFor(t)

	if err := h.createDatabase(ctx, name, template); err != nil {
		t.Fatalf("%v", err)
	}
	t.Cleanup(func() { h.dropDatabase(name) })

	pool, err := pgxpool.New(ctx, replaceDatabase(h.adminDSN, name))
	if err != nil {
		t.Fatalf("connecting to %s: %v", name, err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("pinging %s: %v", name, err)
	}
	return pool
}

// nameCounter keeps database names unique when one test asks for several.
var nameCounter atomic64

func databaseNameFor(t *testing.T) string {
	// Test names contain characters an identifier cannot, and can be long.
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '_'
		}
	}, t.Name())

	const maxNameLength = 40
	if len(safe) > maxNameLength {
		safe = safe[:maxNameLength]
	}

	return fmt.Sprintf("test_%s_%d", safe, nameCounter.next())
}
