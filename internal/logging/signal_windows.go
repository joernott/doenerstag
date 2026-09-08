//go:build windows

package logging

import "os"

// reopenSignals returns no signals on Windows: there is no SIGHUP, and log
// rotation there is not handled by signalling the process. Logging to stdout
// and letting the service manager collect it is the supported arrangement.
func reopenSignals() []os.Signal {
	return nil
}
