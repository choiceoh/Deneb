// Package agent provides agent job tracking, caching, and deduplication
// for the Go gateway.
//
// This mirrors the agent job system from src/gateway/server-methods/agents/agent-job.ts
// in the TypeScript codebase. It tracks running agent jobs, caches results with TTL,
// and prevents duplicate concurrent executions.
package agent

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// RunStatus is the terminal status of an agent run.
type RunStatus string

const (
	RunStatusOK      RunStatus = "ok"
	RunStatusError   RunStatus = "error"
	RunStatusTimeout RunStatus = "timeout"
)

// RunSnapshot represents the cached outcome of an agent run.
type RunSnapshot struct {
	RunID     string    `json:"runId"`
	Status    RunStatus `json:"status"`
	StartedAt *int64    `json:"startedAt,omitempty"`
	EndedAt   *int64    `json:"endedAt,omitempty"`
	Error     string    `json:"error,omitempty"`
	Ts        int64     `json:"ts"`
}

// LifecycleEvent is an agent lifecycle event from the event bus.
type LifecycleEvent struct {
	RunID   string `json:"runId"`
	Phase   string `json:"phase"` // "start", "end", "error"
	Aborted bool   `json:"aborted,omitempty"`
	Error   string `json:"error,omitempty"`
	Ts      int64  `json:"ts,omitempty"`
}

// pendingError tracks an error waiting for grace period to expire.
type pendingError struct {
	snapshot RunSnapshot
	dueAt    int64
	timer    *time.Timer
}

const (
	cacheTTLMs      = 10 * 60 * 1000 // 10 minutes
	errorGraceMs    = 15_000         // 15 seconds
	maxCacheEntries = 500
)

// JobTracker manages agent job tracking, caching, and deduplication.
type JobTracker struct {
	mu        sync.Mutex
	cache     map[string]*RunSnapshot
	pending   map[string]*pendingError
	runStarts map[string]int64 // runId -> startedAt timestamp
	logger    *slog.Logger

	// Subscribers for run lifecycle events.
	subMu       sync.RWMutex
	subscribers map[string]chan<- LifecycleEvent
}

// NewJobTracker creates a new agent job tracker.
func NewJobTracker(logger *slog.Logger) *JobTracker {
	if logger == nil {
		logger = slog.Default()
	}
	return &JobTracker{
		cache:       make(map[string]*RunSnapshot),
		pending:     make(map[string]*pendingError),
		runStarts:   make(map[string]int64),
		logger:      logger,
		subscribers: make(map[string]chan<- LifecycleEvent),
	}
}

// OnLifecycleEvent processes an agent lifecycle event.
func (jt *JobTracker) OnLifecycleEvent(evt LifecycleEvent) {
	jt.mu.Lock()
	defer jt.mu.Unlock()

	now := time.Now().UnixMilli()
	if evt.Ts == 0 {
		evt.Ts = now
	}

	switch evt.Phase {
	case "start":
		jt.runStarts[evt.RunID] = evt.Ts
		jt.clearPendingLocked(evt.RunID)
		delete(jt.cache, evt.RunID) // Clear stale terminal cache.

	case "error":
		snapshot := jt.createSnapshot(evt)
		jt.schedulePendingError(evt.RunID, snapshot)

	case "end":
		snapshot := jt.createSnapshot(evt)
		jt.recordSnapshotLocked(snapshot)
		jt.clearPendingLocked(evt.RunID)
		delete(jt.runStarts, evt.RunID)
	}

	// Notify subscribers.
	jt.notifySubscribers(evt)
}

// CachedSnapshot returns a cached run snapshot if available and not expired.
func (jt *JobTracker) CachedSnapshot(runID string) *RunSnapshot {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	jt.pruneCacheLocked(time.Now().UnixMilli())

	snap, ok := jt.cache[runID]
	if !ok {
		return nil
	}
	return snap
}

// IsRunning returns true if a run is currently active (started but not ended).
func (jt *JobTracker) IsRunning(runID string) bool {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	_, ok := jt.runStarts[runID]
	return ok
}

// WaitForJob waits for an agent job to complete with a timeout.
// Returns the final snapshot, or nil if timeout or context cancelled.
func (jt *JobTracker) WaitForJob(ctx context.Context, runID string, timeoutMs int64, ignoreCached bool) *RunSnapshot {
	// Check cache first.
	if !ignoreCached {
		if snap := jt.CachedSnapshot(runID); snap != nil {
			return snap
		}
	}

	if timeoutMs <= 0 {
		return nil
	}

	// Subscribe to lifecycle events for this run.
	ch := make(chan LifecycleEvent, 16)
	jt.subMu.Lock()
	jt.subscribers[runID] = ch
	jt.subMu.Unlock()
	defer func() {
		jt.subMu.Lock()
		delete(jt.subscribers, runID)
		jt.subMu.Unlock()
	}()

	timeout := time.After(time.Duration(timeoutMs) * time.Millisecond)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timeout:
			return nil
		case evt := <-ch:
			switch evt.Phase {
			case "start":
				// Run restarted; keep waiting.
				continue
			case "end":
				return jt.CachedSnapshot(runID)
			case "error":
				// Wait for grace period; may restart.
				// Use a shorter sub-timeout.
				graceTimeout := time.After(time.Duration(errorGraceMs+1000) * time.Millisecond)
				select {
				case <-ctx.Done():
					return nil
				case <-timeout:
					return nil
				case <-graceTimeout:
					return jt.CachedSnapshot(runID)
				case nextEvt := <-ch:
					if nextEvt.Phase == "start" {
						continue // Restarted during grace.
					}
					return jt.CachedSnapshot(runID)
				}
			}
		}
	}
}

// ActiveRunCount returns the number of currently active runs.
func (jt *JobTracker) ActiveRunCount() int {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	return len(jt.runStarts)
}

// CacheSize returns the number of cached snapshots.
func (jt *JobTracker) CacheSize() int {
	jt.mu.Lock()
	defer jt.mu.Unlock()
	return len(jt.cache)
}

// --- Internal methods ---

func (jt *JobTracker) createSnapshot(evt LifecycleEvent) RunSnapshot {
	snap := RunSnapshot{
		RunID: evt.RunID,
		Ts:    evt.Ts,
	}

	if startedAt, ok := jt.runStarts[evt.RunID]; ok {
		snap.StartedAt = &startedAt
	}

	switch evt.Phase {
	case "error":
		snap.Status = RunStatusError
		snap.Error = evt.Error
	case "end":
		if evt.Aborted {
			snap.Status = RunStatusTimeout
		} else {
			snap.Status = RunStatusOK
		}
		endedAt := evt.Ts
		snap.EndedAt = &endedAt
	}

	return snap
}

func (jt *JobTracker) recordSnapshotLocked(snap RunSnapshot) {
	now := snap.Ts
	if now == 0 {
		now = time.Now().UnixMilli()
	}
	jt.pruneCacheLocked(now)
	jt.cache[snap.RunID] = &snap
}

func (jt *JobTracker) pruneCacheLocked(now int64) {
	if len(jt.cache) < maxCacheEntries {
		return
	}
	for id, snap := range jt.cache {
		if now-snap.Ts > cacheTTLMs {
			delete(jt.cache, id)
		}
	}
}

func (jt *JobTracker) schedulePendingError(runID string, snapshot RunSnapshot) {
	jt.clearPendingLocked(runID)

	pe := &pendingError{
		snapshot: snapshot,
		dueAt:    time.Now().UnixMilli() + errorGraceMs,
	}
	pe.timer = time.AfterFunc(time.Duration(errorGraceMs)*time.Millisecond, func() {
		jt.mu.Lock()
		defer jt.mu.Unlock()
		// Only record if still pending (not cleared by a restart).
		if p, ok := jt.pending[runID]; ok && p == pe {
			jt.recordSnapshotLocked(pe.snapshot)
			delete(jt.pending, runID)
			delete(jt.runStarts, runID)
		}
	})
	jt.pending[runID] = pe
}

func (jt *JobTracker) clearPendingLocked(runID string) {
	if pe, ok := jt.pending[runID]; ok {
		pe.timer.Stop()
		delete(jt.pending, runID)
	}
}

func (jt *JobTracker) notifySubscribers(evt LifecycleEvent) {
	jt.subMu.RLock()
	ch, ok := jt.subscribers[evt.RunID]
	jt.subMu.RUnlock()
	if ok {
		select {
		case ch <- evt:
		default:
			// Drop if subscriber can't keep up.
		}
	}
}
