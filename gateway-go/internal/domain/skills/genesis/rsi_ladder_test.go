package genesis

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The ladder engine turns machine-readable evidence into row states without
// ever flipping a lock: thresholds met read 준비됨, accumulating evidence
// reads 축적 중, and the aggregate card goes LIVE only when a row is READY.
func TestRSIAssessLadderFlipsLiveWhenARowReachesReady(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tr := newTestTracker(t)

	// Empty evidence: every machine row accumulates, aggregate DATA-GATED.
	l := tr.rsiAssessLadder()
	if l.State != rsiStateDataGated || l.Key != "GRAD" {
		t.Fatalf("empty ladder = %s/%s: %s", l.Key, l.State, l.Diagnosis)
	}
	if len(l.Metrics) != 5 {
		t.Fatalf("want 5 ladder rows, got %d", len(l.Metrics))
	}

	// Dispatch cap: 3 landed + 2 declined = 5 decided, 60% land rate → READY.
	dir := tr.dispatchMarkerDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, outcome := range []string{"landed", "landed", "landed", "declined", "declined"} {
		attempt := "attempt-" + string(rune('0'+i))
		body := fmt.Sprintf(`{"id":"m%d","attemptId":"%s","outcome":"%s","dispatchedAt":%d}`,
			i, attempt, outcome, 1000+i)
		if err := os.WriteFile(filepath.Join(dir, "m"+string(rune('0'+i))+".json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if outcome == "landed" {
			appendFunnel(t, tr.selfCorrectionPath, SelfCorrectionCandidateRecord{
				Type: selfCorrectionTypeDispatch, AttemptID: attempt,
				DispatchPhase: selfCorrectionDispatchWatchPassed,
			})
		}
	}
	row := tr.ladderDispatchCapRow()
	if row.State != ladderStateReady || !strings.Contains(row.Detail, "60%") {
		t.Fatalf("cap row = %+v, want READY at 60%%", row)
	}
	// Below the land-rate floor: 1 landed / 4 failed stays accumulating.
	for i := 0; i < 4; i++ {
		name := "f" + string(rune('0'+i))
		if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(`{"id":"`+name+`","outcome":"failed"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if row = tr.ladderDispatchCapRow(); row.State != ladderStateGrowing {
		t.Fatalf("cap row must drop below the floor: %+v", row)
	}

	// Staged supply: a non-allowlist code candidate makes the review row READY.
	if _, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope: "code", Status: SelfCorrectionStatusProposed, SkillName: "sk",
		Title: "novel finding: triage gap", Source: "novel-miner:abcd",
	}); err != nil {
		t.Fatal(err)
	}
	row = tr.ladderStagedSourcesRow()
	if row.State != ladderStateReady || !strings.Contains(row.Detail, "novel-miner 1건") {
		t.Fatalf("staged row = %+v", row)
	}

	// Aggregate flips LIVE and names the actionable rows.
	l = tr.rsiAssessLadder()
	if l.State != rsiStateLive || !strings.Contains(l.Diagnosis, "운영자 결정 가능") {
		t.Fatalf("ladder with READY rows = %s: %s", l.State, l.Diagnosis)
	}
}

func TestLadderDispatchCapUsesLatestTerminalWatchedCohort(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tr := newTestTracker(t)
	dir := tr.dispatchMarkerDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// An old rollback ages out after five newer terminal outcomes. attempted
	// and a merged-but-unwatched landed marker never enter the denominator.
	write := func(name, outcome, attempt string, at int64, phase string) {
		t.Helper()
		body := fmt.Sprintf(`{"attemptId":"%s","outcome":"%s","dispatchedAt":%d}`, attempt, outcome, at)
		if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if phase != "" {
			appendFunnel(t, tr.selfCorrectionPath, SelfCorrectionCandidateRecord{
				Type: selfCorrectionTypeDispatch, AttemptID: attempt, DispatchPhase: phase,
			})
		}
	}
	write("old-rollback", "landed", "old", 1, selfCorrectionDispatchRolledBack)
	write("pending", "attempted", "pending", 100, selfCorrectionDispatchPROpened)
	write("unwatched", "landed", "unwatched", 101, selfCorrectionDispatchMerged)
	for i, outcome := range []string{"landed", "landed", "landed", "declined", "declined"} {
		phase := selfCorrectionDispatchDeclined
		if outcome == "landed" {
			phase = selfCorrectionDispatchWatchPassed
		}
		write(fmt.Sprintf("new-%d", i), outcome, fmt.Sprintf("new-%d", i), int64(200+i), phase)
	}
	if row := tr.ladderDispatchCapRow(); row.State != ladderStateReady {
		t.Fatalf("latest clean cohort should be READY: %+v", row)
	}
	graduations := tr.autoGraduations()
	found := false
	for _, graduation := range graduations {
		if graduation.Key == graduationDispatchCap {
			found = true
		}
	}
	if !found {
		t.Fatalf("clean watched cohort did not auto-graduate: %+v", graduations)
	}
	write("latest-rollback", "landed", "latest-rollback", 300, selfCorrectionDispatchRolledBack)
	if row := tr.ladderDispatchCapRow(); row.State != ladderStateGrowing {
		t.Fatalf("rollback in current cohort must block READY: %+v", row)
	}
	for _, graduation := range tr.autoGraduations() {
		if graduation.Key == graduationDispatchCap {
			t.Fatalf("rollback cohort auto-graduated: %+v", graduation)
		}
	}
}

// plantCalibrationDropIn marks the P5-2 window as still open under HOME.
func plantCalibrationDropIn(t *testing.T) {
	t.Helper()
	path := calibrationDropInPath()
	if path == "" {
		t.Fatal("calibration drop-in path empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# test window\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func isolateCalibrationHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

// Calibration row counts only post-window bench-carrying CYCLE records and
// requires every rotating epoch to reach the target.
func TestLadderCalibrationRowReadyOnlyWhenAllEpochsReachBenchTarget(t *testing.T) {
	isolateCalibrationHome(t)
	plantCalibrationDropIn(t)
	tr := newTestTracker(t)
	seed := func(epoch string, n int) {
		t.Helper()
		for i := 0; i < n; i++ {
			if err := tr.LogMetaRevision(MetaRevisionRecord{
				Epoch: epoch, Artifact: "a.md", Proposed: true,
				BenchShadow: &producerBenchOutcome{Skills: 1},
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	seed(metaEpochProducer, ladderCalibrationBenchTargetFor(metaEpochProducer))
	seed(metaEpochEvaluator, ladderCalibrationBenchTargetFor(metaEpochEvaluator))
	if row := tr.ladderCalibrationRow(); row.State != ladderStateGrowing {
		t.Fatalf("two of three epochs at target must stay accumulating: %+v", row)
	}
	seed(metaEpochGenesis, ladderCalibrationBenchTargetFor(metaEpochGenesis))
	if row := tr.ladderCalibrationRow(); row.State != ladderStateReady {
		t.Fatalf("all epochs at target must read READY: %+v", row)
	}
}

// Producer's per-epoch bench target is lower (5) than evaluator/genesis (10):
// the producer shadow bench samples only when the model returns a scorable
// body, so matching the target to its achievable rate lets the window close on
// real (if thin) noise samples instead of stalling forever.
func TestLadderCalibrationProducerTargetIsLowerThanDefault(t *testing.T) {
	if got := ladderCalibrationBenchTargetFor(metaEpochProducer); got != ladderCalibrationBenchTargetProducer {
		t.Errorf("producer target = %d, want %d", got, ladderCalibrationBenchTargetProducer)
	}
	for _, epoch := range []string{metaEpochEvaluator, metaEpochGenesis} {
		if got := ladderCalibrationBenchTargetFor(epoch); got != ladderCalibrationBenchTargetDefault {
			t.Errorf("%s target = %d, want default %d", epoch, got, ladderCalibrationBenchTargetDefault)
		}
	}
	if ladderCalibrationBenchTargetProducer >= ladderCalibrationBenchTargetDefault {
		t.Fatal("producer target must be strictly below the default")
	}

	// Producer met (5) but evaluator/genesis below their 10 must stay
	// accumulating — the lower producer bar does not leak to other epochs.
	isolateCalibrationHome(t)
	plantCalibrationDropIn(t)
	tr := newTestTracker(t)
	for _, epoch := range []string{metaEpochProducer, metaEpochEvaluator, metaEpochGenesis} {
		for i := 0; i < ladderCalibrationBenchTargetProducer; i++ {
			if err := tr.LogMetaRevision(MetaRevisionRecord{
				Epoch: epoch, Artifact: "a.md", Proposed: true,
				BenchShadow: &producerBenchOutcome{Skills: 1},
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if row := tr.ladderCalibrationRow(); row.State != ladderStateGrowing {
		t.Fatalf("producer met (5) but evaluator/genesis below 10 must stay accumulating: %+v", row)
	}
}

// An auto_adopted cycle carries Action="auto_adopted" AND an Epoch + bench — it
// must count toward calibration. Keying the skip off Action (instead of Epoch)
// dropped exactly the succeeding cycles, stalling the row below target forever.
func TestLadderCalibrationCountsAutoAdoptedCycles(t *testing.T) {
	isolateCalibrationHome(t)
	plantCalibrationDropIn(t)
	tr := newTestTracker(t)
	for _, epoch := range []string{metaEpochProducer, metaEpochEvaluator, metaEpochGenesis} {
		for i := 0; i < ladderCalibrationBenchTargetFor(epoch); i++ {
			if err := tr.LogMetaRevision(MetaRevisionRecord{
				Epoch: epoch, Artifact: "a.md", Action: "auto_adopted",
				BenchShadow: &producerBenchOutcome{Skills: 1},
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if row := tr.ladderCalibrationRow(); row.State != ladderStateReady {
		t.Fatalf("auto_adopted cycles with benches must count toward calibration: %+v", row)
	}
}

// After harvest deletes the drop-in, the row must read 완료 — otherwise the
// dashboard keeps nagging "제거 결정 가능" on a window that already closed.
func TestLadderCalibrationRowDoneWhenDropInRemoved(t *testing.T) {
	isolateCalibrationHome(t)
	tr := newTestTracker(t)
	for _, epoch := range []string{metaEpochProducer, metaEpochEvaluator, metaEpochGenesis} {
		for i := 0; i < ladderCalibrationBenchTargetFor(epoch); i++ {
			if err := tr.LogMetaRevision(MetaRevisionRecord{
				Epoch: epoch, Artifact: "a.md", Proposed: true,
				BenchShadow: &producerBenchOutcome{Skills: 1},
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	row := tr.ladderCalibrationRow()
	if row.State != ladderStateDone {
		t.Fatalf("absent drop-in must read done, got %+v", row)
	}
	if !strings.Contains(row.Detail, "드롭인 제거됨") {
		t.Fatalf("done detail should name the closed window: %+v", row)
	}
	plantCalibrationDropIn(t)
	if row := tr.ladderCalibrationRow(); row.State != ladderStateReady {
		t.Fatalf("re-planted drop-in must return to READY: %+v", row)
	}
}

// The dispatch-cap ladder must not terminate at its first rung. Before this,
// an executed 2→4 unlock made the row report 완료 forever and closed the
// watch's guard — the cap could never rise again no matter how good the
// evidence got, which is how the live lane ended up refusing work three days
// running at cap 4 with a 72% land rate.
func TestLadderDispatchCapOffersNextRungAfterUnlock(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tr := newTestTracker(t)
	dir := tr.dispatchMarkerDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, outcome string, at int64, phase string) {
		t.Helper()
		body := fmt.Sprintf(`{"attemptId":"%s","outcome":"%s","dispatchedAt":%d}`, name, outcome, at)
		if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		appendFunnel(t, tr.selfCorrectionPath, SelfCorrectionCandidateRecord{
			Type: selfCorrectionTypeDispatch, AttemptID: name, DispatchPhase: phase,
		})
	}
	// Cohort that buys the first rung (2 → 4).
	for i := 0; i < 5; i++ {
		write(fmt.Sprintf("first-%d", i), "landed", int64(100+i), selfCorrectionDispatchWatchPassed)
	}
	if row := tr.ladderDispatchCapRow(); row.State != ladderStateReady {
		t.Fatalf("first rung should be READY: %+v", row)
	}
	if fresh, err := tr.unlockGraduation(graduationDispatchCap, "first cohort", 4, true); err != nil || !fresh {
		t.Fatalf("first rung unlock fresh=%v err=%v, want true/nil", fresh, err)
	}

	// The very cohort that bought rung 4 must not also buy rung 8 — the ladder
	// floors evidence at the unlock instant, so it reads as no evidence yet.
	row := tr.ladderDispatchCapRow()
	if row.State == ladderStateDone {
		t.Fatalf("an executed rung is not the top of the ladder: %+v", row)
	}
	if row.State != ladderStateGrowing || !strings.Contains(row.Detail, "현재 캡 4 → 8") {
		t.Fatalf("row should track the next rung on fresh evidence: %+v", row)
	}
	for _, graduation := range tr.autoGraduations() {
		if graduation.Key == graduationDispatchCap {
			t.Fatalf("stale cohort re-bought the next rung: %+v", graduation)
		}
	}

	// Fresh dispatches AT cap 4 earn the next rung.
	now := loadGraduationState().Rows[graduationDispatchCap].UnlockedAt
	for i := 0; i < 5; i++ {
		write(fmt.Sprintf("second-%d", i), "landed", now+int64(1+i), selfCorrectionDispatchWatchPassed)
	}
	if row := tr.ladderDispatchCapRow(); row.State != ladderStateReady {
		t.Fatalf("fresh cohort at the current rung should be READY: %+v", row)
	}
	next := 0
	for _, graduation := range tr.autoGraduations() {
		if graduation.Key == graduationDispatchCap {
			next = graduation.Value
		}
	}
	if next != 8 {
		t.Fatalf("auto-graduation value = %d, want the next rung 8", next)
	}
	if fresh, err := tr.unlockGraduation(graduationDispatchCap, "second cohort", next, true); err != nil || !fresh {
		t.Fatalf("second rung unlock fresh=%v err=%v, want true/nil", fresh, err)
	}

	// Top of the ladder: 완료 belongs here and nowhere earlier.
	if row := tr.ladderDispatchCapRow(); row.State != ladderStateDone {
		t.Fatalf("top rung should read done: %+v", row)
	}
	for _, graduation := range tr.autoGraduations() {
		if graduation.Key == graduationDispatchCap {
			t.Fatalf("ladder ramped past its top rung: %+v", graduation)
		}
	}
}

// A re-lock drops the executor back to its compiled default, so the ladder must
// re-offer the FIRST rung — not the step the veto never let run.
func TestLadderDispatchCapReoffersFirstRungAfterRelock(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tr := newTestTracker(t)
	if _, err := tr.unlockGraduation(graduationDispatchCap, "cohort", 4, true); err != nil {
		t.Fatal(err)
	}
	if err := tr.RelockGraduation(graduationDispatchCap, "operator veto"); err != nil {
		t.Fatal(err)
	}
	row := loadGraduationState().Rows[graduationDispatchCap]
	if current, next := graduationDispatchCapRung(row); current != 0 || next != 4 {
		t.Fatalf("relocked rung = (%d → %d), want (0 → 4)", current, next)
	}
}

func TestNextGraduationDispatchCap(t *testing.T) {
	for _, tc := range []struct{ current, want int }{{0, 4}, {2, 4}, {4, 8}, {8, 0}, {99, 0}} {
		if got := nextGraduationDispatchCap(tc.current); got != tc.want {
			t.Errorf("nextGraduationDispatchCap(%d) = %d, want %d", tc.current, got, tc.want)
		}
	}
}

// Incremental health-finding kinds are permanently non-dispatchable: once they
// are the only staged supply left, the row must report DONE (manual-review
// backlog), not a READY that can never auto-resolve.
func TestLadderStagedSourcesIncrementalBacklog(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tr := newTestTracker(t)
	if _, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope: "code", Status: SelfCorrectionStatusProposed, SkillName: "sk",
		Title: "large refactor: responsibility co-change", Source: "health-finding:responsibility-cochange:abcd",
	}); err != nil {
		t.Fatal(err)
	}
	row := tr.ladderStagedSourcesRow()
	if row.State != ladderStateDone || !strings.Contains(row.Detail, "증분형 health-finding 1건") {
		t.Fatalf("incremental-only staged row = %+v, want DONE with manual backlog note", row)
	}
	// A genuinely novel source on top flips the row back to READY and still
	// carries the manual-backlog note.
	if _, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope: "code", Status: SelfCorrectionStatusProposed, SkillName: "sk",
		Title: "novel finding: triage gap", Source: "novel-miner:xyz",
	}); err != nil {
		t.Fatal(err)
	}
	row = tr.ladderStagedSourcesRow()
	if row.State != ladderStateReady || !strings.Contains(row.Detail, "novel-miner 1건") ||
		!strings.Contains(row.Detail, "증분형 health-finding 1건") {
		t.Fatalf("mixed staged row = %+v, want READY naming novel supply + manual backlog", row)
	}
}
