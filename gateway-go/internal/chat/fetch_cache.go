// fetch_cache.go — Simple in-memory TTL cache for web_fetch results.
//
// Caches the full converted content keyed by URL. Truncation to maxChars
// happens at retrieval time so different maxChars values share cache entries.
// Single-user deployment: sync.Mutex is sufficient.
package chat

import (
	"sync"
	"time"
)

const (
	fetchCacheDefaultMaxSize = 64
	fetchCacheDefaultTTL     = 5 * time.Minute
)

type fetchCacheEntry struct {
	content   string
	createdAt time.Time
}

// FetchCache is a bounded TTL cache for web_fetch results.
type FetchCache struct {
	mu      sync.Mutex
	entries map[string]*fetchCacheEntry
	order   []string // insertion order for FIFO eviction
	maxSize int
	ttl     time.Duration
}

// NewFetchCache creates a cache with default size (64) and TTL (5 min).
func NewFetchCache() *FetchCache {
	return NewFetchCacheWithTTL(fetchCacheDefaultMaxSize, fetchCacheDefaultTTL)
}

// NewFetchCacheWithTTL creates a cache with the given size and TTL.
func NewFetchCacheWithTTL(maxSize int, ttl time.Duration) *FetchCache {
	if maxSize <= 0 {
		maxSize = fetchCacheDefaultMaxSize
	}
	return &FetchCache{
		entries: make(map[string]*fetchCacheEntry, maxSize),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// Get returns cached content for the URL if it exists and hasn't expired.
func (c *FetchCache) Get(url string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[url]
	if !ok {
		return "", false
	}
	if time.Since(entry.createdAt) > c.ttl {
		// Expired — lazy delete.
		delete(c.entries, url)
		c.removeFromOrder(url)
		return "", false
	}
	return entry.content, true
}

// Put stores content for the URL. Evicts the oldest entry if at capacity.
func (c *FetchCache) Put(url string, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Update existing entry in-place (no order change).
	if _, exists := c.entries[url]; exists {
		c.entries[url] = &fetchCacheEntry{content: content, createdAt: time.Now()}
		return
	}

	// Evict oldest if at capacity.
	for len(c.entries) >= c.maxSize && len(c.order) > 0 {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}

	c.entries[url] = &fetchCacheEntry{content: content, createdAt: time.Now()}
	c.order = append(c.order, url)
}

func (c *FetchCache) removeFromOrder(url string) {
	for i, u := range c.order {
		if u == url {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}
