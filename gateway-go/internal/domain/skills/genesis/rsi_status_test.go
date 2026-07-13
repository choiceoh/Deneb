package genesis

import (
	"strings"
	"testing"
)

func rsiLayerByKey(layers []RSILayer, key string) RSILayer {
	for _, l := range layers {
		if l.Key == key {
			return l
		}
	}
	return RSILayer{}
}

// SourceAutoDispatches is the single graduation predicate the L4 count and the
// wire projection both read — it must include tool-quality (:desc/:latency) as
// of the 2026-07-13 graduation, and exclude still-staged sources.
func TestSourceAutoDispatches(t *testing.T) {
	graduated := []string{
		"evolve-tool-gap", "self-harness",
		"health-finding:volatile-hub:46a3", "tool-quality:web:desc", "tool-quality:exec:latency",
	}
	for _, s := range graduated {
		if !SourceAutoDispatches(s) {
			t.Fatalf("%q should be auto-dispatch (graduated)", s)
		}
	}
	staged := []string{"runtime-error:abc", "deadcode-finding:1a2b3c", "sop-mining:xyz", ""}
	for _, s := range staged {
		if SourceAutoDispatches(s) {
			t.Fatalf("%q should be staged (not auto-dispatch)", s)
		}
	}
}

// A tool-quality code candidate now counts as dispatchable in L4 (the graduation
// bug: the allowlist previously omitted it, undercounting the client's L4 view).
func TestRSIStatus_L4ToolQualityGraduated(t *testing.T) {
	tr := newTestTracker(t)
	if _, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope: "code", Status: SelfCorrectionStatusProposed, SkillName: "tool-quality",
		Title: "tool description/schema quality: web", Source: "tool-quality:web:desc",
	}); err != nil {
		t.Fatal(err)
	}
	l := rsiLayerByKey(tr.RSIStatus().Layers, "L4")
	if got := rsiMetricValue(l.Metrics, "배차 가능"); got != "1" {
		t.Fatalf("tool-quality candidate dispatchable = %q, want 1 (%+v)", got, l.Metrics)
	}
}

// The structured health block is always present on the snapshot, and its rates
// stay in [0,1] — clients draw a scoreboard from it instead of parsing strings.
func TestRSIStatus_HealthBlockPresent(t *testing.T) {
	st := newTestTracker(t).RSIStatus()
	if st.Health.ConfirmRate < 0 || st.Health.ConfirmRate > 1 {
		t.Fatalf("confirmRate out of range: %v", st.Health.ConfirmRate)
	}
	if st.Health.FalseAcceptRate < 0 || st.Health.FalseAcceptRate > 1 {
		t.Fatalf("falseAcceptRate out of range: %v", st.Health.FalseAcceptRate)
	}
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
// nothing is dispatchable. The staged metric and diagnosis must say so
// explicitly: supply awaiting review, not a wiring gap.
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
	if got := rsiMetricValue(l.Metrics, "검토 대기(비배차)"); got != "1" {
		t.Fatalf("staged metric = %q, want 1 (%+v)", got, l.Metrics)
	}
	if got := rsiMetricValue(l.Metrics, "배차 가능"); got != "0" {
		t.Fatalf("dispatchable metric = %q, want 0", got)
	}
	if !strings.Contains(l.Diagnosis, "검토 대기") {
		t.Fatalf("diagnosis must name the staged supply, got %q", l.Diagnosis)
	}
}

// Graduation regression (2026-07-12): health-finding cleared its first batch
// review and is in the dispatch allowlist — its candidates count dispatchable
// and turn L4 LIVE while a staged runtime-error next to it keeps counting.
func TestRSIStatus_L4HealthFindingGraduated(t *testing.T) {
	tr := newTestTracker(t)
	for _, rec := range []SelfCorrectionCandidateRecord{
		{
			Scope: "code", Status: SelfCorrectionStatusProposed, SkillName: "codebase-health",
			Title: "structural finding: volatile-hub @ domain/wiki", Source: "health-finding:volatile-hub:46a381ef4981",
		},
		{
			Scope: "code", Status: SelfCorrectionStatusProposed, SkillName: "gateway-runtime",
			Title: "recurring runtime error: x", Source: "runtime-error:abc123",
		},
	} {
		if _, err := tr.RecordSelfCorrectionCandidate(rec); err != nil {
			t.Fatal(err)
		}
	}
	l := rsiLayerByKey(tr.RSIStatus().Layers, "L4")
	if l.State != RSIStateLive {
		t.Fatalf("L4 = %s, want LIVE (%s)", l.State, l.Diagnosis)
	}
	if got := rsiMetricValue(l.Metrics, "배차 가능"); got != "1" {
		t.Fatalf("dispatchable metric = %q, want 1 (%+v)", got, l.Metrics)
	}
	if got := rsiMetricValue(l.Metrics, "검토 대기(비배차)"); got != "1" {
		t.Fatalf("staged metric = %q, want 1", got)
	}
}

// A dispatch-source candidate keeps L4 LIVE regardless of staged extras, and
// staged still counts the non-dispatch supply next to it.
func TestRSIStatus_L4LiveWithStagedExtras(t *testing.T) {
	tr := newTestTracker(t)
	for _, rec := range []SelfCorrectionCandidateRecord{
		{Scope: "code", Status: SelfCorrectionStatusProposed, SkillName: "sk", Title: "tool gap: wiki_search — sk", Source: "evolve-tool-gap"},
		{Scope: "code", Status: SelfCorrectionStatusProposed, SkillName: "gateway-runtime", Title: "recurring runtime error: x", Source: "runtime-error:abc123"},
	} {
		if _, err := tr.RecordSelfCorrectionCandidate(rec); err != nil {
			t.Fatal(err)
		}
	}
	l := rsiLayerByKey(tr.RSIStatus().Layers, "L4")
	if l.State != RSIStateLive {
		t.Fatalf("L4 = %s, want LIVE (%s)", l.State, l.Diagnosis)
	}
	if got := rsiMetricValue(l.Metrics, "검토 대기(비배차)"); got != "1" {
		t.Fatalf("staged metric = %q, want 1 (%+v)", got, l.Metrics)
	}
}

// Review-endorsed (accepted) candidates are live dispatch supply, not settled:
// the heartbeat review lane accepts candidates it cannot implement itself, and
// the dispatcher picks them first (2026-07-12 status-contract fix).
func TestRSIStatus_L4AcceptedCountsDispatchable(t *testing.T) {
	tr := newTestTracker(t)
	rec, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope: "code", Status: SelfCorrectionStatusProposed, SkillName: "codebase-health",
		Title: "structural finding: fanout", Source: "health-finding:fanout-hotspot:aa",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.RecordSelfCorrectionReview(SelfCorrectionCandidateRecord{
		ID: rec.ID, Status: SelfCorrectionStatusAccepted, ReviewNote: "코딩 에이전트 후속",
	}); err != nil {
		t.Fatal(err)
	}
	l := rsiLayerByKey(tr.RSIStatus().Layers, "L4")
	if l.State != RSIStateLive {
		t.Fatalf("L4 = %s, want LIVE (%s)", l.State, l.Diagnosis)
	}
	if got := rsiMetricValue(l.Metrics, "배차 가능"); got != "1" {
		t.Fatalf("dispatchable metric = %q, want 1 (%+v)", got, l.Metrics)
	}
}

func rsiMetricValue(metrics []RSIMetricKV, label string) string {
	for _, m := range metrics {
		if m.Label == label {
			return m.Value
		}
	}
	return ""
}
