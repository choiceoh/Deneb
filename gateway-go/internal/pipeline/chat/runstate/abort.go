// abort_tracker.go — Tracks and manages active agent run abort controllers.
//
// Each async agent run registers an AbortEntry (containing a context.CancelFunc)
// so it can be cancelled by the user or by session lifecycle events. A background
// GC loop cleans up expired entries that were never cancelled.
package runstate

import (
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// AbortEntry describes one cancellable chat run.
type AbortEntry = toolport.AbortEntry

// AbortTracker manages abort entries for active agent runs. Thread-safe.
type AbortTracker struct {
	mu          sync.Mutex
	entries     map[string]*AbortEntry // clientRunId -> entry
	running     map[string]struct{}    // retained until run cleanup, even after cancel
	admissions  int                    // accepted requests not yet finished registering
	done        chan struct{}          // signals GC loop to stop
	gcClosed    bool                   // prevents double-close of done
	draining    bool                   // rejects new top-level runs during shutdown
	drained     chan struct{}          // closed once every accepted run completes
	drainedDone bool                   // prevents double-close of drained
}

// NewAbortTracker creates a ready-to-use AbortTracker and starts its GC loop.
func NewAbortTracker() *AbortTracker {
	at := &AbortTracker{
		entries: make(map[string]*AbortEntry),
		running: make(map[string]struct{}),
		done:    make(chan struct{}),
	}
	go func() {
		// Panic in the GC loop must not take down the whole process — the tracker
		// is long-lived and recovery here keeps abort handling alive.
		defer func() { _ = recover() }()
		at.gcLoop()
	}()
	return at
}

// Register adds an abort entry for a running agent. If clientRunID is empty,
// the call is a no-op (headless runs without tracking).
func (at *AbortTracker) Register(clientRunID string, entry *AbortEntry) {
	_ = at.TryRegister(clientRunID, entry)
}

// TryRegister adds a new top-level run unless shutdown draining has begun.
// The check and insertion share one lock with BeginDrain, so a run can never
// slip into the registry after the drainer has observed it idle.
func (at *AbortTracker) TryRegister(clientRunID string, entry *AbortEntry) bool {
	return at.tryRegister(clientRunID, entry, false, false)
}

// RegisterAdmitted converts a request admission acquired before draining into
// a tracked run. The caller must ReleaseAdmission after this returns. Keeping
// admission and registration under the same mutex prevents shutdown from
// observing an idle gap between a request's side effects and its run entry.
func (at *AbortTracker) RegisterAdmitted(clientRunID string, entry *AbortEntry) bool {
	return at.tryRegister(clientRunID, entry, false, true)
}

// RegisterContinuation preserves work that was accepted before draining, such
// as a message already queued behind an active turn. A continuation is admitted
// during draining only while another registered run still holds the drain open;
// once idle has been observed, the gate remains permanently closed.
func (at *AbortTracker) RegisterContinuation(clientRunID string, entry *AbortEntry) bool {
	return at.tryRegister(clientRunID, entry, true, false)
}

func (at *AbortTracker) tryRegister(clientRunID string, entry *AbortEntry, continuation, admitted bool) bool {
	if clientRunID == "" || entry == nil {
		return false
	}
	at.mu.Lock()
	defer at.mu.Unlock()
	if admitted && at.admissions == 0 {
		return false
	}
	if at.draining && !admitted && (!continuation || len(at.running) == 0) {
		return false
	}
	at.entries[clientRunID] = entry
	at.running[clientRunID] = struct{}{}
	return true
}

// AcquireAdmission reserves one top-level request before it can mutate session
// state. BeginDrain closes this gate atomically and waits for every successful
// reservation to either register its run or return.
func (at *AbortTracker) AcquireAdmission() bool {
	at.mu.Lock()
	defer at.mu.Unlock()
	if at.draining {
		return false
	}
	at.admissions++
	return true
}

// ReleaseAdmission releases a prior successful AcquireAdmission.
func (at *AbortTracker) ReleaseAdmission() {
	at.mu.Lock()
	if at.admissions > 0 {
		at.admissions--
	}
	at.signalDrainedLocked()
	at.mu.Unlock()
}

// Cleanup removes a run's abort entry after the run completes.
func (at *AbortTracker) Cleanup(clientRunID string) {
	if clientRunID == "" {
		return
	}
	at.mu.Lock()
	delete(at.entries, clientRunID)
	delete(at.running, clientRunID)
	at.signalDrainedLocked()
	at.mu.Unlock()
}

// BeginDrain permanently closes admission for top-level runs and returns a
// channel that closes after all runs accepted before the gate closed (including
// their already-queued continuations) have left the registry.
func (at *AbortTracker) BeginDrain() <-chan struct{} {
	at.mu.Lock()
	if !at.draining {
		at.draining = true
		at.drained = make(chan struct{})
	}
	at.signalDrainedLocked()
	drained := at.drained
	at.mu.Unlock()
	return drained
}

// IsDraining reports whether new top-level runs are being rejected.
func (at *AbortTracker) IsDraining() bool {
	at.mu.Lock()
	defer at.mu.Unlock()
	return at.draining
}

func (at *AbortTracker) signalDrainedLocked() {
	if at.draining && len(at.running) == 0 && at.admissions == 0 && !at.drainedDone {
		close(at.drained)
		at.drainedDone = true
	}
}

// HasActiveRun reports whether at least one run is active for the session.
// ActiveRunCount returns accepted-but-unfinished runs (registered actives plus
// admissions not yet registered) — the same population the shutdown drain waits
// for, exposed via /health so the deploy idle gate can wait BEFORE the swap
// instead of spending the drain window (downtime for new requests) on it.
func (at *AbortTracker) ActiveRunCount() int {
	at.mu.Lock()
	defer at.mu.Unlock()
	return len(at.running) + at.admissions
}

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

// HasActiveInteractiveRun reports whether a NON-automation run is active for
// the session. Auto-steer uses it instead of HasActiveRun so a user's message
// never folds into an autonomous relay (heartbeat/cron/mailpoll) that merely
// rides the same session key — see AbortEntry.Automation.
func (at *AbortTracker) HasActiveInteractiveRun(sessionKey string) bool {
	at.mu.Lock()
	defer at.mu.Unlock()
	for _, entry := range at.entries {
		if entry.SessionKey == sessionKey && !entry.Automation {
			return true
		}
	}
	return false
}

// HasOtherActiveRun reports whether a registered run other than clientRunID
// is active for the session. Completion handoff uses this while holding the
// session decision lock: if a successor already registered, that successor
// owns any newly queued message and the finishing run must not start a second
// continuation alongside it.
func (at *AbortTracker) HasOtherActiveRun(sessionKey, clientRunID string) bool {
	at.mu.Lock()
	defer at.mu.Unlock()
	for id, entry := range at.entries {
		if id != clientRunID && entry.SessionKey == sessionKey {
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
	at.signalDrainedLocked()
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
		at.signalDrainedLocked()
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
	at.signalDrainedLocked()
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
	at.running = make(map[string]struct{})
	at.admissions = 0
	if !at.draining {
		at.draining = true
		at.drained = make(chan struct{})
	}
	at.signalDrainedLocked()
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
			at.signalDrainedLocked()
			at.mu.Unlock()
		}
	}
}
