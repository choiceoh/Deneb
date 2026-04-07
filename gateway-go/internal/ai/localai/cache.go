package localai

import (
	"crypto/sha256"
	"sync"
	"time"
)

const (
	defaultCacheTTL        = 5 * time.Minute
	defaultCacheMaxEntries = 200
	cacheJanitorInterval   = 1 * time.Minute
)

type cacheEntry struct {
	text      string
	createdAt time.Time
	lastHit   time.Time
}

// responseCache is a TTL + LRU bounded cache keyed by request content hash.
type responseCache struct {
	mu         sync.RWMutex
	entries    map[[32]byte]*cacheEntry
	defaultTTL time.Duration
	maxEntries int
}

func newResponseCache(ttl time.Duration, maxEntries int) *responseCache {
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	if maxEntries <= 0 {
		maxEntries = defaultCacheMaxEntries
	}
	return &responseCache{
		entries:    make(map[[32]byte]*cacheEntry),
		defaultTTL: ttl,
		maxEntries: maxEntries,
	}
}

// cacheKey computes a SHA-256 hash of the request's semantic identity.
func cacheKey(req *Request) [32]byte {
	h := sha256.New()
	h.Write([]byte(req.System))
	for _, m := range req.Messages {
		h.Write([]byte(m.Role))
		// Content is json.RawMessage — include raw bytes directly.
		h.Write(m.Content)
	}
	// Include maxTokens and response format in the key so requests with
	// different generation parameters don't collide.
	b := [4]byte{byte(req.MaxTokens >> 24), byte(req.MaxTokens >> 16), byte(req.MaxTokens >> 8), byte(req.MaxTokens)}
	h.Write(b[:])
	if req.ResponseFormat != nil {
		h.Write([]byte(req.ResponseFormat.Type))
	}
	var key [32]byte
	h.Sum(key[:0])
	return key
}

// Get returns a cached response if present and not expired.
func (c *responseCache) Get(req *Request, ttl time.Duration) (string, bool) {
	if ttl <= 0 {
		ttl = c.defaultTTL
	}
	key := cacheKey(req)
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok || time.Since(e.createdAt) > ttl {
		return "", false
	}
	c.mu.Lock()
	e.lastHit = time.Now()
	c.mu.Unlock()
	return e.text, true
}

// Put stores a response. If at capacity, evicts the least-recently-hit entry.
func (c *responseCache) Put(req *Request, text string) {
	key := cacheKey(req)
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = &cacheEntry{text: text, createdAt: now, lastHit: now}
	c.evictLocked()
}

// evictLocked removes entries beyond maxEntries (LRU) while holding the lock.
func (c *responseCache) evictLocked() {
	for len(c.entries) > c.maxEntries {
		var oldestKey [32]byte
		var oldestHit time.Time
		first := true
		for k, e := range c.entries {
			if first || e.lastHit.Before(oldestHit) {
				oldestKey = k
				oldestHit = e.lastHit
				first = false
			}
		}
		delete(c.entries, oldestKey)
	}
}

// Cleanup removes expired entries. Called periodically by the janitor goroutine.
func (c *responseCache) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, e := range c.entries {
		if now.Sub(e.createdAt) > c.defaultTTL {
			delete(c.entries, k)
		}
	}
}

// Len returns the current number of cached entries.
func (c *responseCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
