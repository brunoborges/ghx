package metrics

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestMissReasonsAreAtomicBoundedAndMetadataOnly(t *testing.T) {
	s := New()
	var wg sync.WaitGroup
	for _, reason := range []string{"bypass", "expired", "absent", "private-unknown-reason"} {
		for i := 0; i < 25; i++ {
			wg.Add(1)
			go func(reason string) { defer wg.Done(); s.Record("pr_view", "key", ResultMiss, 1, reason) }(reason)
		}
	}
	wg.Wait()
	s.Record("pr_view", "key", ResultCoalesced, 1, "absent")
	snap := s.Snapshot(1, 10)
	if snap.MissReasons != (MissReasons{Bypass: 25, Expired: 25, Absent: 25, Unspecified: 25}) || snap.Misses != 100 || snap.Hits != 1 {
		t.Fatalf("snapshot = %+v", snap)
	}
	data, _ := json.Marshal(snap)
	if strings.Contains(string(data), "private-unknown-reason") || strings.Contains(string(data), `"args"`) {
		t.Fatal("reason counters must not expose arbitrary strings or arguments")
	}
	for _, row := range s.Log(200) {
		if row.Args != nil || row.Fetch != nil {
			t.Fatal("aggregate counters must work without argument logging")
		}
	}
}
