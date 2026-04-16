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
	"errors"
	"sync"
	"time"
)

// ErrMergedIntoNewRun is the cancellation cause attached when the Send
// merge path cancels an in-flight run because the user sent a follow-up
// message within mergeWindowDuration. Run goroutines observe it via
// context.Cause(ctx) to choose a clean teardown — clear the user-message
// emoji and delete the orphan streaming draft instead of showing the
// generic "error" reaction that any other cancel would produce.
var ErrMergedIntoNewRun = errors.New("merged into new run")

// mergeWindowDuration is the cancel-and-merge window: if a new chat.send
// arrives within this interval after the previous one *and* a run for the
// same session is still active, the active run is cancelled and the two
// messages are answered together in a single new run.
const mergeWindowDuration = 3 * time.Second

// MergeWindowTracker remembers the wall-clock time of the most recent
// chat.send per session key and hands out per-session decision mutexes so
// the Touch→HasActiveRun→Cancel→Dispatch path is atomic. Thread-safe.
type MergeWindowTracker struct {
	mu    sync.Mutex
	ts    map[string]time.Time
	locks map[string]*sync.Mutex
}

// NewMergeWindowTracker creates an empty tracker.
func NewMergeWindowTracker() *MergeWindowTracker {
	return &MergeWindowTracker{
		ts:    make(map[string]time.Time),
		locks: make(map[string]*sync.Mutex),
	}
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

// Reset clears all sessions' timestamps and locks.
func (m *MergeWindowTracker) Reset() {
	m.mu.Lock()
	m.ts = make(map[string]time.Time)
	m.locks = make(map[string]*sync.Mutex)
	m.mu.Unlock()
}

// SessionLock returns the per-session mutex used to serialize the Send
// merge decision so that two concurrent chat.send calls for the same
// session cannot both pass the "active run + within window" check and
// double-dispatch.
func (m *MergeWindowTracker) SessionLock(sessionKey string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.locks[sessionKey]
	if !ok {
		l = &sync.Mutex{}
		m.locks[sessionKey] = l
	}
	return l
}
