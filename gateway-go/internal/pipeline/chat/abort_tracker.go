// abort_tracker.go — Tracks and manages active agent run abort controllers.
//
// Each async agent run registers an AbortEntry (containing a context.CancelFunc)
// so it can be cancelled by the user or by session lifecycle events. A background
// GC loop cleans up expired entries that were never cancelled.
package chat

import (
	"sync"
	"time"
)

// AbortTracker manages abort entries for active agent runs. Thread-safe.
type AbortTracker struct {
	mu       sync.Mutex
	entries  map[string]*AbortEntry // clientRunId -> entry
	done     chan struct{}          // signals GC loop to stop
	gcClosed bool                   // prevents double-close of done
}

// NewAbortTracker creates a ready-to-use AbortTracker and starts its GC loop.
func NewAbortTracker() *AbortTracker {
	at := &AbortTracker{
		entries: make(map[string]*AbortEntry),
		done:    make(chan struct{}),
	}
	go at.gcLoop()
	return at
}

// Register adds an abort entry for a running agent. If clientRunID is empty,
// the call is a no-op (headless runs without tracking).
func (at *AbortTracker) Register(clientRunID string, entry *AbortEntry) {
	if clientRunID == "" {
		return
	}
	at.mu.Lock()
	at.entries[clientRunID] = entry
	at.mu.Unlock()
}

// Cleanup removes a run's abort entry after the run completes.
func (at *AbortTracker) Cleanup(clientRunID string) {
	if clientRunID == "" {
		return
	}
	at.mu.Lock()
	delete(at.entries, clientRunID)
	at.mu.Unlock()
}

// HasActiveRun reports whether at least one run is active for the session.
func (at *AbortTracker) HasActiveRun(sessionKey string) bool {
	at.mu.Lock()
	defer at.mu.Unlock()
	for _, entry := range at.entries {
		if entry.SessionKey == sessionKey {
			return true
		}
	}
	return false
}

// CountForSession returns the number of active runs for a session.
func (at *AbortTracker) CountForSession(sessionKey string) int {
	at.mu.Lock()
	defer at.mu.Unlock()
	count := 0
	for _, entry := range at.entries {
		if entry.SessionKey == sessionKey {
			count++
		}
	}
	return count
}

// InterruptSession cancels all active runs for a session key and removes them.
func (at *AbortTracker) InterruptSession(sessionKey string) {
	at.mu.Lock()
	var toDelete []string
	for id, entry := range at.entries {
		if entry.SessionKey == sessionKey {
			entry.CancelFn(nil)
			toDelete = append(toDelete, id)
		}
	}
	for _, id := range toDelete {
		delete(at.entries, id)
	}
	at.mu.Unlock()
}

// CancelByRunID cancels a specific run by its client run ID.
// Returns the cancelled entry's session key and run ID, or empty strings if not found.
func (at *AbortTracker) CancelByRunID(runID string) (sessionKey, abortedRunID string) {
	at.mu.Lock()
	defer at.mu.Unlock()
	if entry, ok := at.entries[runID]; ok {
		entry.CancelFn(nil)
		sessionKey = entry.SessionKey
		abortedRunID = runID
		delete(at.entries, runID)
	}
	return
}

// CancelBySessionKey cancels the first matching run for a session.
// Returns the cancelled run ID and session key, or empty strings if not found.
func (at *AbortTracker) CancelBySessionKey(sessionKey string) (abortedRunID string) {
	return at.CancelBySessionKeyWithCause(sessionKey, nil)
}

// CancelBySessionKeyWithCause cancels all active runs for a session and
// attaches the given cause to each cancellation. The cause is observable
// via context.Cause(ctx) inside the run goroutine, letting it choose
// cleanup behavior (e.g. ErrMergedIntoNewRun → clear emoji and delete
// draft instead of showing an error reaction).
func (at *AbortTracker) CancelBySessionKeyWithCause(sessionKey string, cause error) (abortedRunID string) {
	at.mu.Lock()
	defer at.mu.Unlock()
	for id, entry := range at.entries {
		if entry.SessionKey == sessionKey {
			entry.CancelFn(cause)
			abortedRunID = id
			delete(at.entries, id)
		}
	}
	return
}

// Close stops the GC loop and cancels all active entries.
func (at *AbortTracker) Close() {
	at.mu.Lock()
	if !at.gcClosed {
		close(at.done)
		at.gcClosed = true
	}
	for _, entry := range at.entries {
		entry.CancelFn(nil)
	}
	at.entries = make(map[string]*AbortEntry)
	at.mu.Unlock()
}

// gcLoop periodically cleans up expired abort entries.
func (at *AbortTracker) gcLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-at.done:
			return
		case <-ticker.C:
			at.mu.Lock()
			now := time.Now()
			for id, entry := range at.entries {
				if now.After(entry.ExpiresAt) {
					entry.CancelFn(nil)
					delete(at.entries, id)
				}
			}
			at.mu.Unlock()
		}
	}
}
