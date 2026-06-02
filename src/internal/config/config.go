package config

import (
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	TTL             time.Duration            `yaml:"ttl"`
	TTLOverrides    map[string]time.Duration `yaml:"ttl_overrides"`
	MaxCacheEntries int                      `yaml:"max_cache_entries"`
	SocketPath      string                   `yaml:"socket_path"`
	PIDFile         string                   `yaml:"pid_file"`
	AutoStart       bool                     `yaml:"auto_start"`
	AdditionalCache []string                 `yaml:"additional_cacheable"`
	DashboardPort   int                      `yaml:"dashboard_port"`
	GHPath          string                   `yaml:"gh_path"`
	LogFile         string                   `yaml:"log_file"`
}

func DefaultConfig() *Config {
	ghxDir := defaultGHXDir()
	return &Config{
		TTL:             30 * time.Second,
		TTLOverrides:    make(map[string]time.Duration),
		MaxCacheEntries: 1000,
		SocketPath:      defaultSocketPath(ghxDir),
		PIDFile:         filepath.Join(ghxDir, "ghxd.pid"),
		AutoStart:       true,
		DashboardPort:   9847,
		GHPath:          "gh",
		LogFile:         filepath.Join(ghxDir, "ghxd.log"),
	}
}

func Load() (*Config, error) {
	cfg := DefaultConfig()

	ghxDir := defaultGHXDir()
	configPath := filepath.Join(ghxDir, "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return cfg, err
	}

	// Apply env var overrides
	if v := os.Getenv("GHX_TTL"); v != "" {
		if d, err := time.ParseDuration(v + "s"); err == nil {
			cfg.TTL = d
		} else if d, err := time.ParseDuration(v); err == nil {
			cfg.TTL = d
		}
	}
	if v := os.Getenv("GHX_SOCKET"); v != "" {
		cfg.SocketPath = v
	}
	if v := os.Getenv("GHX_GH_PATH"); v != "" {
		cfg.GHPath = v
	}

	return cfg, nil
}

// CommandTTL returns the TTL for a specific command, falling back to default.
func (c *Config) CommandTTL(cmd string) time.Duration {
	if ttl, ok := c.TTLOverrides[cmd]; ok {
		return ttl
	}
	return c.TTL
}
