package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The exit code is the only thing a shell script, a systemd unit or a cron job
// can see, so it is asserted directly rather than inferred from the output.
func TestRunExitCodes(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"version flag", []string{"--version"}, exitOK},
		{"help flag", []string{"--help"}, exitOK},
		{"bare command shows help", nil, exitOK},
		{"verb help", []string{"server", "--help"}, exitOK},
		{"unimplemented verb", []string{"server"}, exitFailure},
		{"unknown verb", []string{"reticulate"}, exitFailure},
		{"unknown flag", []string{"server", "--nonsense"}, exitFailure},
		{"secret on the command line", []string{"server", "--jwt-secret", "x"}, exitFailure},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}

			if got := run(tc.args, stdout, stderr); got != tc.want {
				t.Errorf("run(%v) = %d, want %d\nstdout: %s\nstderr: %s",
					tc.args, got, tc.want, stdout, stderr)
			}
		})
	}
}

// A failure must say something. An exit code with no output leaves the operator
// with nowhere to start.
func TestEveryFailureExplainsItself(t *testing.T) {
	cases := map[string][]string{
		"unimplemented verb":         {"server"},
		"unknown verb":               {"reticulate"},
		"unknown flag":               {"server", "--nonsense"},
		"secret on the command line": {"server", "--jwt-secret", "x"},
	}

	for name, args := range cases {
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}

		run(args, stdout, stderr)

		if strings.TrimSpace(stderr.String()) == "" {
			t.Errorf("%s: failed silently", name)
		}
	}
}

// A configuration failure is documented as a FATAL security error, so it is
// emitted as a structured log line rather than as prose, and only once.
func TestConfigurationFailureIsASingleFatalLogLine(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	if got := run([]string{"server", "--jwt-secret", "x"}, stdout, stderr); got != exitFailure {
		t.Fatalf("exit code %d, want %d", got, exitFailure)
	}

	lines := nonEmptyLines(stderr.String())
	if len(lines) != 1 {
		t.Fatalf("expected exactly one line, got %d:\n%s", len(lines), stderr)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &decoded); err != nil {
		t.Fatalf("the FATAL line is not JSON: %v (%s)", err, lines[0])
	}
	if decoded["level"] != "fatal" {
		t.Errorf("level is %v, want fatal", decoded["level"])
	}
	if decoded["component"] != "config" {
		t.Errorf("component is %v, want config", decoded["component"])
	}

	errText, _ := decoded["error"].(string)
	if !strings.Contains(errText, "--jwt-secret") {
		t.Errorf("the error does not name the flag: %v", errText)
	}
	// Refusing the flag must not disclose the value it was protecting.
	if strings.Contains(stderr.String(), `"x"`) {
		t.Errorf("the secret value appeared in the output: %s", stderr)
	}
}

func TestUnreadableConfigFileIsFatal(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	missing := filepath.Join(t.TempDir(), "absent.yaml")
	if got := run([]string{"server", "--config", missing}, stdout, stderr); got != exitFailure {
		t.Errorf("exit code %d, want %d", got, exitFailure)
	}
	if !strings.Contains(stderr.String(), "absent.yaml") {
		t.Errorf("the error does not name the file: %s", stderr)
	}
}

// A world-readable configuration file must stop the application, not warn.
func TestWorldReadableConfigFileIsFatal(t *testing.T) {
	if isWindows() {
		t.Skip("permission bits are not enforced on this platform")
	}

	path := filepath.Join(t.TempDir(), "doenerstag.yaml")
	if err := os.WriteFile(path, []byte("log:\n  level: INFO\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	if got := run([]string{"server", "--config", path}, stdout, stderr); got != exitFailure {
		t.Errorf("exit code %d, want %d", got, exitFailure)
	}
	if !strings.Contains(stderr.String(), "chmod") {
		t.Errorf("the error does not say how to fix it: %s", stderr)
	}
}

// The configuration is resolved and logging is started before a verb runs, so a
// verb that reaches its stub proves the whole chain worked.
func TestConfigurationIsWiredBeforeAVerbRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doenerstag.yaml")
	if err := os.WriteFile(path, []byte("log:\n  level: DEBUG\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	// update is used because it is still a stub: the verb has to fail without
	// touching the database, so that what this test observes is the startup
	// sequence rather than a connection attempt.
	run([]string{"update", "--config", path}, stdout, stderr)

	// At DEBUG the startup line is emitted, on stdout, before the stub fails.
	if !strings.Contains(stdout.String(), "configuration resolved") {
		t.Errorf("no startup line was logged:\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	if !strings.Contains(stderr.String(), "not implemented") {
		t.Errorf("the verb stub did not run: %s", stderr)
	}
}

func TestVerbScopesCoverEveryVerb(t *testing.T) {
	for verb := range expectedVerbs {
		if _, ok := verbScopes[verb]; !ok {
			t.Errorf("verb %q has no scope, so its own flags would be ignored", verb)
		}
	}
	for verb := range verbScopes {
		if _, ok := expectedVerbs[verb]; !ok {
			t.Errorf("verbScopes names %q, which is not a verb", verb)
		}
	}
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

func isWindows() bool { return os.PathSeparator == '\\' }
