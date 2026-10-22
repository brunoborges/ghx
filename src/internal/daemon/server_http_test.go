package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brunoborges/ghx/src/internal/config"
	"github.com/brunoborges/ghx/src/internal/metrics"
	"github.com/brunoborges/ghx/src/internal/protocol"
)

func newHTTPTestServer(logArgs bool) *Server {
	cfg := config.DefaultConfig()
	cfg.LogArgs = logArgs
	return NewServer(cfg, "test", "gh")
}

func TestDashboardRejectsForeignHostsAndOrigins(t *testing.T) {
	h := newHTTPTestServer(false).httpHandler()
	tests := []struct {
		name, method, path, host, origin string
		want                             int
	}{
		{"loopback read", "GET", "/api/stats", "127.0.0.1:9847", "", 200},
		{"localhost read", "GET", "/api/log", "localhost:9847", "", 200},
		{"dashboard page", "GET", "/", "localhost:9847", "", 200},
		{"rebound host", "GET", "/api/log", "evil.example:9847", "", 403},
		{"cross-site flush", "POST", "/api/flush", "127.0.0.1:9847", "https://evil.example", 403},
		{"cross-port flush", "POST", "/api/flush", "127.0.0.1:9847", "http://127.0.0.1:3000", 403},
		{"null origin flush", "POST", "/api/flush", "127.0.0.1:9847", "null", 403},
		{"same-origin flush", "POST", "/api/flush", "127.0.0.1:9847", "http://127.0.0.1:9847", 200},
		{"curl flush", "POST", "/api/flush", "127.0.0.1:9847", "", 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Host = tt.host
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
			if acao := rec.Header().Get("Access-Control-Allow-Origin"); acao != "" {
				t.Fatalf("unexpected CORS header %q", acao)
			}
		})
	}
}

func TestDashboardLogArgs(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		s := newHTTPTestServer(enabled)
		s.handler.record(&protocol.Request{Args: []string{"api", "user", "-H", "Authorization: token secret"}}, nil, "api_get", "k", metrics.ResultMiss, 1)

		req := httptest.NewRequest("GET", "/api/log", nil)
		req.Host = "127.0.0.1:9847"
		rec := httptest.NewRecorder()
		s.httpHandler().ServeHTTP(rec, req)
		body := rec.Body.String()
		if strings.Contains(body, "secret") {
			t.Fatalf("secret leaked: %s", body)
		}
		var entries []metrics.LogEntry
		if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil || len(entries) != 1 {
			t.Fatalf("log = %s (%v)", body, err)
		}
		if got := entries[0].Args != nil; got != enabled {
			t.Fatalf("log_args=%v but args present=%v", enabled, got)
		}

		req = httptest.NewRequest("GET", "/api/stats", nil)
		req.Host = "127.0.0.1:9847"
		rec = httptest.NewRecorder()
		s.httpHandler().ServeHTTP(rec, req)
		var stats struct {
			LogArgs bool `json:"log_args"`
		}
		if json.Unmarshal(rec.Body.Bytes(), &stats); stats.LogArgs != enabled || rec.Code != http.StatusOK {
			t.Fatalf("stats log_args = %v, want %v", stats.LogArgs, enabled)
		}
	}
}
