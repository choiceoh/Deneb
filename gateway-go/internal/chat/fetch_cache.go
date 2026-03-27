// fetch_cache.go — Simple in-memory TTL cache for web_fetch results.
//
// Caches the full converted content keyed by URL. Truncation to maxChars
// happens at retrieval time so different maxChars values share cache entries.
// Single-user deployment: sync.Mutex is sufficient.
// Uses a doubly-linked list + map index for O(1) insert/remove/evict.
package chat

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

// FetchCache is a bounded TTL cache for web_fetch results.
type FetchCache struct {
	mu      sync.Mutex
	items   map[string]*fetchCacheItem
	order   *list.List // front = oldest, back = newest
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
		items:   make(map[string]*fetchCacheItem, maxSize),
		order:   list.New(),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// Get returns cached content for the URL if it exists and hasn't expired.
func (c *FetchCache) Get(url string) (string, bool) {
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
	return item.entry.content, true
}

// Put stores content for the URL. Evicts the oldest entry if at capacity.
func (c *FetchCache) Put(url string, content string) {
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
		oldest := front.Value.(string)
		c.order.Remove(front)
		delete(c.items, oldest)
	}

	elem := c.order.PushBack(url)
	c.items[url] = &fetchCacheItem{entry: entry, element: elem}
}

// removeLocked removes an entry by key. Must be called with mu held.
func (c *FetchCache) removeLocked(url string) {
	item, ok := c.items[url]
	if !ok {
		return
	}
	c.order.Remove(item.element)
	delete(c.items, url)
}
