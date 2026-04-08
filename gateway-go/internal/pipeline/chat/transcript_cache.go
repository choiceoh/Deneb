// transcript_cache.go — TTL cache decorator for TranscriptStore.
//
// Wraps any TranscriptStore and caches Load results per session key,
// eliminating redundant JSONL file reads during a single agent run
// (context assembly + compaction can trigger 2-4 loads of the same session).
// Write-through on Append keeps the cache consistent without re-reading.
package chat

import (
	"sync"
	"time"
)

// defaultTranscriptCacheTTL is extended to 60s for single-user DGX Spark
// deployment where no other process modifies transcripts. Reduces redundant
// JSONL reads during long tool executions (web fetch 30s+) that would
// previously expire the 10s cache between context assembly loads.
const defaultTranscriptCacheTTL = 60 * time.Second

type transcriptCacheEntry struct {
	msgs      []ChatMessage
	total     int
	expiresAt time.Time
}

// Compile-time interface compliance.
var _ TranscriptStore = (*CachedTranscriptStore)(nil)

// CachedTranscriptStore wraps a TranscriptStore with a per-session TTL cache.
type CachedTranscriptStore struct {
	inner TranscriptStore
	mu    sync.Mutex
	cache map[string]*transcriptCacheEntry
	ttl   time.Duration
}

// NewCachedTranscriptStore creates a caching decorator around inner.
func NewCachedTranscriptStore(inner TranscriptStore, ttl time.Duration) *CachedTranscriptStore {
	if ttl <= 0 {
		ttl = defaultTranscriptCacheTTL
	}
	return &CachedTranscriptStore{
		inner: inner,
		cache: make(map[string]*transcriptCacheEntry),
		ttl:   ttl,
	}
}

// Load returns messages for the session. On cache hit, returns a copy
// with limit slicing applied. On miss, delegates to the inner store,
// caches the full result, then applies limit.
func (c *CachedTranscriptStore) Load(sessionKey string, limit int) ([]ChatMessage, int, error) { //nolint:gocritic // unnamedResult — naming would shadow local vars
	c.mu.Lock()
	entry, ok := c.cache[sessionKey]
	if ok && time.Now().Before(entry.expiresAt) {
		msgs := entry.msgs
		total := entry.total
		c.mu.Unlock()
		return applyLimit(msgs, limit), total, nil
	}
	c.mu.Unlock()

	// Cache miss or expired — load from inner store (full, no limit).
	msgs, total, err := c.inner.Load(sessionKey, 0)
	if err != nil {
		return nil, 0, err
	}

	c.mu.Lock()
	c.cache[sessionKey] = &transcriptCacheEntry{
		msgs:      msgs,
		total:     total,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()

	return applyLimit(msgs, limit), total, nil
}

// Append writes a message and updates the cache (write-through).
// If no cache entry exists yet (e.g. first message before any Load), a new
// entry is seeded so that the immediately following Load hits the cache
// instead of falling through to disk.
func (c *CachedTranscriptStore) Append(sessionKey string, msg ChatMessage) error {
	if err := c.inner.Append(sessionKey, msg); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if entry, ok := c.cache[sessionKey]; ok && now.Before(entry.expiresAt) {
		entry.msgs = append(entry.msgs, msg)
		entry.total++
		entry.expiresAt = now.Add(c.ttl)
	} else {
		// No entry or expired: seed cache with this message. On a brand-new
		// session there are no prior messages, so [msg] is the full and
		// correct transcript. On a resumed session the gateway always calls
		// Load for context assembly before Append, so this branch is only
		// reached for new sessions in practice.
		c.cache[sessionKey] = &transcriptCacheEntry{
			msgs:      []ChatMessage{msg},
			total:     1,
			expiresAt: now.Add(c.ttl),
		}
	}
	return nil
}

// Delete removes the transcript and evicts the cache entry.
func (c *CachedTranscriptStore) Delete(sessionKey string) error {
	err := c.inner.Delete(sessionKey)

	c.mu.Lock()
	delete(c.cache, sessionKey)
	c.mu.Unlock()

	return err
}

// ListKeys passes through to the inner store.
func (c *CachedTranscriptStore) ListKeys() ([]string, error) {
	return c.inner.ListKeys()
}

// Search passes through to the inner store.
func (c *CachedTranscriptStore) Search(query string, maxResults int) ([]SearchResult, error) {
	return c.inner.Search(query, maxResults)
}

// CloneRecent delegates to the inner store and invalidates the destination cache entry.
func (c *CachedTranscriptStore) CloneRecent(srcKey, dstKey string, limit int) error {
	if err := c.inner.CloneRecent(srcKey, dstKey, limit); err != nil {
		return err
	}
	c.mu.Lock()
	delete(c.cache, dstKey) // invalidate so next Load reads fresh data
	c.mu.Unlock()
	return nil
}

// applyLimit returns a copy of the last `limit` messages (or all if limit <= 0).
// Returns a defensive copy so callers cannot corrupt the cached data.
func applyLimit(msgs []ChatMessage, limit int) []ChatMessage {
	src := msgs
	if limit > 0 && len(src) > limit {
		src = src[len(src)-limit:]
	}
	cp := make([]ChatMessage, len(src))
	copy(cp, src)
	return cp
}
