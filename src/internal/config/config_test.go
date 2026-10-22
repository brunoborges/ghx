package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfigSocketPath(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.SocketPath == "" {
		t.Fatal("SocketPath should not be empty")
	}

	if runtime.GOOS == "windows" {
		if !strings.HasPrefix(cfg.SocketPath, `\\.\pipe\`) {
			t.Errorf("on Windows, SocketPath should be a named pipe, got: %s", cfg.SocketPath)
		}
	} else {
		if !strings.HasSuffix(cfg.SocketPath, "ghxd.sock") {
			t.Errorf("on Unix, SocketPath should end with ghxd.sock, got: %s", cfg.SocketPath)
		}
	}
}

func TestDefaultConfigDirectory(t *testing.T) {
	ghxDir := defaultGHXDir()
	if ghxDir == "" {
		t.Fatal("defaultGHXDir() should not return empty string")
	}

	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(ghxDir, "ghx") {
			t.Errorf("on Windows, directory should end with 'ghx', got: %s", ghxDir)
		}
	} else {
		if !strings.Contains(ghxDir, ".ghx") {
			t.Errorf("on Unix, directory should contain .ghx, got: %s", ghxDir)
		}
	}
}

func TestDefaultConfigPIDFile(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.PIDFile == "" {
		t.Fatal("PIDFile should not be empty")
	}
	if !strings.HasSuffix(cfg.PIDFile, "ghxd.pid") {
		t.Errorf("PIDFile should end with ghxd.pid, got: %s", cfg.PIDFile)
	}
}

func TestLoadReturnsDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() returned nil config")
	}
	if cfg.TTL == 0 {
		t.Error("TTL should not be zero")
	}
	if cfg.MaxCacheEntries == 0 {
		t.Error("MaxCacheEntries should not be zero")
	}
	if cfg.GHPath == "" {
		t.Error("GHPath should not be empty")
	}
}

func TestLoadCacheByteBudget(t *testing.T) {
	for _, tt := range []struct {
		name        string
		yaml        string
		wantBytes   int64
		wantEntries int
	}{
		{name: "missing config", wantBytes: 64 << 20, wantEntries: 1000},
		{name: "legacy config", yaml: "max_cache_entries: 7\n", wantBytes: 64 << 20, wantEntries: 7},
		{name: "custom limit", yaml: "max_cache_entries: 7\nmax_cache_bytes: 4096\n", wantBytes: 4096, wantEntries: 7},
		{name: "zero disables storage", yaml: "max_cache_bytes: 0\n", wantBytes: 0, wantEntries: 1000},
		{name: "negative disables storage", yaml: "max_cache_bytes: -1\n", wantBytes: -1, wantEntries: 1000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			for _, name := range []string{"HOME", "USERPROFILE", "LOCALAPPDATA"} {
				t.Setenv(name, home)
			}
			if tt.yaml != "" {
				dir := defaultGHXDir()
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(tt.yaml), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.MaxCacheBytes != tt.wantBytes || cfg.MaxCacheEntries != tt.wantEntries {
				t.Fatalf("limits = (%d bytes, %d entries), want (%d bytes, %d entries)",
					cfg.MaxCacheBytes, cfg.MaxCacheEntries, tt.wantBytes, tt.wantEntries)
			}
		})
	}
}

func TestCommandTTL(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TTLOverrides["pr_list"] = cfg.TTL * 2

	if got := cfg.CommandTTL("pr_list"); got != cfg.TTL*2 {
		t.Errorf("CommandTTL(pr_list) = %v, want %v", got, cfg.TTL*2)
	}
	if got := cfg.CommandTTL("unknown"); got != cfg.TTL {
		t.Errorf("CommandTTL(unknown) = %v, want default %v", got, cfg.TTL)
	}
}

// Environment overrides must also work on a fresh installation with no YAML file.
func TestLoadEnvironmentOverrides(t *testing.T) {
	for _, withConfig := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing config", true: "existing config"}[withConfig], func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HOME", dir)
			t.Setenv("USERPROFILE", dir)
			t.Setenv("LOCALAPPDATA", dir)
			t.Setenv("GHX_TTL", "45")
			t.Setenv("GHX_SOCKET", "fixture-socket")
			t.Setenv("GHX_GH_PATH", "fixture-gh")
			t.Setenv("GHX_LOG_ARGS", "1")
			if withConfig {
				if err := os.MkdirAll(defaultGHXDir(), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(defaultGHXDir(), "config.yaml"),
					[]byte("ttl: 10s\ngh_path: yaml-gh\nsocket_path: yaml-socket\nlog_args: false\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.TTL != 45*time.Second || cfg.SocketPath != "fixture-socket" || cfg.GHPath != "fixture-gh" || !cfg.LogArgs {
				t.Fatalf("overrides were not applied: %+v", cfg)
			}
		})
	}
}

func TestLifetimeTTLDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.LogArgs {
		t.Error("LogArgs must be off by default")
	}
	if cfg.ImmutableTTL != time.Hour || cfg.SettledTTL != 10*time.Minute {
		t.Fatalf("ImmutableTTL=%v SettledTTL=%v", cfg.ImmutableTTL, cfg.SettledTTL)
	}
}
