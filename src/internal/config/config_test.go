package config

import (
	"runtime"
	"strings"
	"testing"
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
