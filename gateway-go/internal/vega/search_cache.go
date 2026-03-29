// search_cache.go — In-memory TTL cache for Vega search results.
//
// Caches the full search result set keyed by query+limit hash.
// Avoids redundant SGLang embedding, query expansion, and Rust FTS calls
// when the same query is repeated within the TTL window.
// Single-user deployment: sync.Mutex is sufficient.
package vega

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// CPU architecture analogy: L4 cross-session cache. Single-user deployment with
// infrequent knowledge base updates means we can cache more aggressively.
// Increased from 32 entries/3min to 64 entries/10min for better hit rates
// on repeated similar queries within and across sessions.
const (
	searchCacheMaxSize = 64
	searchCacheTTL     = 10 * time.Minute
)

type searchCacheEntry struct {
	results   []SearchResult
	createdAt time.Time
}

type searchCacheItem struct {
	entry   *searchCacheEntry
	element *list.Element
}

// searchCache is a bounded TTL cache for search results.
type searchCache struct {
	mu    sync.Mutex
	items map[string]*searchCacheItem
	order *list.List // front = oldest, back = newest
}

func newSearchCache() *searchCache {
	return &searchCache{
		items: make(map[string]*searchCacheItem, searchCacheMaxSize),
		order: list.New(),
	}
}

// get returns cached results for the query key if they exist and haven't expired.
func (c *searchCache) get(key string) ([]SearchResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	item, ok := c.items[key]
	if !ok {
		return nil, false
	}
	if time.Since(item.entry.createdAt) > searchCacheTTL {
		// Expired — lazy delete.
		c.removeLocked(key)
		return nil, false
	}
	// Return a copy to prevent mutation of cached data.
	copied := make([]SearchResult, len(item.entry.results))
	copy(copied, item.entry.results)
	return copied, true
}

// put stores search results for the query key.
func (c *searchCache) put(key string, results []SearchResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Copy results to prevent caller mutation.
	stored := make([]SearchResult, len(results))
	copy(stored, results)

	entry := &searchCacheEntry{results: stored, createdAt: time.Now()}

	// Update existing entry.
	if item, exists := c.items[key]; exists {
		item.entry = entry
		c.order.MoveToBack(item.element)
		return
	}

	// Evict oldest if at capacity.
	for len(c.items) >= searchCacheMaxSize {
		front := c.order.Front()
		if front == nil {
			break
		}
		oldest := front.Value.(string)
		c.order.Remove(front)
		delete(c.items, oldest)
	}

	elem := c.order.PushBack(key)
	c.items[key] = &searchCacheItem{entry: entry, element: elem}
}

func (c *searchCache) removeLocked(key string) {
	item, ok := c.items[key]
	if !ok {
		return
	}
	c.order.Remove(item.element)
	delete(c.items, key)
}

// searchCacheKey builds a cache key from query text and search options.
func searchCacheKey(query string, opts SearchOpts) string {
	raw := fmt.Sprintf("%s|%d|%d|%s", query, opts.Limit, opts.Offset, opts.Mode)
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:16]) // 128-bit prefix is sufficient
}
