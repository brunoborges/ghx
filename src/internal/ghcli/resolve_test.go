package ghcli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestResolveGHPath_UserOverride(t *testing.T) {
	// Create a fake gh binary
	dir := t.TempDir()
	fakeGH := filepath.Join(dir, ghBinaryName())
	if err := os.WriteFile(fakeGH, []byte("#!/bin/sh\necho fake"), 0755); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveGHPath(fakeGH)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != fakeGH {
		t.Errorf("got %q, want %q", got, fakeGH)
	}
}

func TestResolveGHPath_UserOverride_NotFound(t *testing.T) {
	_, err := ResolveGHPath("/nonexistent/custom-gh")
	if err == nil {
		t.Fatal("expected error for nonexistent user override")
	}
}

func TestFindRealGHInPath_SkipsShim(t *testing.T) {
	dir := t.TempDir()

	// Create a shim script
	shimPath := filepath.Join(dir, ghBinaryName())
	if err := os.WriteFile(shimPath, []byte(ShimContent()), 0755); err != nil {
		t.Fatal(err)
	}

	// Set PATH to only include the shim directory
	orig := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", orig) })
	os.Setenv("PATH", dir)

	got := FindRealGHInPath()
	if got != "" {
		t.Errorf("expected empty string when only shim in PATH, got %q", got)
	}
}

func TestFindRealGHInPath_FindsReal(t *testing.T) {
	dir := t.TempDir()

	// Create a fake real gh binary (ELF header)
	ghPath := filepath.Join(dir, ghBinaryName())
	if err := os.WriteFile(ghPath, []byte{0x7f, 'E', 'L', 'F', 0, 0, 0, 0}, 0755); err != nil {
		t.Fatal(err)
	}

	orig := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", orig) })
	os.Setenv("PATH", dir)

	got := FindRealGHInPath()
	if got != ghPath {
		t.Errorf("got %q, want %q", got, ghPath)
	}
}

func TestFindRealGHInPath_PrefersRealOverShim(t *testing.T) {
	shimDir := t.TempDir()
	realDir := t.TempDir()

	// Shim in first PATH entry
	shimPath := filepath.Join(shimDir, ghBinaryName())
	if err := os.WriteFile(shimPath, []byte(ShimContent()), 0755); err != nil {
		t.Fatal(err)
	}

	// Real binary in second PATH entry
	realPath := filepath.Join(realDir, ghBinaryName())
	if err := os.WriteFile(realPath, []byte{0x7f, 'E', 'L', 'F', 0, 0, 0, 0}, 0755); err != nil {
		t.Fatal(err)
	}

	orig := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", orig) })
	os.Setenv("PATH", shimDir+string(os.PathListSeparator)+realDir)

	got := FindRealGHInPath()
	if got != realPath {
		t.Errorf("got %q, want %q", got, realPath)
	}
}

func TestManagedGHPath_NotEmpty(t *testing.T) {
	path := ManagedGHPath()
	if path == "" {
		t.Fatal("expected non-empty managed path")
	}
	if !filepath.IsAbs(path) {
		t.Errorf("expected absolute path, got %q", path)
	}
}

func TestIsExecutable(t *testing.T) {
	dir := t.TempDir()

	// Executable file
	execFile := filepath.Join(dir, "exec")
	if err := os.WriteFile(execFile, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	if !isExecutable(execFile) {
		t.Error("expected file with 0755 to be executable")
	}

	if runtime.GOOS != "windows" {
		// Non-executable file (permission bits not meaningful on Windows)
		noExecFile := filepath.Join(dir, "noexec")
		if err := os.WriteFile(noExecFile, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if isExecutable(noExecFile) {
			t.Error("expected file with 0644 to NOT be executable")
		}
	}

	// Nonexistent
	if isExecutable(filepath.Join(dir, "nope")) {
		t.Error("expected nonexistent file to NOT be executable")
	}

	// Directory
	subDir := filepath.Join(dir, "subdir")
	os.Mkdir(subDir, 0755)
	if isExecutable(subDir) {
		t.Error("expected directory to NOT be executable")
	}
}

func TestIsManagedGH(t *testing.T) {
	managed := ManagedGHPath()
	if managed == "" {
		t.Skip("cannot determine managed path")
	}

	if !IsManagedGH(managed) {
		t.Error("expected managed path to be recognized as managed")
	}

	if IsManagedGH("/usr/local/bin/gh") {
		t.Error("expected system path to NOT be recognized as managed")
	}

	if IsManagedGH("") {
		t.Error("expected empty path to NOT be recognized as managed")
	}
}

func TestCheckStaleness_Fresh(t *testing.T) {
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	if err := os.WriteFile(ghPath, []byte("fresh"), 0755); err != nil {
		t.Fatal(err)
	}

	// CheckStaleness should not panic or error on non-managed paths
	CheckStaleness(ghPath)
	CheckStaleness("")
	CheckStaleness("/nonexistent")
}

func TestCheckStaleness_OldFile(t *testing.T) {
	// Create a file and backdate it beyond the staleness threshold
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	if err := os.WriteFile(ghPath, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-45 * 24 * time.Hour)
	os.Chtimes(ghPath, oldTime, oldTime)

	// This only warns for managed paths, so it won't print for arbitrary paths.
	// Just verify it doesn't panic.
	CheckStaleness(ghPath)
}
