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
	write("unwatched", "landed", "unwatched", 101, SelfCorrectionDispatchMerged)
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

// Calibration row counts only post-window bench-carrying CYCLE records and
// requires every rotating epoch to reach the target.
func TestLadderCalibrationRowReadyOnlyWhenAllEpochsReachBenchTarget(t *testing.T) {
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
	seed(metaEpochProducer, ladderCalibrationBenchTarget)
	seed(metaEpochEvaluator, ladderCalibrationBenchTarget)
	if row := tr.ladderCalibrationRow(); row.State != ladderStateGrowing {
		t.Fatalf("two of three epochs at target must stay accumulating: %+v", row)
	}
	seed(metaEpochGenesis, ladderCalibrationBenchTarget)
	if row := tr.ladderCalibrationRow(); row.State != ladderStateReady {
		t.Fatalf("all epochs at target must read READY: %+v", row)
	}
}

// An auto_adopted cycle carries Action="auto_adopted" AND an Epoch + bench — it
// must count toward calibration. Keying the skip off Action (instead of Epoch)
// dropped exactly the succeeding cycles, stalling the row below target forever.
func TestLadderCalibrationCountsAutoAdoptedCycles(t *testing.T) {
	tr := newTestTracker(t)
	for _, epoch := range []string{metaEpochProducer, metaEpochEvaluator, metaEpochGenesis} {
		for i := 0; i < ladderCalibrationBenchTarget; i++ {
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
