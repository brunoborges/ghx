package ghcli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// fakeGH writes a shell script that reports the given gh version.
func fakeGH(t *testing.T, path, version string) {
	t.Helper()
	script := fmt.Sprintf("#!/bin/sh\necho \"gh version %s (2026-09-02)\"\n", version)
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
}

// usePackageManagerPaths overrides the well-known candidate list for the test.
func usePackageManagerPaths(t *testing.T, paths ...string) {
	t.Helper()
	orig := packageManagerGHPaths
	t.Cleanup(func() { packageManagerGHPaths = orig })
	packageManagerGHPaths = paths
}

// useProbeCache points the probe cache at a temp file for the test.
func useProbeCache(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ghcli-probe.json")
	orig := probeCachePath
	t.Cleanup(func() { probeCachePath = orig })
	probeCachePath = func() string { return path }
	return path
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"2.99.0", "2.98.0", 1},
		{"2.98.0", "2.99.0", -1},
		{"2.99.0", "2.99.0", 0},
		{"2.99.0", "v2.99.0", 0},
		{"2.100.0", "2.99.0", 1},
		{"3.0.0", "2.99.9", 1},
		{"2.99", "2.99.0", 0},
		{"2.99.1", "2.99", 1},
		{"2.99.0-rc1", "2.99.0", 0},
		{"garbage", "2.99.0", -1},
	}
	for _, tt := range tests {
		if got := compareVersions(tt.a, tt.b); got != tt.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestFindPackageManagerGHs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fakes are not executable on Windows")
	}
	dir := t.TempDir()

	brew := filepath.Join(dir, "brew-gh")
	fakeGH(t, brew, "2.99.0")

	shim := filepath.Join(dir, "shim-gh")
	if err := os.WriteFile(shim, []byte(ShimContent()), 0755); err != nil {
		t.Fatal(err)
	}

	missing := filepath.Join(dir, "missing-gh")

	usePackageManagerPaths(t, brew, shim, missing)

	got := FindPackageManagerGHs()
	if len(got) != 1 {
		t.Fatalf("expected 1 candidate, got %d (%v)", len(got), got)
	}
	if got[0].Path != brew || got[0].Version != "2.99.0" {
		t.Errorf("got %+v, want path %q version 2.99.0", got[0], brew)
	}
}

func TestPreferNewerGH_PrefersShadowedNewer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fakes are not executable on Windows")
	}
	dir := t.TempDir()

	managed := filepath.Join(dir, "managed-gh")
	fakeGH(t, managed, "2.98.0")

	brew := filepath.Join(dir, "brew-gh")
	fakeGH(t, brew, "2.99.0")

	usePackageManagerPaths(t, brew)
	cachePath := useProbeCache(t)

	if got := preferNewerGH(managed); got != brew {
		t.Errorf("got %q, want %q", got, brew)
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Errorf("expected probe cache to be written: %v", err)
	}

	// Second call is served from the cache and must return the same answer.
	if got := preferNewerGH(managed); got != brew {
		t.Errorf("cached call got %q, want %q", got, brew)
	}
}

func TestPreferNewerGH_KeepsManagedWhenNewer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fakes are not executable on Windows")
	}
	dir := t.TempDir()

	managed := filepath.Join(dir, "managed-gh")
	fakeGH(t, managed, "2.99.0")

	brew := filepath.Join(dir, "brew-gh")
	fakeGH(t, brew, "2.98.0")

	usePackageManagerPaths(t, brew)
	useProbeCache(t)

	if got := preferNewerGH(managed); got != managed {
		t.Errorf("got %q, want %q", got, managed)
	}
}

func TestPreferNewerGH_NoCandidates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fakes are not executable on Windows")
	}
	dir := t.TempDir()

	managed := filepath.Join(dir, "managed-gh")
	fakeGH(t, managed, "2.98.0")

	usePackageManagerPaths(t)
	useProbeCache(t)

	if got := preferNewerGH(managed); got != managed {
		t.Errorf("got %q, want %q", got, managed)
	}
}

func TestPreferNewerGH_ReprobesAfterUpgrade(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fakes are not executable on Windows")
	}
	dir := t.TempDir()

	managed := filepath.Join(dir, "managed-gh")
	fakeGH(t, managed, "2.98.0")

	brew := filepath.Join(dir, "brew-gh")
	fakeGH(t, brew, "2.97.0")

	usePackageManagerPaths(t, brew)
	useProbeCache(t)

	if got := preferNewerGH(managed); got != managed {
		t.Fatalf("got %q, want %q", got, managed)
	}

	// Simulate "brew upgrade gh": the candidate changes and must be re-probed.
	fakeGH(t, brew, "2.99.0")
	newTime := time.Now().Add(time.Second)
	if err := os.Chtimes(brew, newTime, newTime); err != nil {
		t.Fatal(err)
	}
	if got := preferNewerGH(managed); got != brew {
		t.Errorf("after upgrade got %q, want %q", got, brew)
	}
}

func TestPreferNewerGH_MissingManaged(t *testing.T) {
	dir := t.TempDir()
	managed := filepath.Join(dir, "absent-gh")
	usePackageManagerPaths(t)
	useProbeCache(t)

	if got := preferNewerGH(managed); got != managed {
		t.Errorf("got %q, want %q", got, managed)
	}
}

func TestIsPackageManagerGH(t *testing.T) {
	usePackageManagerPaths(t, "/opt/homebrew/opt/gh/bin/gh")

	if !IsPackageManagerGH("/opt/homebrew/opt/gh/bin/gh") {
		t.Error("expected well-known path to be recognized")
	}
	if IsPackageManagerGH("/usr/local/bin/gh") {
		t.Error("expected unrelated path to NOT be recognized")
	}
}
