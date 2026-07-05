package toolctx

import (
	"context"
	"sync"
)

// ToolExecStats collects run-scoped tool-execution anomaly counters that only
// the chat tool layer can observe. Malformed-argument repairs happen inside
// ToolRegistry.Execute — invisible to the agent executor's per-call turn.tool
// logging — so they accumulate here and ride run.end
// (agentlog.RunEndData.RepairedToolCalls); Aggregate folds them into the
// cross-session per-tool stats. tool_argrepair.go explicitly gates
// schema-aware repairs on measuring this rate first.
//
// Safe for concurrent use: tools may run on different goroutines.
type ToolExecStats struct {
	mu       sync.Mutex
	repaired map[string]int
}

// NewToolExecStats creates an empty collector.
func NewToolExecStats() *ToolExecStats {
	return &ToolExecStats{}
}

// RecordRepaired counts one malformed-argument repair for the named tool.
// Nil-safe (no-op on a nil receiver).
func (s *ToolExecStats) RecordRepaired(tool string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.repaired == nil {
		s.repaired = make(map[string]int, 2)
	}
	s.repaired[tool]++
}

// RepairedCounts returns a copy of the per-tool repair counters, or nil when
// nothing was repaired (so run.end omits the field entirely).
func (s *ToolExecStats) RepairedCounts() map[string]int {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.repaired) == 0 {
		return nil
	}
	out := make(map[string]int, len(s.repaired))
	for k, v := range s.repaired {
		out[k] = v
	}
	return out
}

// WithToolExecStats attaches a ToolExecStats collector to the context.
func WithToolExecStats(ctx context.Context, s *ToolExecStats) context.Context {
	return context.WithValue(ctx, ctxKeyToolExecStats, s)
}

// ToolExecStatsFromContext extracts the collector, or nil if absent.
func ToolExecStatsFromContext(ctx context.Context) *ToolExecStats {
	s, _ := ctx.Value(ctxKeyToolExecStats).(*ToolExecStats)
	return s
}
