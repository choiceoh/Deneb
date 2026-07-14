package genesis

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func rsiLayerByKey(layers []rsiLayer, key string) rsiLayer {
	for _, l := range layers {
		if l.Key == key {
			return l
		}
	}
	return rsiLayer{}
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

// An untouched tracker reports the four loop layers plus the graduation-
// ladder dashboard, none turning — the honest "nothing has happened yet"
// baseline (all quiet, never a false LIVE).
func TestRSIStatus_EmptyTrackerIsQuiet(t *testing.T) {
	tr := newTestTracker(t)
	st := tr.RSIStatus()
	if len(st.Layers) != 5 {
		t.Fatalf("want 5 layers (L1-L4 + GRAD), got %d", len(st.Layers))
	}
	for i, want := range []string{"L1", "L2", "L3", "L4", "GRAD"} {
		if st.Layers[i].Key != want {
			t.Fatalf("layer %d key = %q, want %q", i, st.Layers[i].Key, want)
		}
		if st.Layers[i].State == rsiStateLive {
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
	if err := tr.logJudgeAccuracy(judgeAccuracyRecord{ // L3
		JudgeVersion: "v1", Pairs: 4, Correct: 3,
		ByClass: map[string][2]int{"safety-drop": {2, 3}},
		Misses:  []judgeMissExhibit{{Skill: "sk", Degradation: "safety-drop", Verdict: "passed_defect"}},
	}); err != nil {
		t.Fatal(err)
	}
	l4, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{ // L4 supply
		Scope: "code", Status: SelfCorrectionStatusProposed, SkillName: "sk",
		Title: "tool gap: wiki_search — sk", Source: "evolve-tool-gap",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.RecordSelfCorrectionDispatch(SelfCorrectionCandidateRecord{ // actual turn
		ID: l4.ID, DispatchPhase: selfCorrectionDispatchStarted, AttemptID: "attempt-live",
	}); err != nil {
		t.Fatal(err)
	}

	st := tr.RSIStatus()
	for _, k := range []string{"L1", "L2", "L3", "L4"} {
		if l := rsiLayerByKey(st.Layers, k); l.State != rsiStateLive {
			t.Fatalf("layer %s = %s (want LIVE): %s", k, l.State, l.Diagnosis)
		}
	}
	if st.Turning != 4 {
		t.Fatalf("turning = %d, want 4", st.Turning)
	}
}

func TestRSIStatus_L3OperatorLabelIsLiveWithoutSyntheticRun(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.LogOperatorJudgeVerdict(OperatorJudgeVerdict{
		DecisionID: "sk@1.0.1", Skill: "sk", Version: "1.0.1",
		Verdict: OperatorJudgeVerdictConfirm, JudgeVersion: "v1", JudgeMargin: 1.0,
	}); err != nil {
		t.Fatal(err)
	}
	l := rsiLayerByKey(tr.RSIStatus().Layers, "L3")
	if l.State != rsiStateLive || rsiMetricValue(l.Metrics, "운영자 라벨(7일)") != "1" {
		t.Fatalf("L3 operator label status = %+v", l)
	}
}

// L1 with only rejections (no committed evolve) is DATA-GATED, not IDLE and not
// LIVE — the lane is active but nothing cleared the gate.
func TestRSIStatus_L1DataGatedOnRejectionsOnly(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.LogEvolveRejected("sk", "self-harness audit rejected"); err != nil {
		t.Fatal(err)
	}
	if l := rsiLayerByKey(tr.RSIStatus().Layers, "L1"); l.State != rsiStateDataGated {
		t.Fatalf("L1 = %s, want DATA-GATED", l.State)
	}
}

// Proposals without commits are also DATA-GATED (Python assess_l1 parity) —
// previously they fell through to IDLE because EvolutionHealth ignored
// evolution_proposal events.
func TestRSIStatus_L1DataGatedOnProposalsOnly(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.LogEvolutionProposal(EvolutionProposalRecord{
		Candidate: "repeatable deploy fix", Route: "genesis",
	}); err != nil {
		t.Fatal(err)
	}
	l := rsiLayerByKey(tr.RSIStatus().Layers, "L1")
	if l.State != rsiStateDataGated {
		t.Fatalf("L1 = %s, want DATA-GATED (%s)", l.State, l.Diagnosis)
	}
	if got := rsiMetricValue(l.Metrics, "제안"); got != "1" {
		t.Fatalf("proposals metric = %q, want 1 (%+v)", got, l.Metrics)
	}
}

// L2 LIVE uses a 14d look-back so a weekly revision older than 7d does not
// flip the slow loop IDLE mid-week (Python assess_l2 parity). Diagnosis must
// quote 14d cycle counts — not the 7d scoreboard (a 10d-old cycle is LIVE with
// 7d metrics still at 0).
func TestRSIStatus_L2LiveWithin14dWindow(t *testing.T) {
	tr := newTestTracker(t)
	tenDaysAgo := time.Now().Add(-10 * 24 * time.Hour).UnixMilli()
	if err := tr.LogMetaRevision(MetaRevisionRecord{
		Epoch: metaEpochProducer, Artifact: "evolve.md", Proposed: true, CreatedAt: tenDaysAgo,
	}); err != nil {
		t.Fatal(err)
	}
	l := rsiLayerByKey(tr.RSIStatus().Layers, "L2")
	if l.State != rsiStateLive {
		t.Fatalf("L2 = %s, want LIVE (%s)", l.State, l.Diagnosis)
	}
	if !strings.Contains(l.Diagnosis, "(14일)") {
		t.Fatalf("L2 diagnosis should cite 14d window, got %q", l.Diagnosis)
	}
	if !strings.Contains(l.Diagnosis, "1 사이클") || !strings.Contains(l.Diagnosis, "1 제안") {
		t.Fatalf("L2 diagnosis should count the 10d-old cycle, got %q", l.Diagnosis)
	}
	// Scoreboard metrics stay on 7d — the 10d-old row must not inflate them.
	if got := rsiMetricValue(l.Metrics, "개정(7일)"); got != "0" {
		t.Fatalf("7d revisions metric = %q, want 0 (%+v)", got, l.Metrics)
	}
}

// A marker receipt alone is not proof that the authoritative dispatch loop is
// turning; only a lifecycle event may make L4 LIVE.
func TestRSIStatus_L4MarkerTodayDoesNotClaimLive(t *testing.T) {
	tr := newTestTracker(t)
	dir := filepath.Join(filepath.Dir(tr.selfCorrectionPath), "coding_dispatch")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sc-today.json"), []byte(`{"id":"sc-today"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	l := rsiLayerByKey(tr.RSIStatus().Layers, "L4")
	if l.State != rsiStateIdle {
		t.Fatalf("L4 = %s, want IDLE (%s)", l.State, l.Diagnosis)
	}
	if got := rsiMetricValue(l.Metrics, "오늘 배차"); got != "1" {
		t.Fatalf("dispatched today metric = %q, want 1 (%+v)", got, l.Metrics)
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
	if l.State != rsiStateStarved {
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
// while a staged runtime-error next to it keeps counting. Supply alone remains
// IDLE until the authoritative dispatcher records a started lifecycle.
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
	if l.State != rsiStateIdle {
		t.Fatalf("L4 = %s, want IDLE (%s)", l.State, l.Diagnosis)
	}
	if got := rsiMetricValue(l.Metrics, "배차 가능"); got != "1" {
		t.Fatalf("dispatchable metric = %q, want 1 (%+v)", got, l.Metrics)
	}
	if got := rsiMetricValue(l.Metrics, "검토 대기(비배차)"); got != "1" {
		t.Fatalf("staged metric = %q, want 1", got)
	}
}

// A dispatch-source candidate remains visible as queued supply alongside
// staged extras without claiming that L4 is already turning.
func TestRSIStatus_L4QueuedWithStagedExtras(t *testing.T) {
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
	if l.State != rsiStateIdle {
		t.Fatalf("L4 = %s, want IDLE (%s)", l.State, l.Diagnosis)
	}
	if got := rsiMetricValue(l.Metrics, "검토 대기(비배차)"); got != "1" {
		t.Fatalf("staged metric = %q, want 1 (%+v)", got, l.Metrics)
	}
}

// Review-endorsed (accepted) candidates are queued dispatch supply, not settled:
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
	if l.State != rsiStateIdle {
		t.Fatalf("L4 = %s, want IDLE (%s)", l.State, l.Diagnosis)
	}
	if got := rsiMetricValue(l.Metrics, "배차 가능"); got != "1" {
		t.Fatalf("dispatchable metric = %q, want 1 (%+v)", got, l.Metrics)
	}
}

func TestRSIStatus_L4DispatcherFailureSurfacesAsStarved(t *testing.T) {
	tr := newTestTracker(t)
	if _, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope: "code", Status: SelfCorrectionStatusProposed, SkillName: "sk",
		Title: "tool gap", Source: "self-harness:test",
	}); err != nil {
		t.Fatal(err)
	}
	statusPath := filepath.Join(filepath.Dir(tr.selfCorrectionPath), "coding_dispatch_status.json")
	if err := os.WriteFile(statusPath, []byte(`{"lastTickAtMs":1,"lastResult":"setup_failed","detail":"branch conflict","consecutiveFailures":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	l := rsiLayerByKey(tr.RSIStatus().Layers, "L4")
	if l.State != rsiStateStarved || !strings.Contains(l.Diagnosis, "2회 연속 실패") {
		t.Fatalf("dispatcher failure hidden: %+v", l)
	}
	if got := rsiMetricValue(l.Metrics, "배차 틱"); !strings.Contains(got, "setup_failed") {
		t.Fatalf("dispatch tick metric = %q", got)
	}
}

func TestRSIStatus_L4FailedAttemptOnlyRequeuesWithoutUnlandedWork(t *testing.T) {
	tr := newTestTracker(t)
	if _, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		ID: "retry", Scope: "code", Status: SelfCorrectionStatusProposed,
		Title: "retry", Source: "self-harness:test",
	}); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{selfCorrectionDispatchStarted, selfCorrectionDispatchFailed} {
		if _, err := tr.RecordSelfCorrectionDispatch(SelfCorrectionCandidateRecord{
			ID: "retry", DispatchPhase: phase, AttemptID: "attempt-1",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(tr.dispatchMarkerDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(tr.dispatchMarkerDir(), "retry.json")
	if err := os.WriteFile(marker, []byte(`{"id":"retry","outcome":"attempted"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := rsiMetricValue(tr.rsiAssessL4().Metrics, "배차 가능"); got != "0" {
		t.Fatalf("unlanded work requeued: dispatchable=%s", got)
	}
	if err := os.WriteFile(marker, []byte(`{"id":"retry","outcome":"failed"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := rsiMetricValue(tr.rsiAssessL4().Metrics, "배차 가능"); got != "1" {
		t.Fatalf("failed attempt did not requeue: dispatchable=%s", got)
	}
}

func TestRSIStatus_L4UsesDispatchLifecycleInsteadOfCountingMarkersAsQueued(t *testing.T) {
	tr := newTestTracker(t)
	makeCandidate := func(id string) {
		t.Helper()
		if _, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
			ID: id, Scope: "code", Title: id, Source: "self-harness:test",
		}); err != nil {
			t.Fatal(err)
		}
	}
	record := func(id, phase string) {
		t.Helper()
		if _, err := tr.RecordSelfCorrectionDispatch(SelfCorrectionCandidateRecord{
			ID: id, DispatchPhase: phase, AttemptID: "attempt-" + id,
		}); err != nil {
			t.Fatalf("%s %s: %v", id, phase, err)
		}
	}

	makeCandidate("in-flight")
	record("in-flight", selfCorrectionDispatchStarted)
	makeCandidate("closed")
	for _, phase := range []string{
		selfCorrectionDispatchStarted, SelfCorrectionDispatchMerged,
		selfCorrectionDispatchDeployed, selfCorrectionDispatchWatchPassed,
	} {
		record("closed", phase)
	}
	makeCandidate("failed")
	record("failed", selfCorrectionDispatchStarted)
	record("failed", selfCorrectionDispatchFailed)

	l := rsiLayerByKey(tr.RSIStatus().Layers, "L4")
	if l.State != rsiStateLive {
		t.Fatalf("L4 = %s, want LIVE (%s)", l.State, l.Diagnosis)
	}
	for label, want := range map[string]string{
		"배차 가능": "1", "진행 중": "1", "감시 통과": "1", "실패/롤백": "1",
	} {
		if got := rsiMetricValue(l.Metrics, label); got != want {
			t.Fatalf("%s = %q, want %q (%+v)", label, got, want, l.Metrics)
		}
	}
}

func rsiMetricValue(metrics []rsiMetric, label string) string {
	for _, m := range metrics {
		if m.Label == label {
			return m.Value
		}
	}
	return ""
}

// Dispatch-outcome accounting (graduation-ladder evidence): recorded marker
// outcomes surface as a land-rate note on the L4 diagnosis; a queue whose
// markers predate outcome accounting stays silent (no fabricated 0% rate).
func TestRSIStatus_DispatchOutcomeNote(t *testing.T) {
	tr := newTestTracker(t)
	dir := tr.dispatchMarkerDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.json", `{"id":"a","outcome":"landed"}`)
	write("b.json", `{"id":"b","outcome":"declined"}`)
	write("c.json", `{"id":"c"}`) // pre-accounting marker — carries no outcome

	l4 := tr.rsiAssessL4()
	if !strings.Contains(l4.Diagnosis, "배차 결과") || !strings.Contains(l4.Diagnosis, "랜딩률 50%") {
		t.Fatalf("L4 diagnosis missing outcome note: %s", l4.Diagnosis)
	}
	if !strings.Contains(l4.Diagnosis, "declined 1") || !strings.Contains(l4.Diagnosis, "landed 1") {
		t.Fatalf("L4 diagnosis missing outcome counts: %s", l4.Diagnosis)
	}

	if d := newTestTracker(t).rsiAssessL4().Diagnosis; strings.Contains(d, "배차 결과") {
		t.Fatalf("no-outcome queue must not fabricate a rate: %s", d)
	}
}

func TestDispatchMarkerBlocks_parityWithPython(t *testing.T) {
	tr := newTestTracker(t)
	dir := tr.dispatchMarkerDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(id, body string) string {
		t.Helper()
		path := filepath.Join(dir, id+".json")
		if err := os.WriteFile(path, []byte(body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	now := time.Now()
	write("landed", `{"outcome":"landed"}`)
	write("attempted", `{"outcome":"attempted"}`)
	write("declined", `{"outcome":"declined"}`)
	write("failed", `{"outcome":"failed"}`)
	write("timeout", `{"outcome":"timeout"}`)
	fresh := write("fresh", `{"id":"fresh"}`)
	stale := write("stale", `{"id":"stale"}`)
	if err := os.Chtimes(fresh, now, now); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-dispatchMarkerAbandonAfter - time.Minute)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		id   string
		want bool
	}{
		{"", false},
		{"missing", false},
		{"landed", true},
		{"attempted", true},
		{"declined", false},
		{"failed", false},
		{"timeout", false},
		{"fresh", true},
		{"stale", false},
	}
	for _, tc := range cases {
		if got := tr.dispatchMarkerBlocksAt(tc.id, now); got != tc.want {
			t.Fatalf("%s: DispatchMarkerBlocks=%v want %v", tc.id, got, tc.want)
		}
	}
}
