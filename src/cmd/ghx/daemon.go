package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/brunoborges/ghx/src/internal/client"
	"github.com/brunoborges/ghx/src/internal/config"
	"github.com/brunoborges/ghx/src/internal/ipc"
	"github.com/brunoborges/ghx/src/internal/protocol"
)

const (
	daemonStartTimeout = 5 * time.Second
	daemonStopTimeout  = 45 * time.Second
	daemonPollInterval = 50 * time.Millisecond
)

func restartDaemon(cfg *config.Config) error {
	cl := client.New(cfg.SocketPath)
	if cl.IsRunning() {
		resp, err := cl.Send(&protocol.Request{Type: protocol.TypeShutdown})
		if err != nil {
			return fmt.Errorf("stop daemon: %w", err)
		}
		if resp.Error != "" || resp.ExitCode != 0 {
			return fmt.Errorf("stop daemon: exit code %d: %s", resp.ExitCode, resp.Error)
		}
		os.Stdout.Write(resp.Stdout)
	}
	if err := waitForDaemonStop(cfg.SocketPath, daemonStopTimeout); err != nil {
		return err
	}
	return startDaemon(cfg)
}

func waitForDaemonStop(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	cl := client.New(path)
	for {
		owned, err := daemonOwnsSocket(path)
		if err != nil {
			return fmt.Errorf("check daemon ownership: %w", err)
		}
		// The listener also covers older daemons that do not hold a lock.
		if !owned && !cl.IsRunning() {
			return nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("daemon did not stop within %s (requests may still be draining)", timeout)
		}
		time.Sleep(min(daemonPollInterval, remaining))
	}
}

// startDaemon launches a detached daemon and confirms it can serve requests.
func startDaemon(cfg *config.Config) error {
	path, err := findGHXD()
	if err != nil {
		return err
	}
	return startDaemonCommand(cfg, exec.Command(path), daemonStartTimeout)
}

func startDaemonCommand(cfg *config.Config, cmd *exec.Cmd, timeout time.Duration) error {
	configureDetached(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch daemon: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	deadline := time.Now().Add(timeout)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(daemonPollInterval)
	defer ticker.Stop()
	var readinessErr error
	for {
		remaining := time.Until(deadline)
		if remaining > 0 {
			readinessErr = daemonReady(cfg.SocketPath, min(100*time.Millisecond, remaining))
			if readinessErr == nil {
				select {
				case err := <-exited:
					return daemonExitError(err)
				default:
					return nil
				}
			}
		}
		select {
		case err := <-exited:
			return daemonExitError(err)
		case <-timer.C:
			if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				return fmt.Errorf("daemon startup timed out; terminate launched daemon: %w", err)
			}
			<-exited
			return fmt.Errorf("daemon did not become ready within %s: %w", timeout, readinessErr)
		case <-ticker.C:
		}
	}
}

func daemonExitError(err error) error {
	if err != nil {
		return fmt.Errorf("daemon exited before becoming ready: %w", err)
	}
	return fmt.Errorf("daemon exited before becoming ready")
}

func daemonReady(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	conn, err := ipc.Dial(path, timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	if err := protocol.WriteMessage(conn, &protocol.Request{Type: protocol.TypeStats}); err != nil {
		return err
	}
	var resp protocol.Response
	if err := protocol.ReadMessage(conn, &resp); err != nil {
		return err
	}
	if resp.Error != "" || resp.ExitCode != 0 {
		return fmt.Errorf("daemon readiness: exit code %d: %s", resp.ExitCode, resp.Error)
	}
	return nil
}
