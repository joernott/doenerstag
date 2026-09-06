package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
)

func renderDefaults(t *testing.T) string {
	t.Helper()

	cfg, err := loadFor(t, ScopeServer|ScopeCleanup, nil, nil, "")
	if err != nil {
		t.Fatalf("loading defaults: %v", err)
	}

	body, err := Render(ValuesFrom(cfg), time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return body
}

// The generated file doubles as the reference, so every setting has to be in
// it. A setting added to the registry and forgotten here would be invisible to
// anyone configuring the application from the file.
func TestEverySettingAppearsInTheGeneratedFile(t *testing.T) {
	body := renderDefaults(t)

	for _, setting := range Settings {
		if !setting.InConfigFile() {
			continue
		}
		if !strings.Contains(body, leafKey(setting.Key)+":") {
			t.Errorf("setting %s is missing from the generated file", setting.Key)
		}
		if !strings.Contains(body, capitalise(setting.Usage)) {
			t.Errorf("setting %s has no explanatory comment", setting.Key)
		}
	}
}

// Install-only settings are documented as never stored. Writing one would put a
// privileged database password on disk.
func TestInstallOnlySettingsAreNeverWritten(t *testing.T) {
	body := renderDefaults(t)

	for _, flag := range []string{
		"database-root-user", "database-root-password",
		"database-admin-user", "database-admin-password",
		"output", "non-interactive", "imprint-file", "legal-notes-file",
	} {
		if strings.Contains(body, flag) {
			t.Errorf("%s appears in the generated file but is documented as never stored", flag)
		}
	}
}

// The file the writer produces must be readable by the loader, or install would
// generate something the server then refuses to start with.
func TestGeneratedFileRoundTripsThroughLoad(t *testing.T) {
	original, err := loadFor(t, ScopeServer|ScopeCleanup, []string{
		"--database-server", "db.example.invalid",
		"--database-port", "6543",
		"--port", "9443",
		"--no-https",
		"--idle-timeout", "2h",
		"--absolute-timeout", "30d",
		"--max-image-size", "2MiB",
		"--retention", "3d",
		"--log-level", "DEBUG",
	}, nil, "")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	body, err := Render(ValuesFrom(original), time.Now())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	path := filepath.Join(t.TempDir(), DefaultConfigFile)
	if err := WriteFile(path, body); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	reloaded, err := loadFor(t, ScopeServer|ScopeCleanup, nil, nil, path)
	if err != nil {
		t.Fatalf("reloading the generated file: %v", err)
	}

	if reloaded.Database.Server != "db.example.invalid" || reloaded.Database.Port != 6543 {
		t.Errorf("database section did not survive: %+v", reloaded.Database)
	}
	if reloaded.Server.Port != 9443 || !reloaded.Server.NoHTTPS {
		t.Errorf("server section did not survive: %+v", reloaded.Server)
	}
	if reloaded.Server.MaxImageSize != 2*1024*1024 {
		t.Errorf("max image size did not survive: %d", reloaded.Server.MaxImageSize)
	}
	if reloaded.Session.IdleTimeout != 2*time.Hour {
		t.Errorf("idle timeout did not survive: %v", reloaded.Session.IdleTimeout)
	}
	if reloaded.Session.AbsoluteTimeout != 30*24*time.Hour {
		t.Errorf("absolute timeout did not survive: %v", reloaded.Session.AbsoluteTimeout)
	}
	if reloaded.Cleanup.Retention != 3*24*time.Hour {
		t.Errorf("retention did not survive: %v", reloaded.Cleanup.Retention)
	}
	if reloaded.Log.Level.String() != "DEBUG" {
		t.Errorf("log level did not survive: %v", reloaded.Log.Level)
	}
}

// Durations go back out in the form a person would write, so a generated file
// reads like a hand-written one rather than like nanoseconds.
func TestDurationsAndSizesAreWrittenInHumanForm(t *testing.T) {
	body := renderDefaults(t)

	for _, want := range []string{`absolute_timeout: "7d"`, `retention: "14d"`,
		`idle_timeout: "6h"`, `max_image_size: "5MiB"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the generated file does not contain %q", want)
		}
	}
	if strings.Contains(body, "604800000000000") {
		t.Error("a duration was written as nanoseconds")
	}
}

// A password containing YAML metacharacters must survive. Unquoted, a value
// like "pa: ss" becomes a nested map and "#x" becomes a comment.
func TestAwkwardSecretsSurviveTheRoundTrip(t *testing.T) {
	awkward := []string{
		"pa: ss", "#hash", "with \"quotes\"", `back\slash`, "colon:colon",
		"0123", "true", "null", "{brace}", "[bracket]", "  padded  ", "Ünïcøde",
	}

	for _, secret := range awkward {
		cfg, err := loadFor(t, ScopeServer, nil, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		values := ValuesFrom(cfg)
		values["database-password"] = secret
		values["jwt-secret"] = secret

		body, err := Render(values, time.Now())
		if err != nil {
			t.Fatalf("Render: %v", err)
		}

		path := filepath.Join(t.TempDir(), DefaultConfigFile)
		if err := WriteFile(path, body); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		reloaded, err := loadFor(t, ScopeServer, nil, nil, path)
		if err != nil {
			t.Errorf("secret %q produced an unreadable file: %v", secret, err)
			continue
		}
		if reloaded.Database.Password != secret {
			t.Errorf("password %q came back as %q", secret, reloaded.Database.Password)
		}
		if reloaded.Session.JWTSecret != secret {
			t.Errorf("jwt secret %q came back as %q", secret, reloaded.Session.JWTSecret)
		}
	}
}

func TestWriteFileUsesOwnerOnlyPermissions(t *testing.T) {
	if SkipPermissionCheck() {
		t.Skip("permission bits are not enforced on this platform")
	}

	path := filepath.Join(t.TempDir(), DefaultConfigFile)
	if err := WriteFile(path, "log:\n  level: \"INFO\"\n"); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != ConfigFileMode {
		t.Errorf("mode is %04o, want %04o", info.Mode().Perm(), ConfigFileMode)
	}
	// And the loader must accept what the writer produced.
	if err := CheckFilePermissions(path, nil); err != nil {
		t.Errorf("the file the writer produced fails its own permission check: %v", err)
	}
}

// Replacing an existing file must not widen its permissions or leave a
// temporary file behind.
func TestWriteFileReplacesCleanly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultConfigFile)

	if err := WriteFile(path, "log:\n  level: \"INFO\"\n"); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, "log:\n  level: \"DEBUG\"\n"); err != nil {
		t.Fatalf("replacing: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "DEBUG") {
		t.Errorf("the replacement did not take: %s", content)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("temporary files were left behind: %v", names)
	}
}

// The whole point of the writability probe: it runs before the operator answers
// anything, and the file it is asked about is usually the one being read for
// its own defaults. Truncating it would destroy those answers.
func TestCheckWritableDoesNotTouchAnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultConfigFile)
	const original = "database:\n  server: \"keep-me\"\n"

	if err := os.WriteFile(path, []byte(original), ConfigFileMode); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := CheckWritable(path); err != nil {
		t.Fatalf("CheckWritable on a writable file: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != original {
		t.Errorf("the probe changed the file: %q", content)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != before.Size() {
		t.Errorf("the probe changed the size from %d to %d", before.Size(), after.Size())
	}
}

func TestCheckWritableOnAMissingFileInAWritableDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist-yet.yaml")
	if err := CheckWritable(path); err != nil {
		t.Errorf("a new file in a writable directory was rejected: %v", err)
	}
}

func TestCheckWritableRejectsAMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "doenerstag.yaml")
	if err := CheckWritable(path); err == nil {
		t.Error("a path in a missing directory was accepted")
	}
}

func TestCheckWritableRejectsADirectory(t *testing.T) {
	dir := t.TempDir()
	if err := CheckWritable(dir); err == nil {
		t.Error("a directory was accepted as a configuration file path")
	}
}

func TestCheckWritableLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultConfigFile)

	if err := CheckWritable(path); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the probe left files behind: %v", names)
	}
}

// A setting whose key uses an unknown section would be dropped from the file
// without a word, so Render refuses rather than losing it.
func TestRenderRejectsAnUnknownSection(t *testing.T) {
	original := Settings
	t.Cleanup(func() { Settings = original })

	Settings = append(append([]Setting{}, original...), Setting{
		Flag: "invented", Key: "nowhere.invented",
		Kind: KindString, Default: "", Usage: "invented", Scopes: ScopeGlobal,
	})

	if _, err := Render(Values{}, time.Now()); err == nil {
		t.Error("a setting in an unknown section was silently dropped")
	} else if !strings.Contains(err.Error(), "nowhere.invented") {
		t.Errorf("the error does not name the offending setting: %v", err)
	}
}

func TestGeneratedFileNamesFlagsAndEnvironmentVariables(t *testing.T) {
	body := renderDefaults(t)

	if !strings.Contains(body, "Flag: --database-server. Environment: DOENER_DATABASE_SERVER.") {
		t.Error("the generated file does not document the flag and environment variable")
	}
	// Secrets get the opposite note, since they have no usable flag.
	if !strings.Contains(body, "Never accepted on the command line") {
		t.Error("the generated file does not warn that secrets cannot be passed as flags")
	}
}

// Guard against the writer and the loader disagreeing about the mapping.
func TestValuesFromCoversEveryStoredSetting(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.SetOutput(nopWriter{})
	RegisterGlobalFlags(flags)
	RegisterScopeFlags(flags, ScopeServer)
	RegisterScopeFlags(flags, ScopeCleanup)
	if err := flags.Parse(nil); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(LoadOptions{
		Flags:  flags,
		Scope:  ScopeServer | ScopeCleanup,
		Getenv: func(string) (string, bool) { return "", false },
	})
	if err != nil {
		t.Fatal(err)
	}

	values := ValuesFrom(cfg)
	for _, setting := range Settings {
		if !setting.InConfigFile() {
			continue
		}
		if _, ok := values[setting.Flag]; !ok {
			t.Errorf("ValuesFrom has no entry for %s", setting.Flag)
		}
	}
}

// An untouched setting must be written exactly as the specification declares
// it, so the generated file reads like the reference it claims to be. "60s"
// coming back as "1m" is the same duration but no longer the documented string.
func TestUntouchedSettingsKeepTheirDeclaredSpelling(t *testing.T) {
	body := renderDefaults(t)

	for _, setting := range Settings {
		if !setting.InConfigFile() {
			continue
		}
		declared := defaultString(setting)
		if declared == "" {
			continue
		}

		want := leafKey(setting.Key) + ": " + yamlScalar(setting, declared)
		if !strings.Contains(body, want) {
			t.Errorf("expected %q in the generated file", want)
		}
	}
}

// A value the operator did change is written as given, not silently replaced
// by the default.
func TestChangedSettingsAreWrittenAsSet(t *testing.T) {
	cfg, err := loadFor(t, ScopeServer|ScopeCleanup,
		[]string{"--retention", "3d", "--http-write-timeout", "90s"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}

	body, err := Render(ValuesFrom(cfg), time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(body, `retention: "3d"`) {
		t.Error("a changed retention was not written as set")
	}
	if !strings.Contains(body, `http_write_timeout: "90s"`) {
		t.Error("a changed write timeout was not written as set")
	}
}
