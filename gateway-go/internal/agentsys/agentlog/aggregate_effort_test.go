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

func TestAggregateEffortByModel(t *testing.T) {
	w := NewWriter(t.TempDir())

	// model A: 2 routed (1 escalated) + 1 kept.
	appendEntry(t, w, "s1", "r1", TypeRunEnd, RunEndData{
		Model: "deepseek-v4-flash", StopReason: "end_turn",
		EffortDecision: "routed:short-conversational", OutputTokens: 100,
	})
	appendEntry(t, w, "s1", "r2", TypeRunEnd, RunEndData{
		Model: "deepseek-v4-flash", StopReason: "timeout",
		EffortDecision: "routed:short-conversational", EffortEscalated: true, OutputTokens: 300,
	})
	appendEntry(t, w, "s1", "r3", TypeRunEnd, RunEndData{
		Model: "deepseek-v4-flash", StopReason: "end_turn",
		EffortDecision: "kept:hard-signal:분석", OutputTokens: 800,
	})
	// model B: 1 kept only.
	appendEntry(t, w, "s2", "r4", TypeRunEnd, RunEndData{
		Model: "qwen3.6-35b", StopReason: "end_turn",
		EffortDecision: "kept:context-heavy", OutputTokens: 600,
	})
	// router-inactive run (no decision) — excluded from every bucket.
	appendEntry(t, w, "s2", "r5", TypeRunEnd, RunEndData{Model: "qwen3.6-35b", StopReason: "end_turn"})

	byModel := w.AggregateEffortByModel(0)

	a := byModel["deepseek-v4-flash"]
	if a.RoutedRuns != 2 || a.KeptRuns != 1 || a.EscalatedRuns != 1 {
		t.Fatalf("model A: routed=%d kept=%d escalated=%d, want 2/1/1", a.RoutedRuns, a.KeptRuns, a.EscalatedRuns)
	}
	if a.RoutedTimeout != 1 || a.RoutedEndTurn != 1 {
		t.Fatalf("model A outcomes: endTurn=%d timeout=%d, want 1/1", a.RoutedEndTurn, a.RoutedTimeout)
	}
	if got := a.EscalationRate(); got != 0.5 {
		t.Fatalf("model A escalation rate=%.2f, want 0.50", got)
	}
	if a.KeptReasons["hard-signal"] != 1 {
		t.Fatalf("model A kept reasons wrong: %v", a.KeptReasons)
	}

	b := byModel["qwen3.6-35b"]
	if b.RoutedRuns != 0 || b.KeptRuns != 1 {
		t.Fatalf("model B: routed=%d kept=%d, want 0/1", b.RoutedRuns, b.KeptRuns)
	}
	if len(byModel) != 2 {
		t.Fatalf("want 2 model buckets, got %d: %v", len(byModel), byModel)
	}

	// Per-model and global must agree on the totals.
	global := w.AggregateEffort(0)
	if a.RoutedRuns+b.RoutedRuns != global.RoutedRuns || a.KeptRuns+b.KeptRuns != global.KeptRuns {
		t.Fatalf("per-model totals diverge from global: per-model routed=%d kept=%d, global routed=%d kept=%d",
			a.RoutedRuns+b.RoutedRuns, a.KeptRuns+b.KeptRuns, global.RoutedRuns, global.KeptRuns)
	}
}

func TestAggregateEffortByModel_EmptyAndNilSafe(t *testing.T) {
	if got := (*Writer)(nil).AggregateEffortByModel(0); got == nil || len(got) != 0 {
		t.Fatalf("nil writer must return an empty non-nil map, got %v", got)
	}
	w := NewWriter(t.TempDir())
	if got := w.AggregateEffortByModel(0); len(got) != 0 {
		t.Fatalf("empty log must yield no buckets, got %v", got)
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
