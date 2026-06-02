//go:build !windows

package daemon

import (
	"os"
	"os/signal"
	"syscall"
)

// removeStaleSocket removes a leftover socket file from a previous run.
func removeStaleSocket(path string) {
	os.Remove(path)
}

// setSocketPermissions restricts socket file access to the owner.
func setSocketPermissions(path string) error {
	return os.Chmod(path, 0600)
}

// notifyShutdownSignals registers platform-appropriate signals for graceful shutdown.
func notifyShutdownSignals(ch chan<- os.Signal) {
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
}
