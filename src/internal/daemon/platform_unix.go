//go:build !windows

package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// removeStaleSocket refuses live or ambiguous endpoints, including older daemons
// that do not acquire the instance lock. Only an unbound Unix socket is stale.
func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to remove non-socket %s", path)
	}
	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err == nil {
		conn.Close()
		return fmt.Errorf("daemon already listening at %s (stop with: ghx xdaemon stop)", path)
	}
	if !errors.Is(err, syscall.ECONNREFUSED) && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cannot determine socket ownership: %w", err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// setSocketPermissions restricts socket file access to the owner.
func setSocketPermissions(path string) error {
	return os.Chmod(path, 0600)
}

// notifyShutdownSignals registers platform-appropriate signals for graceful shutdown.
func notifyShutdownSignals(ch chan<- os.Signal) {
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
}

// acquireInstanceLock atomically claims a socket for the daemon's lifetime.
// The file must never be unlinked: contenders must lock the same inode. PID
// files are informational and must never authorize signalling another process.
func acquireInstanceLock(socketPath string) (*os.File, error) {
	f, err := os.OpenFile(socketPath+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("cannot claim daemon socket %s (stop with: ghx xdaemon stop): %w", socketPath, err)
	}
	return f, nil
}
