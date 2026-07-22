package agentlog

import "testing"

// TestHelperLLMFoldsIntoModelRoleAndSessionUsage locks the contract that one-shot
// helper.llm events — the local-model calls that never run a full agent turn —
// are credited across all three usage views: by model, by requested role, and by
// session. Without this, local models used only for helper work were invisible.
func TestHelperLLMFoldsIntoModelRoleAndSessionUsage(t *testing.T) {
	w := NewWriter(t.TempDir())

	// A normal agent run (cloud model) to prove helper events coexist with, and do
	// not corrupt, run.start/run.end correlation.
	appendEntry(t, w, "client:main", "run-1", TypeRunStart,
		RunStartData{Model: "glm-5.2", Provider: "wormhole"})
	appendEntry(t, w, "client:main", "run-1", TypeRunEnd,
		RunEndData{Turns: 1, InputTokens: 1000, OutputTokens: 100})

	// Two helper calls answered by the SAME local model but requested under two
	// different roles — the case that motivated per-role folding (roles sharing one
	// underlying model must still separate).
	appendEntry(t, w, SessionHelper, "", TypeHelperLLM,
		HelperLLMData{
			Model: "qwen3.6", Provider: "wormhole", Role: "tiny",
			InputTokens: 100, OutputTokens: 20, CacheReadTokens: 5,
		})
	appendEntry(t, w, SessionHelper, "", TypeHelperLLM,
		HelperLLMData{
			Model: "qwen3.6", Provider: "wormhole", Role: "lightweight",
			InputTokens: 200, OutputTokens: 40,
		})

	// By model: the local model appears with both helper calls summed, keyed by
	// provider/model, alongside the cloud model from the real run.
	byModel := map[string]ModelStat{}
	for _, s := range w.AggregateByModel(0) {
		byModel[s.Provider+"/"+s.Model] = s
	}
	local, ok := byModel["wormhole/qwen3.6"]
	if !ok {
		t.Fatalf("local model missing from AggregateByModel: %+v", byModel)
	}
	if local.Runs != 2 || local.InputTokens != 300 || local.OutputTokens != 60 || local.CacheReadTokens != 5 {
		t.Fatalf("local model stat = %+v, want runs=2 in=300 out=60 cacheRead=5", local)
	}
	if _, ok := byModel["wormhole/glm-5.2"]; !ok {
		t.Fatalf("cloud run.end model lost when helper events present: %+v", byModel)
	}

	// By requested role: the two helper calls split by role, not merged.
	byRole := map[string]RequestedModelStat{}
	for _, s := range w.AggregateByRequestedModel(0) {
		byRole[s.RequestedModel] = s
	}
	if tiny := byRole["tiny"]; tiny.Runs != 1 || tiny.InputTokens != 100 || tiny.OutputTokens != 20 {
		t.Fatalf("tiny role stat = %+v, want runs=1 in=100 out=20", tiny)
	}
	if light := byRole["lightweight"]; light.Runs != 1 || light.InputTokens != 200 || light.OutputTokens != 40 {
		t.Fatalf("lightweight role stat = %+v, want runs=1 in=200 out=40", light)
	}

	// By session: both helper calls fold under the shared system:helper bucket.
	bySession := map[string]SessionStat{}
	for _, s := range w.AggregateBySession(0) {
		bySession[s.Session] = s
	}
	helper := bySession[SessionHelper]
	if helper.Runs != 2 || helper.InputTokens != 300 || helper.OutputTokens != 60 || helper.CacheReadTokens != 5 {
		t.Fatalf("system:helper session stat = %+v, want runs=2 in=300 out=60 cacheRead=5", helper)
	}
}
