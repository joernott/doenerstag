//go:build unix

package config

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// 20.32: rewriting the configuration keeps the file's owner and group. Root
// running update used to leave it root's, and the service could not read it.
//
// A test cannot become root, but anybody may give a file they own to a group
// they belong to, so the group is what this changes and checks.
func TestRewritingTheConfigurationKeepsItsGroup(t *testing.T) {
	groups, err := os.Getgroups()
	if err != nil {
		t.Skipf("cannot list this user's groups: %v", err)
	}
	other := -1
	for _, g := range groups {
		if g != os.Getgid() {
			other = g
			break
		}
	}
	if other < 0 {
		t.Skip("this user belongs to no group but its own")
	}

	path := filepath.Join(t.TempDir(), "doenerstag.yaml")
	if err := os.WriteFile(path, []byte("log:\n  level: INFO\n"), ConfigFileMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, -1, other); err != nil {
		t.Skipf("cannot give the file to group %d: %v", other, err)
	}

	if err := WriteFile(path, "log:\n  level: DEBUG\n"); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if gid := int(info.Sys().(*syscall.Stat_t).Gid); gid != other {
		t.Errorf("the rewritten file belongs to group %d, want %d as before", gid, other)
	}
}
