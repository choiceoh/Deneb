package agentlog

import (
	"encoding/json"
	"testing"
)

func appendEntry(t *testing.T, w *Writer, session, runID, typ string, data any) {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := w.Append(LogEntry{Ts: 1000, Type: typ, RunID: runID, Session: session, Data: raw}); err != nil {
		t.Fatalf("append: %v", err)
	}
}

func TestAggregateByModel(t *testing.T) {
	w := NewWriter(t.TempDir())

	// Run 1: gemma4 via vllm — clean run with one tool error.
	appendEntry(t, w, "s1", "r1", TypeRunStart, RunStartData{Model: "gemma4", Provider: "vllm"})
	appendEntry(t, w, "s1", "r1", TypeTurnTool, TurnToolData{Turn: 1, Name: "fs", IsError: true})
	appendEntry(t, w, "s1", "r1", TypeRunEnd, RunEndData{
		StopReason: "end_turn", Turns: 3, InputTokens: 100, OutputTokens: 50,
		TotalMs: 2000, ToolCalls: 4, MaxTokensRecoveries: 1, Compacted: true,
	})

	// Run 2: gemma4 again, in a different session file — stalled (timeout),
	// rescued by a fallback model (run.end carries the answering model).
	appendEntry(t, w, "s2", "r2", TypeRunStart, RunStartData{Model: "gemma4", Provider: "vllm", ThinkingLevel: "high"})
	appendEntry(t, w, "s2", "r2", TypeRunEnd, RunEndData{StopReason: "timeout", Turns: 1, TotalMs: 6000, Model: "glm-5-turbo"})

	// Run 3: glm-5-turbo — errored out.
	appendEntry(t, w, "s1", "r3", TypeRunStart, RunStartData{Model: "glm-5-turbo", Provider: "zai"})
	appendEntry(t, w, "s1", "r3", TypeRunError, RunErrorData{Error: "boom"})

	stats := w.AggregateByModel(0)
	if len(stats) != 2 {
		t.Fatalf("got %d models, want 2: %+v", len(stats), stats)
	}

	g := stats[0] // sorted by runs desc → gemma4 first
	if g.Model != "gemma4" || g.Provider != "vllm" {
		t.Fatalf("stats[0] = %s/%s, want vllm/gemma4", g.Provider, g.Model)
	}
	if g.Runs != 2 || g.Turns != 4 || g.TimeoutRuns != 1 || g.CompactedRuns != 1 {
		t.Errorf("gemma4 counters = %+v", g)
	}
	// Run 2 was answered by a different model → attributed to gemma4 (the
	// requested model) but counted as a fallback rescue.
	if g.FallbackRuns != 1 {
		t.Errorf("gemma4 fallbackRuns = %d, want 1", g.FallbackRuns)
	}
	if g.ThinkingRuns != 1 {
		t.Errorf("gemma4 thinkingRuns = %d, want 1 (only r2 ran with thinking)", g.ThinkingRuns)
	}
	if g.InputTokens != 100 || g.OutputTokens != 50 || g.MaxTokensRecoveries != 1 {
		t.Errorf("gemma4 tokens = %+v", g)
	}
	if g.ToolCalls != 4 || g.ToolErrors != 1 {
		t.Errorf("gemma4 tools = calls %d errors %d, want 4/1", g.ToolCalls, g.ToolErrors)
	}
	if g.AvgMs != 4000 || g.P95Ms != 6000 {
		t.Errorf("gemma4 latency avg=%d p95=%d, want 4000/6000", g.AvgMs, g.P95Ms)
	}

	z := stats[1]
	if z.Model != "glm-5-turbo" || z.Runs != 0 || z.Errors != 1 {
		t.Errorf("glm stat = %+v, want 0 runs / 1 error", z)
	}
}

func TestAggregateByModel_SinceFilterAndNilSafety(t *testing.T) {
	var nilW *Writer
	if got := nilW.AggregateByModel(0); got != nil {
		t.Fatal("nil writer must return nil")
	}

	w := NewWriter(t.TempDir())
	appendEntry(t, w, "s1", "r1", TypeRunStart, RunStartData{Model: "m", Provider: "p"})
	appendEntry(t, w, "s1", "r1", TypeRunEnd, RunEndData{StopReason: "end_turn", TotalMs: 10})
	// All fixture entries carry Ts=1000; a later cutoff excludes everything.
	if got := w.AggregateByModel(2000); len(got) != 0 {
		t.Fatalf("since filter leaked entries: %+v", got)
	}
}
