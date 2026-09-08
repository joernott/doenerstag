// Command doenerstag coordinates food orders for a group.
//
// It is a single binary with several verbs: server runs the web application,
// install provisions the database and writes a configuration file, update
// migrates an existing installation, cleanup removes expired orders, and
// version reports what is installed.
//
// See docs/09_configuration.md for the full configuration reference.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// Exit codes. Anything non-zero means the command did not do its job.
const (
	exitOK      = 0
	exitFailure = 1
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the command tree and returns the process exit code. Keeping it
// separate from main, with explicit arguments and streams, is what makes the
// command tree testable.
func run(args []string, stdout, stderr io.Writer) int {
	root := newRootCommand()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	if err := root.Execute(); err != nil {
		if errors.Is(err, errAlreadyReported) {
			// A FATAL line has already been written; saying it twice in two
			// different formats would only be confusing.
			return exitFailure
		}
		// Errors are silenced on the commands so that cobra does not print
		// them alongside the usage text, which means reporting them here.
		_, _ = fmt.Fprintln(stderr, "Error:", err)
		return exitFailure
	}
	return exitOK
}
