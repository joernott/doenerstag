package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Every documented verb.
var expectedVerbs = map[string]bool{
	"server":  true,
	"install": true,
	"update":  true,
	"cleanup": true,
	"version": true,
	"user":    true,
}

func TestRootCommandHasEveryDocumentedVerb(t *testing.T) {
	root := newRootCommand()

	found := map[string]bool{}
	for _, cmd := range root.Commands() {
		found[cmd.Name()] = true
	}

	for verb := range expectedVerbs {
		if !found[verb] {
			t.Errorf("verb %q is missing from the command tree", verb)
		}
	}

	// Guard against a verb being added here without being documented in
	// docs/09_configuration.md.
	for name := range found {
		if _, expected := expectedVerbs[name]; !expected && !isBuiltinCobraCommand(name) {
			t.Errorf("undocumented verb %q in the command tree", name)
		}
	}
}

func isBuiltinCobraCommand(name string) bool {
	return name == "help" || name == "completion"
}

// install and version are implemented. Without a database they must still fail
// cleanly and say something, rather than crashing or claiming to be a stub.
func TestImplementedVerbsFailCleanlyWithoutADatabase(t *testing.T) {
	for _, verb := range []string{"server", "install", "update", "version"} {
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}

		code := run([]string{verb, "--non-interactive"}, stdout, stderr)
		if code != exitFailure {
			t.Errorf("%s without a database exited %d, want %d", verb, code, exitFailure)
		}
		if strings.TrimSpace(stderr.String()+stdout.String()) == "" {
			t.Errorf("%s failed silently", verb)
		}
		if strings.Contains(stderr.String(), "not implemented") {
			t.Errorf("%s still reports itself as a stub", verb)
		}
	}
}

// A stub failing is not a usage error, so cobra must not dump the usage text
// on top of it. Operators read the first line and stop.
func TestStubFailureDoesNotPrintUsage(t *testing.T) {
	out := &bytes.Buffer{}
	root := newRootCommand()
	root.SetArgs([]string{"server"})
	root.SetOut(out)
	root.SetErr(out)

	_ = root.Execute()

	if strings.Contains(out.String(), "Usage:") {
		t.Errorf("stub failure printed usage:\n%s", out.String())
	}
}

func TestBareCommandShowsHelpAndSucceeds(t *testing.T) {
	out := &bytes.Buffer{}
	root := newRootCommand()
	root.SetArgs(nil)
	root.SetOut(out)
	root.SetErr(out)

	if err := root.Execute(); err != nil {
		t.Fatalf("bare command returned an error: %v", err)
	}

	for _, want := range []string{"doenerstag", "server", "install", "Usage:"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help output does not mention %q:\n%s", want, out.String())
		}
	}
}

func TestUnknownVerbIsAUsageError(t *testing.T) {
	out := &bytes.Buffer{}
	root := newRootCommand()
	root.SetArgs([]string{"reticulate"})
	root.SetOut(out)
	root.SetErr(out)

	err := root.Execute()
	if err == nil {
		t.Fatal("an unknown verb was accepted")
	}

	// The message has to say what was wrong with the invocation: a verb that
	// does not exist and one that exists but failed are different problems.
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("error %q does not say the verb is unknown", err)
	}
}

func TestVerbsRejectPositionalArguments(t *testing.T) {
	for verb := range expectedVerbs {
		out := &bytes.Buffer{}
		root := newRootCommand()
		root.SetArgs([]string{verb, "unexpected"})
		root.SetOut(out)
		root.SetErr(out)

		if err := root.Execute(); err == nil {
			t.Errorf("%s accepted a positional argument", verb)
		}
	}
}

func TestEveryCommandHasHelpText(t *testing.T) {
	var check func(*cobra.Command)
	check = func(cmd *cobra.Command) {
		if !isBuiltinCobraCommand(cmd.Name()) {
			if cmd.Short == "" {
				t.Errorf("command %q has no Short description", cmd.Name())
			}
			if cmd.Long == "" {
				t.Errorf("command %q has no Long description", cmd.Name())
			}
		}
		for _, sub := range cmd.Commands() {
			check(sub)
		}
	}
	check(newRootCommand())
}
