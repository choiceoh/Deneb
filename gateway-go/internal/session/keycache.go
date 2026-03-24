// Session key cache for resolving session keys by run ID.
//
// Mirrors the LRU cache logic in src/gateway/server-session-key.ts:
// three-tier lookup (agent context → LRU map → file store), with
// negative-result caching (1s TTL) to prevent thundering herd.
package session

import (
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
type KeyCache struct {
	mu      sync.Mutex
	entries map[string]*keyCacheEntry
	order   []string // insertion order for FIFO eviction
}

// NewKeyCache creates a new session key cache.
func NewKeyCache() *KeyCache {
	return &KeyCache{
		entries: make(map[string]*keyCacheEntry, KeyCacheLimit),
	}
}

// Get looks up a session key by run ID.
// Returns (sessionKey, true) on cache hit, ("", false) on miss or expired.
func (c *KeyCache) Get(runID string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[runID]
	if !ok {
		return "", false
	}

	// Positive cache entries never expire.
	if entry.isHit {
		return entry.sessionKey, true
	}

	// Negative cache entries expire after TTL.
	if time.Now().Before(entry.expiresAt) {
		// Still within TTL — return "known miss" as empty hit.
		return "", true
	}

	// Expired negative entry — remove and report miss.
	delete(c.entries, runID)
	c.removeFromOrder(runID)
	return "", false
}

// Put stores a positive session key mapping.
func (c *KeyCache) Put(runID, sessionKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictIfNeeded(runID)
	c.entries[runID] = &keyCacheEntry{
		sessionKey: sessionKey,
		isHit:      true,
	}
	c.appendOrder(runID)
}

// PutMiss stores a negative (miss) result with TTL.
func (c *KeyCache) PutMiss(runID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictIfNeeded(runID)
	c.entries[runID] = &keyCacheEntry{
		isHit:     false,
		expiresAt: time.Now().Add(KeyCacheMissTTL),
	}
	c.appendOrder(runID)
}

// Len returns the number of entries in the cache.
func (c *KeyCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// Clear removes all entries.
func (c *KeyCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*keyCacheEntry, KeyCacheLimit)
	c.order = c.order[:0]
}

// evictIfNeeded removes the oldest entry if at capacity (FIFO).
// Must be called with mu held.
func (c *KeyCache) evictIfNeeded(newKey string) {
	if _, exists := c.entries[newKey]; exists {
		return // updating existing entry, no eviction needed
	}
	if len(c.entries) >= KeyCacheLimit && len(c.order) > 0 {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
}

func (c *KeyCache) appendOrder(key string) {
	// Remove existing occurrence to prevent duplicates.
	c.removeFromOrder(key)
	c.order = append(c.order, key)
}

func (c *KeyCache) removeFromOrder(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}
