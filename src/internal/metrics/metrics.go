package metrics

import (
	"fmt"
	"sync"
	"time"
)

// RequestResult describes the outcome of a request.
type RequestResult string

const (
	ResultHit         RequestResult = "hit"
	ResultMiss        RequestResult = "miss"
	ResultPassthrough RequestResult = "passthrough"
	ResultCoalesced   RequestResult = "coalesced"
)

// LogEntry records a single request in the log.
type LogEntry struct {
	Timestamp time.Time     `json:"timestamp"`
	Command   string        `json:"command"`
	CacheKey  string        `json:"cache_key,omitempty"`
	Result    RequestResult `json:"result"`
	LatencyMs float64       `json:"latency_ms"`
}

// CommandStats tracks per-command hit/miss statistics.
type CommandStats struct {
	Hits           int64   `json:"hits"`
	Misses         int64   `json:"misses"`
	Passthrough    int64   `json:"passthrough"`
	Coalesced      int64   `json:"coalesced"`
	TotalLatencyMs float64 `json:"total_latency_ms"`
	RequestCount   int64   `json:"request_count"`
}

// AvgLatencyMs returns the average latency in milliseconds.
func (cs *CommandStats) AvgLatencyMs() float64 {
	if cs.RequestCount == 0 {
		return 0
	}
	return cs.TotalLatencyMs / float64(cs.RequestCount)
}

// Stats holds all metrics for the daemon.
type Stats struct {
	mu            sync.RWMutex
	startedAt     time.Time
	total         int64
	hits          int64
	misses        int64
	passthrough   int64
	coalesced     int64
	evictions     int64
	invalidations int64
	commands      map[string]*CommandStats
	log           []LogEntry
	maxLog        int

	// Inter-request intervals for TTL analysis
	lastSeen  map[string]time.Time
	intervals map[string][]time.Duration
}

// New creates a new Stats tracker.
func New() *Stats {
	return &Stats{
		startedAt: time.Now(),
		commands:  make(map[string]*CommandStats),
		log:       make([]LogEntry, 0, 200),
		maxLog:    200,
		lastSeen:  make(map[string]time.Time),
		intervals: make(map[string][]time.Duration),
	}
}

// Record logs a request and updates counters.
func (s *Stats) Record(cmdKey string, cacheKey string, result RequestResult, latencyMs float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.total++

	cs, ok := s.commands[cmdKey]
	if !ok {
		cs = &CommandStats{}
		s.commands[cmdKey] = cs
	}

	cs.RequestCount++
	cs.TotalLatencyMs += latencyMs

	switch result {
	case ResultHit:
		s.hits++
		cs.Hits++
	case ResultMiss:
		s.misses++
		cs.Misses++
	case ResultPassthrough:
		s.passthrough++
		cs.Passthrough++
	case ResultCoalesced:
		s.coalesced++
		cs.Coalesced++
		s.hits++ // coalesced counts as a hit for rate calc
		cs.Hits++
	}

	// Track inter-request intervals for TTL analysis
	if cacheKey != "" {
		if last, ok := s.lastSeen[cacheKey]; ok {
			interval := time.Since(last)
			if interval < 10*time.Minute { // ignore gaps > 10 min
				intervals := s.intervals[cacheKey]
				if len(intervals) > 100 {
					intervals = intervals[1:]
				}
				s.intervals[cacheKey] = append(intervals, interval)
			}
		}
		s.lastSeen[cacheKey] = time.Now()
	}

	// Append to log ring buffer
	entry := LogEntry{
		Timestamp: time.Now(),
		Command:   cmdKey,
		CacheKey:  cacheKey,
		Result:    result,
		LatencyMs: latencyMs,
	}
	if len(s.log) >= s.maxLog {
		s.log = s.log[1:]
	}
	s.log = append(s.log, entry)
}

// RecordEviction increments the eviction counter.
func (s *Stats) RecordEviction() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictions++
}

// RecordInvalidation increments the invalidation counter.
func (s *Stats) RecordInvalidation(count int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invalidations += int64(count)
}

// Snapshot represents a point-in-time view of all stats.
type Snapshot struct {
	Uptime        string                   `json:"uptime"`
	UptimeSeconds float64                  `json:"uptime_seconds"`
	Total         int64                    `json:"total"`
	Hits          int64                    `json:"hits"`
	Misses        int64                    `json:"misses"`
	Passthrough   int64                    `json:"passthrough"`
	Coalesced     int64                    `json:"coalesced"`
	Evictions     int64                    `json:"evictions"`
	Invalidations int64                    `json:"invalidations"`
	HitRate       float64                  `json:"hit_rate"`
	CacheSize     int                      `json:"cache_size"`
	MaxCacheSize  int                      `json:"max_cache_size"`
	Commands      map[string]*CommandStats `json:"commands"`
}

// Snapshot returns a point-in-time copy of all stats.
func (s *Stats) Snapshot(cacheSize, maxCacheSize int) Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	uptime := time.Since(s.startedAt)
	hitTotal := s.hits + s.misses
	var hitRate float64
	if hitTotal > 0 {
		hitRate = float64(s.hits) / float64(hitTotal) * 100
	}

	// Deep copy commands
	cmds := make(map[string]*CommandStats, len(s.commands))
	for k, v := range s.commands {
		copy := *v
		cmds[k] = &copy
	}

	return Snapshot{
		Uptime:        formatDuration(uptime),
		UptimeSeconds: uptime.Seconds(),
		Total:         s.total,
		Hits:          s.hits,
		Misses:        s.misses,
		Passthrough:   s.passthrough,
		Coalesced:     s.coalesced,
		Evictions:     s.evictions,
		Invalidations: s.invalidations,
		HitRate:       hitRate,
		CacheSize:     cacheSize,
		MaxCacheSize:  maxCacheSize,
		Commands:      cmds,
	}
}

// Log returns the most recent log entries.
func (s *Stats) Log(limit int) []LogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 || limit > len(s.log) {
		limit = len(s.log)
	}
	start := len(s.log) - limit
	entries := make([]LogEntry, limit)
	copy(entries, s.log[start:])
	return entries
}

// TTLAnalysis returns recommended TTLs based on observed inter-request intervals.
type TTLRecommendation struct {
	CacheKey       string  `json:"cache_key"`
	SampleCount    int     `json:"sample_count"`
	MedianInterval float64 `json:"median_interval_seconds"`
	P90Interval    float64 `json:"p90_interval_seconds"`
}

func (s *Stats) TTLAnalysis() []TTLRecommendation {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var recs []TTLRecommendation
	for key, intervals := range s.intervals {
		if len(intervals) < 3 {
			continue
		}
		sorted := make([]time.Duration, len(intervals))
		copy(sorted, intervals)
		sortDurations(sorted)

		recs = append(recs, TTLRecommendation{
			CacheKey:       key,
			SampleCount:    len(sorted),
			MedianInterval: sorted[len(sorted)/2].Seconds(),
			P90Interval:    sorted[int(float64(len(sorted))*0.9)].Seconds(),
		})
	}
	return recs
}

func formatDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func sortDurations(d []time.Duration) {
	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && d[j] < d[j-1]; j-- {
			d[j], d[j-1] = d[j-1], d[j]
		}
	}
}
