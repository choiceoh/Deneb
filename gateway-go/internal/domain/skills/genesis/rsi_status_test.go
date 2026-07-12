package genesis

import "testing"

func rsiLayerByKey(layers []RSILayer, key string) RSILayer {
	for _, l := range layers {
		if l.Key == key {
			return l
		}
	}
	return RSILayer{}
}

// An untouched tracker reports four layers, none turning — the honest "nothing
// has happened yet" baseline (all IDLE, never a false LIVE).
func TestRSIStatus_EmptyTrackerIsQuiet(t *testing.T) {
	tr := newTestTracker(t)
	st := tr.RSIStatus()
	if len(st.Layers) != 4 {
		t.Fatalf("want 4 layers, got %d", len(st.Layers))
	}
	for i, want := range []string{"L1", "L2", "L3", "L4"} {
		if st.Layers[i].Key != want {
			t.Fatalf("layer %d key = %q, want %q", i, st.Layers[i].Key, want)
		}
		if st.Layers[i].State == RSIStateLive {
			t.Fatalf("empty tracker layer %s must not be LIVE: %+v", want, st.Layers[i])
		}
	}
	if st.Turning != 0 {
		t.Fatalf("turning = %d, want 0", st.Turning)
	}
}

// Each layer flips to LIVE from its own signal, and Turning counts them.
func TestRSIStatus_AllLayersLive(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.LogEvolve("sk", "1.0.1", "tightened"); err != nil { // L1
		t.Fatal(err)
	}
	if err := tr.LogMetaRevision(MetaRevisionRecord{Epoch: metaEpochProducer, Artifact: "evolve.md", Proposed: true}); err != nil { // L2
		t.Fatal(err)
	}
	if err := tr.LogJudgeAccuracy(JudgeAccuracyRecord{ // L3
		JudgeVersion: "v1", Pairs: 4, Correct: 3,
		ByClass: map[string][2]int{"safety-drop": {2, 3}},
		Misses:  []JudgeMissExhibit{{Skill: "sk", Degradation: "safety-drop", Verdict: "passed_defect"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{ // L4
		Scope: "code", Status: SelfCorrectionStatusProposed, SkillName: "sk",
		Title: "tool gap: wiki_search — sk", Source: "evolve-tool-gap",
	}); err != nil {
		t.Fatal(err)
	}

	st := tr.RSIStatus()
	for _, k := range []string{"L1", "L2", "L3", "L4"} {
		if l := rsiLayerByKey(st.Layers, k); l.State != RSIStateLive {
			t.Fatalf("layer %s = %s (want LIVE): %s", k, l.State, l.Diagnosis)
		}
	}
	if st.Turning != 4 {
		t.Fatalf("turning = %d, want 4", st.Turning)
	}
}

// L1 with only rejections (no committed evolve) is DATA-GATED, not IDLE and not
// LIVE — the lane is active but nothing cleared the gate.
func TestRSIStatus_L1DataGatedOnRejectionsOnly(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.LogEvolveRejected("sk", "self-harness audit rejected"); err != nil {
		t.Fatal(err)
	}
	if l := rsiLayerByKey(tr.RSIStatus().Layers, "L1"); l.State != RSIStateDataGated {
		t.Fatalf("L1 = %s, want DATA-GATED", l.State)
	}
}

// L4 with only a non-dispatchable code candidate (e.g. the staged runtime-error
// source, not yet in the dispatch allowlist) is STARVED — code fuel exists but
// nothing is dispatchable. This is the exact honesty the viewer must show.
func TestRSIStatus_L4StarvedOnNonDispatchableCode(t *testing.T) {
	tr := newTestTracker(t)
	if _, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope: "code", Status: SelfCorrectionStatusProposed, SkillName: "gateway-runtime",
		Title: "recurring runtime error: x", Source: "runtime-error:abc123",
	}); err != nil {
		t.Fatal(err)
	}
	l := rsiLayerByKey(tr.RSIStatus().Layers, "L4")
	if l.State != RSIStateStarved {
		t.Fatalf("L4 = %s, want STARVED (%s)", l.State, l.Diagnosis)
	}
}
