package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/brunoborges/ghx/src/internal/cache"
	"github.com/brunoborges/ghx/src/internal/config"
	"github.com/brunoborges/ghx/src/internal/protocol"
)

func TestServerByteBudgetAndDiagnostics(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.MaxCacheBytes != cache.DefaultMaxBytes {
		t.Fatalf("default byte budget = %d, want %d", cfg.MaxCacheBytes, cache.DefaultMaxBytes)
	}
	cfg.MaxCacheBytes = 1
	s := NewServer(cfg, "test", "gh")
	data, err := os.ReadFile("../../../LICENSE")
	if err != nil {
		t.Fatal(err)
	}
	s.cache.Set(&cache.Entry{Key: "license", Stdout: data, CachedAt: time.Now(), TTL: time.Minute})
	var stats struct {
		CacheSize int   `json:"cache_size"`
		Bytes     int64 `json:"cache_bytes"`
		MaxBytes  int64 `json:"max_cache_bytes"`
		Rejected  int64 `json:"cache_rejected"`
	}
	if err := json.Unmarshal(s.handler.handleStats().Stdout, &stats); err != nil {
		t.Fatal(err)
	}
	if stats.CacheSize != 0 || stats.Bytes != 0 || stats.MaxBytes != 1 || stats.Rejected != 1 {
		t.Fatalf("stats = %+v; configured budget and rejection must be visible", stats)
	}
}

func TestServerCacheStatsTransports(t *testing.T) {
	for _, tt := range []struct {
		name     string
		retained bool
		rejected bool
	}{
		{name: "empty"},
		{name: "retained", retained: true},
		{name: "rejected with retained response", retained: true, rejected: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.MaxCacheBytes = 6
			s := NewServer(cfg, "test", "gh")
			if tt.retained {
				s.cache.Set(&cache.Entry{
					Key: "retained", Stdout: []byte("out"), Stderr: []byte("err"),
					CachedAt: time.Now(), TTL: time.Minute,
				})
			}
			if tt.rejected {
				s.cache.Set(&cache.Entry{
					Key: "oversized", Stdout: []byte("toolong"),
					CachedAt: time.Now(), TTL: time.Minute,
				})
			}

			for _, transport := range []string{"ipc", "http"} {
				t.Run(transport, func(t *testing.T) {
					var data []byte
					if transport == "ipc" {
						resp := s.handler.Handle(&protocol.Request{Type: protocol.TypeStats})
						if resp.ExitCode != 0 || resp.Error != "" {
							t.Fatalf("stats response = %+v", resp)
						}
						data = resp.Stdout
					} else {
						recorder := httptest.NewRecorder()
						req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
						req.Host = "127.0.0.1:9847"
						s.httpHandler().ServeHTTP(recorder, req)
						if recorder.Code != http.StatusOK {
							t.Fatalf("HTTP status = %d, want 200", recorder.Code)
						}
						if got := recorder.Header().Get("Content-Type"); got != "application/json" {
							t.Fatalf("Content-Type = %q, want application/json", got)
						}
						data = recorder.Body.Bytes()
					}
					var stats struct {
						Version      string `json:"version"`
						CacheSize    int    `json:"cache_size"`
						MaxCacheSize int    `json:"max_cache_size"`
						Bytes        int64  `json:"cache_bytes"`
						MaxBytes     int64  `json:"max_cache_bytes"`
						Rejected     int64  `json:"cache_rejected"`
					}
					if err := json.Unmarshal(data, &stats); err != nil {
						t.Fatal(err)
					}
					wantSize := 0
					wantUsage := cache.Usage{MaxBytes: 6}
					if tt.retained {
						wantSize = 1
						wantUsage.Bytes = 6
					}
					if tt.rejected {
						wantUsage.Rejected = 1
					}
					usage := cache.Usage{Bytes: stats.Bytes, MaxBytes: stats.MaxBytes, Rejected: stats.Rejected}
					if stats.CacheSize != wantSize || stats.MaxCacheSize != cfg.MaxCacheEntries || usage != wantUsage {
						t.Fatalf("stats = %+v; want size %d, max size %d, usage %+v",
							stats, wantSize, cfg.MaxCacheEntries, wantUsage)
					}
					if transport == "http" && stats.Version != "test" {
						t.Fatalf("version = %q, want test", stats.Version)
					}
				})
			}
		})
	}
}
