package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAcceptableFileMode(t *testing.T) {
	accepted := []fs.FileMode{0o600, 0o400}
	for _, mode := range accepted {
		if !AcceptableFileMode(mode) {
			t.Errorf("mode %04o was rejected, want accepted", mode)
		}
	}

	rejected := []fs.FileMode{
		0o644, // world-readable, the common mistake
		0o640, // group-readable
		0o666,
		0o777,
		0o604,
		0o660,
		0o700, // owner-executable: a config file is not a program
		0o000, // unreadable even by its owner
	}
	for _, mode := range rejected {
		if AcceptableFileMode(mode) {
			t.Errorf("mode %04o was accepted, want rejected", mode)
		}
	}
}

func TestCheckFilePermissionsRejectsAWorldReadableFile(t *testing.T) {
	if SkipPermissionCheck() {
		t.Skip("permission bits are not enforced on this platform")
	}

	path := writeFileWithMode(t, "doenerstag.yaml", 0o644)

	err := CheckFilePermissions(path, nil)
	if err == nil {
		t.Fatal("a 0644 configuration file was accepted")
	}

	var permErr *FilePermissionError
	if !errors.As(err, &permErr) {
		t.Fatalf("error is %T, want *FilePermissionError", err)
	}
	if permErr.Path != path {
		t.Errorf("error names %q, want %q", permErr.Path, path)
	}

	// The message has to be actionable: an operator should be able to paste the
	// fix out of it.
	message := err.Error()
	for _, want := range []string{"0644", "chmod 0600", path} {
		if !strings.Contains(message, want) {
			t.Errorf("error message does not contain %q: %s", want, message)
		}
	}
}

func TestCheckFilePermissionsAcceptsOwnerOnlyModes(t *testing.T) {
	if SkipPermissionCheck() {
		t.Skip("permission bits are not enforced on this platform")
	}

	for _, mode := range []fs.FileMode{0o600, 0o400} {
		path := writeFileWithMode(t, "doenerstag.yaml", mode)
		if err := CheckFilePermissions(path, nil); err != nil {
			t.Errorf("mode %04o was rejected: %v", mode, err)
		}
	}
}

// Group-readable is the mode a well-meaning operator reaches for when they want
// a service account to read the file. It still exposes the database password to
// everyone in that group, so it is refused.
func TestGroupReadableIsRejected(t *testing.T) {
	if SkipPermissionCheck() {
		t.Skip("permission bits are not enforced on this platform")
	}

	path := writeFileWithMode(t, "doenerstag.yaml", 0o640)
	if err := CheckFilePermissions(path, nil); err == nil {
		t.Error("a group-readable configuration file was accepted")
	}
}

func TestPermissionCheckIsSkippedOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("this asserts the Windows behaviour")
	}

	if !SkipPermissionCheck() {
		t.Error("SkipPermissionCheck() is false on Windows")
	}

	// A Go FileMode on Windows is synthesised from the read-only attribute
	// rather than read from an ACL, so enforcing it would reject correctly
	// secured files and accept insecure ones. The check must not fire.
	path := writeFileWithMode(t, "doenerstag.yaml", 0o666)
	if err := CheckFilePermissions(path, nil); err != nil {
		t.Errorf("the permission check fired on Windows: %v", err)
	}
}

func TestCheckFilePermissionsOnAMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.yaml")
	if err := CheckFilePermissions(path, nil); err == nil && !SkipPermissionCheck() {
		t.Error("a missing file produced no error")
	}
}

func writeFileWithMode(t *testing.T, name string, mode fs.FileMode) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("database:\n  server: localhost\n"), mode); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	// WriteFile applies the process umask, so set the mode explicitly.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %04o %s: %v", mode, path, err)
	}
	return path
}
