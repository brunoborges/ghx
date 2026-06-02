//go:build windows

package config

import (
	"os"
	"path/filepath"
)

// defaultGHXDir returns the default ghx configuration/data directory.
// On Windows, this is %LOCALAPPDATA%\ghx.
func defaultGHXDir() string {
	if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
		return filepath.Join(dir, "ghx")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ghx")
}

// defaultSocketPath returns the default IPC endpoint address.
// On Windows, this is a named pipe.
func defaultSocketPath(_ string) string {
	return `\\.\pipe\ghxd`
}
