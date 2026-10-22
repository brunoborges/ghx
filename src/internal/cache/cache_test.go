package cache

import (
	"os"
	"sync"
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

// The byte-budget tests retain real repository bytes rather than generated output.
func budgetEntry(t *testing.T, key string, n int) *Entry {
	t.Helper()
	data, err := os.ReadFile("../../../LICENSE")
	if err != nil {
		t.Fatal(err)
	}
	return &Entry{Key: key, Stdout: append([]byte(nil), data[:n]...), CachedAt: time.Now(), TTL: time.Minute}
}

func TestByteBudgetLRU(t *testing.T) {
	c := NewWithByteLimit(10, 6)
	c.Set(budgetEntry(t, "a", 3))
	c.Set(budgetEntry(t, "b", 3))
	c.Get("a") // Reading a makes b the eviction candidate.
	c.Set(budgetEntry(t, "c", 3))
	if c.Get("a") == nil || c.Get("b") != nil || c.Get("c") == nil {
		t.Fatal("byte pressure must evict the least recently used entry")
	}
	if got := c.Usage(); got.Bytes != 6 || got.MaxBytes != 6 || got.Rejected != 0 {
		t.Fatalf("usage = %+v", got)
	}
}

func TestByteBudgetReplacement(t *testing.T) {
	c := NewWithByteLimit(10, 6)
	c.Set(budgetEntry(t, "a", 3))
	c.Set(budgetEntry(t, "b", 3))
	c.Set(budgetEntry(t, "a", 5))
	if c.Get("b") != nil || c.Size() != 1 || c.Usage().Bytes != 5 {
		t.Fatal("growing a replacement must evict other entries and release old bytes")
	}
	c.Set(budgetEntry(t, "a", 1))
	if c.Usage().Bytes != 1 {
		t.Fatal("shrinking a replacement must release old bytes")
	}
	c.Set(budgetEntry(t, "b", 3))
	c.Set(budgetEntry(t, "a", 7))
	if c.Get("a") != nil || c.Get("b") == nil || c.Usage().Bytes != 3 || c.Usage().Rejected != 1 {
		t.Fatal("oversized replacement must drop the old value and preserve other entries")
	}
}

func TestByteBudgetOwnsResponseBuffers(t *testing.T) {
	c := NewWithByteLimit(1, 6)
	entry := &Entry{
		Key:      "response",
		Stdout:   make([]byte, 3, 1<<20),
		Stderr:   make([]byte, 3, 1<<20),
		CachedAt: time.Now(),
		TTL:      time.Minute,
	}
	copy(entry.Stdout, "out")
	copy(entry.Stderr, "err")
	c.Set(entry)

	got := c.Get(entry.Key)
	if got == nil {
		t.Fatal("response at the byte limit must be retained")
	}
	if got == entry {
		t.Fatal("cache must own a copy of the entry")
	}
	for name, data := range map[string][]byte{"stdout": got.Stdout, "stderr": got.Stderr} {
		if cap(data) != len(data) {
			t.Errorf("%s capacity = %d, want %d; cache must not retain oversized backing arrays", name, cap(data), len(data))
		}
	}

	entry.Stdout[0] = 'X'
	entry.Stderr[0] = 'Y'
	if string(got.Stdout) != "out" || string(got.Stderr) != "err" {
		t.Fatalf("cached buffers alias the input: stdout = %q, stderr = %q", got.Stdout, got.Stderr)
	}
	if usage := c.Usage(); usage.Bytes != 6 || usage.Rejected != 0 {
		t.Fatalf("usage = %+v, want 6 bytes and no rejections", usage)
	}
}

func TestByteBudgetEvictsMultipleEntries(t *testing.T) {
	c := NewWithByteLimit(10, 8)
	for _, key := range []string{"a", "b", "c"} {
		c.Set(budgetEntry(t, key, 2))
	}
	c.Set(budgetEntry(t, "large", 7))
	for _, key := range []string{"a", "b", "c"} {
		if c.Get(key) != nil {
			t.Errorf("%q must be evicted to fit the larger response", key)
		}
	}
	if c.Get("large") == nil || c.Size() != 1 || c.Usage().Bytes != 7 {
		t.Fatalf("size = %d, usage = %+v; want one entry using 7 bytes", c.Size(), c.Usage())
	}
}

func TestEntryLimitReleasesResponseBytes(t *testing.T) {
	c := NewWithByteLimit(2, 100)
	c.Set(budgetEntry(t, "a", 3))
	c.Set(budgetEntry(t, "b", 2))
	c.Set(budgetEntry(t, "c", 4))
	if c.Get("a") != nil || c.Get("b") == nil || c.Get("c") == nil {
		t.Fatal("entry pressure must evict only the least recently used entry")
	}
	if usage := c.Usage(); usage.Bytes != 6 || usage.Rejected != 0 {
		t.Fatalf("usage = %+v, want 6 bytes and no rejections", usage)
	}
}

func TestProtocolOversizedResponsesPreserveCache(t *testing.T) {
	payload := make([]byte, 8<<20)
	for _, tt := range []struct {
		name       string
		maxEntries int
		maxBytes   int64
		key        string
	}{
		{name: "byte pressure", maxEntries: 10, maxBytes: int64(len(payload)) + 2, key: "large"},
		{name: "entry pressure", maxEntries: 2, maxBytes: DefaultMaxBytes, key: "large"},
		{name: "replacement", maxEntries: 10, maxBytes: int64(len(payload)) + 2, key: "a"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := NewWithByteLimit(tt.maxEntries, tt.maxBytes)
			c.Set(budgetEntry(t, "a", 3))
			c.Set(budgetEntry(t, "b", 3))
			var evictions int
			c.OnEvict(func(string) { evictions++ })
			c.Set(&Entry{
				Key: tt.key, Stdout: payload,
				CachedAt: time.Now(), TTL: time.Minute,
			})
			if c.Get(tt.key) != nil {
				t.Fatal("response over the encoded IPC limit must not be cached")
			}
			if c.Get("b") == nil || (tt.key != "a" && c.Get("a") == nil) {
				t.Fatal("rejected response must preserve unrelated entries")
			}
			wantSize := 2
			wantBytes := int64(6)
			if tt.key == "a" {
				wantSize, wantBytes = 1, 3
			}
			wantUsage := Usage{Bytes: wantBytes, MaxBytes: tt.maxBytes, Rejected: 1}
			if c.Size() != wantSize || c.Usage() != wantUsage || evictions != 0 {
				t.Fatalf("size = %d, usage = %+v, evictions = %d; want size %d, usage %+v, no evictions",
					c.Size(), c.Usage(), evictions, wantSize, wantUsage)
			}
		})
	}
}

func TestByteBudgetCountsStderrAndReleases(t *testing.T) {
	c := NewWithByteLimit(2, 6)
	a := budgetEntry(t, "a", 3)
	a.Stderr = a.Stdout
	c.Set(a)
	if c.Usage().Bytes != 6 {
		t.Fatal("stderr must count toward the response budget")
	}
	c.Set(budgetEntry(t, "b", 1))
	if c.Usage().Bytes != 1 {
		t.Fatal("eviction must release stdout and stderr bytes")
	}
	b := c.Get("b")
	b.CachedAt = time.Now().Add(-2 * time.Minute)
	if c.Get("b") != nil || c.Usage().Bytes != 0 {
		t.Fatal("expiry must release bytes")
	}
	c.Set(a)
	c.InvalidateNamespace("", "", a.Resource)
	if c.Usage().Bytes != 0 {
		t.Fatal("invalidation must release bytes")
	}
	c.Set(a)
	c.Flush()
	if c.Usage().Bytes != 0 {
		t.Fatal("flush must release bytes")
	}
}

func TestNonpositiveCapacityDisablesStorage(t *testing.T) {
	for _, limits := range []struct {
		entries int
		bytes   int64
	}{{0, 6}, {-1, 6}, {1, 0}, {1, -1}} {
		c := NewWithByteLimit(limits.entries, limits.bytes)
		c.Set(budgetEntry(t, "a", 1))
		if c.Get("a") != nil || c.Size() != 0 || c.Usage().Bytes != 0 || c.Usage().Rejected != 1 {
			t.Fatalf("nonpositive capacity %+v must disable storage", limits)
		}
	}
}

func TestByteBudgetConcurrent(t *testing.T) {
	c := NewWithByteLimit(3, 6)
	entry := budgetEntry(t, "same", 2)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				c.Set(entry)
				c.Get(entry.Key)
				c.Usage()
			}
		})
	}
	wg.Wait()
	if c.Usage().Bytes != 2 || c.Size() != 1 {
		t.Fatal("concurrent replacement must preserve accounting")
	}
}

func TestByteBudgetConcurrentEviction(t *testing.T) {
	for _, tt := range []struct {
		name     string
		maxBytes int64
	}{
		{name: "entry pressure", maxBytes: 100},
		{name: "byte pressure", maxBytes: 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := NewWithByteLimit(3, tt.maxBytes)
			var entries []*Entry
			for i, key := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
				entry := budgetEntry(t, key, i%3+1)
				entry.Host = "github.com"
				entry.Repo = "o/r"
				entry.Resource = allowlist.ResourcePR
				if i%2 == 0 {
					entry.Resource = allowlist.ResourceIssue
				}
				entries = append(entries, entry)
			}

			start := make(chan struct{})
			var wg sync.WaitGroup
			for worker := range len(entries) {
				wg.Go(func() {
					<-start
					for i := range 100 {
						entry := entries[(worker+i)%len(entries)]
						c.Set(entry)
						c.Get(entry.Key)
						if i%7 == 0 {
							c.InvalidateNamespace("github.com", "o/r", allowlist.ResourcePR)
						}
						if i%13 == 0 {
							c.Flush()
						}
						if usage := c.Usage(); usage.Bytes < 0 || usage.Bytes > tt.maxBytes || usage.Rejected != 0 {
							t.Errorf("usage outside the budget: %+v", usage)
							return
						}
						if size := c.Size(); size < 0 || size > 3 {
							t.Errorf("size = %d, want at most 3 entries", size)
							return
						}
					}
				})
			}
			close(start)
			wg.Wait()

			var retained int64
			keys := c.Keys()
			for _, key := range keys {
				entry := c.Get(key)
				if entry == nil {
					t.Fatalf("retained key %q has no entry", key)
				}
				retained += int64(len(entry.Stdout)) + int64(len(entry.Stderr))
			}
			if usage := c.Usage(); usage.Bytes != retained {
				t.Fatalf("accounted bytes = %d, retained payload bytes = %d", usage.Bytes, retained)
			}
			if c.Size() != len(keys) {
				t.Fatalf("size = %d, want %d retained keys", c.Size(), len(keys))
			}
		})
	}
}

func TestInvalidateRepo(t *testing.T) {
	c := New(10)
	for _, e := range []*Entry{
		{Key: "a", Host: "h", Repo: "o/r", Resource: allowlist.ResourcePR},
		{Key: "b", Host: "h", Repo: "o/r", Resource: allowlist.ResourceAPI},
		{Key: "c", Host: "h", Repo: "o/x", Resource: allowlist.ResourcePR},
	} {
		e.TTL, e.CachedAt = time.Minute, time.Now()
		c.Set(e)
	}
	if n := c.InvalidateRepo("h", "o/r"); n != 2 || c.Get("c") == nil || c.Size() != 1 {
		t.Fatalf("InvalidateRepo removed %d, keys=%v", n, c.Keys())
	}
}
