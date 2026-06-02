package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brunoborges/ghx/src/internal/allowlist"
	"github.com/brunoborges/ghx/src/internal/cache"
	"github.com/brunoborges/ghx/src/internal/config"
	execctx "github.com/brunoborges/ghx/src/internal/context"
	"github.com/brunoborges/ghx/src/internal/executor"
	"github.com/brunoborges/ghx/src/internal/ghcli"
	"github.com/brunoborges/ghx/src/internal/metrics"
	"github.com/brunoborges/ghx/src/internal/protocol"
)

// Handler processes incoming requests from clients.
type Handler struct {
	cfg        *config.Config
	cache      *cache.Cache
	classifier *allowlist.Classifier
	stats      *metrics.Stats

	// ghPath holds the resolved gh binary path, refreshed periodically.
	ghPath atomic.Value // stores string

	// singleflight: one in-flight request per cache key
	mu       sync.Mutex
	inflight map[string]*call
}

type call struct {
	wg  sync.WaitGroup
	res *executor.Result
}

func NewHandler(cfg *config.Config, c *cache.Cache, cl *allowlist.Classifier, s *metrics.Stats) *Handler {
	h := &Handler{
		cfg:        cfg,
		cache:      c,
		classifier: cl,
		stats:      s,
		inflight:   make(map[string]*call),
	}
	h.ghPath.Store(cfg.GHPath)
	c.OnEvict(func(key string) {
		s.RecordEviction()
	})
	return h
}

// GHPath returns the current resolved gh binary path.
func (h *Handler) GHPath() string {
	return h.ghPath.Load().(string)
}

// SetGHPath atomically updates the resolved gh binary path.
func (h *Handler) SetGHPath(path string) {
	h.ghPath.Store(path)
}

// execGH runs gh and retries once with a re-resolved path if the binary is not found.
func (h *Handler) execGH(args []string, workDir string) *executor.Result {
	ghPath := h.GHPath()
	if executor.IsBinaryNotFound(ghPath) {
		if newPath := h.reResolveGHPath(); newPath != "" {
			ghPath = newPath
		}
	}
	return executor.Execute(context.Background(), ghPath, args, workDir)
}

// reResolveGHPath attempts to find a new gh binary and updates the stored path.
// Returns the new path on success or empty string on failure.
func (h *Handler) reResolveGHPath() string {
	resolved, err := ghcli.ResolveGHPath(h.cfg.GHPath)
	if err != nil {
		log.Printf("gh re-resolve failed: %v", err)
		return ""
	}
	current := h.GHPath()
	if resolved != current {
		log.Printf("gh path updated: %s -> %s", current, resolved)
		h.SetGHPath(resolved)
	}
	return resolved
}

// Handle processes a single client request and returns a response.
func (h *Handler) Handle(req *protocol.Request) *protocol.Response {
	switch req.Type {
	case protocol.TypeStats:
		return h.handleStats()
	case protocol.TypeFlush:
		return h.handleFlush(req)
	case protocol.TypeKeys:
		return h.handleKeys()
	case protocol.TypeShutdown:
		return &protocol.Response{Stdout: []byte("shutting down\n")}
	default:
		return h.handleExec(req)
	}
}

func (h *Handler) handleExec(req *protocol.Request) *protocol.Response {
	start := time.Now()

	classification := h.classifier.Classify(req.Args)
	cmdKey := classification.CmdKey
	if cmdKey == "" {
		cmdKey = sanitizeCmdKey(req.Args)
	}

	// Non-cacheable: execute directly via daemon (captures output)
	if classification.Type == allowlist.Passthrough || req.NoCache {
		result := h.execGH(req.Args, req.WorkDir)
		latency := time.Since(start).Seconds() * 1000
		h.stats.Record(cmdKey, "", metrics.ResultPassthrough, latency)

		return &protocol.Response{
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
			ExitCode: result.ExitCode,
		}
	}

	// Mutation: execute directly, then invalidate
	if classification.Type == allowlist.Mutation {
		result := h.execGH(req.Args, req.WorkDir)
		latency := time.Since(start).Seconds() * 1000
		h.stats.Record(cmdKey, "", metrics.ResultPassthrough, latency)

		if classification.Resource != allowlist.ResourceUnknown {
			count := h.cache.InvalidateNamespace(req.Context.Host, req.Context.Repo, classification.Resource)
			h.stats.RecordInvalidation(count)
		}

		return &protocol.Response{
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
			ExitCode: result.ExitCode,
		}
	}

	// Cacheable: check cache → singleflight → execute → store
	cacheKey := execctx.CacheKey(req.Context, req.Args)

	// Check cache
	if entry := h.cache.Get(cacheKey); entry != nil {
		latency := time.Since(start).Seconds() * 1000
		h.stats.Record(cmdKey, cacheKey, metrics.ResultHit, latency)
		return &protocol.Response{
			Stdout:   entry.Stdout,
			Stderr:   entry.Stderr,
			ExitCode: entry.ExitCode,
			Cached:   true,
		}
	}

	// Singleflight: coalesce concurrent requests for the same key
	result, coalesced := h.doSingleflight(cacheKey, req)
	latency := time.Since(start).Seconds() * 1000

	if coalesced {
		h.stats.Record(cmdKey, cacheKey, metrics.ResultCoalesced, latency)
	} else {
		h.stats.Record(cmdKey, cacheKey, metrics.ResultMiss, latency)
	}

	// Store in cache
	ttl := h.cfg.CommandTTL(cmdKey)
	if req.TTLOverride > 0 {
		ttl = time.Duration(req.TTLOverride) * time.Second
	}

	h.cache.Set(&cache.Entry{
		Key:      cacheKey,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: result.ExitCode,
		CachedAt: time.Now(),
		TTL:      ttl,
		Resource: classification.Resource,
		Host:     req.Context.Host,
		Repo:     req.Context.Repo,
	})

	return &protocol.Response{
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: result.ExitCode,
	}
}

func (h *Handler) doSingleflight(key string, req *protocol.Request) (*executor.Result, bool) {
	h.mu.Lock()
	if c, ok := h.inflight[key]; ok {
		h.mu.Unlock()
		c.wg.Wait()
		return c.res, true // coalesced
	}

	c := &call{}
	c.wg.Add(1)
	h.inflight[key] = c
	h.mu.Unlock()

	c.res = h.execGH(req.Args, req.WorkDir)
	c.wg.Done()

	h.mu.Lock()
	delete(h.inflight, key)
	h.mu.Unlock()

	return c.res, false
}

func (h *Handler) handleStats() *protocol.Response {
	snap := h.stats.Snapshot(h.cache.Size(), h.cfg.MaxCacheEntries)
	data, _ := json.Marshal(snap)
	return &protocol.Response{Stdout: data}
}

func (h *Handler) handleFlush(req *protocol.Request) *protocol.Response {
	count := h.cache.Flush()
	msg := []byte(fmt.Sprintf("flushed %d entries\n", count))
	return &protocol.Response{Stdout: msg}
}

func (h *Handler) handleKeys() *protocol.Response {
	keys := h.cache.Keys()
	data, _ := json.Marshal(keys)
	return &protocol.Response{Stdout: data}
}

// sanitizeCmdKey builds a metrics key from at most the first two args
// (subcommand + action), avoiding exposure of flags or values that could
// contain sensitive data.
func sanitizeCmdKey(args []string) string {
	switch {
	case len(args) >= 2:
		return args[0] + "_" + args[1]
	case len(args) == 1:
		return args[0]
	default:
		return "unknown"
	}
}
