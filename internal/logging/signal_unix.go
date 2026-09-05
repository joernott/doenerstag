//go:build !windows

package logging

import (
	"os"
	"syscall"
)

// reopenSignals returns the signals that cause the log file to be reopened.
// SIGHUP is what logrotate's postrotate script sends.
func reopenSignals() []os.Signal {
	return []os.Signal{syscall.SIGHUP}
}
