//go:build !windows

package ipc

import (
	"net"
	"time"
)

// Listen creates a platform-specific IPC listener.
// On Unix, this creates a Unix domain socket.
func Listen(addr string) (net.Listener, error) {
	return net.Listen("unix", addr)
}

// Dial connects to a platform-specific IPC endpoint.
// On Unix, this connects to a Unix domain socket.
func Dial(addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", addr, timeout)
}
