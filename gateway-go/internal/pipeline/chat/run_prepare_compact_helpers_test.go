package chat

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	compact "github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
)

func TestCompactionTierReturnsHighestImpactOutcome(t *testing.T) {
	tests := []struct {
		name   string
		result compact.Result
		want   string
		ok     bool
	}{
		{"no change stays silent", compact.Result{}, "", false},
		{"micro pruning", compact.Result{MicroPruned: 1}, "micro", true},
		{"recency beats micro", compact.Result{MicroPruned: 1, RecencyCompacted: true}, "tier3-recency", true},
		{"embedding beats recency", compact.Result{RecencyCompacted: true, EmbeddingCompacted: true}, "tier2-embedding-mmr", true},
		{"llm beats embedding", compact.Result{EmbeddingCompacted: true, LLMCompacted: true}, "tier1-llm", true},
		{"emergency beats every tier", compact.Result{LLMCompacted: true, EmergencyEvicted: 1}, "emergency", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := compactionTier(tt.result)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("compactionTier(%+v) = (%q, %v), want (%q, %v)", tt.result, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestReportCompactionDegradedBroadcastsOnlyWhenCompactionRemainsOverBudget(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	params := RunParams{SessionKey: "session-1"}
	var events []ChatCompactionDegradedEvent
	deps := runDeps{broadcast: func(event string, payload json.RawMessage) (int, []error) {
		if event != "chat.compaction_degraded" {
			t.Fatalf("event = %q", event)
		}
		var ev ChatCompactionDegradedEvent
		if err := json.Unmarshal(payload, &ev); err != nil {
			t.Fatalf("unmarshal degraded payload: %v", err)
		}
		events = append(events, ev)
		return 1, nil
	}}

	reportCompactionDegraded(params, deps, 100, compact.Result{TokensBefore: 120, TokensAfter: 110}, logger)
	reportCompactionDegraded(params, deps, 100, compact.Result{TokensBefore: 90, TokensAfter: 110}, logger)
	reportCompactionDegraded(params, deps, 100, compact.Result{TokensBefore: 120, TokensAfter: 90}, logger)
	reportCompactionDegraded(params, deps, 0, compact.Result{TokensBefore: 120, TokensAfter: 110}, logger)

	if len(events) != 1 {
		t.Fatalf("broadcast count = %d, want 1", len(events))
	}
	want := ChatCompactionDegradedEvent{
		Session:      "session-1",
		TokensBefore: 120,
		TokensAfter:  110,
		Budget:       100,
	}
	if events[0] != want {
		t.Fatalf("event = %+v, want %+v", events[0], want)
	}
}
