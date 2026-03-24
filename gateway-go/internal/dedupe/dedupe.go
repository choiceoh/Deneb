// Package dedupe provides request ID deduplication with TTL expiry.
//
// This prevents duplicate processing of the same request when a client
// retries or a message is delivered more than once. Mirrors the deduplication
// logic from DEDUPE_TTL_MS and DEDUPE_MAX in src/gateway/server-constants.ts.
package dedupe

import (
	"sync"
	"time"
)

// Tracker tracks recently seen request IDs and rejects duplicates within
// a configurable TTL window.
type Tracker struct {
	mu      sync.Mutex
	entries map[string]time.Time
	ttl     time.Duration
	maxSize int
}

// NewTracker creates a deduplication tracker.
// ttl is how long to remember a request ID; maxSize caps the map to prevent
// unbounded growth (oldest entries are evicted on overflow).
func NewTracker(ttl time.Duration, maxSize int) *Tracker {
	return &Tracker{
		entries: make(map[string]time.Time, maxSize),
		ttl:     ttl,
		maxSize: maxSize,
	}
}

// Check returns true if the request ID has NOT been seen recently (i.e., it's new).
// Returns false if the ID is a duplicate. Automatically records new IDs.
func (t *Tracker) Check(id string) bool {
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()

	// Check if already seen and not expired.
	if seenAt, ok := t.entries[id]; ok {
		if now.Sub(seenAt) < t.ttl {
			return false // duplicate
		}
	}

	// Evict expired entries if at capacity.
	if len(t.entries) >= t.maxSize {
		t.evictExpired(now)
	}
	// If still at capacity after eviction, drop oldest.
	if len(t.entries) >= t.maxSize {
		t.evictOldest()
	}

	t.entries[id] = now
	return true
}

func (t *Tracker) evictExpired(now time.Time) {
	for id, seenAt := range t.entries {
		if now.Sub(seenAt) >= t.ttl {
			delete(t.entries, id)
		}
	}
}

func (t *Tracker) evictOldest() {
	var oldestID string
	var oldestTime time.Time
	first := true
	for id, seenAt := range t.entries {
		if first || seenAt.Before(oldestTime) {
			oldestID = id
			oldestTime = seenAt
			first = false
		}
	}
	if oldestID != "" {
		delete(t.entries, oldestID)
	}
}

// Len returns the number of tracked entries (for testing/monitoring).
func (t *Tracker) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.entries)
}
