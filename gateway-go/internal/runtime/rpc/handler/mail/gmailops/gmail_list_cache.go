package gmailops

import (
	"sync"
	"time"
)

// listCacheTTL bounds how long a cached inbox page is served as-is before
// a fresh Gmail fetch is wanted. Long enough to make the common "open a
// mail, hit back, open another" loop instant; short enough that externally
// arriving mail surfaces quickly.
//
// Only actions that change inbox *membership* (archive, trash)
// invalidate the cache, so the TTL just governs how stale an otherwise
// unchanged inbox can look. mark_read is deliberately NOT an
// invalidator: it leaves membership intact (the default
// {in:inbox is:unread} query still matches the message via in:inbox) and
// the Mini App updates the read dot optimistically — and since the
// client fires mark_read automatically on every mail open, invalidating
// there would blow the cache away on each tap and defeat it entirely.
const listCacheTTL = 30 * time.Second

// listCacheStaleServeCeiling is the stale-while-revalidate window: a page
// past listCacheTTL but younger than this is still served instantly while
// a background refresh replaces it (cold list_recent measures ~2.5s on
// prod — that wait moves off the user's tap). Past the ceiling the entry
// is dead and the caller fetches synchronously.
const listCacheStaleServeCeiling = 5 * time.Minute

// listCache is a tiny TTL cache for miniapp.gmail.list_recent payloads.
// Single-operator deployment: the inbox belongs to one user, so a flat
// map keyed by (query|limit|pageToken) with coarse whole-cache
// invalidation is enough — no per-key eviction, no LRU. Every method is
// nil-safe so callers (and tests) can pass a nil cache to disable
// caching without branching.
//
// Concurrency: a single mutex guards entries; each method holds it only
// for the map op and never calls out while holding it, so there is no
// re-entrancy or lock-ordering hazard.
type listCache struct {
	mu         sync.Mutex
	ttl        time.Duration
	entries    map[string]cachedList
	refreshing map[string]bool
}

type cachedList struct {
	payload  map[string]any
	storedAt time.Time
}

func newListCache(ttl time.Duration) *listCache {
	return &listCache{ttl: ttl, entries: make(map[string]cachedList), refreshing: make(map[string]bool)}
}

// get returns the cached payload for key when present and within TTL.
// now is passed in (rather than read internally) so tests can drive the
// clock deterministically.
func (c *listCache) get(key string, now time.Time) (map[string]any, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || now.Sub(e.storedAt) > c.ttl {
		return nil, false
	}
	return e.payload, true
}

// getStale returns a payload past TTL but within the stale-serve ceiling —
// the stale-while-revalidate read. The bool reports whether the CALLER won
// the right to refresh this key in the background (single-flight: only one
// concurrent refresher per key; the winner must call refreshDone).
func (c *listCache) getStale(key string, now time.Time) (map[string]any, bool, bool) {
	if c == nil {
		return nil, false, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false, false
	}
	age := now.Sub(e.storedAt)
	if age <= c.ttl || age > listCacheStaleServeCeiling {
		return nil, false, false
	}
	shouldRefresh := !c.refreshing[key]
	if shouldRefresh {
		c.refreshing[key] = true
	}
	return e.payload, true, shouldRefresh
}

// refreshDone releases the single-flight refresh claim taken by getStale.
func (c *listCache) refreshDone(key string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.refreshing, key)
}

// put stores payload under key, stamped at now.
func (c *listCache) put(key string, payload map[string]any, now time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cachedList{payload: payload, storedAt: now}
}

// invalidate drops every cached page so the next list reflects an
// inbox-membership change (archive/trash) immediately.
func (c *listCache) invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
}
