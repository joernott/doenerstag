//go:build unix

package config

import (
	"os"
	"syscall"
)

// keepOwner gives file the owner and group existing has.
//
// update is run as root (docs/10_operations.md), and a file root writes is
// root's: the configuration it rewrote was then unreadable to the service user
// it belongs to, and the server refused to start (20.32). Called on the
// temporary file before it is renamed into place, so there is never a moment
// when the configuration is root's.
func keepOwner(existing os.FileInfo, file string) error {
	stat, ok := existing.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	return os.Chown(file, int(stat.Uid), int(stat.Gid))
}
