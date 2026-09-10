package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/joernott/doenerstag/internal/logging"
)

// A log file that cannot be opened does not stop an administrative verb.
//
// The configuration file names the server's log, which is owned by the service
// user. Every verb reads that same file to find the database, so `doenerstag
// update` run by a person at a terminal used to exit FATAL with "permission
// denied" before doing anything at all. Reported from a real installation.
func TestAnUnwritableLogFileDoesNotStopAnAdminVerb(t *testing.T) {
	// A path that cannot be opened for any user: a file inside a file.
	blocked := filepath.Join(t.TempDir(), "not-a-directory", "doenerstag.log")
	if err := os.WriteFile(filepath.Dir(blocked), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	opts := logging.Options{Level: logging.LevelInfo, File: blocked}
	if _, err := logging.New(opts); err == nil {
		t.Fatal("the blocked path opened; the test proves nothing")
	} else {
		t.Logf("blocked as expected: %v", err)
	}

	for _, verb := range []string{"update", "user", "restaurant", "order", "cleanup"} {
		t.Run(verb, func(t *testing.T) {
			cmd := &cobra.Command{Use: verb}
			root := &cobra.Command{Use: "doenerstag"}
			root.AddCommand(cmd)

			stderr := &bytes.Buffer{}
			cmd.SetErr(stderr)

			_, err := logging.New(opts)
			logger, err := logToStderrInstead(cmd, opts, err)
			if err != nil {
				t.Fatalf("%s was refused a logger: %v", verb, err)
			}
			defer func() { _ = logger.Close() }()

			// It says what happened rather than doing it silently: an operator
			// who expected the log to be written needs to know it was not.
			written := stderr.String()
			if !strings.Contains(written, "standard error") {
				t.Errorf("no warning was written: %q", written)
			}
			if !strings.Contains(written, blocked) {
				t.Errorf("the warning does not name the log file: %q", written)
			}
		})
	}
}

// The server is the exception, and deliberately.
//
// It is a daemon started by systemd and meant to run as the user that owns the
// log. One that quietly logged to a terminal instead would run for months with
// nobody noticing it had recorded nothing.
func TestTheServerStillRefusesToStartWithoutItsLog(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "not-a-directory", "doenerstag.log")
	if err := os.WriteFile(filepath.Dir(blocked), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	root := &cobra.Command{Use: "doenerstag"}
	server := &cobra.Command{Use: "server"}
	root.AddCommand(server)

	opts := logging.Options{Level: logging.LevelInfo, File: blocked}
	_, cause := logging.New(opts)
	if cause == nil {
		t.Fatal("the blocked path opened; the test proves nothing")
	}

	if _, err := logToStderrInstead(server, opts, cause); !errors.Is(err, cause) {
		t.Errorf("the server was given a fallback logger: %v", err)
	}
}

// Nothing changes for a verb that was logging to a stream in the first place:
// there is no file to fail to open, so there is nothing to fall back from.
func TestNoFallbackWhenNoLogFileWasAskedFor(t *testing.T) {
	root := &cobra.Command{Use: "doenerstag"}
	user := &cobra.Command{Use: "user"}
	root.AddCommand(user)

	cause := errors.New("something else went wrong")
	if _, err := logToStderrInstead(user, logging.Options{}, cause); !errors.Is(err, cause) {
		t.Errorf("a stream logger was replaced: %v", err)
	}
}
