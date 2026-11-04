package cache

import (
	"testing"
	"time"
)

func TestLookupAndRemovalReasons(t *testing.T) {
	c := NewWithByteLimit(1, 100)
	if e, reason := c.GetWithStatus("new"); e != nil || reason != LookupAbsent {
		t.Fatalf("cold lookup = %v, %s", e, reason)
	}
	c.Set(&Entry{Key: "expired", CachedAt: time.Now().Add(-time.Minute), TTL: time.Second})
	if e, reason := c.GetWithStatus("expired"); e != nil || reason != LookupExpired {
		t.Fatalf("expired lookup = %v, %s", e, reason)
	}
	entry := func(key string) *Entry {
		return &Entry{Key: key, Host: "github.com", Repo: "o/r", CachedAt: time.Now(), TTL: time.Minute}
	}
	c.Set(entry("a"))
	c.Set(entry("b"))
	if e, reason := c.GetWithStatus("b"); e == nil || reason != LookupHit {
		t.Fatal("live lookup must hit")
	}
	if c.InvalidateRepo("github.com", "o/r") != 1 {
		t.Fatal("expected one invalidated entry")
	}
	if e, reason := c.GetWithStatus("b"); e != nil || reason != LookupAbsent {
		t.Fatal("removed keys are absent; do not claim cold or retain key history")
	}
	c.Set(entry("c"))
	c.Flush()
	want := RemovalReasons{Expired: 1, Capacity: 1, Invalidated: 1, Flushed: 1}
	if got := c.Usage().RemovalReasons; got != want {
		t.Fatalf("removals = %+v, want %+v", got, want)
	}
	snapshot := c.Usage()
	snapshot.RemovalReasons.Expired++
	if c.Usage().RemovalReasons != want {
		t.Fatal("usage must not expose mutable counter state")
	}
}
