//go:build !windows

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

// TestEnsureSingleInstance_NoPIDFile verifies that ensureSingleInstance is a
// no-op when the PID file does not exist.
func TestEnsureSingleInstance_NoPIDFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ghxd.pid")
	if err := ensureSingleInstance(path); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// TestEnsureSingleInstance_DeadPID verifies that ensureSingleInstance is a
// no-op when the PID file contains a PID that is no longer alive.
func TestEnsureSingleInstance_DeadPID(t *testing.T) {
	// PID 1 is always alive (init/systemd) on Linux, but we need a PID that is no longer alive.
	// Use the PID of a short-lived subprocess that has already exited.
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "ghxd.pid")

	// Start a process, capture its PID, then wait for it to exit.
	cmd := exec.Command("true") // exits immediately
	if err := cmd.Run(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	deadPID := cmd.ProcessState.Pid()

	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", deadPID)), 0600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	if err := ensureSingleInstance(pidFile); err != nil {
		t.Fatalf("expected no error for dead PID, got: %v", err)
	}
}

// TestEnsureSingleInstance_KillsLiveProcess verifies that ensureSingleInstance
// terminates a still-running process whose PID is recorded in the PID file.
func TestEnsureSingleInstance_KillsLiveProcess(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "ghxd.pid")

	// Start a long-running subprocess that we can detect and kill.
	cmd := exec.Command("sleep", "300")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start subprocess: %v", err)
	}
	pid := cmd.Process.Pid

	// Write PID file as ghxd would.
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)+"\n"), 0600); err != nil {
		cmd.Process.Kill()
		t.Fatalf("write pid file: %v", err)
	}

	// Ensure the subprocess is cleaned up regardless of test outcome.
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })

	if err := ensureSingleInstance(pidFile); err != nil {
		t.Fatalf("ensureSingleInstance returned error: %v", err)
	}

	// Reap the subprocess so Signal(0) reflects the true exit state.
	cmd.Wait()

	// After ensureSingleInstance returns and the zombie is reaped, the process
	// must no longer be alive.
	if err := cmd.Process.Signal(syscall.Signal(0)); err == nil {
		t.Errorf("process PID %d is still alive after ensureSingleInstance", pid)
	}
}
