package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"

	"github.com/joernott/doenerstag/internal/logging"
)

// loadFor resolves configuration from an explicit set of sources, so that a
// test can vary one and hold the rest still.
func loadFor(t *testing.T, scope Scope, args []string, env map[string]string, configFile string) (*Config, error) {
	t.Helper()

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.SetOutput(nopWriter{})
	RegisterGlobalFlags(flags)
	RegisterScopeFlags(flags, scope)

	if configFile != "" {
		args = append([]string{"--config", configFile}, args...)
	}
	if err := flags.Parse(args); err != nil {
		t.Fatalf("parsing %v: %v", args, err)
	}

	return Load(LoadOptions{
		Flags: flags,
		Scope: scope,
		Getenv: func(name string) (string, bool) {
			v, ok := env[name]
			return v, ok
		},
	})
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), DefaultConfigFile)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config file: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	return path
}

// The four sources, each overriding the one before it. This is the central
// promise of docs/09_configuration.md, so it is asserted one layer at a time on
// a single setting.
func TestPrecedenceDefaultThenFileThenEnvironmentThenFlag(t *testing.T) {
	const key = "DOENER_DATABASE_SERVER"

	file := writeConfig(t, "database:\n  server: from-file\n")

	t.Run("default", func(t *testing.T) {
		cfg, err := loadFor(t, ScopeGlobal, nil, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Database.Server != "localhost" {
			t.Errorf("got %q, want the built-in default localhost", cfg.Database.Server)
		}
	})

	t.Run("file beats default", func(t *testing.T) {
		cfg, err := loadFor(t, ScopeGlobal, nil, nil, file)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Database.Server != "from-file" {
			t.Errorf("got %q, want from-file", cfg.Database.Server)
		}
	})

	t.Run("environment beats file", func(t *testing.T) {
		cfg, err := loadFor(t, ScopeGlobal, nil,
			map[string]string{key: "from-env"}, file)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Database.Server != "from-env" {
			t.Errorf("got %q, want from-env", cfg.Database.Server)
		}
	})

	t.Run("flag beats environment", func(t *testing.T) {
		cfg, err := loadFor(t, ScopeGlobal,
			[]string{"--database-server", "from-flag"},
			map[string]string{key: "from-env"}, file)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Database.Server != "from-flag" {
			t.Errorf("got %q, want from-flag", cfg.Database.Server)
		}
	})
}

// The environment variable derives from the flag name, not from the config key,
// so --port is DOENER_PORT and not DOENER_SERVER_PORT. Easy to get wrong and
// invisible when it is wrong, so it is asserted for every setting.
func TestEnvironmentVariableNamesDeriveFromFlagNames(t *testing.T) {
	cases := map[string]string{
		"database-server":        "DOENER_DATABASE_SERVER",
		"port":                   "DOENER_PORT",
		"max-connection-pool":    "DOENER_MAX_CONNECTION_POOL",
		"idle-timeout":           "DOENER_IDLE_TIMEOUT",
		"jwt-secret":             "DOENER_JWT_SECRET",
		"retention":              "DOENER_RETENTION",
		"cors-allowed-origins":   "DOENER_CORS_ALLOWED_ORIGINS",
		"database-root-password": "DOENER_DATABASE_ROOT_PASSWORD",
	}
	for flag, want := range cases {
		setting, ok := SettingByFlag(flag)
		if !ok {
			t.Fatalf("%s is not in the registry", flag)
		}
		if got := setting.Env(); got != want {
			t.Errorf("%s: env is %s, want %s", flag, got, want)
		}
	}

	// And no setting derives its variable from the configuration key.
	port, _ := SettingByFlag("port")
	if port.Env() == "DOENER_SERVER_PORT" {
		t.Error("the environment variable was derived from the config key")
	}
}

func TestDefaultsMatchTheDocumentedValues(t *testing.T) {
	cfg, err := loadFor(t, ScopeServer|ScopeCleanup, nil, nil, "")
	if err != nil {
		t.Fatalf("loading defaults: %v", err)
	}

	if cfg.Database.Server != "localhost" ||
		cfg.Database.Port != 5432 ||
		cfg.Database.Name != "doenerstag" ||
		cfg.Database.User != "doener" ||
		cfg.Database.SSLMode != "prefer" ||
		cfg.Database.MaxConnectionPool != 10 {
		t.Errorf("database defaults are wrong: %+v", cfg.Database)
	}

	if cfg.Log.Level != logging.LevelInfo || cfg.Log.File != "" {
		t.Errorf("log defaults are wrong: %+v", cfg.Log)
	}

	if cfg.Server.Port != 8443 ||
		cfg.Server.TLSCert != "server.crt" ||
		cfg.Server.TLSKey != "server.key" ||
		cfg.Server.StaticDir != "static" ||
		cfg.Server.NoHTTPS || cfg.Server.NoSwagger {
		t.Errorf("server defaults are wrong: %+v", cfg.Server)
	}

	if cfg.Server.HTTPReadTimeout != 30*time.Second ||
		cfg.Server.HTTPWriteTimeout != 60*time.Second ||
		cfg.Server.HTTPIdleTimeout != 120*time.Second ||
		cfg.Server.ShutdownGrace != 30*time.Second {
		t.Errorf("HTTP timeout defaults are wrong: %+v", cfg.Server)
	}

	if cfg.Server.MaxImageSize != 5*1024*1024 {
		t.Errorf("max image size is %d, want 5MiB", cfg.Server.MaxImageSize)
	}

	if cfg.Session.IdleTimeout != 6*time.Hour ||
		cfg.Session.AbsoluteTimeout != 7*24*time.Hour ||
		cfg.Session.LoginRateLimitUser != 10 ||
		cfg.Session.LoginRateLimitIP != 60 ||
		cfg.Session.LoginRateLimitWindow != 15*time.Minute {
		t.Errorf("session defaults are wrong: %+v", cfg.Session)
	}
}

func TestRetentionDefaultIsFourteenDays(t *testing.T) {
	cfg, err := loadFor(t, ScopeCleanup, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cleanup.Retention != 14*24*time.Hour {
		t.Errorf("retention is %v, want 14d", cfg.Cleanup.Retention)
	}
}

func TestNestedConfigFileKeysAreRead(t *testing.T) {
	file := writeConfig(t, `
database:
  server: db.example.invalid
  port: 6543
  name: doener_test
  sslmode: require
  max_connection_pool: 25
server:
  port: 9443
  no_https: true
  static_dir: /srv/static
  http_read_timeout: 45s
  max_image_size: 2MiB
session:
  idle_timeout: 2h
  absolute_timeout: 30d
log:
  level: debug
cleanup:
  retention: 3d
`)

	cfg, err := loadFor(t, ScopeServer|ScopeCleanup, nil, nil, file)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	if cfg.Database.Server != "db.example.invalid" || cfg.Database.Port != 6543 {
		t.Errorf("database section not read: %+v", cfg.Database)
	}
	if cfg.Database.SSLMode != "require" || cfg.Database.MaxConnectionPool != 25 {
		t.Errorf("database section not read: %+v", cfg.Database)
	}
	if cfg.Server.Port != 9443 || !cfg.Server.NoHTTPS || cfg.Server.StaticDir != "/srv/static" {
		t.Errorf("server section not read: %+v", cfg.Server)
	}
	if cfg.Server.HTTPReadTimeout != 45*time.Second {
		t.Errorf("http_read_timeout is %v, want 45s", cfg.Server.HTTPReadTimeout)
	}
	if cfg.Server.MaxImageSize != 2*1024*1024 {
		t.Errorf("max_image_size is %d, want 2MiB", cfg.Server.MaxImageSize)
	}
	if cfg.Session.IdleTimeout != 2*time.Hour || cfg.Session.AbsoluteTimeout != 30*24*time.Hour {
		t.Errorf("session section not read: %+v", cfg.Session)
	}
	if cfg.Log.Level != logging.LevelDebug {
		t.Errorf("log level is %v, want DEBUG", cfg.Log.Level)
	}
	if cfg.Cleanup.Retention != 3*24*time.Hour {
		t.Errorf("retention is %v, want 3d", cfg.Cleanup.Retention)
	}
	if cfg.File != file {
		t.Errorf("cfg.File is %q, want %q", cfg.File, file)
	}
}

// The two secret settings that do live in the configuration file must actually
// be read from it, since the command line is closed to them.
func TestSecretsAreReadFromTheFileAndEnvironment(t *testing.T) {
	file := writeConfig(t, "database:\n  password: from-file\nsession:\n  jwt_secret: file-secret\n")

	cfg, err := loadFor(t, ScopeServer, nil, nil, file)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Password != "from-file" || cfg.Session.JWTSecret != "file-secret" {
		t.Errorf("secrets not read from the file: %q / %q",
			cfg.Database.Password, cfg.Session.JWTSecret)
	}

	cfg, err = loadFor(t, ScopeServer, nil, map[string]string{
		"DOENER_DATABASE_PASSWORD": "from-env",
		"DOENER_JWT_SECRET":        "env-secret",
	}, file)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Password != "from-env" || cfg.Session.JWTSecret != "env-secret" {
		t.Errorf("the environment did not override the file: %q / %q",
			cfg.Database.Password, cfg.Session.JWTSecret)
	}
}

// Install-only settings are never written to the configuration file, so they
// come from flags and the environment alone.
func TestInstallSettingsComeFromFlagsAndEnvironment(t *testing.T) {
	cfg, err := loadFor(t, ScopeInstall,
		[]string{"--database-root-user", "postgres", "--non-interactive"},
		map[string]string{
			"DOENER_DATABASE_ROOT_PASSWORD":  "root-pw",
			"DOENER_DATABASE_ADMIN_PASSWORD": "admin-pw",
		}, "")
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Install.RootUser != "postgres" {
		t.Errorf("root user is %q, want postgres", cfg.Install.RootUser)
	}
	if !cfg.Install.NonInteractive {
		t.Error("--non-interactive was not picked up")
	}
	if cfg.Install.RootPassword != "root-pw" || cfg.Install.AdminPassword != "admin-pw" {
		t.Errorf("installer passwords not read from the environment: %q / %q",
			cfg.Install.RootPassword, cfg.Install.AdminPassword)
	}

	// None of these has a configuration file key.
	for _, flag := range []string{
		"output", "database-root-user", "database-root-password",
		"database-admin-user", "database-admin-password",
		"imprint-file", "legal-notes-file", "non-interactive",
	} {
		setting, ok := SettingByFlag(flag)
		if !ok {
			t.Fatalf("%s is not in the registry", flag)
		}
		if setting.InConfigFile() {
			t.Errorf("%s would be written to the configuration file, but is documented as never stored", flag)
		}
	}
}

func TestLoadRejectsASecretOnTheCommandLine(t *testing.T) {
	_, err := loadFor(t, ScopeGlobal, []string{"--database-password", "x"}, nil, "")
	if err == nil {
		t.Fatal("Load accepted a secret on the command line")
	}
	if !strings.Contains(err.Error(), "must not be passed on the command line") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadRejectsAWorldReadableConfigFile(t *testing.T) {
	if SkipPermissionCheck() {
		t.Skip("permission bits are not enforced on this platform")
	}

	path := filepath.Join(t.TempDir(), DefaultConfigFile)
	if err := os.WriteFile(path, []byte("database:\n  server: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := loadFor(t, ScopeGlobal, nil, nil, path); err == nil {
		t.Fatal("Load accepted a world-readable configuration file")
	}
}

// A missing default file is normal: defaults, environment and flags still
// apply. A file the operator named explicitly and that is not there is a
// mistake worth reporting.
func TestMissingConfigFile(t *testing.T) {
	cfg, err := loadFor(t, ScopeGlobal, nil, nil, "")
	if err != nil {
		t.Fatalf("a missing default config file was an error: %v", err)
	}
	if cfg.File != "" {
		t.Errorf("cfg.File is %q, want empty", cfg.File)
	}

	missing := filepath.Join(t.TempDir(), "absent.yaml")
	if _, err := loadFor(t, ScopeGlobal, nil, nil, missing); err == nil {
		t.Error("an explicitly named missing config file was accepted")
	}
}

func TestInvalidValuesAreReported(t *testing.T) {
	cases := map[string][]string{
		"log level": {"--log-level", "verbose"},
		"duration":  {"--idle-timeout", "7 fortnights"},
		"byte size": {"--max-image-size", "quite big"},
	}
	for name, args := range cases {
		if _, err := loadFor(t, ScopeServer, args, nil, ""); err == nil {
			t.Errorf("%s: an invalid value was accepted", name)
		}
	}
}

// Several bad values should all be reported, not just the first, so that one
// run of the command surfaces every problem with the configuration.
func TestInvalidValuesAreReportedTogether(t *testing.T) {
	_, err := loadFor(t, ScopeServer, []string{
		"--idle-timeout", "nonsense",
		"--max-image-size", "also nonsense",
	}, nil, "")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "idle-timeout") ||
		!strings.Contains(err.Error(), "max-image-size") {
		t.Errorf("not every invalid value was reported: %v", err)
	}
}

func TestLoadRequiresAFlagSet(t *testing.T) {
	if _, err := Load(LoadOptions{}); err == nil {
		t.Error("Load accepted a nil flag set")
	}
}

// Durations and byte sizes are registered as string flags so that "7d" and
// "5MiB" reach this package's parsers rather than pflag's. If one were
// registered as a native duration, pflag would reject "7d" before it got here.
func TestDurationAndSizeSettingsAreStringFlags(t *testing.T) {
	flags := newTestFlagSet()
	for _, setting := range Settings {
		if setting.Kind != KindDuration && setting.Kind != KindByteSize {
			continue
		}
		flag := flags.Lookup(setting.Flag)
		if flag == nil {
			t.Errorf("%s is not registered", setting.Flag)
			continue
		}
		if flag.Value.Type() != "string" {
			t.Errorf("%s is registered as %s; pflag would reject the documented default %v",
				setting.Flag, flag.Value.Type(), setting.Default)
		}
	}
}

// Every setting's declared default must itself be parseable, or the application
// cannot start without an explicit value for it.
func TestEveryDeclaredDefaultParses(t *testing.T) {
	for _, setting := range Settings {
		switch setting.Kind {
		case KindDuration:
			raw, _ := setting.Default.(string)
			if _, err := ParseDuration(raw); err != nil {
				t.Errorf("%s: default %q does not parse: %v", setting.Flag, raw, err)
			}
		case KindByteSize:
			raw, _ := setting.Default.(string)
			if _, err := ParseByteSize(raw); err != nil {
				t.Errorf("%s: default %q does not parse: %v", setting.Flag, raw, err)
			}
		}
	}
}

// The registry is the single source for flags, environment variables, config
// keys and defaults, so it must not contain duplicates.
func TestRegistryIsInternallyConsistent(t *testing.T) {
	flags := map[string]bool{}
	keys := map[string]bool{}
	shorts := map[string]string{}

	for _, setting := range Settings {
		if flags[setting.Flag] {
			t.Errorf("duplicate flag %s", setting.Flag)
		}
		flags[setting.Flag] = true

		if setting.Key != "" {
			if keys[setting.Key] {
				t.Errorf("duplicate config key %s", setting.Key)
			}
			keys[setting.Key] = true
		}

		if setting.Usage == "" {
			t.Errorf("%s has no usage text", setting.Flag)
		}
		if setting.Scopes == 0 {
			t.Errorf("%s belongs to no scope, so no verb would accept it", setting.Flag)
		}

		// Shorthands must be unique among settings that can appear together:
		// global settings share a command line with every other scope.
		if setting.Short == "" {
			continue
		}
		if other, taken := shorts[setting.Short]; taken {
			otherSetting, _ := SettingByFlag(other)
			if sharesCommandLine(setting, otherSetting) {
				t.Errorf("shorthand -%s is used by both %s and %s",
					setting.Short, other, setting.Flag)
			}
		}
		shorts[setting.Short] = setting.Flag
	}

	// -c is taken by --config, which is not in the registry.
	if flag, taken := shorts["c"]; taken {
		t.Errorf("shorthand -c is used by %s, but belongs to --config", flag)
	}
	// -v is taken by --version, and -h by --help.
	for _, reserved := range []string{"v", "h"} {
		if flag, taken := shorts[reserved]; taken {
			t.Errorf("shorthand -%s is used by %s, but is reserved by cobra", reserved, flag)
		}
	}
}

func sharesCommandLine(a, b Setting) bool {
	if a.Scopes.Has(ScopeGlobal) || b.Scopes.Has(ScopeGlobal) {
		return true
	}
	return a.Scopes&b.Scopes != 0
}

// The configuration a verb resolves has to describe the whole file, not the
// part that verb has flags for.
//
// update runs under the install scope and rewrites the file it read. While
// Server and Session were populated only for the server scope, that rewrite
// replaced every one of their settings with the declared default: a tuned port
// and timeouts silently reverted, and session.jwt_secret was written empty,
// which leaves a configuration the server refuses to start from with "the
// session signing secret is 0 characters".
func TestEverySectionIsResolvedWhateverTheScope(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultConfigFile)
	body := `database:
  server: "db.example"
  password: "runtime-pw"
server:
  port: 9443
  tls_cert: "/etc/ssl/doener.crt"
  http_write_timeout: "90s"
session:
  jwt_secret: "a-secret-long-enough-to-be-usable-here"
cleanup:
  retention: "30d"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	// The install scope, which is what update uses, and which registers no
	// server, session or cleanup flags at all.
	cfg, err := loadFor(t, ScopeInstall, nil, nil, path)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	if cfg.Server.Port != 9443 {
		t.Errorf("server.port resolved to %d, want the file's 9443", cfg.Server.Port)
	}
	if cfg.Server.TLSCert != "/etc/ssl/doener.crt" {
		t.Errorf("server.tls_cert resolved to %q, want the file's path", cfg.Server.TLSCert)
	}
	if cfg.Server.HTTPWriteTimeout != 90*time.Second {
		t.Errorf("http_write_timeout resolved to %s, want 90s", cfg.Server.HTTPWriteTimeout)
	}
	if cfg.Session.JWTSecret != "a-secret-long-enough-to-be-usable-here" {
		t.Errorf("the session signing secret resolved to %q", cfg.Session.JWTSecret)
	}
	if cfg.Cleanup.Retention != 30*24*time.Hour {
		t.Errorf("cleanup.retention resolved to %s, want 30d", cfg.Cleanup.Retention)
	}

	// And the file that would be written back keeps every one of them.
	values := ValuesFrom(cfg)
	for flag, want := range map[string]string{
		"port":               "9443",
		"tls-cert":           "/etc/ssl/doener.crt",
		"jwt-secret":         "a-secret-long-enough-to-be-usable-here",
		"http-write-timeout": "90s",
		"retention":          "30d",
	} {
		if got := values[flag]; got != want {
			t.Errorf("update would write %s as %q, want %q", flag, got, want)
		}
	}
}
