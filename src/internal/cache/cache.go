package cache

import (
	"container/list"
	"sync"
	"time"

	"github.com/brunoborges/ghx/src/internal/allowlist"
	"github.com/brunoborges/ghx/src/internal/protocol"
)

// Entry is a cached response.
type Entry struct {
	Key      string
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	CachedAt time.Time
	TTL      time.Duration
	Resource allowlist.ResourceType
	Host     string
	Repo     string
}

// IsExpired returns true if the entry has outlived its TTL.
func (e *Entry) IsExpired() bool {
	return time.Since(e.CachedAt) > e.TTL
}

// DefaultMaxBytes bounds retained stdout and stderr payloads to 64 MiB.
const DefaultMaxBytes int64 = 64 << 20

// Usage reports the retained response-byte budget and rejected insertions.
type Usage struct {
	Bytes    int64 `json:"cache_bytes"`
	MaxBytes int64 `json:"max_cache_bytes"`
	Rejected int64 `json:"cache_rejected"`
}

type storedEntry struct {
	entry *Entry
	bytes int64
}

// Cache is a thread-safe LRU cache with TTL support and namespace invalidation.
type Cache struct {
	mu       sync.RWMutex
	maxSize  int
	maxBytes int64
	bytes    int64
	rejected int64
	items    map[string]*list.Element
	order    *list.List // front = most recently used
	onEvict  func(key string)
}

// New creates a cache with an entry limit and the default response-byte limit.
func New(maxSize int) *Cache {
	return NewWithByteLimit(maxSize, DefaultMaxBytes)
}

// NewWithByteLimit creates a cache bounded by entries and stdout/stderr bytes.
// A nonpositive limit disables storage. Entries must not be mutated after Set.
func NewWithByteLimit(maxSize int, maxBytes int64) *Cache {
	return &Cache{
		maxSize:  maxSize,
		maxBytes: maxBytes,
		items:    make(map[string]*list.Element),
		order:    list.New(),
	}
}

// OnEvict sets a callback that fires when an entry is evicted.
func (c *Cache) OnEvict(fn func(key string)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onEvict = fn
}

// Get retrieves an entry by key. Returns nil if not found or expired.
func (c *Cache) Get(key string) *Entry {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		return nil
	}

	entry := elem.Value.(*storedEntry).entry
	if entry.IsExpired() {
		c.removeElement(elem)
		return nil
	}

	// Move to front (most recently used)
	c.order.MoveToFront(elem)
	return entry
}

// Set stores an entry in the cache.
func (c *Cache) Set(entry *Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Drop a previous value even if its replacement cannot be retained.
	if elem, ok := c.items[entry.Key]; ok {
		c.bytes -= elem.Value.(*storedEntry).bytes
		delete(c.items, entry.Key)
		c.order.Remove(elem)
	}

	bytes := int64(len(entry.Stdout)) + int64(len(entry.Stderr))
	if c.maxSize <= 0 || c.maxBytes <= 0 || bytes > c.maxBytes ||
		!protocol.ExecResponseFits(entry.Stdout, entry.Stderr, entry.ExitCode) {
		c.rejected++
		return
	}

	// Subtraction avoids overflow when checking available space.
	for c.order.Len() >= c.maxSize || c.bytes > c.maxBytes-bytes {
		c.evictOldest()
	}

	stored := *entry
	if entry.Stdout != nil {
		stored.Stdout = make([]byte, len(entry.Stdout))
		copy(stored.Stdout, entry.Stdout)
	}
	if entry.Stderr != nil {
		stored.Stderr = make([]byte, len(entry.Stderr))
		copy(stored.Stderr, entry.Stderr)
	}
	elem := c.order.PushFront(&storedEntry{entry: &stored, bytes: bytes})
	c.items[entry.Key] = elem
	c.bytes += bytes
}

// InvalidateNamespace removes all entries matching the given host, repo, and resource type.
func (c *Cache) InvalidateNamespace(host, repo string, resource allowlist.ResourceType) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	var toRemove []*list.Element
	for elem := c.order.Front(); elem != nil; elem = elem.Next() {
		entry := elem.Value.(*storedEntry).entry
		if entry.Host == host && entry.Repo == repo && entry.Resource == resource {
			toRemove = append(toRemove, elem)
		}
	}

	for _, elem := range toRemove {
		c.removeElement(elem)
	}
	return len(toRemove)
}

// Flush removes all entries from the cache.
func (c *Cache) Flush() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := c.order.Len()
	c.items = make(map[string]*list.Element)
	c.order.Init()
	c.bytes = 0
	return count
}

// Size returns the current number of cached entries.
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.order.Len()
}

// Usage returns a consistent snapshot of byte accounting and rejected inserts.
func (c *Cache) Usage() Usage {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Usage{Bytes: c.bytes, MaxBytes: c.maxBytes, Rejected: c.rejected}
}

// Keys returns all current cache keys (for debugging).
func (c *Cache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, c.order.Len())
	for elem := c.order.Front(); elem != nil; elem = elem.Next() {
		keys = append(keys, elem.Value.(*storedEntry).entry.Key)
	}
	return keys
}

func (c *Cache) evictOldest() {
	elem := c.order.Back()
	if elem != nil {
		c.removeElement(elem)
	}
}

func (c *Cache) removeElement(elem *list.Element) {
	entry := elem.Value.(*storedEntry).entry
	delete(c.items, entry.Key)
	c.order.Remove(elem)
	c.bytes -= elem.Value.(*storedEntry).bytes
	if c.onEvict != nil {
		c.onEvict(entry.Key)
	}
}
