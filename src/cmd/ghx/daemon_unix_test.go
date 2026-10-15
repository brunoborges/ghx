//go:build !windows

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/brunoborges/ghx/src/internal/client"
	"github.com/brunoborges/ghx/src/internal/config"
	"github.com/brunoborges/ghx/src/internal/daemon"
	"github.com/brunoborges/ghx/src/internal/ipc"
	"github.com/brunoborges/ghx/src/internal/protocol"
)

func daemonFixture(t *testing.T) *config.Config {
	t.Helper()
	dir, err := os.MkdirTemp("", "ghx-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	cfg := config.DefaultConfig()
	cfg.SocketPath = filepath.Join(dir, "s")
	cfg.PIDFile = filepath.Join(dir, "pid")
	cfg.DashboardPort = 0
	return cfg
}

// The test executable is a secretless replacement daemon and gh subprocess.
func TestDaemonLifecycleProcess(t *testing.T) {
	path := os.Getenv("GHX_TEST_DAEMON_SOCKET")
	if path == "" {
		return
	}
	switch os.Getenv("GHX_TEST_DAEMON_MODE") {
	case "exit failure":
		os.Exit(7)
	case "exit success":
		os.Exit(0)
	case "never ready":
		time.Sleep(time.Hour)
		return
	case "listener only":
		ln, err := ipc.Listen(path)
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		for {
			conn, err := ln.Accept()
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, conn)
			conn.Close()
		}
	case "delayed ready":
		time.Sleep(200 * time.Millisecond)
	}
	cfg := config.DefaultConfig()
	cfg.SocketPath = path
	cfg.PIDFile = filepath.Join(filepath.Dir(path), "pid")
	cfg.DashboardPort = 0
	if err := daemon.NewServer(cfg, "replacement", os.Args[0]).Run(); err != nil {
		t.Fatal(err)
	}
}

func TestDaemonDrainProcess(t *testing.T) {
	dir := os.Getenv("GHX_TEST_DRAIN_DIR")
	if dir == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "executing"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		_, err := os.Stat(filepath.Join(dir, "release"))
		if err == nil {
			fmt.Println("drained")
			os.Exit(0)
		}
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("command was not released")
}

func TestWaitForDaemonStopRetainsOwnership(t *testing.T) {
	cfg := daemonFixture(t)
	lock, err := os.OpenFile(cfg.SocketPath+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err := waitForDaemonStop(cfg.SocketPath, 100*time.Millisecond); err == nil {
		t.Fatal("reported shutdown while the lock was held without a listener")
	}
	lock.Close()
	if err := waitForDaemonStop(cfg.SocketPath, time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfg.SocketPath + ".lock"); err != nil {
		t.Fatalf("persistent lock file removed: %v", err)
	}
}

func TestWaitForDaemonStopLegacyListener(t *testing.T) {
	cfg := daemonFixture(t)
	ln, err := ipc.Listen(cfg.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := waitForDaemonStop(cfg.SocketPath, 100*time.Millisecond); err == nil {
		t.Fatal("reported shutdown while an older daemon was listening")
	}
	ln.Close()
	if err := waitForDaemonStop(cfg.SocketPath, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForDaemonStopOwnershipError(t *testing.T) {
	cfg := daemonFixture(t)
	if err := os.Mkdir(cfg.SocketPath+".lock", 0700); err != nil {
		t.Fatal(err)
	}
	if err := waitForDaemonStop(cfg.SocketPath, time.Second); err == nil ||
		!strings.Contains(err.Error(), "check daemon ownership:") {
		t.Fatalf("ownership error not surfaced: %v", err)
	}
}

func TestStartDaemonConfirmsReadiness(t *testing.T) {
	for _, mode := range []string{"ready", "delayed ready", "exit failure", "exit success", "never ready", "listener only"} {
		t.Run(mode, func(t *testing.T) {
			cfg := daemonFixture(t)
			cmd := exec.Command(os.Args[0], "-test.run=^TestDaemonLifecycleProcess$")
			cmd.Env = append(os.Environ(), "GHX_TEST_DAEMON_SOCKET="+cfg.SocketPath, "GHX_TEST_DAEMON_MODE="+mode)
			timeout := 300 * time.Millisecond
			wantReady := mode == "ready" || mode == "delayed ready"
			if wantReady {
				timeout = 5 * time.Second
			} else if strings.HasPrefix(mode, "exit") {
				timeout = 2 * time.Second
			}
			start := time.Now()
			err := startDaemonCommand(cfg, cmd, timeout)
			if wantReady {
				t.Cleanup(func() {
					if _, err := client.New(cfg.SocketPath).Send(&protocol.Request{Type: protocol.TypeShutdown}); err != nil {
						t.Error(err)
						cmd.Process.Kill()
					}
					if err := waitForDaemonStop(cfg.SocketPath, 5*time.Second); err != nil {
						t.Error(err)
					}
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := daemonReady(cfg.SocketPath, time.Second); err != nil {
					t.Fatalf("start returned before readiness: %v", err)
				}
			} else {
				if err == nil {
					cmd.Process.Kill()
					t.Fatal("start reported success without a responsive daemon")
				}
				if strings.HasPrefix(mode, "exit") && !strings.Contains(err.Error(), "exited before becoming ready") {
					t.Fatalf("expected child exit error, got: %v", err)
				}
				if mode == "never ready" || mode == "listener only" {
					if !strings.Contains(err.Error(), "did not become ready") {
						t.Fatalf("expected readiness timeout, got: %v", err)
					}
					if cmd.ProcessState == nil {
						t.Fatal("timed-out child was not reaped")
					}
					if elapsed := time.Since(start); elapsed > 2*time.Second {
						t.Fatalf("startup timeout was not bounded: %s", elapsed)
					}
				}
			}
		})
	}
}

func waitForFixture(t *testing.T, ready func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("fixture did not become ready")
}

func installDaemonFixture(t *testing.T, cfg *config.Config, mode string) {
	t.Helper()
	dir := filepath.Dir(cfg.SocketPath)
	t.Setenv("GHX_TEST_DAEMON_SOCKET", cfg.SocketPath)
	t.Setenv("GHX_TEST_DAEMON_MODE", mode)
	// findGHXD must reach only this fixture, never an installed user daemon.
	binary := "'" + strings.ReplaceAll(os.Args[0], "'", "'\\''") + "'"
	script := "#!/bin/sh\nexec " + binary + " -test.run=^TestDaemonLifecycleProcess$\n"
	if err := os.WriteFile(filepath.Join(dir, "ghxd"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if path, err := findGHXD(); err != nil || path != filepath.Join(dir, "ghxd") {
		t.Fatalf("replacement fixture was not selected: path=%q err=%v", path, err)
	}
}

func TestEnsureDaemonAcceptsConcurrentStarter(t *testing.T) {
	cfg := daemonFixture(t)
	installDaemonFixture(t, cfg, "exit failure")
	srv := daemon.NewServer(cfg, "concurrent starter", os.Args[0])
	finished := make(chan error, 1)
	go func() {
		time.Sleep(200 * time.Millisecond)
		finished <- srv.Run()
	}()
	t.Cleanup(func() {
		srv.Shutdown()
		select {
		case err := <-finished:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("concurrent daemon did not stop")
		}
	})
	ready, err := ensureDaemon(cfg, client.New(cfg.SocketPath))
	if !ready || err != nil {
		t.Fatalf("did not attach to concurrent starter: ready=%v err=%v", ready, err)
	}
}

func TestRestartDaemonWaitsForCommandDrain(t *testing.T) {
	cfg := daemonFixture(t)
	dir := filepath.Dir(cfg.SocketPath)
	t.Setenv("GHX_TEST_DRAIN_DIR", dir)
	installDaemonFixture(t, cfg, "ready")

	srv := daemon.NewServer(cfg, "original", os.Args[0])
	finished := make(chan error, 1)
	go func() { finished <- srv.Run() }()
	t.Cleanup(func() {
		if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
			t.Error(err)
		}
		srv.Shutdown()
		if client.New(cfg.SocketPath).IsRunning() {
			if _, err := client.New(cfg.SocketPath).Send(&protocol.Request{Type: protocol.TypeShutdown}); err != nil {
				t.Error(err)
			}
		}
		if err := waitForDaemonStop(cfg.SocketPath, 5*time.Second); err != nil {
			t.Error(err)
		}
	})
	waitForFixture(t, func() bool { return daemonReady(cfg.SocketPath, 100*time.Millisecond) == nil })

	requestDone := make(chan error, 1)
	go func() {
		resp, err := client.New(cfg.SocketPath).Send(&protocol.Request{
			Type: protocol.TypeExec, Args: []string{"-test.run=^TestDaemonDrainProcess$"},
		})
		if err == nil && (resp.ExitCode != 0 || string(resp.Stdout) != "drained\n") {
			err = fmt.Errorf("drained response: %+v", resp)
		}
		requestDone <- err
	}()
	waitForFixture(t, func() bool {
		_, err := os.Stat(filepath.Join(dir, "executing"))
		return err == nil
	})
	restarted := make(chan error, 1)
	go func() { restarted <- restartDaemon(cfg) }()
	waitForFixture(t, func() bool { return !client.New(cfg.SocketPath).IsRunning() })
	select {
	case err := <-restarted:
		t.Fatalf("restart returned before command drain: %v", err)
	case <-time.After(700 * time.Millisecond):
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, result := range []<-chan error{requestDone, finished, restarted} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("restart did not complete after command drain")
		}
	}
	if err := daemonReady(cfg.SocketPath, time.Second); err != nil {
		t.Fatalf("replacement not responsive: %v", err)
	}
	pid, err := os.ReadFile(cfg.PIDFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(pid) == strconv.Itoa(os.Getpid()) {
		t.Fatal("restart did not replace the original daemon")
	}
}
