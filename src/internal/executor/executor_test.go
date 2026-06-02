package executor

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestExecute_WorkDir(t *testing.T) {
	dir := t.TempDir()

	// Use pwd (or cd on Windows) to verify the subprocess runs in the specified directory.
	var result *Result
	if runtime.GOOS == "windows" {
		result = Execute(context.Background(), "cmd", []string{"/C", "cd"}, dir)
	} else {
		result = Execute(context.Background(), "pwd", nil, dir)
	}

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d: %s", result.ExitCode, result.Stderr)
	}
	got := strings.TrimSpace(string(result.Stdout))
	if got != dir {
		t.Errorf("expected workdir %q, got %q", dir, got)
	}
}

func TestExecute_EmptyWorkDir(t *testing.T) {
	// Empty workDir should inherit the current process's working directory.
	cwd, _ := os.Getwd()

	var result *Result
	if runtime.GOOS == "windows" {
		result = Execute(context.Background(), "cmd", []string{"/C", "cd"}, "")
	} else {
		result = Execute(context.Background(), "pwd", nil, "")
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
		result = Execute(context.Background(), "cmd", []string{"/C", "cd"}, "relative/path")
	} else {
		result = Execute(context.Background(), "pwd", nil, "relative/path")
	}

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d: %s", result.ExitCode, result.Stderr)
	}
	got := strings.TrimSpace(string(result.Stdout))
	if got != cwd {
		t.Errorf("relative path should be ignored; expected cwd %q, got %q", cwd, got)
	}
}
