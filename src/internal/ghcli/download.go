package ghcli

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Overridable for testing.
var (
	releasesAPIURL  = "https://api.github.com/repos/cli/cli/releases/latest"
	releasesBaseURL = "https://github.com/cli/cli/releases/download"
)

const (
	lockTimeout = 5 * time.Minute
	lockRetries = 60 // up to 60 seconds waiting for lock
)

// Download downloads the GitHub CLI binary to destPath.
// It uses a lock file to prevent concurrent downloads, verifies checksums,
// and uses atomic file operations. Skips download if destPath already exists.
func Download(destPath string) error {
	return downloadTo(destPath, false)
}

// Upgrade re-downloads the latest GitHub CLI to destPath, replacing any existing binary.
// Returns the installed version string.
func Upgrade(destPath string) (string, error) {
	return downloadToWithVersion(destPath, true)
}

func downloadTo(destPath string, force bool) error {
	_, err := downloadToWithVersion(destPath, force)
	return err
}

func downloadToWithVersion(destPath string, force bool) (string, error) {
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("creating directory %s: %w", destDir, err)
	}

	// Acquire lock to prevent concurrent downloads
	lockPath := destPath + ".lock"
	unlock, err := acquireLock(lockPath)
	if err != nil {
		return "", fmt.Errorf("acquiring download lock: %w", err)
	}
	defer unlock()

	// Re-check after lock — another process may have completed the download
	if !force && isExecutable(destPath) {
		return "", nil
	}

	version, err := getLatestVersion()
	if err != nil {
		return "", fmt.Errorf("getting latest gh version: %w", err)
	}

	fmt.Fprintf(os.Stderr, "ghx: downloading GitHub CLI v%s...\n", version)

	if err := fetchAndInstall(version, destPath); err != nil {
		return "", err
	}

	fmt.Fprintf(os.Stderr, "ghx: GitHub CLI v%s installed to %s\n", version, destPath)
	return version, nil
}

// fetchAndInstall downloads, verifies, extracts, and installs the gh binary.
func fetchAndInstall(version, destPath string) error {
	asset, err := assetName(version)
	if err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "ghx-gh-download-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, asset)
	if err := downloadAndVerify(version, asset, archivePath); err != nil {
		return err
	}

	// Extract gh binary
	ghBinaryPath := filepath.Join(tmpDir, "gh-extracted")
	if err := extractGH(archivePath, ghBinaryPath); err != nil {
		return fmt.Errorf("extracting gh binary: %w", err)
	}

	if err := os.Chmod(ghBinaryPath, 0755); err != nil {
		return fmt.Errorf("setting permissions: %w", err)
	}

	// Atomic install: rename or copy
	if err := atomicInstall(ghBinaryPath, destPath); err != nil {
		return fmt.Errorf("installing gh binary: %w", err)
	}
	return nil
}

// downloadAndVerify downloads the archive and verifies its checksum if available.
func downloadAndVerify(version, asset, archivePath string) error {
	// Download checksums for verification (failure is non-fatal)
	checksums, checksumErr := downloadChecksums(version)
	if checksumErr != nil {
		fmt.Fprintf(os.Stderr, "ghx: warning: could not download checksums: %v\n", checksumErr)
	}

	downloadURL := fmt.Sprintf("%s/v%s/%s", releasesBaseURL, version, asset)
	if err := downloadFile(downloadURL, archivePath); err != nil {
		return fmt.Errorf("downloading %s: %w", asset, err)
	}

	if checksums != nil {
		if expected, ok := checksums[asset]; ok {
			if err := verifyChecksum(archivePath, expected); err != nil {
				return fmt.Errorf("checksum verification failed for %s: %w", asset, err)
			}
		}
	}
	return nil
}

type ghRelease struct {
	TagName string `json:"tag_name"`
}

// GetLatestVersion fetches the latest GitHub CLI version from the releases API.
func GetLatestVersion() (string, error) {
	return getLatestVersion()
}

// InstalledVersion returns the version of the gh binary at the given path
// by running "gh --version" and parsing the output.
func InstalledVersion(ghPath string) (string, error) {
	cmd := exec.Command(ghPath, "--version")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("running %s --version: %w", ghPath, err)
	}
	// Output format: "gh version 2.74.0 (2025-01-15)\nhttps://..."
	parts := strings.Fields(string(out))
	for i, p := range parts {
		if p == "version" && i+1 < len(parts) {
			return parts[i+1], nil
		}
	}
	return strings.TrimSpace(string(out)), nil
}

func getLatestVersion() (string, error) {
	resp, err := http.Get(releasesAPIURL)
	if err != nil {
		return "", fmt.Errorf("fetching releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	var release ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("parsing release JSON: %w", err)
	}

	version := strings.TrimPrefix(release.TagName, "v")
	if version == "" {
		return "", fmt.Errorf("empty version in release tag %q", release.TagName)
	}

	return version, nil
}

func assetName(version string) (string, error) {
	osName, err := ghOSName()
	if err != nil {
		return "", err
	}

	archName, err := ghArchName()
	if err != nil {
		return "", err
	}

	ext := ".tar.gz"
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		ext = ".zip"
	}

	return fmt.Sprintf("gh_%s_%s_%s%s", version, osName, archName, ext), nil
}

func ghOSName() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return "macOS", nil
	case "linux":
		return "linux", nil
	case "windows":
		return "windows", nil
	default:
		return "", fmt.Errorf("unsupported OS for gh download: %s", runtime.GOOS)
	}
}

func ghArchName() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64", nil
	case "arm64":
		return "arm64", nil
	case "386":
		return "386", nil
	default:
		return "", fmt.Errorf("unsupported architecture for gh download: %s", runtime.GOARCH)
	}
}

func downloadChecksums(version string) (map[string]string, error) {
	url := fmt.Sprintf("%s/v%s/gh_%s_checksums.txt", releasesBaseURL, version, version)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	checksums := make(map[string]string)
	for _, line := range strings.Split(string(body), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 {
			// Format: "sha256hash  filename"
			checksums[parts[1]] = parts[0]
		}
	}

	return checksums, nil
}

func downloadFile(url, destPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func verifyChecksum(filePath, expected string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}

	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("expected SHA256 %s, got %s", expected, actual)
	}

	return nil
}

func extractGH(archivePath, destPath string) error {
	if strings.HasSuffix(archivePath, ".tar.gz") || strings.HasSuffix(archivePath, ".tgz") {
		return extractFromTarGz(archivePath, destPath)
	}
	if strings.HasSuffix(archivePath, ".zip") {
		return extractFromZip(archivePath, destPath)
	}
	return fmt.Errorf("unsupported archive format: %s", filepath.Base(archivePath))
}

func extractFromTarGz(archivePath, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	ghSuffix := "/bin/gh"

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar: %w", err)
		}

		// Security: reject symlinks and path traversal
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if strings.Contains(hdr.Name, "..") {
			continue
		}

		if strings.HasSuffix(hdr.Name, ghSuffix) {
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, tr)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			return closeErr
		}
	}

	return fmt.Errorf("gh binary (*/bin/gh) not found in tar archive")
}

func extractFromZip(archivePath, destPath string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	ghSuffix := "/bin/gh"
	if runtime.GOOS == "windows" {
		ghSuffix = "/bin/gh.exe"
	}

	for _, zf := range r.File {
		// Security: reject path traversal
		if strings.Contains(zf.Name, "..") {
			continue
		}
		// Skip directories and symlinks
		if zf.FileInfo().IsDir() || zf.FileInfo().Mode()&os.ModeSymlink != 0 {
			continue
		}

		if strings.HasSuffix(zf.Name, ghSuffix) {
			rc, err := zf.Open()
			if err != nil {
				return err
			}

			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				rc.Close()
				return err
			}

			_, copyErr := io.Copy(out, rc)
			closeErr := out.Close()
			rc.Close()
			if copyErr != nil {
				return copyErr
			}
			return closeErr
		}
	}

	return fmt.Errorf("gh binary (*/bin/gh) not found in zip archive")
}

func acquireLock(lockPath string) (func(), error) {
	for i := 0; i < lockRetries; i++ {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			f.Close()
			return func() { os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		// Stale lock detection
		info, statErr := os.Stat(lockPath)
		if statErr == nil && time.Since(info.ModTime()) > lockTimeout {
			os.Remove(lockPath)
			continue
		}
		time.Sleep(time.Second)
	}
	return nil, fmt.Errorf("timeout waiting for download lock (%s)", lockPath)
}

func atomicInstall(src, dst string) error {
	// Try rename first (same filesystem = atomic)
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	// Cross-device fallback: copy then remove source
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
