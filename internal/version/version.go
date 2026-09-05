// Package version reports what this binary is.
//
// The values are injected at link time by the Makefile:
//
//	go build -ldflags "-X github.com/joernott/doenerstag/internal/version.version=1.0.0 ..."
//
// A binary built without those flags reports the development placeholders
// rather than claiming a version it does not have.
package version

import "fmt"

// Injected via -ldflags. These must stay package-level vars with these exact
// names; the Makefile refers to them by path.
var (
	version   = "0.0.0-dev"
	commit    = "unknown"
	buildDate = "unknown"
)

// Version returns the semantic version of this binary.
func Version() string { return version }

// Commit returns the git commit it was built from.
func Commit() string { return commit }

// BuildDate returns the RFC 3339 timestamp of the build.
func BuildDate() string { return buildDate }

// IsDevelopment reports whether this binary was built without version
// information, which means it came from a plain `go build` rather than from
// the Makefile.
func IsDevelopment() bool { return version == "0.0.0-dev" }

// String renders the full version line printed by --version.
//
// This is what the binary is. It is deliberately distinct from the `version`
// verb, which reports what the database says is installed; the two can differ,
// and that difference is exactly what the startup check in task 4.8 looks for.
func String() string {
	return fmt.Sprintf("doenerstag %s (commit %s, built %s)", version, commit, buildDate)
}
