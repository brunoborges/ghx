package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/brunoborges/ghx/src/internal/allowlist"
	"github.com/brunoborges/ghx/src/internal/cache"
	"github.com/brunoborges/ghx/src/internal/config"
	"github.com/brunoborges/ghx/src/internal/dashboard"
	"github.com/brunoborges/ghx/src/internal/ghcli"
	"github.com/brunoborges/ghx/src/internal/ipc"
	"github.com/brunoborges/ghx/src/internal/metrics"
	"github.com/brunoborges/ghx/src/internal/protocol"
)

// ghPathRefreshInterval is how often the daemon re-resolves the gh binary path.
// This ensures the daemon picks up gh upgrades without requiring a restart.
const ghPathRefreshInterval = 1 * time.Minute

// Server is the ghxd daemon.
type Server struct {
	cfg     *config.Config
	cache   *cache.Cache
	stats   *metrics.Stats
	handler *Handler
	ln      net.Listener
	httpSrv *http.Server
	done    chan struct{}
	wg      sync.WaitGroup
	version string
}

// NewServer creates a new daemon server.
func NewServer(cfg *config.Config, version string, resolvedGHPath string) *Server {
	c := cache.New(cfg.MaxCacheEntries)
	stats := metrics.New()
	classifier := allowlist.NewClassifier(cfg.AdditionalCache)
	handler := NewHandler(cfg, c, classifier, stats)
	handler.SetGHPath(resolvedGHPath)

	return &Server{
		cfg:     cfg,
		cache:   c,
		stats:   stats,
		handler: handler,
		done:    make(chan struct{}),
		version: version,
	}
}

// Run starts the daemon and blocks until shutdown.
func (s *Server) Run() error {
	// Ensure socket directory exists
	socketDir := filepath.Dir(s.cfg.SocketPath)
	if err := os.MkdirAll(socketDir, 0700); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}

	// Remove stale socket (no-op on Windows)
	removeStaleSocket(s.cfg.SocketPath)

	// Start IPC listener (Unix socket or Windows named pipe)
	ln, err := ipc.Listen(s.cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	s.ln = ln

	// Set socket permissions (no-op on Windows)
	if err := setSocketPermissions(s.cfg.SocketPath); err != nil {
		ln.Close()
		return fmt.Errorf("set socket permissions: %w", err)
	}

	// Write PID file
	if err := s.writePIDFile(); err != nil {
		ln.Close()
		return fmt.Errorf("write pid: %w", err)
	}
	defer s.removePIDFile()

	// Start HTTP server for dashboard (skip if port is 0)
	if s.cfg.DashboardPort != 0 {
		s.startHTTP()
	}

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	notifyShutdownSignals(sigCh)

	go func() {
		select {
		case <-sigCh:
			log.Println("received shutdown signal")
			s.Shutdown()
		case <-s.done:
		}
	}()

	log.Printf("ghxd started (socket: %s", s.cfg.SocketPath)
	if s.cfg.DashboardPort != 0 {
		log.Printf("  dashboard: http://127.0.0.1:%d/", s.cfg.DashboardPort)
	}

	// Start periodic gh path re-resolution
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.refreshGHPath()
	}()

	// Accept connections
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.done:
				s.wg.Wait()
				return nil
			default:
				log.Printf("accept error: %v", err)
				continue
			}
		}

		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(60 * time.Second))

	var req protocol.Request
	if err := protocol.ReadMessage(conn, &req); err != nil {
		// Don't log EOF — it's just a health check probe from IsRunning()
		if err.Error() != "read length: EOF" {
			log.Printf("read request: %v", err)
		}
		return
	}

	resp := s.handler.Handle(&req)

	// Check for shutdown request
	if req.Type == protocol.TypeShutdown {
		protocol.WriteMessage(conn, resp)
		go s.Shutdown()
		return
	}

	if err := protocol.WriteMessage(conn, resp); err != nil {
		log.Printf("write response: %v", err)
	}
}

func (s *Server) startHTTP() {
	mux := http.NewServeMux()

	// Dashboard
	mux.HandleFunc("/", dashboard.Handler())

	// JSON API
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		snap := s.stats.Snapshot(s.cache.Size(), s.cfg.MaxCacheEntries)
		resp := struct {
			Version string `json:"version"`
			metrics.Snapshot
		}{s.version, snap}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/api/log", func(w http.ResponseWriter, r *http.Request) {
		const maxLogLimit = 200
		limit := maxLogLimit
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				if n > maxLogLimit {
					limit = maxLogLimit
				} else {
					limit = n
				}
			}
		}
		entries := s.stats.Log(limit)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(entries)
	})

	mux.HandleFunc("/api/ttl-analysis", func(w http.ResponseWriter, r *http.Request) {
		recs := s.stats.TTLAnalysis()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(recs)
	})

	// Mutating endpoints — POST only, origin-validated
	mux.HandleFunc("/api/flush", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		count := s.cache.Flush()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"flushed": count})
	})

	mux.HandleFunc("/api/shutdown", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "shutting_down"})
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		go func() {
			time.Sleep(100 * time.Millisecond)
			s.Shutdown()
		}()
	})

	s.httpSrv = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", s.cfg.DashboardPort),
		Handler: mux,
	}

	go func() {
		if err := s.httpSrv.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()
}

// Shutdown gracefully stops the daemon.
func (s *Server) Shutdown() {
	select {
	case <-s.done:
		return // already shutting down
	default:
		close(s.done)
	}

	// Stop HTTP server
	if s.httpSrv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.httpSrv.Shutdown(ctx)
	}

	// Stop accepting connections
	if s.ln != nil {
		s.ln.Close()
	}

	log.Println("ghxd shutdown complete")
}

func (s *Server) writePIDFile() error {
	dir := filepath.Dir(s.cfg.PIDFile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(s.cfg.PIDFile, []byte(strconv.Itoa(os.Getpid())), 0600)
}

func (s *Server) removePIDFile() {
	os.Remove(s.cfg.PIDFile)
	os.Remove(s.cfg.SocketPath)
}

// refreshGHPath periodically re-resolves the gh binary path to pick up upgrades.
func (s *Server) refreshGHPath() {
	ticker := time.NewTicker(ghPathRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			resolved, err := ghcli.ResolveGHPath(s.cfg.GHPath)
			if err != nil {
				log.Printf("gh path refresh: %v (keeping current)", err)
				continue
			}
			current := s.handler.GHPath()
			if resolved != current {
				log.Printf("gh path updated: %s -> %s", current, resolved)
				s.handler.SetGHPath(resolved)
			}
		}
	}
}
