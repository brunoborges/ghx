//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func configureDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// A closed listener does not imply that in-flight requests have drained.
func daemonOwnsSocket(path string) (bool, error) {
	lock, err := os.OpenFile(path+".lock", os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

// execDirect runs gh directly, replacing the current process (bypass daemon entirely).
func execDirect(ghPath string, args []string) {
	path, err := exec.LookPath(ghPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ghx: gh not found: %v\n", err)
		os.Exit(1)
	}
	allArgs := append([]string{ghPath}, args...)
	syscall.Exec(path, allArgs, os.Environ())
}

// execReplace replaces the current process with the given binary.
func execReplace(path string, argv []string, env []string) {
	syscall.Exec(path, argv, env)
}
