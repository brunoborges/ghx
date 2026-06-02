package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	execctx "github.com/brunoborges/ghx/src/internal/context"
)

// Request is sent from the ghx client to the ghxd daemon.
type Request struct {
	// Args are the gh command arguments (everything after "gh").
	Args []string `json:"args"`

	// Context is the resolved execution context.
	Context execctx.ExecContext `json:"context"`

	// WorkDir is the client's working directory, so gh runs in the correct location.
	WorkDir string `json:"work_dir,omitempty"`

	// NoCache skips cache lookup for this request.
	NoCache bool `json:"no_cache,omitempty"`

	// TTLOverride overrides the default TTL for this request (in seconds).
	TTLOverride int `json:"ttl_override,omitempty"`

	// Type distinguishes regular gh commands from control commands.
	Type RequestType `json:"type"`
}

type RequestType string

const (
	TypeExec     RequestType = "exec"     // Execute a gh command
	TypeFlush    RequestType = "flush"    // Flush cache
	TypeStats    RequestType = "stats"    // Get stats
	TypeKeys     RequestType = "keys"     // List cache keys
	TypeShutdown RequestType = "shutdown" // Graceful shutdown
)

// Response is sent from the ghxd daemon to the ghx client.
type Response struct {
	Stdout   []byte `json:"stdout,omitempty"`
	Stderr   []byte `json:"stderr,omitempty"`
	ExitCode int    `json:"exit_code"`
	Cached   bool   `json:"cached"`
	Error    string `json:"error,omitempty"`
}

// WriteMessage writes a length-prefixed JSON message to a writer.
func WriteMessage(w io.Writer, msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	// Write 4-byte length prefix (big-endian)
	length := uint32(len(data))
	if err := binary.Write(w, binary.BigEndian, length); err != nil {
		return fmt.Errorf("write length: %w", err)
	}

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write data: %w", err)
	}

	return nil
}

// ReadMessage reads a length-prefixed JSON message from a reader.
func ReadMessage(r io.Reader, msg interface{}) error {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return fmt.Errorf("read length: %w", err)
	}

	if length > 10*1024*1024 { // 10MB sanity limit
		return fmt.Errorf("message too large: %d bytes", length)
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return fmt.Errorf("read data: %w", err)
	}

	if err := json.Unmarshal(data, msg); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	return nil
}
