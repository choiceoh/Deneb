package agentlog

import "testing"

func TestAggregateEffort(t *testing.T) {
	w := NewWriter(t.TempDir())

	// Two routed-off runs: one clean, one that escalated (restored thinking).
	appendEntry(t, w, "s1", "r1", TypeRunEnd, RunEndData{
		StopReason: "end_turn", EffortDecision: "routed:short-conversational", OutputTokens: 100,
	})
	appendEntry(t, w, "s1", "r2", TypeRunEnd, RunEndData{
		StopReason: "end_turn", EffortDecision: "routed:short-conversational", EffortEscalated: true, OutputTokens: 300,
	})
	// Two kept-on runs via different gates.
	appendEntry(t, w, "s2", "r3", TypeRunEnd, RunEndData{
		StopReason: "end_turn", EffortDecision: "kept:hard-signal:분석", OutputTokens: 800,
	})
	appendEntry(t, w, "s2", "r4", TypeRunEnd, RunEndData{
		StopReason: "end_turn", EffortDecision: "kept:context-heavy", OutputTokens: 600,
	})
	// A run where the router was inactive (empty decision) — must be ignored.
	appendEntry(t, w, "s2", "r5", TypeRunEnd, RunEndData{StopReason: "end_turn", OutputTokens: 999})

	s := w.AggregateEffort(0)

	if s.RoutedRuns != 2 || s.KeptRuns != 2 {
		t.Fatalf("routed=%d kept=%d, want 2/2", s.RoutedRuns, s.KeptRuns)
	}
	if s.EscalatedRuns != 1 {
		t.Fatalf("escalated=%d, want 1", s.EscalatedRuns)
	}
	if got := s.EscalationRate(); got != 0.5 {
		t.Fatalf("escalation rate=%.2f, want 0.50", got)
	}
	if got := s.RoutedShare(); got != 0.5 {
		t.Fatalf("routed share=%.2f, want 0.50", got)
	}
	if s.KeptReasons["hard-signal"] != 1 || s.KeptReasons["context-heavy"] != 1 {
		t.Fatalf("kept reasons histogram wrong: %v", s.KeptReasons)
	}
	if s.RoutedOutputTokens != 400 || s.KeptOutputTokens != 1400 {
		t.Fatalf("output tokens routed=%d kept=%d, want 400/1400", s.RoutedOutputTokens, s.KeptOutputTokens)
	}
}

func TestAggregateEffort_EmptyAndNilSafe(t *testing.T) {
	if got := (*Writer)(nil).AggregateEffort(0); got.RoutedRuns != 0 || got.KeptReasons == nil {
		t.Fatalf("nil writer must return a zero stat with an initialized map, got %+v", got)
	}
	w := NewWriter(t.TempDir())
	if got := w.AggregateEffort(0); got.RoutedRuns != 0 || got.KeptRuns != 0 {
		t.Fatalf("empty log must yield zero counts, got %+v", got)
	}
}

func TestEffortReasonCategory(t *testing.T) {
	cases := map[string]string{
		"kept:hard-signal:분석": "hard-signal",
		"kept:context-heavy":  "context-heavy",
		"kept:":               "unknown",
	}
	for in, want := range cases {
		if got := effortReasonCategory(in); got != want {
			t.Errorf("effortReasonCategory(%q)=%q, want %q", in, got, want)
		}
	}
}
