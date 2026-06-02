//go:build windows

package daemon

import (
	"os"
	"os/signal"
)

// removeStaleSocket is a no-op on Windows; named pipes are managed by the OS.
func removeStaleSocket(_ string) {}

// setSocketPermissions is a no-op on Windows; named pipes use security descriptors.
func setSocketPermissions(_ string) error { return nil }

// notifyShutdownSignals registers platform-appropriate signals for graceful shutdown.
// On Windows, only os.Interrupt is supported. The shutdown IPC command can also be used.
func notifyShutdownSignals(ch chan<- os.Signal) {
	signal.Notify(ch, os.Interrupt)
}
