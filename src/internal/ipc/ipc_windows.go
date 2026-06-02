//go:build windows

package ipc

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// Listen creates a platform-specific IPC listener.
// On Windows, this creates a named pipe.
func Listen(addr string) (net.Listener, error) {
	return winio.ListenPipe(addr, nil)
}

// Dial connects to a platform-specific IPC endpoint.
// On Windows, this connects to a named pipe.
func Dial(addr string, timeout time.Duration) (net.Conn, error) {
	return winio.DialPipe(addr, &timeout)
}
