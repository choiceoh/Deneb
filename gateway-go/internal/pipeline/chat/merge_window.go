// merge_window.go — Tracks per-session timestamps of the most recent
// chat.send arrival to support the "merge consecutive messages" feature.
//
// When the user sends a new message within mergeWindowDuration of the
// previous one while a run is still active, Send() cancels that run and
// starts a new one so both messages are answered together. The previous
// user message is already persisted in the transcript (run_exec.go appends
// it before any LLM call), so the new run sees both turns and produces a
// single combined response.
package chat

import (
	"sync"
	"time"
)

// mergeWindowDuration is the cancel-and-merge window: if a new chat.send
// arrives within this interval after the previous one *and* a run for the
// same session is still active, the active run is cancelled and the two
// messages are answered together in a single new run.
const mergeWindowDuration = 3 * time.Second

// MergeWindowTracker remembers the wall-clock time of the most recent
// chat.send per session key. Thread-safe.
type MergeWindowTracker struct {
	mu sync.Mutex
	ts map[string]time.Time
}

// NewMergeWindowTracker creates an empty tracker.
func NewMergeWindowTracker() *MergeWindowTracker {
	return &MergeWindowTracker{ts: make(map[string]time.Time)}
}

// Touch records that a chat.send was just observed for sessionKey and
// returns the previous timestamp (zero Time if none).
func (m *MergeWindowTracker) Touch(sessionKey string) time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	prev := m.ts[sessionKey]
	m.ts[sessionKey] = time.Now()
	return prev
}

// Clear removes the timestamp for sessionKey (used on /reset and abort).
func (m *MergeWindowTracker) Clear(sessionKey string) {
	m.mu.Lock()
	delete(m.ts, sessionKey)
	m.mu.Unlock()
}

// Reset clears all sessions' timestamps.
func (m *MergeWindowTracker) Reset() {
	m.mu.Lock()
	m.ts = make(map[string]time.Time)
	m.mu.Unlock()
}
