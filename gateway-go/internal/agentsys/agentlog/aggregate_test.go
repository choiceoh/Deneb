package agentlog

import (
	"testing"
	"time"
)

func TestAggregate(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)

	// One agent run with three tool calls (fs x2, exec x1-with-error) and an end.
	rl := NewRunLogger(w, "client:main", "run1")
	rl.LogTurnTool(TurnToolData{Turn: 1, Name: "fs", DurationMs: 100})
	rl.LogTurnTool(TurnToolData{Turn: 1, Name: "fs", DurationMs: 200})
	rl.LogTurnTool(TurnToolData{Turn: 2, Name: "exec", DurationMs: 50, IsError: true})
	rl.LogEnd(RunEndData{
		Turns:           2,
		InputTokens:     1000,
		OutputTokens:    200,
		CacheReadTokens: 800,
		Compacted:       true,
	})

	// Standalone behavioral events.
	w.LogEvent(SessionProactive, TypeProactiveRelay, ProactiveRelayData{Decision: "delivered"})
	w.LogEvent(SessionProactive, TypeProactiveRelay, ProactiveRelayData{Decision: "suppressed", Reason: "contentless"})
	w.LogEvent(SessionBackground, TypeBackgroundJob, BackgroundJobData{Kind: "autonomous", Name: "gmailpoll", Outcome: "ok"})
	w.LogEvent(SessionBackground, TypeBackgroundJob, BackgroundJobData{Kind: "autonomous", Name: "gmailpoll", Outcome: "error"})

	agg := w.Aggregate(0)

	if agg.Runs != 1 {
		t.Errorf("Runs = %d, want 1", agg.Runs)
	}
	if agg.CompactedRuns != 1 {
		t.Errorf("CompactedRuns = %d, want 1", agg.CompactedRuns)
	}
	if agg.CacheReadTokens != 800 {
		t.Errorf("CacheReadTokens = %d, want 800", agg.CacheReadTokens)
	}
	if agg.TotalInputTokens != 1000 || agg.TotalOutputTokens != 200 {
		t.Errorf("tokens = %d/%d, want 1000/200", agg.TotalInputTokens, agg.TotalOutputTokens)
	}

	// Tools sorted by calls desc: fs (2 calls, avg 150ms, 0 err) then exec (1 call, 1 err).
	if len(agg.Tools) != 2 {
		t.Fatalf("Tools len = %d, want 2", len(agg.Tools))
	}
	if agg.Tools[0].Name != "fs" || agg.Tools[0].Calls != 2 || agg.Tools[0].AvgMs != 150 || agg.Tools[0].Errors != 0 {
		t.Errorf("Tools[0] = %+v, want fs/calls2/avg150/err0", agg.Tools[0])
	}
	if agg.Tools[1].Name != "exec" || agg.Tools[1].Calls != 1 || agg.Tools[1].Errors != 1 {
		t.Errorf("Tools[1] = %+v, want exec/calls1/err1", agg.Tools[1])
	}

	// Proactive funnel keyed by decision[:reason].
	if agg.ProactiveDecisions["delivered"] != 1 {
		t.Errorf("delivered = %d, want 1", agg.ProactiveDecisions["delivered"])
	}
	if agg.ProactiveDecisions["suppressed:contentless"] != 1 {
		t.Errorf("suppressed:contentless = %d, want 1", agg.ProactiveDecisions["suppressed:contentless"])
	}

	// Background jobs by name + error subcount.
	if agg.BackgroundJobs["gmailpoll"] != 2 {
		t.Errorf("gmailpoll cycles = %d, want 2", agg.BackgroundJobs["gmailpoll"])
	}
	if agg.BackgroundErrors["gmailpoll"] != 1 {
		t.Errorf("gmailpoll errors = %d, want 1", agg.BackgroundErrors["gmailpoll"])
	}
}

// TestAggregateSince verifies the sinceMs cutoff excludes older entries: a
// cutoff in the future yields an empty roll-up even though entries exist.
func TestAggregateSince(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	rl := NewRunLogger(w, "client:main", "run1")
	rl.LogTurnTool(TurnToolData{Turn: 1, Name: "fs", DurationMs: 100})
	rl.LogEnd(RunEndData{Turns: 1})

	future := time.Now().Add(time.Hour).UnixMilli()
	agg := w.Aggregate(future)
	if agg.Runs != 0 || len(agg.Tools) != 0 {
		t.Errorf("future cutoff: Runs=%d Tools=%d, want 0/0", agg.Runs, len(agg.Tools))
	}
}
