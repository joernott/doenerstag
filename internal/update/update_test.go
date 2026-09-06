package update_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joernott/doenerstag/internal/auth"
	"github.com/joernott/doenerstag/internal/config"
	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/install"
	"github.com/joernott/doenerstag/internal/testdb"
	"github.com/joernott/doenerstag/internal/update"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testdb.Shutdown()
	os.Exit(code)
}

const rootPassword = "Correct-Horse9"

// fixture is an installation that has already happened, which is the state
// update exists to operate on.
type fixture struct {
	opts       update.Options
	configPath string
	database   string
	server     db.Options
	runtime    install.Identity
	adminUser  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	root, err := db.OptionsFromDSN(testdb.AdminDSN(t))
	if err != nil {
		t.Fatalf("parsing the test DSN: %v", err)
	}

	suffix := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))
	if len(suffix) > 24 {
		suffix = suffix[:24]
	}

	f := &fixture{
		configPath: filepath.Join(t.TempDir(), config.DefaultConfigFile),
		database:   "upd_" + suffix,
		server:     root,
		runtime:    install.Identity{User: "urun_" + suffix, Password: "runtime-pw"},
		adminUser:  "uadm_" + suffix,
	}

	cfg := &config.Config{}
	cfg.Database.Server = root.Host
	cfg.Database.Port = root.Port
	cfg.Database.Name = f.database
	cfg.Database.User = f.runtime.User
	cfg.Database.Password = f.runtime.Password
	cfg.Database.SSLMode = root.SSLMode
	cfg.Database.MaxConnectionPool = 5
	cfg.Install.RootUser = root.User
	cfg.Install.RootPassword = root.Password
	cfg.Install.AdminUser = f.adminUser
	cfg.Install.AdminPassword = "admin-pw"
	cfg.Install.RootAccountPassword = rootPassword

	// Install first: update updates something.
	installOpts := install.Options{
		Config:     cfg,
		OutputPath: f.configPath,
		AppVersion: "1.0.0",
		Prompter:   install.NewPrompter(strings.NewReader(""), &bytes.Buffer{}, true),
		Now:        func() time.Time { return time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC) },
	}
	if _, err := install.Run(context.Background(), installOpts); err != nil {
		t.Fatalf("the installation this test updates failed: %v", err)
	}

	updated := *cfg
	updated.File = f.configPath

	f.opts = update.Options{
		Config:       &updated,
		OutputPath:   f.configPath,
		ExistingPath: f.configPath,
		AppVersion:   "1.1.0",
		Prompter:     install.NewPrompter(strings.NewReader(""), &bytes.Buffer{}, true),
		Now:          func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) },
	}

	t.Cleanup(func() {
		dropEverything(t, &install.Provisioner{
			Server:   f.server,
			Root:     install.Identity{User: f.server.User, Password: f.server.Password},
			Admin:    install.Identity{User: f.adminUser},
			Runtime:  f.runtime,
			Database: f.database,
		})
	})
	return f
}

// dropEverything removes the database and the roles the fixture created.
func dropEverything(t *testing.T, p *install.Provisioner) {
	t.Helper()

	ctx := context.Background()
	opts := p.Server.WithUser(p.Root.User, p.Root.Password).WithDatabase("postgres")
	pool, err := db.Connect(ctx, opts, nil)
	if err != nil {
		t.Logf("cleaning up: %v", err)
		return
	}
	defer pool.Close()

	for _, statement := range []string{
		`DROP DATABASE IF EXISTS "` + p.Database + `" WITH (FORCE)`,
		`DROP ROLE IF EXISTS "` + p.Runtime.User + `"`,
		`DROP ROLE IF EXISTS "` + p.Admin.User + `"`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Logf("cleaning up: %v", err)
		}
	}
}

func TestUpdateAppliesNothingWhenTheSchemaIsCurrent(t *testing.T) {
	f := newFixture(t)

	result, err := update.Run(context.Background(), f.opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Nothing to apply is a normal outcome, not a failure: the schema and the
	// binary agree, which is what an update between two versions with no
	// migrations between them looks like.
	if result.SchemaBefore != result.SchemaAfter {
		t.Errorf("schema went from %d to %d, want no change",
			result.SchemaBefore, result.SchemaAfter)
	}
	if result.SchemaAfter == 0 {
		t.Error("the schema version is 0, so nothing was ever installed")
	}
}

func TestUpdateRecordsTheNewVersion(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := update.Run(ctx, f.opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	pool := f.runtimePool(t)
	installed, err := install.ReadVersion(ctx, pool)
	if err != nil {
		t.Fatalf("reading the recorded version: %v", err)
	}

	// The newest row is the update's, not the installation's.
	want := install.SemanticVersion{Major: 1, Minor: 1, Patch: 0}
	if installed.SemanticVersion != want {
		t.Errorf("app_version records %v, want %v", installed.SemanticVersion, want)
	}
}

func TestUpdateRewritesTheConfigurationInPlace(t *testing.T) {
	f := newFixture(t)

	before, err := os.ReadFile(f.configPath)
	if err != nil {
		t.Fatalf("reading the installed configuration: %v", err)
	}
	if !strings.Contains(string(before), "Generated by doenerstag install") {
		t.Fatal("the installed file does not say install wrote it")
	}

	if _, err := update.Run(context.Background(), f.opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	after, err := os.ReadFile(f.configPath)
	if err != nil {
		t.Fatalf("reading the updated configuration: %v", err)
	}
	body := string(after)

	if !strings.Contains(body, "Generated by doenerstag update") {
		t.Error("the rewritten file does not say update wrote it")
	}
	// Every value the operator had is still there: the database password and
	// the signing secret above all, because losing either bricks the
	// installation.
	for _, kept := range []string{f.runtime.Password, f.database, f.runtime.User} {
		if !strings.Contains(body, kept) {
			t.Errorf("the rewritten file lost %q", kept)
		}
	}
	if !strings.Contains(body, "jwt_secret:") {
		t.Error("the rewritten file has no session signing secret")
	}
}

func TestUpdateKeepsASettingItNoLongerKnows(t *testing.T) {
	f := newFixture(t)

	// A file written by some future version, carrying a setting this binary has
	// never heard of.
	original, err := os.ReadFile(f.configPath)
	if err != nil {
		t.Fatalf("reading the configuration: %v", err)
	}
	withExtra := string(original) + "\nserver:\n  telepathy_timeout: 30s\n"
	if err := config.WriteFile(f.configPath, withExtra); err != nil {
		t.Fatalf("writing the configuration: %v", err)
	}

	result, err := update.Run(context.Background(), f.opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Retired) != 1 || result.Retired[0] != "server.telepathy_timeout" {
		t.Fatalf("retired = %v, want [server.telepathy_timeout]", result.Retired)
	}

	body, err := os.ReadFile(f.configPath)
	if err != nil {
		t.Fatalf("reading the rewritten configuration: %v", err)
	}
	// Commented out rather than deleted: an operator's line is information, and
	// silently dropping it hides that the setting has gone.
	if !strings.Contains(string(body), "# server.telepathy_timeout: 30s") {
		t.Errorf("the retired setting is not in the file:\n%s", body)
	}
}

func TestUpdateResetsTheAdministratorPasswordWhenAsked(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	const replacement = "Neues-Kennwort7"
	f.opts.ResetRootPassword = true
	// Unattended, the way an operator scripting an update would do it:
	// --root-password resolves into this field, and the prompt takes it as its
	// default rather than asking for it.
	f.opts.Config.Install.RootAccountPassword = replacement

	result, err := update.Run(ctx, f.opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.RootPasswordReset {
		t.Fatal("the result does not report the reset")
	}

	pool := f.runtimePool(t)
	var hash string
	err = pool.QueryRow(ctx,
		`SELECT password_hash FROM app_user WHERE name = $1`,
		install.AdministratorName).Scan(&hash)
	if err != nil {
		t.Fatalf("reading the administrator: %v", err)
	}
	if err := auth.Verify(replacement, hash); err != nil {
		t.Errorf("the new password does not verify: %v", err)
	}
	if err := auth.Verify(rootPassword, hash); err == nil {
		t.Error("the old password still verifies")
	}
}

func TestUpdateRefusesADatabaseNewerThanTheBinary(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// Pretend a newer doenerstag has already migrated this database.
	pool := f.adminPool(t)
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET version = 9999`); err != nil {
		t.Fatalf("faking a newer schema: %v", err)
	}

	_, err := update.Run(ctx, f.opts)
	if err == nil {
		t.Fatal("Run accepted a schema newer than the binary")
	}
	// The message has to say what to do, not merely that something is wrong.
	for _, want := range []string{"9999", "newer doenerstag"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
}

func TestUpdateRefusesBeforeTheFirstRelease(t *testing.T) {
	f := newFixture(t)

	// docs/09_configuration.md: until the first release ships, update says so
	// and does nothing. The machinery above is written and tested regardless,
	// so that the day it is needed is not the day it is first run.
	f.opts.AppVersion = "0.0.0-dev"

	_, err := update.Run(context.Background(), f.opts)
	if err == nil {
		t.Fatal("a pre-release binary updated anyway")
	}
	if !strings.Contains(err.Error(), "before the first release") {
		t.Errorf("the error does not explain why: %v", err)
	}
}

func TestUpdateProvesTheFileIsWritableFirst(t *testing.T) {
	f := newFixture(t)

	// A directory cannot be written as a file, and the check has to happen
	// before anything else does: half an update followed by "permission denied"
	// is the outcome this ordering exists to prevent.
	f.opts.OutputPath = t.TempDir()

	if _, err := update.Run(context.Background(), f.opts); err == nil {
		t.Fatal("Run accepted an unwritable output path")
	}
}

// runtimePool connects the way the running application would.
func (f *fixture) runtimePool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	opts := f.server.WithUser(f.runtime.User, f.runtime.Password).WithDatabase(f.database)
	pool, err := db.Connect(context.Background(), opts, nil)
	if err != nil {
		t.Fatalf("connecting as the runtime user: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// adminPool connects as the schema owner, for the tests that have to change
// something the runtime user is not allowed to touch.
func (f *fixture) adminPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	opts := f.server.WithUser(f.adminUser, "admin-pw").WithDatabase(f.database)
	pool, err := db.Connect(context.Background(), opts, nil)
	if err != nil {
		t.Fatalf("connecting as the schema owner: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
