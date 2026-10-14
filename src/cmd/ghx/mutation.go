package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/brunoborges/ghx/src/internal/protocol"
)

// runMutation keeps stdin and terminal interaction in the invoking process.
// Only a cache invalidation request is sent after gh exits, including failures
// that may have partially changed remote state. Mutations are never replayed.
func runMutation(cmd *exec.Cmd, flush *protocol.Request, send func(*protocol.Request) (*protocol.Response, error)) int {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(cmd.Stderr, "ghx: %v\n", err)
		return 1
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var runErr error
wait:
	for {
		select {
		case sig := <-signals:
			// Keep ghx alive to invalidate partial writes when gh is interrupted.
			_ = cmd.Process.Signal(sig)
		case runErr = <-done:
			break wait
		}
	}

	exitCode := 0
	if runErr != nil {
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				exitCode = 128 + int(status.Signal())
			}
		} else {
			fmt.Fprintf(cmd.Stderr, "ghx: %v\n", runErr)
		}
	}

	resp, err := send(flush)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
		return exitCode // No daemon is listening, so no cache needs invalidation.
	}
	if err == nil && resp != nil && (resp.Error != "" || resp.ExitCode != 0) {
		err = fmt.Errorf("daemon rejected invalidation: %s (exit %d)", resp.Error, resp.ExitCode)
	}
	if err != nil {
		fmt.Fprintf(cmd.Stderr, "ghx: warning: cache invalidation failed: %v; run 'ghx xcache flush' before cached reads\n", err)
	}
	return exitCode
}
