//go:build !windows

package daemon

import (
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
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

// ensureSingleInstance checks the PID file and terminates any still-running
// previous ghxd process before the new instance binds the socket. This
// prevents two daemons from fighting over the Unix socket, which would corrupt
// writes and cause ghx clients to silently bypass the cache.
func ensureSingleInstance(pidFile string) error {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		// No PID file or unreadable — nothing to do.
		return nil
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}

	// Signal 0 checks process existence without actually signalling it.
	if proc.Signal(syscall.Signal(0)) != nil {
		// Process is already dead — nothing to do.
		return nil
	}

	log.Printf("ghxd: previous instance found (PID %d), sending SIGTERM", pid)
	_ = proc.Signal(syscall.SIGTERM)

	// Give it up to 500 ms to exit cleanly.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		if proc.Signal(syscall.Signal(0)) != nil {
			return nil // exited cleanly
		}
	}

	// Still alive — force kill.
	log.Printf("ghxd: previous instance (PID %d) did not exit after SIGTERM, sending SIGKILL", pid)
	_ = proc.Signal(syscall.SIGKILL)
	time.Sleep(100 * time.Millisecond)

	return nil
}
