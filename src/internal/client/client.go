package client

import (
	"fmt"
	"time"

	"github.com/brunoborges/ghx/src/internal/ipc"
	"github.com/brunoborges/ghx/src/internal/protocol"
)

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
func (c *Client) Send(req *protocol.Request) (*protocol.Response, error) {
	conn, err := ipc.Dial(c.socketPath, c.timeout)
	if err != nil {
		return nil, fmt.Errorf("connect to daemon: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(60 * time.Second))

	if err := protocol.WriteMessage(conn, req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	var resp protocol.Response
	if err := protocol.ReadMessage(conn, &resp); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
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
