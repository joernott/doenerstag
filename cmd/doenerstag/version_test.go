package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/joernott/doenerstag/internal/config"
	"github.com/joernott/doenerstag/internal/version"
)

func TestVersionFlagPrintsTheBinaryVersion(t *testing.T) {
	for _, flag := range []string{"--version", "-v"} {
		out := &bytes.Buffer{}
		root := newRootCommand()
		root.SetArgs([]string{flag})
		root.SetOut(out)
		root.SetErr(out)

		if err := root.Execute(); err != nil {
			t.Fatalf("%s returned an error: %v", flag, err)
		}

		got := strings.TrimSpace(out.String())
		if got != version.String() {
			t.Errorf("%s printed %q, want %q", flag, got, version.String())
		}
	}
}

// --version must not need a database, a configuration file or anything else. It
// is what an operator runs when the application will not start.
func TestVersionFlagDoesNotLoadConfiguration(t *testing.T) {
	out := &bytes.Buffer{}
	root := newRootCommand()
	root.SetArgs([]string{"--version", "--config", "/nonexistent/doenerstag.yaml"})
	root.SetOut(out)
	root.SetErr(out)

	if err := root.Execute(); err != nil {
		t.Fatalf("--version failed with an unreadable config file: %v", err)
	}
	if !strings.Contains(out.String(), "doenerstag") {
		t.Errorf("--version printed %q", out.String())
	}
}

func TestVersionStringNamesTheBinaryAndItsBuild(t *testing.T) {
	s := version.String()
	for _, want := range []string{"doenerstag", version.Version(), version.Commit(), version.BuildDate()} {
		if !strings.Contains(s, want) {
			t.Errorf("version string %q does not contain %q", s, want)
		}
	}
}

// A binary built by plain `go build` must say so rather than claiming a release
// version it does not have.
func TestUnstampedBuildReportsItself(t *testing.T) {
	if !version.IsDevelopment() {
		t.Skip("this binary was built with version information")
	}
	if !strings.Contains(version.Version(), "dev") {
		t.Errorf("an unstamped build reports version %q", version.Version())
	}
}

func TestGlobalFlagsArePersistentOnRoot(t *testing.T) {
	root := newRootCommand()

	// Every global setting, plus --config itself.
	wanted := []string{config.ConfigFlag}
	for _, setting := range config.SettingsInScope(config.ScopeGlobal) {
		wanted = append(wanted, setting.Flag)
	}

	for _, name := range wanted {
		if root.PersistentFlags().Lookup(name) == nil {
			t.Errorf("global flag --%s is not registered on the root command", name)
		}
	}
}

// Global flags are persistent, so every verb must accept them without each verb
// registering them itself.
func TestEveryVerbInheritsTheGlobalFlags(t *testing.T) {
	for verb := range expectedVerbs {
		root := newRootCommand()
		cmd, _, err := root.Find([]string{verb})
		if err != nil {
			t.Fatalf("finding %s: %v", verb, err)
		}
		if cmd.InheritedFlags().Lookup("log-level") == nil {
			t.Errorf("%s does not inherit --log-level", verb)
		}
		if cmd.InheritedFlags().Lookup(config.ConfigFlag) == nil {
			t.Errorf("%s does not inherit --config", verb)
		}
	}
}

func TestVerbsCarryTheirOwnScopeFlags(t *testing.T) {
	cases := map[string][]string{
		"server":  {"port", "tls-cert", "tls-key", "bind-address", "no-https", "idle-timeout"},
		"install": {"output", "database-root-user", "database-admin-user", "non-interactive"},
		"update":  {"output", "database-root-user"},
		"cleanup": {"retention", "dry-run"},
	}

	for verb, flags := range cases {
		root := newRootCommand()
		cmd, _, err := root.Find([]string{verb})
		if err != nil {
			t.Fatalf("finding %s: %v", verb, err)
		}
		for _, flag := range flags {
			if cmd.Flags().Lookup(flag) == nil {
				t.Errorf("%s does not accept --%s", verb, flag)
			}
		}
	}
}

// A flag belonging to another verb must be rejected rather than silently
// ignored, so that a mistyped invocation is caught.
func TestVerbsRejectFlagsFromOtherScopes(t *testing.T) {
	out := &bytes.Buffer{}
	root := newRootCommand()
	root.SetArgs([]string{"cleanup", "--port", "9000"})
	root.SetOut(out)
	root.SetErr(out)

	if err := root.Execute(); err == nil {
		t.Error("cleanup accepted --port, which belongs to server")
	}
}

// Every registered shorthand must be unique within a verb, or pflag panics at
// registration time. Constructing the tree is the assertion.
func TestNoShorthandCollisions(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("building the command tree panicked, most likely a duplicate shorthand: %v", r)
		}
	}()

	root := newRootCommand()
	for _, cmd := range root.Commands() {
		cmd.InheritedFlags() // forces the merge of persistent and local flags
		_ = cmd.Flags().FlagUsages()
	}
}
