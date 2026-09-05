package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

// The four settings docs/09_configuration.md forbids on the command line.
var forbiddenOnCommandLine = []string{
	"database-password",
	"database-root-password",
	"database-admin-password",
	"jwt-secret",
}

func TestExactlyTheDocumentedSettingsAreSecret(t *testing.T) {
	got := map[string]bool{}
	for _, setting := range SecretSettings() {
		got[setting.Flag] = true
	}

	for _, flag := range forbiddenOnCommandLine {
		if !got[flag] {
			t.Errorf("%s is not marked Secret", flag)
		}
	}
	if len(got) != len(forbiddenOnCommandLine) {
		t.Errorf("SecretSettings() has %d entries, want %d: %v",
			len(got), len(forbiddenOnCommandLine), got)
	}
}

func TestEachSecretOnTheCommandLineIsRejected(t *testing.T) {
	for _, flag := range forbiddenOnCommandLine {
		flags := newTestFlagSet()
		if err := flags.Parse([]string{"--" + flag, "hunter2"}); err != nil {
			t.Fatalf("%s: parsing: %v", flag, err)
		}

		err := CheckCommandLineSecrets(flags)
		if err == nil {
			t.Errorf("--%s was accepted on the command line", flag)
			continue
		}

		var secretErr *SecretOnCommandLineError
		if !errors.As(err, &secretErr) {
			t.Errorf("--%s: error is %T, want *SecretOnCommandLineError", flag, err)
			continue
		}
		if !strings.Contains(err.Error(), "--"+flag) {
			t.Errorf("--%s: error %q does not name the flag", flag, err)
		}
	}
}

// The message must never echo the value, or refusing the flag would itself
// disclose the secret it is protecting.
func TestRejectionNeverEchoesTheValue(t *testing.T) {
	const secret = "correct-horse-battery-staple"

	flags := newTestFlagSet()
	if err := flags.Parse([]string{"--jwt-secret", secret}); err != nil {
		t.Fatal(err)
	}

	err := CheckCommandLineSecrets(flags)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("the error message echoed the secret: %s", err)
	}
}

func TestRejectionSaysWhereTheValueBelongs(t *testing.T) {
	// A runtime secret may live in the configuration file.
	flags := newTestFlagSet()
	if err := flags.Parse([]string{"--database-password", "x"}); err != nil {
		t.Fatal(err)
	}
	err := CheckCommandLineSecrets(flags)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "configuration file") {
		t.Errorf("error does not mention the configuration file: %s", err)
	}
	if !strings.Contains(err.Error(), "DOENER_") {
		t.Errorf("error does not mention the environment variable: %s", err)
	}

	// An installer secret is never stored, so the file must not be offered.
	flags = newTestFlagSet()
	if err := flags.Parse([]string{"--database-root-password", "x"}); err != nil {
		t.Fatal(err)
	}
	err = CheckCommandLineSecrets(flags)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "configuration file") {
		t.Errorf("an installer secret was wrongly offered the configuration file: %s", err)
	}
	if !strings.Contains(err.Error(), "prompt") {
		t.Errorf("error does not mention the interactive prompt: %s", err)
	}
}

func TestSeveralSecretsAreAllReported(t *testing.T) {
	flags := newTestFlagSet()
	err := flags.Parse([]string{
		"--database-password", "a",
		"--jwt-secret", "b",
	})
	if err != nil {
		t.Fatal(err)
	}

	checkErr := CheckCommandLineSecrets(flags)
	if checkErr == nil {
		t.Fatal("expected an error")
	}

	var secretErr *SecretOnCommandLineError
	if !errors.As(checkErr, &secretErr) {
		t.Fatalf("error is %T, want *SecretOnCommandLineError", checkErr)
	}
	if len(secretErr.Flags) != 2 {
		t.Errorf("reported %v, want both offending flags", secretErr.Flags)
	}
	// Sorted, so the message does not depend on registry order.
	if secretErr.Flags[0] != "database-password" || secretErr.Flags[1] != "jwt-secret" {
		t.Errorf("flags are not in a stable order: %v", secretErr.Flags)
	}
}

func TestNonSecretFlagsAreAccepted(t *testing.T) {
	flags := newTestFlagSet()
	err := flags.Parse([]string{
		"--database-server", "db.example.invalid",
		"--database-user", "doener",
		"--database-root-user", "postgres",
		"--log-level", "DEBUG",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := CheckCommandLineSecrets(flags); err != nil {
		t.Errorf("ordinary flags were rejected: %v", err)
	}
}

// The privileged user names are documented as "never stored", which is not the
// same as "must not be passed on the command line". A user name is not a
// secret and forbidding it would be a needless obstacle.
func TestPrivilegedUserNamesAreNotSecret(t *testing.T) {
	for _, flag := range []string{"database-root-user", "database-admin-user"} {
		setting, ok := SettingByFlag(flag)
		if !ok {
			t.Fatalf("%s is not in the registry", flag)
		}
		if setting.Secret {
			t.Errorf("%s is marked Secret, but only its password is", flag)
		}
	}
}

// The secret flags must be registered so that this check can see them, and
// hidden so that help does not invite their use.
func TestSecretFlagsAreRegisteredButHidden(t *testing.T) {
	flags := newTestFlagSet()

	for _, flag := range forbiddenOnCommandLine {
		f := flags.Lookup(flag)
		if f == nil {
			t.Errorf("%s is not registered, so passing it would produce "+
				"'unknown flag' rather than the security error", flag)
			continue
		}
		if !f.Hidden {
			t.Errorf("%s appears in help, which invites its use", flag)
		}
	}

	if strings.Contains(flags.FlagUsages(), "jwt-secret") {
		t.Error("a secret flag appears in the usage text")
	}
}

// newTestFlagSet builds a flag set carrying every setting, regardless of scope,
// so that a test can exercise any flag without constructing a command.
func newTestFlagSet() *pflag.FlagSet {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.SetOutput(nopWriter{})
	RegisterGlobalFlags(flags)
	RegisterScopeFlags(flags, ScopeServer)
	RegisterScopeFlags(flags, ScopeInstall)
	RegisterScopeFlags(flags, ScopeCleanup)
	return flags
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }
