package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/brunoborges/ghx/src/internal/allowlist"
	execctx "github.com/brunoborges/ghx/src/internal/context"
	"github.com/brunoborges/ghx/src/internal/ipc"
	"github.com/brunoborges/ghx/src/internal/protocol"
)

func TestMutationHelperProcess(t *testing.T) {
	if os.Getenv("GHX_TEST_MUTATION_HELPER") != "1" {
		return
	}
	_, _ = io.Copy(os.Stdout, os.Stdin)
	_, _ = io.WriteString(os.Stderr, "child stderr\n")
	code, _ := strconv.Atoi(os.Getenv("GHX_TEST_MUTATION_EXIT"))
	os.Exit(code)
}

func TestRunMutationPreservesStreamsAndInvalidatesAfterExit(t *testing.T) {
	for _, code := range []int{0, 7} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			input := []byte(strings.Repeat("payload\x00with spaces\n", 4096))
			var stdout, stderr bytes.Buffer
			cmd := exec.Command(os.Args[0], "-test.run=^TestMutationHelperProcess$")
			cmd.Env = append(os.Environ(), "GHX_TEST_MUTATION_HELPER=1", "GHX_TEST_MUTATION_EXIT="+strconv.Itoa(code))
			cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(input), &stdout, &stderr
			flush := &protocol.Request{Type: protocol.TypeFlush, Resource: allowlist.ResourcePR,
				Context: execctx.ExecContext{Host: "github.com", Repo: "owner/repo"}}
			calls := 0
			got := runMutation(cmd, flush, func(req *protocol.Request) (*protocol.Response, error) {
				calls++
				if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
					t.Error("invalidation happened before gh exited")
				}
				if !bytes.Equal(stdout.Bytes(), input) {
					t.Error("stdin/stdout bytes changed")
				}
				if req.Type != protocol.TypeFlush || req.Resource != allowlist.ResourcePR ||
					req.Context.Repo != "owner/repo" || len(req.Args) != 0 || len(req.AuthEnv) != 0 {
					t.Errorf("unexpected invalidation request: %+v", req)
				}
				return &protocol.Response{}, nil
			})
			if got != code || calls != 1 || stderr.String() != "child stderr\n" {
				t.Fatalf("exit=%d invalidations=%d stderr=%q", got, calls, stderr.String())
			}
		})
	}
}

func TestRunMutationInvalidationFailureDoesNotReplay(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		response *protocol.Response
		warning  bool
	}{
		{"absent daemon", os.ErrNotExist, nil, false},
		{"failed response", errors.New("fixture connection closed"), nil, true},
		{"daemon rejection", nil, &protocol.Response{Error: "fixture failure", ExitCode: 1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			cmd := exec.Command(os.Args[0], "-test.run=^TestMutationHelperProcess$")
			cmd.Env = append(os.Environ(), "GHX_TEST_MUTATION_HELPER=1", "GHX_TEST_MUTATION_EXIT=7")
			cmd.Stdin, cmd.Stdout, cmd.Stderr = strings.NewReader("once"), &stdout, &stderr
			calls := 0
			got := runMutation(cmd, &protocol.Request{Type: protocol.TypeFlush}, func(*protocol.Request) (*protocol.Response, error) {
				calls++
				return tt.response, tt.err
			})
			if got != 7 || stdout.String() != "once" || calls != 1 {
				t.Fatalf("exit=%d stdout=%q invalidations=%d", got, stdout.String(), calls)
			}
			if strings.Contains(stderr.String(), "cache invalidation failed") != tt.warning {
				t.Fatalf("stderr=%q", stderr.String())
			}
		})
	}
}

// The test executable serves as both the client and a secretless fake gh.
// No real GitHub command or daemon executable is reachable from this fixture.
func TestMain(m *testing.M) {
	if os.Getenv("GHX_TEST_CLI_HELPER") == "1" {
		switch os.Args[1] {
		case "--ghx-client":
			os.Args = append(os.Args[:1], os.Args[2:]...)
			main()
		case "auth":
			fmt.Println("fixture-token")
		case "pr":
			if len(os.Args) < 3 || os.Args[2] != "edit" {
				os.Exit(79)
			}
			_, _ = io.Copy(os.Stdout, os.Stdin)
		default:
			os.Exit(79)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestMainMutationUsesNativeStdinAndFlush(t *testing.T) {
	for _, mode := range []string{"normal", "no-cache", "no daemon"} {
		t.Run(mode, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "ghx-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			address := filepath.Join(dir, "s")
			if runtime.GOOS == "windows" {
				address = fmt.Sprintf(`\\.\pipe\ghx-main-%d-%d`, os.Getpid(), time.Now().UnixNano())
			}
			requests := make(chan *protocol.Request, 1)
			if mode != "no daemon" {
				ln, err := ipc.Listen(address)
				if err != nil {
					t.Fatal(err)
				}
				defer ln.Close()
				go func() {
					conn, err := ln.Accept()
					if err != nil {
						return
					}
					defer conn.Close()
					var req protocol.Request
					if protocol.ReadMessage(conn, &req) != nil {
						return
					}
					requests <- &req
					// This also models the old daemon's supported global flush.
					_ = protocol.WriteMessage(conn, &protocol.Response{})
				}()
			}
			args := []string{"--ghx-client"}
			if mode == "no-cache" {
				args = append(args, "--no-cache")
			}
			args = append(args, "pr", "edit", "1", "--body-file", "-")
			cmd := exec.Command(os.Args[0], args...)
			cmd.Dir = dir
			cmd.Env = []string{
				"PATH=" + dir, "HOME=" + dir, "USERPROFILE=" + dir, "LOCALAPPDATA=" + dir,
				"GOCOVERDIR=" + t.TempDir(),
				"GHX_GH_PATH=" + os.Args[0], "GHX_SOCKET=" + address,
				"GH_REPO=owner/repo", "GHX_TEST_CLI_HELPER=1",
				// A resolver regression must fail without downloading a real gh binary.
				"HTTP_PROXY=http://127.0.0.1:1", "HTTPS_PROXY=http://127.0.0.1:1",
			}
			if value := os.Getenv("SystemRoot"); value != "" {
				cmd.Env = append(cmd.Env, "SystemRoot="+value)
			}
			payload := "first line\nsecond\x00line\n"
			var stdout, stderr bytes.Buffer
			cmd.Stdin, cmd.Stdout, cmd.Stderr = strings.NewReader(payload), &stdout, &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("client failed: %v; stderr=%s", err, &stderr)
			}
			if stdout.String() != payload || stderr.Len() != 0 {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			if mode != "no daemon" {
				select {
				case req := <-requests:
					if req.Type != protocol.TypeFlush || req.Resource != allowlist.ResourcePR ||
						len(req.Args) != 0 || len(req.AuthEnv) != 0 {
						t.Fatalf("mutation was sent through IPC: %+v", req)
					}
				case <-time.After(time.Second):
					t.Fatal("missing post-mutation invalidation")
				}
			}
			if _, err := os.Stat(filepath.Join(dir, ".ghx")); !os.IsNotExist(err) {
				t.Fatalf("unexpected daemon/config directory: %v", err)
			}
		})
	}
}
