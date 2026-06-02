package cache

import (
	"testing"
	"time"

	"github.com/brunoborges/ghx/src/internal/allowlist"
)

func TestGetSetBasic(t *testing.T) {
	c := New(10)
	c.Set(&Entry{
		Key:      "k1",
		Stdout:   []byte("hello"),
		ExitCode: 0,
		CachedAt: time.Now(),
		TTL:      10 * time.Second,
	})

	e := c.Get("k1")
	if e == nil {
		t.Fatal("expected entry, got nil")
	}
	if string(e.Stdout) != "hello" {
		t.Fatalf("expected 'hello', got %q", string(e.Stdout))
	}
}

func TestTTLExpiry(t *testing.T) {
	c := New(10)
	c.Set(&Entry{
		Key:      "k1",
		Stdout:   []byte("old"),
		CachedAt: time.Now().Add(-5 * time.Second),
		TTL:      2 * time.Second,
	})

	if e := c.Get("k1"); e != nil {
		t.Fatal("expected nil for expired entry")
	}
}

func TestLRUEviction(t *testing.T) {
	c := New(2)
	c.Set(&Entry{Key: "a", CachedAt: time.Now(), TTL: time.Minute})
	c.Set(&Entry{Key: "b", CachedAt: time.Now(), TTL: time.Minute})
	c.Set(&Entry{Key: "c", CachedAt: time.Now(), TTL: time.Minute})

	// "a" should have been evicted (LRU)
	if c.Get("a") != nil {
		t.Fatal("expected 'a' to be evicted")
	}
	if c.Get("b") == nil {
		t.Fatal("expected 'b' to exist")
	}
	if c.Get("c") == nil {
		t.Fatal("expected 'c' to exist")
	}
}

func TestInvalidateNamespace(t *testing.T) {
	c := New(100)
	c.Set(&Entry{Key: "pr1", Host: "github.com", Repo: "o/r", Resource: allowlist.ResourcePR, CachedAt: time.Now(), TTL: time.Minute})
	c.Set(&Entry{Key: "pr2", Host: "github.com", Repo: "o/r", Resource: allowlist.ResourcePR, CachedAt: time.Now(), TTL: time.Minute})
	c.Set(&Entry{Key: "issue1", Host: "github.com", Repo: "o/r", Resource: allowlist.ResourceIssue, CachedAt: time.Now(), TTL: time.Minute})

	n := c.InvalidateNamespace("github.com", "o/r", allowlist.ResourcePR)
	if n != 2 {
		t.Fatalf("expected 2 invalidated, got %d", n)
	}
	if c.Get("pr1") != nil || c.Get("pr2") != nil {
		t.Fatal("PR entries should be gone")
	}
	if c.Get("issue1") == nil {
		t.Fatal("issue entry should still exist")
	}
}

func TestFlushAll(t *testing.T) {
	c := New(100)
	c.Set(&Entry{Key: "a", CachedAt: time.Now(), TTL: time.Minute})
	c.Set(&Entry{Key: "b", CachedAt: time.Now(), TTL: time.Minute})

	n := c.Flush()
	if n != 2 {
		t.Fatalf("expected 2 flushed, got %d", n)
	}
	if c.Size() != 0 {
		t.Fatal("cache should be empty")
	}
}
