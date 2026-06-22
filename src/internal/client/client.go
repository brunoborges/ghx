package client

import (
	"fmt"
	"time"

	"github.com/brunoborges/ghx/src/internal/ipc"
	"github.com/brunoborges/ghx/src/internal/protocol"
)

// ErrRequestSent is returned by Send when the request was successfully delivered
// to the daemon but reading the response failed (e.g. due to a connection reset
// or unexpected EOF). Because the daemon may have already executed the command,
// the caller must NOT re-execute it — doing so could cause silent double-execution
// and duplicate API side-effects.
type ErrRequestSent struct {
	Cause error
}

func (e *ErrRequestSent) Error() string {
	return e.Cause.Error()
}

func (e *ErrRequestSent) Unwrap() error {
	return e.Cause
}

// Client communicates with the ghxd daemon over IPC (Unix socket or Windows named pipe).
type Client struct {
	socketPath string
	timeout    time.Duration
}

// New creates a client that connects to the daemon at the given socket path.
func New(socketPath string) *Client {
	return &Client{
		socketPath: socketPath,
		timeout:    5 * time.Second,
	}
}

// Send sends a request to the daemon and returns the response.
//
// Deadline behaviour:
//   - A short write deadline covers sending the request (guards against a hung daemon).
//   - The deadline is cleared before reading the response so that long-running
//     commands (multi-minute GraphQL paginated queries, large JSON downloads, …)
//     are never aborted mid-flight by a fixed socket timeout.
//
// If the request was sent but reading the response fails, Send returns
// *ErrRequestSent so the caller can distinguish "safe to retry" (connection
// error) from "do not retry" (daemon may have already executed the command).
func (c *Client) Send(req *protocol.Request) (*protocol.Response, error) {
	conn, err := ipc.Dial(c.socketPath, c.timeout)
	if err != nil {
		return nil, fmt.Errorf("connect to daemon: %w", err)
	}
	defer conn.Close()

	// Short deadline covers only the request write — guarding against a stuck daemon.
	conn.SetWriteDeadline(time.Now().Add(30 * time.Second))

	if err := protocol.WriteMessage(conn, req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	// Remove all deadlines: the wrapped gh command may run for several minutes
	// and return a large payload. A fixed timeout here causes the client to give
	// up and fall back to direct execution, silently double-spending API quota.
	conn.SetDeadline(time.Time{})

	var resp protocol.Response
	if err := protocol.ReadMessage(conn, &resp); err != nil {
		// The daemon received the request and may have already executed the command.
		// Wrap the error so the caller knows it must NOT fall back to re-execution.
		return nil, &ErrRequestSent{Cause: fmt.Errorf("read response: %w", err)}
	}

	return &resp, nil
}

// IsRunning checks if the daemon is listening on its socket.
func (c *Client) IsRunning() bool {
	conn, err := ipc.Dial(c.socketPath, 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
