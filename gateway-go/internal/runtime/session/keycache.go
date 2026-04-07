// Session key cache for resolving session keys by run ID.
//
// Mirrors the LRU cache logic in src/gateway/server-session-key.ts:
// three-tier lookup (agent context → LRU map → file store), with
// negative-result caching (1s TTL) to prevent thundering herd.
package session

import (
	"container/list"
	"sync"
	"time"
)

const (
	// KeyCacheLimit is the maximum number of entries before LRU eviction.
	KeyCacheLimit = 256
	// KeyCacheMissTTL is how long a negative (miss) result is cached.
	KeyCacheMissTTL = 1 * time.Second
)

type keyCacheEntry struct {
	sessionKey string // empty string means negative cache (miss)
	expiresAt  time.Time
	isHit      bool // true = positive result, false = negative (miss)
}

// KeyCache is an LRU cache that maps run IDs to session keys.
// Thread-safe for concurrent access.
// Uses a doubly-linked list + map index for O(1) insert/remove/evict.
type KeyCache struct {
	mu    sync.Mutex
	items map[string]*keyCacheItem
	order *list.List // front = oldest, back = newest
}

// keyCacheItem pairs the cache entry with its position in the LRU list.
type keyCacheItem struct {
	entry   *keyCacheEntry
	element *list.Element
}

// NewKeyCache creates a new session key cache.
func NewKeyCache() *KeyCache {
	return &KeyCache{
		items: make(map[string]*keyCacheItem, KeyCacheLimit),
		order: list.New(),
	}
}

// Get looks up a session key by run ID. Three possible outcomes:
//   - (sessionKey, true): positive cache hit — run ID maps to a known session.
//   - ("", true): negative cache hit — run ID was recently looked up and not found (within TTL).
//   - ("", false): cache miss — run ID is unknown or the negative entry expired.
func (c *KeyCache) Get(runID string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	item, ok := c.items[runID]
	if !ok {
		return "", false
	}

	// Positive cache entries never expire.
	if item.entry.isHit {
		return item.entry.sessionKey, true
	}

	// Negative cache entries expire after TTL.
	if time.Now().Before(item.entry.expiresAt) {
		// Still within TTL — return "known miss" as empty hit.
		return "", true
	}

	// Expired negative entry — remove and report miss.
	c.removeLocked(runID)
	return "", false
}

// Put stores a positive session key mapping.
func (c *KeyCache) Put(runID, sessionKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictIfNeeded(runID)
	c.setLocked(runID, &keyCacheEntry{
		sessionKey: sessionKey,
		isHit:      true,
	})
}

// PutMiss stores a negative (miss) result with TTL.
func (c *KeyCache) PutMiss(runID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictIfNeeded(runID)
	c.setLocked(runID, &keyCacheEntry{
		isHit:     false,
		expiresAt: time.Now().Add(KeyCacheMissTTL),
	})
}

// Len returns the number of entries in the cache.
func (c *KeyCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

// Clear removes all entries.
func (c *KeyCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*keyCacheItem, KeyCacheLimit)
	c.order.Init()
}

// setLocked inserts or updates an entry, moving it to the back (newest).
// Must be called with mu held.
func (c *KeyCache) setLocked(key string, entry *keyCacheEntry) {
	if item, exists := c.items[key]; exists {
		item.entry = entry
		c.order.MoveToBack(item.element)
		return
	}
	elem := c.order.PushBack(key)
	c.items[key] = &keyCacheItem{entry: entry, element: elem}
}

// removeLocked removes an entry by key. Must be called with mu held.
func (c *KeyCache) removeLocked(key string) {
	item, ok := c.items[key]
	if !ok {
		return
	}
	c.order.Remove(item.element)
	delete(c.items, key)
}

// evictIfNeeded removes the oldest entry if at capacity (FIFO).
// Must be called with mu held.
func (c *KeyCache) evictIfNeeded(newKey string) {
	if _, exists := c.items[newKey]; exists {
		return // updating existing entry, no eviction needed
	}
	if len(c.items) >= KeyCacheLimit {
		front := c.order.Front()
		if front != nil {
			oldest := front.Value.(string)
			c.order.Remove(front)
			delete(c.items, oldest)
		}
	}
}
