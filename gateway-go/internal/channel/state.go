package channel

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// StatusPatch represents a partial update to the run state.
type StatusPatch struct {
	Busy              *bool  `json:"busy,omitempty"`
	ActiveRuns        *int   `json:"activeRuns,omitempty"`
	LastRunActivityAt *int64 `json:"lastRunActivityAt,omitempty"`
	// Heartbeat is true when the patch originates from a periodic tick rather
	// than an actual state transition. Sinks can use this to suppress noisy
	// no-op log entries.
	Heartbeat bool `json:"heartbeat,omitempty"`
}

// StatusSink is a callback for run state changes.
type StatusSink func(patch StatusPatch)

// RunStateMachine tracks active runs and emits heartbeats.
// Mirrors createRunStateMachine() from src/channels/run-state-machine.ts.
type RunStateMachine struct {
	mu         sync.Mutex
	active     int
	sink       StatusSink
	heartbeat  *time.Ticker
	cancelFunc context.CancelFunc
	done       sync.WaitGroup
	closed     atomic.Bool
}

// NewRunStateMachine creates a new run state machine with heartbeat emission.
func NewRunStateMachine(ctx context.Context, sink StatusSink, heartbeatInterval time.Duration) *RunStateMachine {
	ctx, cancel := context.WithCancel(ctx)
	sm := &RunStateMachine{
		sink:       sink,
		cancelFunc: cancel,
	}

	if heartbeatInterval > 0 {
		sm.heartbeat = time.NewTicker(heartbeatInterval)
		sm.done.Add(1)
		go sm.heartbeatLoop(ctx)
	}

	return sm
}

// StartRun increments the active run counter.
func (sm *RunStateMachine) StartRun() {
	sm.mu.Lock()
	sm.active++
	active := sm.active
	sm.mu.Unlock()

	busy := true
	ts := time.Now().UnixMilli()
	sm.sink(StatusPatch{
		Busy:              &busy,
		ActiveRuns:        &active,
		LastRunActivityAt: &ts,
	})
}

// EndRun decrements the active run counter.
func (sm *RunStateMachine) EndRun() {
	sm.mu.Lock()
	if sm.active > 0 {
		sm.active--
	}
	active := sm.active
	sm.mu.Unlock()

	busy := active > 0
	ts := time.Now().UnixMilli()
	sm.sink(StatusPatch{
		Busy:              &busy,
		ActiveRuns:        &active,
		LastRunActivityAt: &ts,
	})
}

// Close stops the heartbeat and waits for the goroutine to exit.
// After Close returns, no more sink callbacks will be invoked.
func (sm *RunStateMachine) Close() {
	sm.closed.Store(true)
	sm.cancelFunc()
	if sm.heartbeat != nil {
		sm.heartbeat.Stop()
	}
	sm.done.Wait()
}

func (sm *RunStateMachine) heartbeatLoop(ctx context.Context) {
	defer sm.done.Done()
	if sm.heartbeat == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-sm.heartbeat.C:
			// Check closed flag to prevent sink call after Close() returns.
			if sm.closed.Load() {
				return
			}
			sm.mu.Lock()
			active := sm.active
			sm.mu.Unlock()
			busy := active > 0
			sm.sink(StatusPatch{Busy: &busy, ActiveRuns: &active, Heartbeat: true})
		}
	}
}
