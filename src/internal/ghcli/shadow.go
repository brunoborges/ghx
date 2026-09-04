package ghcli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// GHCandidate is a discoverable gh binary and its reported version.
type GHCandidate struct {
	Path    string
	Version string
}

// packageManagerGHPaths lists well-known package-manager install locations for gh.
// These are shadowed when the ghx shim is installed as "gh" on PATH, because the
// package manager's own bin directory only contains a symlink named "gh".
// Overridable for testing.
var packageManagerGHPaths = defaultPackageManagerGHPaths()

func defaultPackageManagerGHPaths() []string {
	if runtime.GOOS == "windows" {
		return nil
	}
	return []string{
		"/opt/homebrew/opt/gh/bin/gh",
		"/usr/local/opt/gh/bin/gh",
		"/home/linuxbrew/.linuxbrew/opt/gh/bin/gh",
	}
}

// probeCachePath is the file storing the last shadowed-gh probe result.
// Overridable for testing.
var probeCachePath = defaultProbeCachePath

func defaultProbeCachePath() string {
	managed := ManagedGHPath()
	if managed == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(filepath.Dir(managed)), "ghcli-probe.json")
}

type probeCache struct {
	Fingerprint string `json:"fingerprint"`
	Chosen      string `json:"chosen"`
}

// FindPackageManagerGHs returns the gh binaries found in well-known package-manager
// locations, skipping ghx shims and any binary whose version cannot be read.
func FindPackageManagerGHs() []GHCandidate {
	ghxPath := selfPath()
	var found []GHCandidate
	seen := make(map[string]bool)

	for _, candidate := range packageManagerGHPaths {
		if !isExecutable(candidate) {
			continue
		}
		key := candidate
		if resolved, err := filepath.EvalSymlinks(candidate); err == nil {
			key = resolved
		}
		if seen[key] || IsManagedGH(candidate) || IsShim(candidate, ghxPath) {
			continue
		}
		seen[key] = true

		ver, err := InstalledVersion(candidate)
		if err != nil {
			continue
		}
		found = append(found, GHCandidate{Path: candidate, Version: ver})
	}

	return found
}

// preferNewerGH returns the gh binary to use when resolution landed on the ghx-managed
// binary. If a package-manager gh that the shim shadows reports a newer version, that
// binary is preferred and a one-time notice is printed to stderr.
//
// The decision is cached in ~/.ghx/ghcli-probe.json and only re-evaluated when one of
// the involved binaries changes, so the common path costs no subprocess executions.
func preferNewerGH(managed string) string {
	fingerprint := probeFingerprint(managed)
	if fingerprint == "" {
		return managed
	}

	if cached, ok := readProbeCache(); ok && cached.Fingerprint == fingerprint {
		if cached.Chosen != "" && isExecutable(cached.Chosen) {
			return cached.Chosen
		}
		return managed
	}

	chosen, chosenVer := managed, ""
	if v, err := InstalledVersion(managed); err == nil {
		chosenVer = v
	}
	managedVer := chosenVer

	for _, candidate := range FindPackageManagerGHs() {
		if chosenVer == "" || compareVersions(candidate.Version, chosenVer) > 0 {
			chosen, chosenVer = candidate.Path, candidate.Version
		}
	}

	writeProbeCache(probeCache{Fingerprint: fingerprint, Chosen: chosen})

	if chosen != managed {
		fmt.Fprintf(os.Stderr, "ghx: using %s (v%s) — newer than the managed gh (v%s) shadowed by the ghx shim\n",
			chosen, chosenVer, displayVersion(managedVer))
	}
	return chosen
}

func displayVersion(v string) string {
	if v == "" {
		return "unknown"
	}
	return v
}

// probeFingerprint builds a cheap identity for the managed binary plus every existing
// package-manager candidate, so the probe is redone whenever any of them changes.
func probeFingerprint(managed string) string {
	info, err := os.Stat(managed)
	if err != nil {
		return ""
	}
	parts := []string{fmt.Sprintf("%s:%d:%d", managed, info.ModTime().UnixNano(), info.Size())}
	for _, candidate := range packageManagerGHPaths {
		ci, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%d:%d", candidate, ci.ModTime().UnixNano(), ci.Size()))
	}
	return strings.Join(parts, "|")
}

func readProbeCache() (probeCache, bool) {
	path := probeCachePath()
	if path == "" {
		return probeCache{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return probeCache{}, false
	}
	var c probeCache
	if err := json.Unmarshal(data, &c); err != nil {
		return probeCache{}, false
	}
	return c, true
}

func writeProbeCache(c probeCache) {
	path := probeCachePath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0644)
}

// IsPackageManagerGH reports whether path is one of the well-known package-manager
// install locations for gh.
func IsPackageManagerGH(path string) bool {
	for _, candidate := range packageManagerGHPaths {
		if candidate == path {
			return true
		}
	}
	return false
}

// CompareVersions compares two dotted version strings numerically.
// Returns -1 if a < b, 0 if equal, 1 if a > b.
func CompareVersions(a, b string) int {
	return compareVersions(a, b)
}

// compareVersions compares two dotted version strings numerically.
// Returns -1 if a < b, 0 if equal, 1 if a > b.
func compareVersions(a, b string) int {
	as := strings.Split(strings.TrimPrefix(strings.TrimSpace(a), "v"), ".")
	bs := strings.Split(strings.TrimPrefix(strings.TrimSpace(b), "v"), ".")

	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		av := versionPart(as, i)
		bv := versionPart(bs, i)
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionPart(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	digits := parts[i]
	// Strip any pre-release suffix (e.g. "0-rc1")
	if idx := strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' }); idx >= 0 {
		digits = digits[:idx]
	}
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 0
	}
	return n
}
