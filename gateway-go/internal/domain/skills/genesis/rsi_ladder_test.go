package genesis

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The ladder engine turns machine-readable evidence into row states without
// ever flipping a lock: thresholds met read 준비됨, accumulating evidence
// reads 축적 중, and the aggregate card goes LIVE only when a row is READY.
func TestRSILadder(t *testing.T) {
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
		body := `{"id":"m` + string(rune('0'+i)) + `","outcome":"` + outcome + `"}`
		if err := os.WriteFile(filepath.Join(dir, "m"+string(rune('0'+i))+".json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
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
		Title: "runtime error: nil deref", Source: "runtime-error:abcd",
	}); err != nil {
		t.Fatal(err)
	}
	row = tr.ladderStagedSourcesRow()
	if row.State != ladderStateReady || !strings.Contains(row.Detail, "runtime-error 1건") {
		t.Fatalf("staged row = %+v", row)
	}

	// Aggregate flips LIVE and names the actionable rows.
	l = tr.rsiAssessLadder()
	if l.State != rsiStateLive || !strings.Contains(l.Diagnosis, "운영자 결정 가능") {
		t.Fatalf("ladder with READY rows = %s: %s", l.State, l.Diagnosis)
	}
}

// Calibration row counts only post-window bench-carrying CYCLE records and
// requires every rotating epoch to reach the target.
func TestRSILadder_CalibrationRow(t *testing.T) {
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
