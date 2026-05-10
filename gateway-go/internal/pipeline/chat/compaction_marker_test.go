package chat

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
)

func TestCompactionProducedSummary(t *testing.T) {
	cases := []struct {
		name string
		in   compaction.Result
		want bool
	}{
		{"empty", compaction.Result{}, false},
		{"micro only", compaction.Result{MicroPruned: 5}, false},
		{"stub only", compaction.Result{OldToolResultsStubbed: 3}, false},
		{"micro + stub only", compaction.Result{MicroPruned: 5, OldToolResultsStubbed: 3}, false},
		{"LLM compacted", compaction.Result{LLMCompacted: true}, true},
		{"embedding compacted", compaction.Result{EmbeddingCompacted: true}, true},
		{"recency compacted", compaction.Result{RecencyCompacted: true}, true},
		{"emergency evicted", compaction.Result{EmergencyEvicted: 4}, true},
		{"micro + LLM (real summary)", compaction.Result{MicroPruned: 5, LLMCompacted: true}, true},
		{"stub + recency (real summary)", compaction.Result{OldToolResultsStubbed: 2, RecencyCompacted: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := compactionProducedSummary(tc.in); got != tc.want {
				t.Errorf("compactionProducedSummary(%+v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestMarkCompactionFired_SafeWithMissingDeps verifies markCompactionFired
// is a no-op when the session manager is nil or the session key is empty,
// so callers in test environments and sub-agent paths don't crash.
func TestMarkCompactionFired_SafeWithMissingDeps(t *testing.T) {
	// nil deps.sessions
	markCompactionFired(runDeps{}, "session-1", compaction.Result{LLMCompacted: true})
	// empty sessionKey
	markCompactionFired(runDeps{}, "", compaction.Result{LLMCompacted: true})
	// cheap-only result with a non-nil but unused sessions field would
	// still short-circuit before any Get; the result-not-summary guard
	// keeps the path safe.
	markCompactionFired(runDeps{}, "session-1", compaction.Result{MicroPruned: 9})
}
