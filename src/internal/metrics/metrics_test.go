package metrics

import (
	"testing"
)

func TestRecordAndSnapshot(t *testing.T) {
	s := New()

	s.Record("pr_list", "key1", ResultMiss, 100.0)
	s.Record("pr_list", "key1", ResultHit, 0.5)
	s.Record("pr_list", "key1", ResultHit, 0.3)
	s.Record("issue_view", "key2", ResultPassthrough, 50.0)

	snap := s.Snapshot(5, 1000)

	if snap.Total != 4 {
		t.Errorf("total=%d, want 4", snap.Total)
	}
	if snap.Hits != 2 {
		t.Errorf("hits=%d, want 2", snap.Hits)
	}
	if snap.Misses != 1 {
		t.Errorf("misses=%d, want 1", snap.Misses)
	}
	if snap.Passthrough != 1 {
		t.Errorf("passthrough=%d, want 1", snap.Passthrough)
	}
	if snap.CacheSize != 5 {
		t.Errorf("cache_size=%d, want 5", snap.CacheSize)
	}

	prStats := snap.Commands["pr_list"]
	if prStats == nil {
		t.Fatal("pr_list stats missing")
	}
	if prStats.Hits != 2 || prStats.Misses != 1 {
		t.Errorf("pr_list: hits=%d misses=%d, want 2/1", prStats.Hits, prStats.Misses)
	}
	hitRate := float64(prStats.Hits) / float64(prStats.Hits+prStats.Misses) * 100
	if hitRate < 66 || hitRate > 67 {
		t.Errorf("pr_list hit rate=%.1f, want ~66.7", hitRate)
	}
}

func TestLogRingBuffer(t *testing.T) {
	s := New()

	for i := 0; i < 250; i++ {
		s.Record("cmd", "key", ResultHit, 1.0)
	}

	log := s.Log(0)
	if len(log) != 200 {
		t.Errorf("log size=%d, want 200 (ring buffer max)", len(log))
	}
}

func TestCoalescedCountsAsHit(t *testing.T) {
	s := New()
	s.Record("pr_list", "key1", ResultCoalesced, 1.0)

	snap := s.Snapshot(0, 100)
	if snap.Hits != 1 {
		t.Errorf("coalesced should count as hit: hits=%d", snap.Hits)
	}
	if snap.Coalesced != 1 {
		t.Errorf("coalesced=%d, want 1", snap.Coalesced)
	}
}
