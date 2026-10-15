//go:build !unix

package config

import "os"

// keepOwner has nothing to keep where files have no Unix owner and group.
func keepOwner(os.FileInfo, string) error { return nil }
