package ghcli

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAssetName(t *testing.T) {
	name, err := assetName("2.45.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedOS, _ := ghOSName()
	expectedArch, _ := ghArchName()
	ext := ".tar.gz"
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		ext = ".zip"
	}
	expected := fmt.Sprintf("gh_2.45.0_%s_%s%s", expectedOS, expectedArch, ext)
	if name != expected {
		t.Errorf("got %q, want %q", name, expected)
	}
}

func TestGHOSName(t *testing.T) {
	name, err := ghOSName()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	switch runtime.GOOS {
	case "darwin":
		if name != "macOS" {
			t.Errorf("expected macOS, got %s", name)
		}
	case "linux":
		if name != "linux" {
			t.Errorf("expected linux, got %s", name)
		}
	}
}

func TestGHArchName(t *testing.T) {
	name, err := ghArchName()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if name != runtime.GOARCH {
		t.Errorf("expected %s, got %s", runtime.GOARCH, name)
	}
}

func TestVerifyChecksum(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.bin")
	content := []byte("hello world")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	h := sha256.Sum256(content)
	correctChecksum := hex.EncodeToString(h[:])

	// Correct checksum
	if err := verifyChecksum(testFile, correctChecksum); err != nil {
		t.Errorf("expected correct checksum to pass: %v", err)
	}

	// Wrong checksum
	if err := verifyChecksum(testFile, "0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Error("expected wrong checksum to fail")
	}
}

func TestExtractFromTarGz(t *testing.T) {
	dir := t.TempDir()

	// Create a test tar.gz with gh binary
	archivePath := filepath.Join(dir, "gh_2.0.0_linux_amd64.tar.gz")
	ghContent := []byte("#!/bin/sh\necho gh")
	createTestTarGz(t, archivePath, "gh_2.0.0_linux_amd64/bin/gh", ghContent)

	destPath := filepath.Join(dir, "gh")
	if err := extractFromTarGz(archivePath, destPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("reading extracted file: %v", err)
	}
	if string(got) != string(ghContent) {
		t.Errorf("got %q, want %q", string(got), string(ghContent))
	}
}

func TestExtractFromTarGz_NotFound(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "empty.tar.gz")
	createTestTarGz(t, archivePath, "other/file.txt", []byte("not gh"))

	destPath := filepath.Join(dir, "gh")
	err := extractFromTarGz(archivePath, destPath)
	if err == nil {
		t.Fatal("expected error when gh not in archive")
	}
}

func TestExtractFromTarGz_RejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "malicious.tar.gz")
	createTestTarGz(t, archivePath, "../../etc/bin/gh", []byte("evil"))

	destPath := filepath.Join(dir, "gh")
	err := extractFromTarGz(archivePath, destPath)
	if err == nil {
		t.Fatal("expected error for path traversal entry")
	}
}

func TestExtractFromZip(t *testing.T) {
	dir := t.TempDir()

	entryName := "gh_2.0.0_macOS_arm64/bin/gh"
	if runtime.GOOS == "windows" {
		entryName = "gh_2.0.0_windows_amd64/bin/gh.exe"
	}

	archivePath := filepath.Join(dir, "gh_2.0.0_macOS_arm64.zip")
	ghContent := []byte("#!/bin/sh\necho gh")
	createTestZip(t, archivePath, entryName, ghContent)

	destPath := filepath.Join(dir, "gh")
	if err := extractFromZip(archivePath, destPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("reading extracted file: %v", err)
	}
	if string(got) != string(ghContent) {
		t.Errorf("got %q, want %q", string(got), string(ghContent))
	}
}

func TestExtractFromZip_NotFound(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "empty.zip")
	createTestZip(t, archivePath, "other/file.txt", []byte("not gh"))

	destPath := filepath.Join(dir, "gh")
	err := extractFromZip(archivePath, destPath)
	if err == nil {
		t.Fatal("expected error when gh not in archive")
	}
}

func TestAcquireLock(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	unlock, err := acquireLock(lockPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Lock file should exist
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Error("lock file should exist while held")
	}

	unlock()

	// Lock file should be removed
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("lock file should be removed after unlock")
	}
}

func TestAtomicInstall(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	content := []byte("binary content")
	if err := os.WriteFile(src, content, 0755); err != nil {
		t.Fatal(err)
	}

	if err := atomicInstall(src, dst); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading dest: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("got %q, want %q", string(got), string(content))
	}
}

func TestGetLatestVersion_MockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ghRelease{TagName: "v2.45.0"})
	}))
	defer server.Close()

	orig := releasesAPIURL
	releasesAPIURL = server.URL
	t.Cleanup(func() { releasesAPIURL = orig })

	version, err := getLatestVersion()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "2.45.0" {
		t.Errorf("got %q, want %q", version, "2.45.0")
	}
}

func TestGetLatestVersion_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	orig := releasesAPIURL
	releasesAPIURL = server.URL
	t.Cleanup(func() { releasesAPIURL = orig })

	_, err := getLatestVersion()
	if err == nil {
		t.Fatal("expected error for server error response")
	}
}

func TestDownloadChecksums_MockServer(t *testing.T) {
	checksumContent := "abc123  gh_2.45.0_linux_amd64.tar.gz\ndef456  gh_2.45.0_macOS_arm64.zip\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(checksumContent))
	}))
	defer server.Close()

	orig := releasesBaseURL
	releasesBaseURL = server.URL
	t.Cleanup(func() { releasesBaseURL = orig })

	checksums, err := downloadChecksums("2.45.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := checksums["gh_2.45.0_linux_amd64.tar.gz"]; got != "abc123" {
		t.Errorf("linux checksum: got %q, want %q", got, "abc123")
	}
	if got := checksums["gh_2.45.0_macOS_arm64.zip"]; got != "def456" {
		t.Errorf("macOS checksum: got %q, want %q", got, "def456")
	}
}

func TestExtractGH_UnsupportedFormat(t *testing.T) {
	err := extractGH("/tmp/archive.rar", "/tmp/gh")
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

// --- test helpers ---

func createTestTarGz(t *testing.T, archivePath, entryName string, content []byte) {
	t.Helper()
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	hdr := &tar.Header{
		Name: entryName,
		Mode: 0755,
		Size: int64(len(content)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
}

func createTestZip(t *testing.T, archivePath, entryName string, content []byte) {
	t.Helper()
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	w, err := zw.Create(entryName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
}
