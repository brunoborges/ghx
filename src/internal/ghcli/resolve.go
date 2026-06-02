// Package ghcli manages the discovery and installation of the real GitHub CLI (gh) binary.
// It provides resolution logic to find gh in managed locations, PATH, or by auto-downloading
// from the cli/cli GitHub releases.
package ghcli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// StalenessThreshold is how old a managed gh binary can be before a warning is shown.
const StalenessThreshold = 7 * 24 * time.Hour // 7 days

// ghBinaryName returns "gh.exe" on Windows, "gh" elsewhere.
func ghBinaryName() string {
	if runtime.GOOS == "windows" {
		return "gh.exe"
	}
	return "gh"
}

// ResolveGHPath finds the real GitHub CLI (gh) binary.
//
// Resolution order:
//  1. User override — if cfgGHPath is not the default "gh", use it directly
//  2. PATH scan — search for a real gh in PATH, skipping ghx shims
//  3. Managed location — ~/.ghx/bin/gh (previously downloaded)
//  4. Auto-download — download from cli/cli releases to managed location
func ResolveGHPath(cfgGHPath string) (string, error) {
	// User explicitly configured a specific path
	if cfgGHPath != "" && cfgGHPath != "gh" {
		if _, err := exec.LookPath(cfgGHPath); err != nil {
			return "", fmt.Errorf("configured gh_path %q not found: %w", cfgGHPath, err)
		}
		return cfgGHPath, nil
	}

	// Search PATH for a real gh (not a shim)
	if realPath := FindRealGHInPath(); realPath != "" {
		return realPath, nil
	}

	// Check managed location
	managed := ManagedGHPath()
	if managed != "" && isExecutable(managed) {
		return managed, nil
	}

	// Auto-download
	if managed == "" {
		return "", fmt.Errorf("gh not found and cannot determine home directory for auto-download\nInstall gh manually: https://cli.github.com")
	}

	fmt.Fprintf(os.Stderr, "ghx: GitHub CLI (gh) not found, downloading...\n")
	if err := Download(managed); err != nil {
		return "", fmt.Errorf("gh not found and auto-download failed: %w\nInstall gh manually: https://cli.github.com", err)
	}
	return managed, nil
}

// ManagedGHPath returns the path where ghx manages its own gh binary (~/.ghx/bin/gh).
func ManagedGHPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ghx", "bin", ghBinaryName())
}

// IsManagedGH reports whether the resolved gh path points to the ghx-managed binary.
func IsManagedGH(resolvedPath string) bool {
	managed := ManagedGHPath()
	if managed == "" || resolvedPath == "" {
		return false
	}
	// Compare resolved paths to handle symlinks
	a, err1 := filepath.EvalSymlinks(resolvedPath)
	b, err2 := filepath.EvalSymlinks(managed)
	if err1 != nil || err2 != nil {
		return resolvedPath == managed
	}
	return a == b
}

// CheckStaleness prints a warning to stderr if the managed gh binary is older than
// StalenessThreshold. Only applies to the managed binary at ~/.ghx/bin/gh, not
// user-installed or PATH-resolved gh.
func CheckStaleness(resolvedPath string) {
	if !IsManagedGH(resolvedPath) {
		return
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return
	}
	age := time.Since(info.ModTime())
	if age > StalenessThreshold {
		days := int(age.Hours() / 24)
		fmt.Fprintf(os.Stderr, "ghx: managed gh binary is %d days old — run 'ghx ghcli upgrade' to update\n", days)
	}
}

// FindRealGHInPath searches PATH for a real gh binary, skipping ghx shims.
func FindRealGHInPath() string {
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		return ""
	}

	ghxPath := selfPath()

	for _, dir := range filepath.SplitList(pathEnv) {
		candidate := filepath.Join(dir, ghBinaryName())
		if !isExecutable(candidate) {
			continue
		}
		if IsShim(candidate, ghxPath) {
			continue
		}
		return candidate
	}

	return ""
}

// selfPath returns the resolved path to the current executable (ghx/ghxd).
func selfPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe
	}
	return resolved
}

// isExecutable checks if a file exists and is executable.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		// On Windows, permission bits are not meaningful.
		// A regular file is considered executable.
		return true
	}
	return info.Mode()&0111 != 0
}
