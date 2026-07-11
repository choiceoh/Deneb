package genesis

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
)

func accuracyFixture(t *testing.T) (*JudgeAccuracyTask, *Tracker) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	catalog := skills.NewCatalog(slog.Default())
	dir := t.TempDir()
	body := "# 스킬\n\n## When to Use\n" + fmt.Sprintf("%0400d", 0) + "\n\n## Procedure\n필수 절차 문구.\n\n## Verification\n검증."
	path := filepath.Join(dir, "sk.md")
	if err := os.WriteFile(path, []byte("---\nname: sk\nversion: 1.0.0\n---\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
	e := skills.SkillEntry{}
	e.Skill.Name = "sk"
	e.Skill.FilePath = path
	catalog.Register(e)
	task := &JudgeAccuracyTask{
		Evolver: &Evolver{catalog: catalog, tracker: tr, logger: slog.Default()},
		Meta:    generation.NewMetaArtifacts(t.TempDir(), slog.Default()),
		Tracker: tr,
		Logger:  slog.Default(),
	}
	return task, tr
}

// The lane ledgers per-class accuracy with miss exhibits, attributed to the
// judge prompt version — the P3 label substrate.
func TestJudgeAccuracyTask_Run(t *testing.T) {
	task, tr := accuracyFixture(t)
	task.verdictFn = func(_ context.Context, _, _, degraded string) (judgeVerdict, error) {
		// Miss exactly the fake-tool class (pass the defect); catch the rest.
		if strings.Contains(degraded, "deneb-hyperfix") {
			return judgeVerdict{Pass: true}, nil
		}
		return judgeVerdict{Pass: false}, nil
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	recs, err := tr.RecentJudgeAccuracy(5)
	if err != nil || len(recs) != 1 {
		t.Fatalf("ledger = %+v err=%v", recs, err)
	}
	rec := recs[0]
	if rec.Pairs == 0 || rec.Correct != rec.Pairs-1 {
		t.Fatalf("accuracy accounting wrong: %+v", rec)
	}
	if len(rec.Misses) != 1 || rec.Misses[0].Degradation != "fake-tool" || rec.Misses[0].Verdict != "passed_defect" {
		t.Fatalf("miss exhibit wrong: %+v", rec.Misses)
	}
	if ft := rec.ByClass["fake-tool"]; ft[0] != 0 || ft[1] != 1 {
		t.Fatalf("per-class accounting wrong: %+v", rec.ByClass)
	}
	if rec.JudgeVersion == "" {
		t.Fatal("judge version attribution missing")
	}
}

// False-reject mining: a rejected body that outscores the current body with
// zero flips gets exhibited; a worse one does not.
func TestMineFalseRejects(t *testing.T) {
	task, tr := accuracyFixture(t)
	if err := tr.RecordSkillValidationCase(SkillValidationCaseRecord{
		SkillName: "sk", ID: "case-1", RequiredSubstrings: []string{"필수 절차 문구", "누락된 새 규칙"},
	}); err != nil {
		t.Fatal(err)
	}
	// Better: has both substrings (current body has only one).
	if err := tr.RecordRejectedSkillEdit(RejectedSkillEditRecord{
		SkillName: "sk", Reason: "judge rejected", CandidateBody: "필수 절차 문구 그리고 누락된 새 규칙 포함 본문",
	}); err != nil {
		t.Fatal(err)
	}
	// Worse: has neither.
	if err := tr.RecordRejectedSkillEdit(RejectedSkillEditRecord{
		SkillName: "sk", Reason: "judge rejected worse", CandidateBody: "아무 관련 없는 본문",
	}); err != nil {
		t.Fatal(err)
	}
	got := task.mineFalseRejects()
	if len(got) != 1 || got[0].RejectScore <= got[0].CurrentScore {
		t.Fatalf("false-reject mining = %+v", got)
	}
}

// The charter subset is deterministic, frozen, and a reasonable minority.
func TestIsCharterCase(t *testing.T) {
	charter := 0
	for i := 0; i < 400; i++ {
		rec := SkillValidationCaseRecord{SkillName: "sk", ID: fmt.Sprintf("case-%d", i), RequiredSubstrings: []string{"x"}}
		first := IsCharterCase(rec)
		if first != IsCharterCase(rec) {
			t.Fatal("charter membership not deterministic")
		}
		if first {
			charter++
		}
	}
	if charter < 60 || charter > 140 {
		t.Fatalf("charter proportion off (~25%% expected): %d/400", charter)
	}
	// Independence from the blind pool split: both charter and non-charter
	// cases must appear in both pools.
	var cb, cv int
	for i := 0; i < 400; i++ {
		rec := SkillValidationCaseRecord{SkillName: "sk", ID: fmt.Sprintf("case-%d", i), RequiredSubstrings: []string{"x"}}
		if IsCharterCase(rec) {
			if validationCaseBlindHeldOut(rec) {
				cb++
			} else {
				cv++
			}
		}
	}
	if cb == 0 || cv == 0 {
		t.Fatalf("charter must straddle both pools: blind=%d visible=%d", cb, cv)
	}
}

// The env knob accelerates the lane.
func TestJudgeAccuracyInterval(t *testing.T) {
	task := &JudgeAccuracyTask{}
	t.Setenv("DENEB_JUDGE_ACCURACY_INTERVAL_HOURS", "")
	if task.Interval() != judgeAccuracyDefaultInterval {
		t.Fatalf("default = %v", task.Interval())
	}
	t.Setenv("DENEB_JUDGE_ACCURACY_INTERVAL_HOURS", "6")
	if task.Interval() != 6*time.Hour {
		t.Fatalf("override = %v", task.Interval())
	}
}
