//go:build !windows

package daemon

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/brunoborges/ghx/src/internal/config"
	"github.com/brunoborges/ghx/src/internal/dashboard"
	"github.com/brunoborges/ghx/src/internal/protocol"
)

func socketTempDir(t *testing.T) string {
	t.Helper()
	// t.TempDir includes the test name, which can exceed Unix socket path limits.
	dir, err := os.MkdirTemp("", "ghxd-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove socket directory: %v", err)
		}
	})
	return dir
}

// Run the real kernel lock in a separate process; no in-memory substitute.
func TestInstanceLockProcess(t *testing.T) {
	path := os.Getenv("GHX_TEST_LOCK_PATH")
	if path == "" {
		return
	}
	lock, err := acquireInstanceLock(path)
	if err != nil {
		os.Exit(3)
	}
	fmt.Println("locked")
	io.Copy(io.Discard, os.Stdin)
	lock.Close()
	os.Exit(0)
}

func TestInstanceLockAcrossProcesses(t *testing.T) {
	path := filepath.Join(socketTempDir(t), "ghxd.sock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestInstanceLockProcess$")
	cmd.Env = append(os.Environ(), "GHX_TEST_LOCK_PATH="+path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	ready := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		if err == nil && line != "locked\n" {
			err = fmt.Errorf("unexpected readiness: %q", line)
		}
		ready <- err
	}()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lock holder did not become ready")
	}
	if lock, err := acquireInstanceLock(path); err == nil {
		lock.Close()
		t.Fatal("second process acquired an owned socket")
	}
	// Process death releases the lock without deleting the persistent inode.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	stdin.Close()
	if err := cmd.Wait(); err == nil {
		t.Fatal("expected killed holder to exit unsuccessfully")
	}
	lock, err := acquireInstanceLock(path)
	if err != nil {
		t.Fatalf("lock not released on exit: %v", err)
	}
	defer lock.Close()
	if _, err := os.Stat(path + ".lock"); err != nil {
		t.Fatal(err)
	}
}

func TestStaleSocketCleanup(t *testing.T) {
	path := filepath.Join(socketTempDir(t), "ghxd.sock")
	if err := removeStaleSocket(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}
	if err := removeStaleSocket(path); err == nil {
		t.Fatal("removed a regular file")
	}
	os.Remove(path)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := removeStaleSocket(path); err == nil {
		t.Fatal("removed a live socket")
	}
	ln.SetUnlinkOnClose(false)
	ln.Close()
	if err := removeStaleSocket(path); err != nil {
		t.Fatalf("stale socket: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("stale socket still exists")
	}
}

func TestServerOwnershipAndConcurrentShutdown(t *testing.T) {
	// A stale PID may now belong to an unrelated live process.
	other := exec.Command("sleep", "30")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { other.Process.Kill(); other.Wait() }()
	cfg := config.DefaultConfig()
	dir := socketTempDir(t)
	cfg.SocketPath = filepath.Join(dir, "ghxd.sock")
	cfg.PIDFile = filepath.Join(dir, "ghxd.pid")
	cfg.DashboardPort = 0
	if err := os.WriteFile(cfg.PIDFile, []byte(strconv.Itoa(other.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewServer(cfg, "test", os.Args[0])
	finished := make(chan error, 1)
	go func() { finished <- s.Run() }()
	defer s.Shutdown()
	var conn net.Conn
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		conn, err = net.DialTimeout("unix", cfg.SocketPath, 50*time.Millisecond)
		if err == nil {
			break
		}
		select {
		case err := <-finished:
			t.Fatalf("startup: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if conn == nil {
		t.Fatal("daemon did not become ready")
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := protocol.WriteMessage(conn, &protocol.Request{Type: protocol.TypeStats}); err != nil {
		t.Fatal(err)
	}
	var response protocol.Response
	if err := protocol.ReadMessage(conn, &response); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if err := other.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("startup signalled unrelated PID: %v", err)
	}
	secondCfg := *cfg
	secondCfg.PIDFile = filepath.Join(dir, "other.pid")
	if err := NewServer(&secondCfg, "test", os.Args[0]).Run(); err == nil {
		t.Fatal("second server acquired same socket with different PID file")
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(s.Shutdown)
	}
	wg.Wait()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	for _, path := range []string{cfg.SocketPath, cfg.PIDFile} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("shutdown retained %s", path)
		}
	}
	lock, err := acquireInstanceLock(cfg.SocketPath)
	if err != nil {
		t.Fatalf("shutdown retained lock: %v", err)
	}
	lock.Close()
}

func TestStartupFailureReleasesSocketAndLock(t *testing.T) {
	cfg := config.DefaultConfig()
	dir := socketTempDir(t)
	cfg.SocketPath = filepath.Join(dir, "ghxd.sock")
	cfg.PIDFile = filepath.Join(dir, "not-directory", "ghxd.pid")
	cfg.DashboardPort = 0
	if err := os.WriteFile(filepath.Dir(cfg.PIDFile), []byte{}, 0600); err != nil {
		t.Fatal(err)
	}
	if err := NewServer(cfg, "test", os.Args[0]).Run(); err == nil {
		t.Fatal("expected PID write failure")
	}
	if _, err := os.Stat(cfg.SocketPath); !os.IsNotExist(err) {
		t.Fatal("failed startup retained socket")
	}
	lock, err := acquireInstanceLock(cfg.SocketPath)
	if err != nil {
		t.Fatalf("failed startup retained lock: %v", err)
	}
	lock.Close()
}

func TestOwnershipRetainedUntilHTTPDrain(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	dash := dashboard.Handler()
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		dash(w, r)
	}))
	defer func() { unblock(); httpServer.Close() }()
	go func() {
		resp, err := http.Get(httpServer.URL)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP request did not start")
	}
	cfg := config.DefaultConfig()
	dir := socketTempDir(t)
	cfg.SocketPath = filepath.Join(dir, "ghxd.sock")
	cfg.PIDFile = filepath.Join(dir, "ghxd.pid")
	cfg.DashboardPort = 0
	s := NewServer(cfg, "test", os.Args[0])
	s.httpSrv = httpServer.Config
	finished := make(chan error, 1)
	go func() { finished <- s.Run() }()
	deadline := time.Now().Add(5 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", cfg.SocketPath, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("daemon did not become ready")
	}
	defer s.Shutdown()
	go s.Shutdown()
	<-s.done
	select {
	case err := <-finished:
		t.Fatalf("Run returned before HTTP drained: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if lock, err := acquireInstanceLock(cfg.SocketPath); err == nil {
		lock.Close()
		t.Fatal("daemon released ownership before HTTP drained")
	}
	unblock()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not finish after HTTP drained")
	}
}
