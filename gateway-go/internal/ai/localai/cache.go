package localai

import (
	"crypto/sha256"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/corecache"
)

const (
	defaultCacheTTL        = 5 * time.Minute
	defaultCacheMaxEntries = 200
	cacheJanitorInterval   = 1 * time.Minute
)

type cachedResponse struct {
	text      string
	createdAt time.Time
}

type responseCache struct {
	lru        *corecache.LRU[[32]byte, cachedResponse]
	defaultTTL time.Duration
}

func newResponseCache(ttl time.Duration, maxEntries int) *responseCache {
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	if maxEntries <= 0 {
		maxEntries = defaultCacheMaxEntries
	}
	return &responseCache{
		lru:        corecache.NewLRU[[32]byte, cachedResponse](maxEntries, 0),
		defaultTTL: ttl,
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
	b := [4]byte{byte(req.MaxTokens >> 24), byte(req.MaxTokens >> 16), byte(req.MaxTokens >> 8), byte(req.MaxTokens)} //nolint:gosec // G115 — extracting individual bytes from int for hashing
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
	entry, ok := c.lru.Get(key)
	if !ok || time.Since(entry.createdAt) > ttl {
		return "", false
	}
	return entry.text, true
}

// Put stores a response.
func (c *responseCache) Put(req *Request, text string) {
	key := cacheKey(req)
	c.lru.Put(key, cachedResponse{text: text, createdAt: time.Now()})
}

// Cleanup removes expired entries. Called periodically by the janitor goroutine.
func (c *responseCache) Cleanup() {
	c.lru.PruneFunc(func(_ [32]byte, v cachedResponse) bool {
		return time.Since(v.createdAt) > c.defaultTTL
	})
}

// Len returns the current number of cached entries.
func (c *responseCache) Len() int {
	return c.lru.Len()
}
