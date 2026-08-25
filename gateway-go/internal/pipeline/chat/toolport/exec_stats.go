package toolport

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
	// cacheHits counts run-cache hits per tool (the call never reached the
	// tool fn, so the executor's turn.tool duration/output stats undercount
	// real demand without this).
	cacheHits map[string]int
	// truncated counts head/tail output truncations per tool. Truncation
	// happens before the executor sees the output — turn.tool's OutputLen is
	// the post-truncation length — so the signal only exists here. Feeds
	// per-tool MaxOutput budget tuning (tool_schemas.json max_output).
	truncated map[string]int
	// unknownArgs counts tool calls that carried a top-level argument the
	// tool's schema does not declare. The harness drops those keys silently,
	// so the model can believe it filtered when it did not; this counter is
	// the measurement that gates surfacing it (chat/tool_argcheck.go).
	unknownArgs map[string]int
}

// NewToolExecStats creates an empty collector.
func NewToolExecStats() *ToolExecStats {
	return &ToolExecStats{}
}

// bump increments a per-tool counter family, lazily allocating the map.
// Nil-safe (no-op on a nil receiver); m points at one of the struct's maps.
func (s *ToolExecStats) bump(m *map[string]int, tool string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if *m == nil {
		*m = make(map[string]int, 2)
	}
	(*m)[tool]++
}

// snapshot returns a copy of a counter family, or nil when empty (so run.end
// omits the field entirely). Nil-safe.
func (s *ToolExecStats) snapshot(m *map[string]int) map[string]int {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return copyCounts(*m)
}

// The public methods must nil-check BEFORE taking a field address — &s.field
// on a nil receiver is itself the nil dereference, so bump/snapshot's own
// guard would come too late.

// RecordRepaired counts one malformed-argument repair for the named tool.
func (s *ToolExecStats) RecordRepaired(tool string) {
	if s != nil {
		s.bump(&s.repaired, tool)
	}
}

// RecordUnknownArgs counts one call carrying schema-undeclared arguments.
func (s *ToolExecStats) RecordUnknownArgs(tool string) {
	if s != nil {
		s.bump(&s.unknownArgs, tool)
	}
}

// RecordCacheHit counts one run-cache hit for the named tool.
func (s *ToolExecStats) RecordCacheHit(tool string) {
	if s != nil {
		s.bump(&s.cacheHits, tool)
	}
}

// RecordTruncated counts one output truncation for the named tool.
func (s *ToolExecStats) RecordTruncated(tool string) {
	if s != nil {
		s.bump(&s.truncated, tool)
	}
}

// RepairedCounts returns a copy of the per-tool repair counters.
func (s *ToolExecStats) RepairedCounts() map[string]int {
	if s == nil {
		return nil
	}
	return s.snapshot(&s.repaired)
}

// UnknownArgCounts returns a copy of the per-tool unknown-argument counters.
func (s *ToolExecStats) UnknownArgCounts() map[string]int {
	if s == nil {
		return nil
	}
	return s.snapshot(&s.unknownArgs)
}

// CacheHitCounts returns a copy of the per-tool cache-hit counters.
func (s *ToolExecStats) CacheHitCounts() map[string]int {
	if s == nil {
		return nil
	}
	return s.snapshot(&s.cacheHits)
}

// TruncatedCounts returns a copy of the per-tool truncation counters.
func (s *ToolExecStats) TruncatedCounts() map[string]int {
	if s == nil {
		return nil
	}
	return s.snapshot(&s.truncated)
}

// copyCounts copies a counter map, returning nil for an empty one. Caller
// must hold s.mu.
func copyCounts(m map[string]int) map[string]int {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]int, len(m))
	for k, v := range m {
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
