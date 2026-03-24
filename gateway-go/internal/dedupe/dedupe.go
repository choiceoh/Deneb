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
	stopGC  chan struct{}
}

// NewTracker creates a deduplication tracker with a background GC goroutine
// that sweeps expired entries at half the TTL interval.
// Call Close() to stop the GC goroutine.
func NewTracker(ttl time.Duration, maxSize int) *Tracker {
	t := &Tracker{
		entries: make(map[string]time.Time, maxSize),
		ttl:     ttl,
		maxSize: maxSize,
		stopGC:  make(chan struct{}),
	}

	// Background GC at half the TTL so expired entries are cleaned
	// before they accumulate to maxSize, keeping Check() O(1) on average.
	// Minimum 100ms to avoid busy-looping; for production TTLs (minutes)
	// this floor is never hit.
	gcInterval := ttl / 2
	if gcInterval < 100*time.Millisecond {
		gcInterval = 100 * time.Millisecond
	}
	go t.gcLoop(gcInterval)

	return t
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

	// If at capacity, drop oldest as a fallback (GC should prevent this).
	if len(t.entries) >= t.maxSize {
		t.evictOldest()
	}

	t.entries[id] = now
	return true
}

// Close stops the background GC goroutine.
func (t *Tracker) Close() {
	select {
	case <-t.stopGC:
	default:
		close(t.stopGC)
	}
}

// gcLoop periodically sweeps expired entries so Check() stays fast.
func (t *Tracker) gcLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopGC:
			return
		case now := <-ticker.C:
			t.mu.Lock()
			for id, seenAt := range t.entries {
				if now.Sub(seenAt) >= t.ttl {
					delete(t.entries, id)
				}
			}
			t.mu.Unlock()
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
