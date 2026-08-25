package agentlog

import "testing"

// TestAggregateByServedModelKeysOnProviderReportedModel locks the label
// contract that motivated this view: a run pinned to one model id but answered
// by another (z.ai serving glm-5.3 behind a "glm-5.1" pin) must report the
// weights that actually ran, while AggregateByModel keeps attributing the run
// to the requested id.
func TestAggregateByServedModelKeysOnProviderReportedModel(t *testing.T) {
	w := NewWriter(t.TempDir())

	appendEntry(t, w, "client:sub:x", "r1", TypeRunStart,
		RunStartData{Model: "glm-5.1", Provider: "zai-subagent"})
	appendEntry(t, w, "client:sub:x", "r1", TypeTurnLLM, TurnLLMData{
		Turn: 1, OutputTokens: 121, ToolCalls: 1, CacheReadTokens: 37504, ProviderModel: "glm-5.3",
	})
	appendEntry(t, w, "client:sub:x", "r1", TypeTurnLLM, TurnLLMData{
		Turn: 2, InputTokens: 10, OutputTokens: 104, CacheReadTokens: 37632, ProviderModel: "glm-5.3",
	})
	appendEntry(t, w, "client:sub:x", "r1", TypeRunEnd, RunEndData{
		StopReason: "end_turn", Turns: 2, InputTokens: 10, OutputTokens: 225,
		Model: "glm-5.1", RequestedModel: "glm-5.1", CacheReadTokens: 75136, ToolCalls: 1,
	})

	stats := w.AggregateByServedModel(0)
	if len(stats) != 1 {
		t.Fatalf("got %d rows, want 1: %+v", len(stats), stats)
	}
	s := stats[0]
	if s.Model != "glm-5.3" || s.Provider != "zai-subagent" {
		t.Fatalf("row = %s/%s, want zai-subagent/glm-5.3", s.Provider, s.Model)
	}
	// One run, and the run's totals — per-turn sums equal run.end's.
	if s.Runs != 1 || s.InputTokens != 10 || s.OutputTokens != 225 || s.CacheReadTokens != 75136 || s.ToolCalls != 1 {
		t.Fatalf("served stat = %+v, want runs=1 in=10 out=225 cacheRead=75136 tools=1", s)
	}

	// The requested-model view is untouched: the tuner still blames the pin.
	byModel := w.AggregateByModel(0)
	if len(byModel) != 1 || byModel[0].Model != "glm-5.1" {
		t.Fatalf("AggregateByModel = %+v, want the requested glm-5.1 row", byModel)
	}
}

// TestAggregateByServedModelSplitsMidRunFallbackAndKeepsUnreportedTurns covers
// the two shapes a served-model view must not smear: a run whose fallback chain
// fired mid-flight splits its tokens across both models (counting as one run
// for each), and a turn whose provider reported no model falls back to the
// requested id instead of vanishing.
func TestAggregateByServedModelSplitsMidRunFallbackAndKeepsUnreportedTurns(t *testing.T) {
	w := NewWriter(t.TempDir())

	appendEntry(t, w, "s1", "r1", TypeRunStart, RunStartData{Model: "k3", Provider: "kimi"})
	appendEntry(t, w, "s1", "r1", TypeTurnLLM, TurnLLMData{Turn: 1, OutputTokens: 10, ProviderModel: "k3"})
	appendEntry(t, w, "s1", "r1", TypeTurnLLM, TurnLLMData{Turn: 2, OutputTokens: 20, ProviderModel: "k3"})
	appendEntry(t, w, "s1", "r1", TypeTurnLLM, TurnLLMData{Turn: 3, OutputTokens: 5, ProviderModel: "deepseek-v4-flash"})

	// A provider that reports no model at all → labeled by the requested id.
	appendEntry(t, w, "s2", "r2", TypeRunStart, RunStartData{Model: "qwen3.6", Provider: "vllm"})
	appendEntry(t, w, "s2", "r2", TypeTurnLLM, TurnLLMData{Turn: 1, InputTokens: 7, OutputTokens: 3})

	got := map[string]ServedModelStat{}
	for _, s := range w.AggregateByServedModel(0) {
		got[s.Provider+"/"+s.Model] = s
	}
	if k3 := got["kimi/k3"]; k3.Runs != 1 || k3.OutputTokens != 30 {
		t.Errorf("kimi/k3 = %+v, want runs=1 out=30 (turns 1-2 only)", k3)
	}
	// The rescue model keeps the run's provider credentials as its label and
	// counts the run once for itself — the run is on both rows, not double-
	// counted inside either.
	if ds := got["kimi/deepseek-v4-flash"]; ds.Runs != 1 || ds.OutputTokens != 5 {
		t.Errorf("kimi/deepseek-v4-flash = %+v, want runs=1 out=5", ds)
	}
	if q := got["vllm/qwen3.6"]; q.Runs != 1 || q.InputTokens != 7 || q.OutputTokens != 3 {
		t.Errorf("vllm/qwen3.6 = %+v, want runs=1 in=7 out=3 (requested id as fallback label)", q)
	}
}

// TestAggregateByServedModelCreditsHelperCallsAndIsolatesRunIDsPerSession keeps
// the two invariants inherited from the other aggregators: one-shot helper.llm
// calls (no turns of their own) still appear, and a run ID reused in another
// session file cannot borrow the first file's requested model as a label.
func TestAggregateByServedModelCreditsHelperCallsAndIsolatesRunIDsPerSession(t *testing.T) {
	w := NewWriter(t.TempDir())

	appendEntry(t, w, SessionHelper, "", TypeHelperLLM, HelperLLMData{
		Model: "qwen3.6", Provider: "wormhole", Role: "tiny",
		InputTokens: 100, OutputTokens: 20, CacheReadTokens: 5,
	})

	appendEntry(t, w, "s1", "shared", TypeRunStart, RunStartData{Model: "glm-5.2", Provider: "zai"})
	// Same run ID in a different session file, with no run.start of its own and
	// no provider-reported model: unattributable, so it must be dropped rather
	// than credited to s1's glm-5.2.
	appendEntry(t, w, "s2", "shared", TypeTurnLLM, TurnLLMData{Turn: 1, OutputTokens: 99})

	got := map[string]ServedModelStat{}
	for _, s := range w.AggregateByServedModel(0) {
		got[s.Provider+"/"+s.Model] = s
	}
	if h := got["wormhole/qwen3.6"]; h.Runs != 1 || h.InputTokens != 100 || h.OutputTokens != 20 || h.CacheReadTokens != 5 {
		t.Errorf("helper stat = %+v, want runs=1 in=100 out=20 cacheRead=5", h)
	}
	if _, leaked := got["zai/glm-5.2"]; leaked {
		t.Errorf("cross-session run ID leaked a label: %+v", got)
	}
}

// TestAggregateByServedModelCountsRunsReusingOneRunIDSeparately guards the
// gateway-restart shape: the per-session run counter restarts at run_0000, so
// one session file holds several runs under the same ID. Each must count as its
// own run — otherwise the served view silently reports fewer runs than
// AggregateByModel over the same window.
func TestAggregateByServedModelCountsRunsReusingOneRunIDSeparately(t *testing.T) {
	w := NewWriter(t.TempDir())

	for i := 0; i < 3; i++ {
		appendEntry(t, w, "client:main", "run_0000", TypeRunStart,
			RunStartData{Model: "glm-5.2", Provider: "wormhole"})
		appendEntry(t, w, "client:main", "run_0000", TypeTurnLLM,
			TurnLLMData{Turn: 1, OutputTokens: 10, ProviderModel: "glm-5.3"})
		appendEntry(t, w, "client:main", "run_0000", TypeRunEnd,
			RunEndData{StopReason: "end_turn", Turns: 1, OutputTokens: 10, Model: "glm-5.2"})
	}
	// A fourth run that ended before any turn completed — no turn.llm line at
	// all. It still has to appear in the run count.
	appendEntry(t, w, "client:main", "run_0000", TypeRunStart,
		RunStartData{Model: "glm-5.2", Provider: "wormhole"})
	appendEntry(t, w, "client:main", "run_0000", TypeRunEnd,
		RunEndData{StopReason: "aborted", Model: "glm-5.2"})

	stats := w.AggregateByServedModel(0)
	byKey := map[string]ServedModelStat{}
	total := 0
	for _, s := range stats {
		byKey[s.Provider+"/"+s.Model] = s
		total += s.Runs
	}
	if served := byKey["wormhole/glm-5.3"]; served.Runs != 3 || served.OutputTokens != 30 {
		t.Errorf("served glm-5.3 = %+v, want runs=3 out=30", served)
	}
	if aborted := byKey["wormhole/glm-5.2"]; aborted.Runs != 1 {
		t.Errorf("turn-less run = %+v, want runs=1 under the requested label", aborted)
	}
	// Run and token totals must match the requested-model view exactly — the
	// served view relabels usage, it never loses it.
	var wantRuns int
	var wantOut int64
	for _, s := range w.AggregateByModel(0) {
		wantRuns += s.Runs
		wantOut += s.OutputTokens
	}
	var gotOut int64
	for _, s := range stats {
		gotOut += s.OutputTokens
	}
	if total != wantRuns || gotOut != wantOut {
		t.Errorf("served totals runs=%d out=%d, want runs=%d out=%d", total, gotOut, wantRuns, wantOut)
	}
}
