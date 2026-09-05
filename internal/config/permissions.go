package config

import (
	"fmt"
	"io/fs"
	"os"
	"runtime"
)

// Accepted modes for the configuration file.
//
// 0600 is what install creates. 0400 is accepted alongside it because a
// read-only configuration file is strictly safer, and refusing it would push
// operators towards loosening permissions in order to satisfy a permissions
// check.
const (
	ConfigFileMode         fs.FileMode = 0o600
	ConfigFileReadOnlyMode fs.FileMode = 0o400
)

// FilePermissionError reports a configuration file whose permissions allow
// someone other than its owner to read it.
//
// This is fatal rather than a warning. The file holds the database password and
// the session signing secret. A warning about that scrolls past in a log nobody
// reads and the file stays world-readable for years; a startup failure is fixed
// in the time it takes to type chmod.
type FilePermissionError struct {
	Path string
	Mode fs.FileMode
}

func (e *FilePermissionError) Error() string {
	return fmt.Sprintf(
		"configuration file %s has mode %04o, which allows users other than its "+
			"owner to read it.\nIt holds the database password and the session "+
			"signing secret, so this is refused.\nFix it with: chmod %04o %s",
		e.Path, e.Mode.Perm(), ConfigFileMode, e.Path)
}

// CheckFilePermissions verifies that the configuration file is readable only by
// its owner. Passing a non-nil info avoids a second stat when the caller has
// already made one.
//
// The check applies to the POSIX permission bits and is therefore skipped on
// Windows, where they do not carry the same meaning: a Go FileMode there is
// synthesised from the read-only attribute rather than read from an ACL, so
// enforcing it would reject correctly secured files and accept insecure ones.
func CheckFilePermissions(path string, info fs.FileInfo) error {
	if runtime.GOOS == "windows" {
		return nil
	}

	if info == nil {
		var err error
		info, err = os.Stat(path)
		if err != nil {
			return fmt.Errorf("checking permissions of %s: %w", path, err)
		}
	}

	if !AcceptableFileMode(info.Mode()) {
		return &FilePermissionError{Path: path, Mode: info.Mode()}
	}
	return nil
}

// AcceptableFileMode reports whether a mode is permitted for the configuration
// file: no permission at all for group or other, and no execute bit for the
// owner.
//
// Expressed as a mask rather than as equality with 0600 and 0400 so that the
// intent is visible: what matters is that nobody but the owner can read it.
func AcceptableFileMode(mode fs.FileMode) bool {
	perm := mode.Perm()
	if perm&0o077 != 0 {
		return false // readable, writable or executable by group or other
	}
	if perm&0o100 != 0 {
		return false // a configuration file has no business being executable
	}
	return perm != 0 // mode 0000 is unreadable even by its owner
}

// SkipPermissionCheck reports whether the permission check is inactive on this
// platform. Callers log a DEBUG line saying so, and tests use it to decide
// what to assert.
func SkipPermissionCheck() bool {
	return runtime.GOOS == "windows"
}
