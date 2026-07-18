package executor

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/brunoborges/ghx/src/internal/authenv"
)

func TestExecute_WorkDir(t *testing.T) {
	dir := t.TempDir()

	// Use pwd (or cd on Windows) to verify the subprocess runs in the specified directory.
	var result *Result
	if runtime.GOOS == "windows" {
		result = Execute(context.Background(), "cmd", []string{"/C", "cd"}, dir, nil)
	} else {
		result = Execute(context.Background(), "pwd", nil, dir, nil)
	}

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d: %s", result.ExitCode, result.Stderr)
	}
	got := strings.TrimSpace(string(result.Stdout))
	gotInfo, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat reported workdir %q: %v", got, err)
	}
	wantInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat expected workdir %q: %v", dir, err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Errorf("expected workdir %q, got %q", dir, got)
	}
}

func TestExecute_EmptyWorkDir(t *testing.T) {
	// Empty workDir should inherit the current process's working directory.
	cwd, _ := os.Getwd()

	var result *Result
	if runtime.GOOS == "windows" {
		result = Execute(context.Background(), "cmd", []string{"/C", "cd"}, "", nil)
	} else {
		result = Execute(context.Background(), "pwd", nil, "", nil)
	}

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d: %s", result.ExitCode, result.Stderr)
	}
	got := strings.TrimSpace(string(result.Stdout))
	if got != cwd {
		t.Errorf("expected cwd %q, got %q", cwd, got)
	}
}

func TestExecute_RelativeWorkDir_Ignored(t *testing.T) {
	// Relative paths should be ignored (not set as cmd.Dir).
	cwd, _ := os.Getwd()

	var result *Result
	if runtime.GOOS == "windows" {
		result = Execute(context.Background(), "cmd", []string{"/C", "cd"}, "relative/path", nil)
	} else {
		result = Execute(context.Background(), "pwd", nil, "relative/path", nil)
	}

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d: %s", result.ExitCode, result.Stderr)
	}
	got := strings.TrimSpace(string(result.Stdout))
	if got != cwd {
		t.Errorf("relative path should be ignored; expected cwd %q, got %q", cwd, got)
	}
}

func TestExecute_UsesClientAuthEnvironment(t *testing.T) {
	t.Setenv("GH_TOKEN", "stale-daemon-token")
	args := []string{"-test.run=^TestExecuteAuthEnvironmentHelper$", "--"}

	withToken := Execute(context.Background(), os.Args[0], args, "", authenv.Environment{
		"GH_TOKEN": "current-client-token",
	})
	if got := firstOutputLine(withToken.Stdout); got != "current-client-token" {
		t.Fatalf("client token execution = %q, want %q", got, "current-client-token")
	}

	withoutToken := Execute(context.Background(), os.Args[0], args, "", nil)
	if got := firstOutputLine(withoutToken.Stdout); got != "<unset>" {
		t.Fatalf("token-free execution = %q, want stale daemon token removed", got)
	}
}

func TestExecuteAuthEnvironmentHelper(t *testing.T) {
	if token, ok := os.LookupEnv("GH_TOKEN"); ok {
		fmt.Println(token)
	} else {
		fmt.Println("<unset>")
	}
}

func firstOutputLine(output []byte) string {
	line, _, _ := strings.Cut(string(output), "\n")
	return strings.TrimSpace(line)
}
