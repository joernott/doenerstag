package install_test

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
)

const testAdminPassword = "Correct-Horse9"

// installFixture is one complete unattended installation against the test
// server, with everything needed to inspect the result afterwards.
type installFixture struct {
	opts       install.Options
	configPath string
	database   string
	server     db.Options
	runtime    install.Identity
}

// newInstallFixture prepares an install run driven entirely from configuration,
// which is what --non-interactive does: the same questions, answered from their
// defaults rather than from a terminal.
func newInstallFixture(t *testing.T) *installFixture {
	t.Helper()

	root, err := db.OptionsFromDSN(testdb.AdminDSN(t))
	if err != nil {
		t.Fatalf("parsing the test DSN: %v", err)
	}

	suffix := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))
	if len(suffix) > 26 {
		suffix = suffix[:26]
	}

	f := &installFixture{
		configPath: filepath.Join(t.TempDir(), config.DefaultConfigFile),
		database:   "inst_" + suffix,
		server:     root,
		runtime:    install.Identity{User: "irun_" + suffix, Password: "runtime-pw"},
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
	cfg.Install.AdminUser = "iadm_" + suffix
	cfg.Install.AdminPassword = "admin-pw"

	// The doenerstag root account password has no configuration key: unattended
	// it arrives from --root-password or DOENER_ROOT_PASSWORD, which config.Load
	// resolves into this field.
	cfg.Install.RootAccountPassword = testAdminPassword

	f.opts = install.Options{
		Config:     cfg,
		OutputPath: f.configPath,
		AppVersion: "1.2.3",
		Prompter:   install.NewPrompter(strings.NewReader(""), &bytes.Buffer{}, true),
		Now:        func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) },
	}

	t.Cleanup(func() { f.dropEverything(t, cfg.Install.AdminUser) })
	return f
}

func (f *installFixture) dropEverything(t *testing.T, adminUser string) {
	t.Helper()
	dropEverything(t, &install.Provisioner{
		Server:   f.server,
		Root:     install.Identity{User: f.server.User, Password: f.server.Password},
		Admin:    install.Identity{User: adminUser},
		Runtime:  f.runtime,
		Database: f.database,
	})
}

// runtimePool connects the way the installed application would: as the runtime
// user, using the settings the generated configuration file records.
func (f *installFixture) runtimePool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	opts := f.server.WithUser(f.runtime.User, f.runtime.Password).WithDatabase(f.database)
	pool, err := db.Connect(context.Background(), opts, nil)
	if err != nil {
		t.Fatalf("connecting as the runtime user: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// The milestone for sprint 3: a clean machine reaches a provisioned database, a
// root account and a valid configuration file.
func TestInstallProducesAWorkingInstallation(t *testing.T) {
	f := newInstallFixture(t)
	ctx := context.Background()

	result, err := install.Run(ctx, f.opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Database != f.database {
		t.Errorf("result names database %q, want %q", result.Database, f.database)
	}
	if result.SchemaVersion == 0 {
		t.Error("result reports no schema version")
	}

	pool := f.runtimePool(t)

	// The schema is there, with its seed data.
	var allergens int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM allergen").Scan(&allergens); err != nil {
		t.Fatalf("reading the schema as the runtime user: %v", err)
	}
	if allergens != 14 {
		t.Errorf("found %d allergens, want 14", allergens)
	}

	// The administrator exists, is an administrator, and its password verifies.
	var hash string
	var isAdmin bool
	err = pool.QueryRow(ctx,
		`SELECT password_hash, is_admin FROM app_user WHERE name = $1`,
		install.AdministratorName).Scan(&hash, &isAdmin)
	if err != nil {
		t.Fatalf("the administrator was not created: %v", err)
	}
	if !isAdmin {
		t.Error("the root account is not marked as an administrator")
	}
	if err := auth.Verify(testAdminPassword, hash); err != nil {
		t.Errorf("the administrator password does not verify: %v", err)
	}

	// The version was recorded.
	installed, err := install.ReadVersion(ctx, pool)
	if err != nil {
		t.Fatalf("ReadVersion: %v", err)
	}
	if installed.Major != 1 || installed.Minor != 2 || installed.Patch != 3 {
		t.Errorf("recorded version is %d.%d.%d, want 1.2.3",
			installed.Major, installed.Minor, installed.Patch)
	}
	if uint(installed.SchemaVersion) != result.SchemaVersion { //nolint:gosec // small positive
		t.Errorf("recorded schema version %d, want %d",
			installed.SchemaVersion, result.SchemaVersion)
	}
}

// The configuration file install writes has to be one the server can start
// from, or the installation is not finished.
func TestGeneratedConfigurationIsUsable(t *testing.T) {
	f := newInstallFixture(t)

	if _, err := install.Run(context.Background(), f.opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	info, err := os.Stat(f.configPath)
	if err != nil {
		t.Fatalf("the configuration file was not written: %v", err)
	}
	if !config.SkipPermissionCheck() && info.Mode().Perm() != config.ConfigFileMode {
		t.Errorf("configuration file mode is %04o, want %04o",
			info.Mode().Perm(), config.ConfigFileMode)
	}
	if err := config.CheckFilePermissions(f.configPath, nil); err != nil {
		t.Errorf("the file install wrote fails the check the server applies: %v", err)
	}

	content, err := os.ReadFile(f.configPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(content)

	// The runtime credentials are recorded, so the server can connect.
	if !strings.Contains(body, f.runtime.User) {
		t.Error("the configuration file does not record the runtime user")
	}
	if !strings.Contains(body, f.database) {
		t.Error("the configuration file does not record the database name")
	}

	// A session signing secret was generated and stored.
	if strings.Contains(body, `jwt_secret: ""`) {
		t.Error("no session signing secret was written; the server would refuse to start")
	}
}

// The privileged accounts exist only to do the installing. Writing either to
// disk would defeat the point of separating them.
func TestPrivilegedCredentialsAreNeverWritten(t *testing.T) {
	f := newInstallFixture(t)

	// The root credentials have to stay real, since the run authenticates with
	// them. The admin role is one this installation creates, so it can carry a
	// value distinctive enough to search the generated file for.
	f.opts.Config.Install.AdminPassword = "admin-secret-value"

	if _, err := install.Run(context.Background(), f.opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	content, err := os.ReadFile(f.configPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(content)

	for _, secret := range []string{
		"admin-secret-value",
		f.opts.Config.Install.RootPassword,
		f.opts.Config.Install.RootUser,
		f.opts.Config.Install.AdminUser,
		testAdminPassword,
	} {
		if strings.Contains(body, secret) {
			t.Errorf("the configuration file contains %q, which is documented as never stored", secret)
		}
	}
}

// Re-running install is how a lost root password is recovered and how a partial
// installation is finished, so it has to be safe.
func TestInstallIsRepeatable(t *testing.T) {
	f := newInstallFixture(t)
	ctx := context.Background()

	if _, err := install.Run(ctx, f.opts); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Read the secret the first run generated: a second run must keep it, or
	// everyone would be logged out by a routine re-install.
	firstConfig, err := os.ReadFile(f.configPath)
	if err != nil {
		t.Fatal(err)
	}
	firstSecret := valueOf(t, string(firstConfig), "jwt_secret")
	if firstSecret == "" {
		t.Fatal("the first run wrote no session signing secret")
	}
	f.opts.Config.Session.JWTSecret = firstSecret

	if _, err := install.Run(ctx, f.opts); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	secondConfig, err := os.ReadFile(f.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := valueOf(t, string(secondConfig), "jwt_secret"); got != firstSecret {
		t.Error("a re-run rotated the session signing secret, logging everyone out")
	}

	// Exactly one administrator, and one version row per run.
	pool := f.runtimePool(t)

	var admins int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM app_user WHERE is_admin").Scan(&admins); err != nil {
		t.Fatal(err)
	}
	if admins != 1 {
		t.Errorf("found %d administrators after two runs, want 1", admins)
	}

	var versions int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM app_version").Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 2 {
		t.Errorf("found %d version rows after two runs, want one per run", versions)
	}
}

// Recovering a forgotten root password is a documented reason to re-run.
func TestReinstallResetsTheAdministratorPassword(t *testing.T) {
	f := newInstallFixture(t)
	ctx := context.Background()

	if _, err := install.Run(ctx, f.opts); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	const replacement = "Different-Pass8"
	f.opts.Config.Install.RootAccountPassword = replacement

	if _, err := install.Run(ctx, f.opts); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	pool := f.runtimePool(t)
	var hash string
	if err := pool.QueryRow(ctx,
		`SELECT password_hash FROM app_user WHERE name = $1`,
		install.AdministratorName).Scan(&hash); err != nil {
		t.Fatal(err)
	}

	if err := auth.Verify(replacement, hash); err != nil {
		t.Errorf("the new administrator password does not verify: %v", err)
	}
	if err := auth.Verify(testAdminPassword, hash); err == nil {
		t.Error("the old administrator password still works after a reset")
	}
}

// Writability is proven before anything is asked or created, so a run that
// cannot deliver a configuration file fails before touching the database.
func TestInstallRefusesAnUnwritableOutputBeforeTouchingAnything(t *testing.T) {
	f := newInstallFixture(t)
	f.opts.OutputPath = filepath.Join(t.TempDir(), "no-such-directory", "doenerstag.yaml")

	if _, err := install.Run(context.Background(), f.opts); err == nil {
		t.Fatal("an unwritable output path was accepted")
	}

	// Nothing should have been created.
	conn, err := db.Connect(context.Background(),
		f.server.WithDatabase(install.MaintenanceDatabase), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var exists bool
	if err := conn.QueryRow(context.Background(),
		"SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)",
		f.database).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("the database was created despite the output being unwritable")
	}
}

// Unattended, a question with no answer must stop the run rather than proceed
// with an empty value.
func TestInstallFailsUnattendedWithoutARootPassword(t *testing.T) {
	f := newInstallFixture(t)
	f.opts.Config.Install.RootAccountPassword = ""

	_, err := install.Run(context.Background(), f.opts)
	if err == nil {
		t.Fatal("the run completed without an administrator password")
	}
	if !strings.Contains(err.Error(), "DOENER_ROOT_PASSWORD") {
		t.Errorf("the error does not say how to supply the value: %v", err)
	}
}

// A weak administrator password must be refused at install time, not
// discovered later when the rules are applied to everyone else.
func TestInstallRefusesAWeakAdministratorPassword(t *testing.T) {
	f := newInstallFixture(t)
	f.opts.Config.Install.RootAccountPassword = "password"

	if _, err := install.Run(context.Background(), f.opts); err == nil {
		t.Fatal("a password failing the complexity rules was accepted")
	}
}

func TestInstallLoadsContentPages(t *testing.T) {
	f := newInstallFixture(t)
	dir := t.TempDir()

	imprint := filepath.Join(dir, "imprint.html")
	if err := os.WriteFile(imprint,
		[]byte(`<h2>Operated by</h2><p>The lunch committee</p>`), 0o600); err != nil {
		t.Fatal(err)
	}

	// A snippet carrying a script: only the administrator can set these, but
	// the stored HTML is rendered with innerHTML, so it is sanitised on the way
	// in rather than trusted.
	legal := filepath.Join(dir, "legal.html")
	if err := os.WriteFile(legal,
		[]byte(`<p>Privacy notice</p><script>alert(1)</script><a href="javascript:alert(2)">x</a>`),
		0o600); err != nil {
		t.Fatal(err)
	}

	f.opts.Config.Install.ImprintFile = imprint
	f.opts.Config.Install.LegalNotesFile = legal

	if _, err := install.Run(context.Background(), f.opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	pool := f.runtimePool(t)

	var stored string
	if err := pool.QueryRow(context.Background(),
		`SELECT html FROM content_page WHERE key = 'imprint'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored, "lunch committee") {
		t.Errorf("the imprint was not stored: %q", stored)
	}

	if err := pool.QueryRow(context.Background(),
		`SELECT html FROM content_page WHERE key = 'legal_notes'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored, "Privacy notice") {
		t.Errorf("the legal notes were not stored: %q", stored)
	}
	if strings.Contains(stored, "<script") || strings.Contains(stored, "javascript:") {
		t.Errorf("the snippet was stored without being sanitised: %q", stored)
	}
}

// Leaving the paths empty must keep whatever is already there, which on a fresh
// database is the placeholder the migration seeded.
func TestInstallWithoutContentPagesKeepsThePlaceholders(t *testing.T) {
	f := newInstallFixture(t)

	if _, err := install.Run(context.Background(), f.opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	pool := f.runtimePool(t)
	for _, key := range install.ContentPageKeys {
		var html string
		if err := pool.QueryRow(context.Background(),
			`SELECT html FROM content_page WHERE key = $1`, key).Scan(&html); err != nil {
			t.Errorf("page %q is missing: %v", key, err)
			continue
		}
		if strings.TrimSpace(html) == "" {
			t.Errorf("page %q is empty", key)
		}
	}
}

// valueOf pulls a scalar out of the generated YAML without a parser.
func valueOf(t *testing.T, body, key string) string {
	t.Helper()

	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		prefix := key + ": "
		if strings.HasPrefix(trimmed, prefix) {
			return strings.Trim(strings.TrimPrefix(trimmed, prefix), `"`)
		}
	}
	return ""
}

// A binary stamped with a version install cannot record must say so before it
// does anything, not after it has created the database.
//
// This is not hypothetical: `git describe --tags --always` returns a bare
// commit hash in a repository with no tags, and the Makefile used to stamp
// that. The resulting binary asked for three passwords, created two roles,
// created the database and applied eight migrations, and only then refused.
func TestInstallRefusesAnUnusableVersionBeforeTouchingAnything(t *testing.T) {
	f := newInstallFixture(t)
	f.opts.AppVersion = "7c81636"

	_, err := install.Run(context.Background(), f.opts)
	if err == nil {
		t.Fatal("a version that is not major.minor.patch was accepted")
	}
	if !strings.Contains(err.Error(), "major.minor.patch") {
		t.Errorf("the error does not say what is wrong with the version: %v", err)
	}

	conn, err := db.Connect(context.Background(),
		f.server.WithDatabase(install.MaintenanceDatabase), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var exists bool
	if err := conn.QueryRow(context.Background(),
		"SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)",
		f.database).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("the database was created despite the version being unusable")
	}
}
