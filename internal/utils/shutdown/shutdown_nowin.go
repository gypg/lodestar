//go:build !windows

package shutdown

import "os"

func setupPlatformSignals(_ chan<- os.Signal) {
	// On non-Windows platforms, signal.Notify handles SIGINT/SIGTERM/SIGHUP.
}
