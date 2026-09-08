package install_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/install"
	"github.com/joernott/doenerstag/internal/testdb"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testdb.Shutdown()
	os.Exit(code)
}

// provisionerFor builds a Provisioner against the test server, using its
// superuser as the root identity and unique names so tests do not collide.
func provisionerFor(t *testing.T) *install.Provisioner {
	t.Helper()

	root, err := db.OptionsFromDSN(testdb.AdminDSN(t))
	if err != nil {
		t.Fatalf("parsing the test DSN: %v", err)
	}

	suffix := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))
	if len(suffix) > 30 {
		suffix = suffix[:30]
	}

	p := &install.Provisioner{
		Server:   root,
		Root:     install.Identity{User: root.User, Password: root.Password},
		Admin:    install.Identity{User: "adm_" + suffix, Password: "admin-pw"},
		Runtime:  install.Identity{User: "run_" + suffix, Password: "runtime-pw"},
		Database: "db_" + suffix,
	}

	t.Cleanup(func() { dropEverything(t, p) })
	return p
}

func dropEverything(t *testing.T, p *install.Provisioner) {
	t.Helper()

	ctx := context.Background()
	conn, err := pgx.Connect(ctx,
		p.Server.WithUser(p.Root.User, p.Root.Password).
			WithDatabase(install.MaintenanceDatabase).DSN())
	if err != nil {
		return
	}
	defer func() { _ = conn.Close(ctx) }()

	_, _ = conn.Exec(ctx, `DROP DATABASE IF EXISTS "`+p.Database+`" WITH (FORCE)`)
	_, _ = conn.Exec(ctx, `DROP ROLE IF EXISTS "`+p.Runtime.User+`"`)
	_, _ = conn.Exec(ctx, `DROP ROLE IF EXISTS "`+p.Admin.User+`"`)
}

func connectAs(t *testing.T, p *install.Provisioner, who install.Identity) *pgx.Conn {
	t.Helper()

	conn, err := pgx.Connect(context.Background(),
		p.Server.WithUser(who.User, who.Password).WithDatabase(p.Database).DSN())
	if err != nil {
		t.Fatalf("connecting as %s: %v", who.User, err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func TestProvisionCreatesRolesAndDatabase(t *testing.T) {
	p := provisionerFor(t)
	ctx := context.Background()

	if err := p.Provision(ctx); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	// Both roles can log in, and the database exists and is owned by admin.
	admin := connectAs(t, p, p.Admin)

	var owner string
	err := admin.QueryRow(ctx, `
		SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = $1`,
		p.Database).Scan(&owner)
	if err != nil {
		t.Fatalf("reading the database owner: %v", err)
	}
	if owner != p.Admin.User {
		t.Errorf("the database is owned by %q, want the admin user %q", owner, p.Admin.User)
	}

	connectAs(t, p, p.Runtime)
}

// Re-running install after a failure part way through has to work, so every
// step adopts what it finds rather than failing on it.
func TestProvisionIsIdempotent(t *testing.T) {
	p := provisionerFor(t)
	ctx := context.Background()

	if err := p.Provision(ctx); err != nil {
		t.Fatalf("first Provision: %v", err)
	}
	if err := p.Provision(ctx); err != nil {
		t.Fatalf("second Provision: %v", err)
	}
	if err := p.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// And again with the schema already in place.
	if err := p.Provision(ctx); err != nil {
		t.Fatalf("Provision after migrating: %v", err)
	}
}

// An operator re-running install may be correcting a password they have since
// forgotten, so an existing role has its password updated rather than skipped.
func TestProvisionUpdatesAnExistingRolePassword(t *testing.T) {
	p := provisionerFor(t)
	ctx := context.Background()

	if err := p.Provision(ctx); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	p.Runtime.Password = "a-different-password"
	if err := p.Provision(ctx); err != nil {
		t.Fatalf("re-provisioning with a new password: %v", err)
	}

	connectAs(t, p, p.Runtime)
}

func TestMigrateAppliesTheSchemaAsAdmin(t *testing.T) {
	p := provisionerFor(t)
	ctx := context.Background()

	if err := p.Provision(ctx); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if err := p.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	admin := connectAs(t, p, p.Admin)

	var version int64
	if err := admin.QueryRow(ctx, "SELECT version FROM schema_migrations").Scan(&version); err != nil {
		t.Fatalf("reading schema_migrations: %v", err)
	}
	latest, err := db.LatestVersion()
	if err != nil {
		t.Fatal(err)
	}
	if uint(version) != latest { //nolint:gosec // a schema version is small and positive
		t.Errorf("schema is at version %d, want %d", version, latest)
	}

	// The seeded reference data came with it.
	var allergens int
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM allergen").Scan(&allergens); err != nil {
		t.Fatalf("counting allergens: %v", err)
	}
	if allergens != 14 {
		t.Errorf("found %d allergens, want 14", allergens)
	}
}

// The point of separating the identities: a compromise of the running
// application must yield an account that cannot change the schema.
func TestRuntimeUserCanReadAndWriteRowsButNotChangeTheSchema(t *testing.T) {
	p := provisionerFor(t)
	ctx := context.Background()

	if err := p.Provision(ctx); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if err := p.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	runtime := connectAs(t, p, p.Runtime)

	// Reading is allowed.
	var count int
	if err := runtime.QueryRow(ctx, "SELECT count(*) FROM currency").Scan(&count); err != nil {
		t.Errorf("the runtime user cannot read: %v", err)
	}

	// Writing rows is allowed.
	_, err := runtime.Exec(ctx, `
		INSERT INTO app_user (id, name, password_hash)
		VALUES (gen_random_uuid(), 'anna', 'x')`)
	if err != nil {
		t.Errorf("the runtime user cannot insert: %v", err)
	}
	if _, err := runtime.Exec(ctx,
		`UPDATE app_user SET display_name = 'Anna' WHERE name = 'anna'`); err != nil {
		t.Errorf("the runtime user cannot update: %v", err)
	}
	if _, err := runtime.Exec(ctx, `DELETE FROM app_user WHERE name = 'anna'`); err != nil {
		t.Errorf("the runtime user cannot delete: %v", err)
	}

	// Changing the schema is not.
	forbidden := map[string]string{
		"create a table": "CREATE TABLE intruder (id int)",
		"drop a table":   "DROP TABLE app_user",
		"alter a table":  "ALTER TABLE app_user ADD COLUMN intruder text",
		"truncate":       "TRUNCATE app_user",
		"create a role":  "CREATE ROLE intruder LOGIN",
	}
	for what, stmt := range forbidden {
		if _, err := runtime.Exec(ctx, stmt); err == nil {
			t.Errorf("the runtime user was allowed to %s", what)
		}
	}
}

// Tables the migrations create after the grant must be reachable too, which is
// what ALTER DEFAULT PRIVILEGES is for. Granting only on existing tables would
// leave every migrated table unreadable.
func TestRuntimeUserReachesTablesCreatedAfterTheGrant(t *testing.T) {
	p := provisionerFor(t)
	ctx := context.Background()

	// Grants happen during Provision, before Migrate creates any table.
	if err := p.Provision(ctx); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if err := p.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	runtime := connectAs(t, p, p.Runtime)

	for _, table := range []string{
		"app_user", "restaurant", "menu_item", "food_order", "order_item", "image",
	} {
		var n int
		if err := runtime.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Errorf("the runtime user cannot read %s: %v", table, err)
		}
	}
}

func TestProvisionRefusesTheSameUserForAdminAndRuntime(t *testing.T) {
	p := provisionerFor(t)
	p.Runtime = p.Admin

	err := p.Provision(context.Background())
	if err == nil {
		t.Fatal("the admin and runtime users were allowed to be the same")
	}
	if !strings.Contains(err.Error(), "must differ") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProvisionValidatesItsInputs(t *testing.T) {
	cases := map[string]func(*install.Provisioner){
		"no database":     func(p *install.Provisioner) { p.Database = "" },
		"no root user":    func(p *install.Provisioner) { p.Root.User = "" },
		"no admin user":   func(p *install.Provisioner) { p.Admin.User = "" },
		"no runtime user": func(p *install.Provisioner) { p.Runtime.User = "" },
	}

	for name, breakIt := range cases {
		p := provisionerFor(t)
		breakIt(p)
		if err := p.Provision(context.Background()); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// Identifiers and passwords cannot be bind parameters in DDL, so they are
// interpolated. A name or password full of quotes must produce a badly named
// role, not a broken server.
func TestAwkwardNamesAndPasswordsAreQuoted(t *testing.T) {
	p := provisionerFor(t)
	ctx := context.Background()

	p.Runtime.User = `run "quoted" user`
	p.Runtime.Password = `pw'with"quotes\and\backslashes`

	if err := p.Provision(ctx); err != nil {
		t.Fatalf("Provision with an awkward role: %v", err)
	}

	// The password must have been stored exactly as given, or the operator
	// could not log in with what they typed.
	connectAs(t, p, p.Runtime)
}
