package chat

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
	results map[string]*turnResult_  // keyed by tool_use_id
	waiters map[string][]chan struct{} // signals when a result is stored
}

// turnResult_ holds the outcome of a single tool execution within a turn.
// Named with trailing underscore to avoid collision with the existing
// turnResult type in agent.go (which tracks LLM stream parsing state).
type turnResult_ struct {
	ToolName string
	Output   string
	IsError  bool
	Duration time.Duration
}

// NewTurnContext creates an empty turn context.
func NewTurnContext() *TurnContext {
	return &TurnContext{
		results: make(map[string]*turnResult_),
		waiters: make(map[string][]chan struct{}),
	}
}

// Store records a tool's result, keyed by tool_use_id.
// Any goroutines waiting on this ID via Wait are unblocked.
func (tc *TurnContext) Store(toolUseID string, result *turnResult_) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	tc.results[toolUseID] = result

	// Unblock all waiters for this ID.
	if chs, ok := tc.waiters[toolUseID]; ok {
		for _, ch := range chs {
			close(ch)
		}
		delete(tc.waiters, toolUseID)
	}
}

// Load returns the result for the given tool_use_id, or nil if not yet stored.
func (tc *TurnContext) Load(toolUseID string) *turnResult_ {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.results[toolUseID]
}

// Wait blocks until the result for toolUseID is stored, the timeout expires,
// or the context is cancelled (e.g., agent run aborted).
// Returns the result and true if available, or nil and false on timeout/cancel.
func (tc *TurnContext) Wait(ctx context.Context, toolUseID string, timeout time.Duration) (*turnResult_, bool) {
	tc.mu.Lock()

	// Already available — fast path.
	if r, ok := tc.results[toolUseID]; ok {
		tc.mu.Unlock()
		return r, true
	}

	// Register a waiter channel.
	ch := make(chan struct{})
	tc.waiters[toolUseID] = append(tc.waiters[toolUseID], ch)
	tc.mu.Unlock()

	// Wait for signal, timeout, or context cancellation.
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ch:
		// Result is now available.
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
// refs maps tool_use_id → the ID it depends on ($ref target).
// Returns an error describing the cycle if one is found.
func DetectCycle(refs map[string]string) error {
	// For each starting node, walk the chain and detect revisits.
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
