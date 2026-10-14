package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestExecResponseFitsBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name       string
		stderrOnly bool
		both       bool
		exitCode   int
		atLimit    bool
		overByOne  bool
	}{
		{name: "stdout"},
		{name: "stderr", stderrOnly: true},
		{name: "both streams", both: true},
		{name: "exact limit", exitCode: 123, atLimit: true},
		{name: "negative exit code", exitCode: -12, atLimit: true},
		{name: "one byte over", exitCode: 1234, overByOne: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resp := Response{Stdout: []byte{0}, ExitCode: tt.exitCode}
			if tt.stderrOnly {
				resp.Stdout, resp.Stderr = nil, []byte{0}
			}
			if tt.both {
				resp.Stderr = []byte{0}
			}
			sample, err := json.Marshal(&resp)
			if err != nil {
				t.Fatal(err)
			}
			// Remove the variable stream's four-byte base64 probe from the actual JSON.
			overhead := len(sample) - 4
			maxPayload := (MaxMessageSize - overhead) / 4 * 3
			payload := make([]byte, maxPayload+1)
			for _, fits := range []bool{true, false} {
				n := maxPayload
				if !fits {
					n++
				}
				if tt.stderrOnly {
					resp.Stderr = payload[:n]
				} else {
					resp.Stdout = payload[:n]
				}
				if got := ExecResponseFits(resp.Stdout, resp.Stderr, resp.ExitCode); got != fits {
					t.Fatalf("payload %d: fits = %v, want %v", n, got, fits)
				}

				var wire bytes.Buffer
				if err := WriteMessage(&wire, &resp); err != nil {
					t.Fatal(err)
				}
				encodedSize := wire.Len() - 4
				if fits && tt.atLimit && encodedSize != MaxMessageSize {
					t.Fatalf("encoded size = %d, want exactly %d", encodedSize, MaxMessageSize)
				}
				if !fits && tt.overByOne && encodedSize != MaxMessageSize+1 {
					t.Fatalf("encoded size = %d, want exactly %d", encodedSize, MaxMessageSize+1)
				}
				var decoded Response
				err = ReadMessage(&wire, &decoded)
				if fits {
					if err != nil {
						t.Fatalf("admitted response is unreadable: %v", err)
					}
					if !bytes.Equal(decoded.Stdout, resp.Stdout) || !bytes.Equal(decoded.Stderr, resp.Stderr) ||
						decoded.ExitCode != resp.ExitCode {
						t.Fatal("round trip changed the response")
					}
				} else if err == nil || !strings.HasPrefix(err.Error(), "message too large:") {
					t.Fatalf("over-limit response read error = %v, want message too large", err)
				}
			}
		})
	}
}

func TestExecResponseFitsEmptyPayloads(t *testing.T) {
	for _, payload := range [][]byte{nil, {}} {
		if !ExecResponseFits(payload, payload, 0) {
			t.Fatal("omitted empty payloads must fit the message limit")
		}
	}
}

func TestExecResponseFitsRejectsLargeRawPayloads(t *testing.T) {
	payload := make([]byte, MaxMessageSize+1)
	if ExecResponseFits(payload, nil, 0) || ExecResponseFits(nil, payload, 0) {
		t.Fatal("raw payloads larger than the IPC frame cannot fit after encoding")
	}
}
