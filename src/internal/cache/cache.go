package cache

import (
	"container/list"
	"sync"
	"time"

	"github.com/brunoborges/ghx/src/internal/allowlist"
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

// Cache is a thread-safe LRU cache with TTL support and namespace invalidation.
type Cache struct {
	mu      sync.RWMutex
	maxSize int
	items   map[string]*list.Element
	order   *list.List // front = most recently used
	onEvict func(key string)
}

// New creates a cache with the given max number of entries.
func New(maxSize int) *Cache {
	return &Cache{
		maxSize: maxSize,
		items:   make(map[string]*list.Element),
		order:   list.New(),
	}
}

// OnEvict sets a callback that fires when an entry is evicted.
func (c *Cache) OnEvict(fn func(key string)) {
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

	entry := elem.Value.(*Entry)
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

	// Update existing entry
	if elem, ok := c.items[entry.Key]; ok {
		c.order.MoveToFront(elem)
		elem.Value = entry
		return
	}

	// Evict LRU if at capacity
	for c.order.Len() >= c.maxSize {
		c.evictOldest()
	}

	elem := c.order.PushFront(entry)
	c.items[entry.Key] = elem
}

// InvalidateNamespace removes all entries matching the given host, repo, and resource type.
func (c *Cache) InvalidateNamespace(host, repo string, resource allowlist.ResourceType) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	var toRemove []*list.Element
	for elem := c.order.Front(); elem != nil; elem = elem.Next() {
		entry := elem.Value.(*Entry)
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
	return count
}

// Size returns the current number of cached entries.
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.order.Len()
}

// Keys returns all current cache keys (for debugging).
func (c *Cache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, c.order.Len())
	for elem := c.order.Front(); elem != nil; elem = elem.Next() {
		keys = append(keys, elem.Value.(*Entry).Key)
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
	entry := elem.Value.(*Entry)
	delete(c.items, entry.Key)
	c.order.Remove(elem)
	if c.onEvict != nil {
		c.onEvict(entry.Key)
	}
}
