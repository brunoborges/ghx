//go:build !windows

package config

import (
	"os"
	"path/filepath"
)

// defaultGHXDir returns the default ghx configuration/data directory.
// On Unix, this is ~/.ghx.
func defaultGHXDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ghx")
}

// defaultSocketPath returns the default IPC socket path.
// On Unix, this is a Unix domain socket inside the ghx directory.
func defaultSocketPath(ghxDir string) string {
	return filepath.Join(ghxDir, "ghxd.sock")
}
