package ghcli

import (
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// ShimMarker is the comment marker embedded in gh shim scripts for detection.
const ShimMarker = "# ghx-shim"

// ShimContent returns the content for the gh shim shell script.
func ShimContent() string {
	return `#!/bin/sh
# ghx-shim: this script redirects gh commands through ghx for caching
exec ghx "$@"
`
}

// IsShim checks if the given ghPath is a ghx shim rather than the real GitHub CLI.
// It uses multiple detection strategies:
//  1. Resolved path / inode comparison (catches symlinks and hardlinks to ghx)
//  2. File header marker check (catches shell script shims)
func IsShim(ghPath string, ghxPath string) bool {
	if ghxPath != "" {
		// Check by resolved path (catches symlinks)
		resolved, err := filepath.EvalSymlinks(ghPath)
		if err == nil {
			ghxResolved, err2 := filepath.EvalSymlinks(ghxPath)
			if err2 == nil && resolved == ghxResolved {
				return true
			}
		}

		// Check by inode (catches hardlinks)
		ghInfo, err := os.Stat(ghPath)
		if err == nil {
			ghxInfo, err2 := os.Stat(ghxPath)
			if err2 == nil && os.SameFile(ghInfo, ghxInfo) {
				return true
			}
		}
	}

	// Check file header for shim marker (only read first 512 bytes)
	f, err := os.Open(ghPath)
	if err != nil {
		return false
	}
	header, readErr := io.ReadAll(io.LimitReader(f, 512))
	if err := f.Close(); err != nil {
		log.Printf("close gh executable: %v", err)
	}
	if readErr != nil || len(header) == 0 {
		return false
	}

	// Compiled binaries (the real gh) won't contain our text marker
	if !isTextHeader(header) {
		return false
	}

	return strings.Contains(string(header), ShimMarker)
}

// binaryMagicPrefixes contains byte sequences that identify compiled binaries.
var binaryMagicPrefixes = [][]byte{
	{0x7f, 'E', 'L', 'F'},    // ELF
	{0xca, 0xfe, 0xba, 0xbe}, // Mach-O universal/fat binary
	{0xcf, 0xfa, 0xed, 0xfe}, // Mach-O 64-bit
	{0xce, 0xfa, 0xed, 0xfe}, // Mach-O 32-bit
	{'M', 'Z'},               // PE (Windows)
}

// isTextHeader checks if the byte slice looks like a text file (not a compiled binary).
func isTextHeader(data []byte) bool {
	for _, magic := range binaryMagicPrefixes {
		if bytes.HasPrefix(data, magic) {
			return false
		}
	}
	return true
}
