package toolctx

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// TurnContext is a thread-safe store for sharing tool results within a single
// agent turn. Tools executing in parallel can store their results here, and
// other tools can wait for and reference those results via $ref.
//
// Lifecycle: created at the start of each turn in RunAgent, attached to
// context.Context, and discarded when the turn ends.
type TurnContext struct {
	mu      sync.Mutex
	results map[string]*TurnResult     // keyed by tool_use_id
	waiters map[string][]chan struct{} // signals when a result is stored
	stats   map[string]*toolStat       // per-tool-name timing stats
}

// toolStat accumulates completion-time samples for a single tool name within a turn.
type toolStat struct {
	n     int
	total time.Duration
	min   time.Duration
	max   time.Duration
}

// ToolTimingStats is a snapshot of aggregated completion times for a tool within a turn.
type ToolTimingStats struct {
	Count int
	Mean  time.Duration
	Min   time.Duration
	Max   time.Duration
}

// TurnResult holds the outcome of a single tool execution within a turn.
type TurnResult struct {
	ToolName string
	Output   string
	IsError  bool
	Duration time.Duration
}

// NewTurnContext creates an empty turn context.
func NewTurnContext() *TurnContext {
	return &TurnContext{}
}

// Store records a tool's result, keyed by tool_use_id.
// Any goroutines waiting on this ID via Wait are unblocked.
func (tc *TurnContext) Store(toolUseID string, result *TurnResult) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if tc.results == nil {
		tc.results = make(map[string]*TurnResult, 4)
	}
	tc.results[toolUseID] = result

	// Record timing stats keyed by tool name.
	if result.ToolName != "" && result.Duration > 0 {
		if tc.stats == nil {
			tc.stats = make(map[string]*toolStat, 4)
		}
		s := tc.stats[result.ToolName]
		if s == nil {
			s = &toolStat{min: result.Duration, max: result.Duration}
			tc.stats[result.ToolName] = s
		}
		s.n++
		s.total += result.Duration
		if result.Duration < s.min {
			s.min = result.Duration
		}
		if result.Duration > s.max {
			s.max = result.Duration
		}
	}

	// Unblock all waiters for this ID.
	if chs, ok := tc.waiters[toolUseID]; ok {
		for _, ch := range chs {
			close(ch)
		}
		delete(tc.waiters, toolUseID)
	}
}

// ToolTiming returns aggregated completion-time stats for the named tool within
// this turn. Returns false if no completions have been recorded yet.
func (tc *TurnContext) ToolTiming(toolName string) (ToolTimingStats, bool) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	s, ok := tc.stats[toolName]
	if !ok {
		return ToolTimingStats{}, false
	}
	return ToolTimingStats{
		Count: s.n,
		Mean:  s.total / time.Duration(s.n),
		Min:   s.min,
		Max:   s.max,
	}, true
}

// Load returns the result for the given tool_use_id, or nil if not yet stored.
func (tc *TurnContext) Load(toolUseID string) *TurnResult {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.results[toolUseID]
}

// Wait blocks until the result for toolUseID is stored, the timeout expires,
// or the context is cancelled.
func (tc *TurnContext) Wait(ctx context.Context, toolUseID string, timeout time.Duration) (*TurnResult, bool) {
	tc.mu.Lock()

	// Already available — fast path.
	if r, ok := tc.results[toolUseID]; ok {
		tc.mu.Unlock()
		return r, true
	}

	// Register a waiter channel.
	ch := make(chan struct{})
	if tc.waiters == nil {
		tc.waiters = make(map[string][]chan struct{}, 4)
	}
	tc.waiters[toolUseID] = append(tc.waiters[toolUseID], ch)
	tc.mu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ch:
		tc.mu.Lock()
		r := tc.results[toolUseID]
		tc.mu.Unlock()
		return r, true
	case <-timer.C:
		return nil, false
	case <-ctx.Done():
		return nil, false
	}
}

// IDs returns all stored tool_use_ids.
func (tc *TurnContext) IDs() []string {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	ids := make([]string, 0, len(tc.results))
	for id := range tc.results {
		ids = append(ids, id)
	}
	return ids
}

// DetectCycle checks whether the given refs form a cycle.
func DetectCycle(refs map[string]string) error {
	for start := range refs {
		visited := map[string]bool{start: true}
		current := refs[start]
		for current != "" {
			if visited[current] {
				return fmt.Errorf("circular $ref: %s → %s (cycle detected)", start, current)
			}
			visited[current] = true
			current = refs[current]
		}
	}
	return nil
}
