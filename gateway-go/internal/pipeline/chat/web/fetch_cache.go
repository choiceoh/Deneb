// fetch_cache.go — Simple in-memory TTL cache for web_fetch results.
//
// Caches the full converted content keyed by URL. Truncation to maxChars
// happens at retrieval time so different maxChars values share cache entries.
// Single-user deployment: sync.Mutex is sufficient.
// Uses a doubly-linked list + map index for O(1) insert/remove/evict.
package web

import (
	"container/list"
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

// fetchCacheItem pairs the cache entry with its position in the LRU list.
type fetchCacheItem struct {
	entry   *fetchCacheEntry
	element *list.Element
}

// fetchCache is a bounded TTL cache for web_fetch results.
type fetchCache struct {
	mu      sync.Mutex
	items   map[string]*fetchCacheItem
	order   *list.List // front = oldest, back = newest
	maxSize int
	ttl     time.Duration
}

// NewFetchCache creates a cache with default size (64) and TTL (5 min).
func NewFetchCache() *fetchCache {
	return newFetchCacheWithTTL(fetchCacheDefaultMaxSize, fetchCacheDefaultTTL)
}

// newFetchCacheWithTTL creates a cache with the given size and TTL.
func newFetchCacheWithTTL(maxSize int, ttl time.Duration) *fetchCache {
	if maxSize <= 0 {
		maxSize = fetchCacheDefaultMaxSize
	}
	return &fetchCache{
		items:   make(map[string]*fetchCacheItem, maxSize),
		order:   list.New(),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// Get returns cached content for the URL if it exists and hasn't expired.
func (c *fetchCache) Get(url string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	item, ok := c.items[url]
	if !ok {
		return "", false
	}
	if time.Since(item.entry.createdAt) > c.ttl {
		// Expired — lazy delete.
		c.removeLocked(url)
		return "", false
	}
	// Promote on hit so frequently read URLs survive eviction.
	c.order.MoveToBack(item.element)
	return item.entry.content, true
}

// Put stores content for the URL. Evicts the oldest entry if at capacity.
func (c *fetchCache) Put(url, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := &fetchCacheEntry{content: content, createdAt: time.Now()}

	// Update existing entry: refresh data and move to back (newest).
	if item, exists := c.items[url]; exists {
		item.entry = entry
		c.order.MoveToBack(item.element)
		return
	}

	// Evict oldest if at capacity.
	for len(c.items) >= c.maxSize {
		front := c.order.Front()
		if front == nil {
			break
		}
		oldest, _ := front.Value.(string) //nolint:errcheck // best-effort
		c.order.Remove(front)
		delete(c.items, oldest)
	}

	elem := c.order.PushBack(url)
	c.items[url] = &fetchCacheItem{entry: entry, element: elem}
}

// removeLocked removes an entry by key. Must be called with mu held.
func (c *fetchCache) removeLocked(url string) {
	item, ok := c.items[url]
	if !ok {
		return
	}
	c.order.Remove(item.element)
	delete(c.items, url)
}
