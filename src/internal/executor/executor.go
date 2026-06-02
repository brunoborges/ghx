package executor

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Result holds the output of a gh command execution.
type Result struct {
	Stdout   []byte        `json:"stdout"`
	Stderr   []byte        `json:"stderr"`
	ExitCode int           `json:"exit_code"`
	Duration time.Duration `json:"duration_ms"`
}

// Execute runs a gh command with the given arguments and returns its output.
// If workDir is non-empty and absolute, the command runs in that directory.
func Execute(ctx context.Context, ghPath string, args []string, workDir string) *Result {
	start := time.Now()

	cmd := exec.CommandContext(ctx, ghPath, args...)
	if workDir != "" && filepath.IsAbs(workDir) {
		cmd.Dir = workDir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := &Result{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: 0,
		Duration: time.Since(start),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
			result.Stderr = append(result.Stderr, []byte("\nghx: "+err.Error())...)
		}
	}

	return result
}

// IsBinaryNotFound reports whether the given gh binary path cannot be found or resolved.
func IsBinaryNotFound(ghPath string) bool {
	_, err := exec.LookPath(ghPath)
	return err != nil && (errors.Is(err, exec.ErrNotFound) || errors.Is(err, exec.ErrDot) || os.IsNotExist(err) || errors.Is(err, fs.ErrNotExist))
}
