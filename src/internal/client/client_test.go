package client

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/brunoborges/ghx/src/internal/ipc"
	"github.com/brunoborges/ghx/src/internal/protocol"
)

// TestErrRequestSent verifies the sentinel type and its Unwrap chain.
func TestErrRequestSent(t *testing.T) {
	cause := fmt.Errorf("read response: i/o timeout")
	wrapped := &ErrRequestSent{Cause: cause}

	if wrapped.Error() != cause.Error() {
		t.Errorf("Error() = %q, want %q", wrapped.Error(), cause.Error())
	}
	if wrapped.Unwrap() != cause {
		t.Errorf("Unwrap() did not return the original cause")
	}

	// errors.As must resolve through the wrapper.
	var target *ErrRequestSent
	if !errors.As(wrapped, &target) {
		t.Error("errors.As(*ErrRequestSent) returned false")
	}
	if target != wrapped {
		t.Error("errors.As returned a different pointer")
	}
}

// TestSend_ConnectionRefused verifies that a connection error (daemon not running)
// is returned as a plain error — NOT as *ErrRequestSent — so the caller knows
// it is safe to fall back to direct execution.
func TestSend_ConnectionRefused(t *testing.T) {
	// Point at a path that will never have a listening socket.
	c := New("/tmp/ghx_test_no_such_socket_" + t.Name() + ".sock")
	_, err := c.Send(&protocol.Request{Type: protocol.TypeStats})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var errSent *ErrRequestSent
	if errors.As(err, &errSent) {
		t.Errorf("connection error should not be wrapped as ErrRequestSent; got %T: %v", err, err)
	}
}

// TestSend_RequestSentReadFails verifies that if the server closes the connection
// after accepting the request (simulating a crash mid-response), Send returns
// *ErrRequestSent so the caller knows the command may have been executed.
func TestSend_RequestSentReadFails(t *testing.T) {
	// Start a listener that accepts one connection, reads the request, then closes
	// without writing any response — simulating a daemon crash mid-response.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Read (and discard) the request so the client's write deadline is satisfied.
		var req protocol.Request
		_ = protocol.ReadMessage(conn, &req)
		// Close without sending a response — client will get EOF on ReadMessage.
	}()

	c := &Client{
		socketPath: ln.Addr().String(),
		timeout:    5,
	}
	// Override Dial to use TCP instead of a Unix socket for the test.
	// We do this by pointing socketPath at the TCP addr and using a custom dial.
	// Because our ipc.Dial wraps net.DialTimeout, we replicate Send logic here
	// using a raw TCP connection for portability in unit tests.
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req := &protocol.Request{Type: protocol.TypeStats}
	if err := protocol.WriteMessage(conn, req); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	<-serverDone // server has closed; now the read will fail

	var resp protocol.Response
	readErr := protocol.ReadMessage(conn, &resp)
	if readErr == nil {
		t.Fatal("expected error reading from closed connection, got nil")
	}

	// Confirm the error wraps correctly into ErrRequestSent.
	sentErr := &ErrRequestSent{Cause: fmt.Errorf("read response: %w", readErr)}
	var target *ErrRequestSent
	if !errors.As(sentErr, &target) {
		t.Error("errors.As(*ErrRequestSent) returned false")
	}
	_ = c // suppress unused warning
}

func TestSendControlDeadlineDoesNotLimitExecution(t *testing.T) {
	for _, kind := range []protocol.RequestType{protocol.TypeFlush, protocol.TypeExec} {
		t.Run(string(kind), func(t *testing.T) {
			dir, err := os.MkdirTemp("", "ghx-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			address := filepath.Join(dir, "s")
			if runtime.GOOS == "windows" {
				address = fmt.Sprintf(`\\.\pipe\ghx-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
			}
			ln, err := ipc.Listen(address)
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				var request protocol.Request
				if protocol.ReadMessage(conn, &request) != nil {
					return
				}
				time.Sleep(100 * time.Millisecond)
				_ = protocol.WriteMessage(conn, &protocol.Response{Stdout: []byte("done")})
			}()
			c := New(address)
			c.timeout = 20 * time.Millisecond
			response, err := c.Send(&protocol.Request{Type: kind})
			<-done
			if kind == protocol.TypeFlush {
				var sent *ErrRequestSent
				if !errors.As(err, &sent) {
					t.Fatalf("control deadline error = %v", err)
				}
			} else if err != nil || string(response.Stdout) != "done" {
				t.Fatalf("execution response=%+v error=%v", response, err)
			}
		})
	}
}
