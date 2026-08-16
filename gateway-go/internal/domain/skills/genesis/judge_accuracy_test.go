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
	body := "# 스킬\n\n## When to Use\n" + fmt.Sprintf("%0400d", 0) + "\n\n## Procedure\n필수 절차 문구를 빠짐없이 지켜 진행한다.\n\n## Verification\n검증."
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
func TestJudgeAccuracyTaskRunLedgersPerClassAccuracyWithMissExhibits(t *testing.T) {
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
	recs, err := tr.recentJudgeAccuracy(5)
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

func TestOperatorJudgeVerdictIsIdempotentAndSeparateFromLaneRuns(t *testing.T) {
	_, tr := accuracyFixture(t)
	verdict := OperatorJudgeVerdict{
		DecisionID: "sk@1.0.1", Skill: "sk", Version: "1.0.1",
		Verdict: OperatorJudgeVerdictConfirm, JudgeVersion: "judge-v1", JudgeMargin: 1.5,
	}
	if err := tr.LogOperatorJudgeVerdict(verdict); err != nil {
		t.Fatal(err)
	}
	if err := tr.LogOperatorJudgeVerdict(verdict); err != nil {
		t.Fatal(err)
	}
	if runs, err := tr.recentJudgeAccuracy(5); err != nil || len(runs) != 0 {
		t.Fatalf("operator label leaked into scheduled runs: %+v err=%v", runs, err)
	}
	labels := tr.RecentOperatorJudgeVerdicts(time.Hour, 5)
	if len(labels) != 1 || labels[0].DecisionID != verdict.DecisionID {
		t.Fatalf("labels = %+v, want one idempotent verdict", labels)
	}
	if err := tr.LogOperatorJudgeVerdict(OperatorJudgeVerdict{
		DecisionID: "bad", Skill: "sk", Version: "1", Verdict: "maybe",
	}); err == nil {
		t.Fatal("invalid operator verdict accepted")
	}
}

// The probe curriculum ladder: weaken-tier pairs deploy only after
// judgeEscalationWindow consecutive zero-miss drop-tier runs of the incumbent
// judge; a drop-tier miss or a judge revision (version change) re-locks it.
func TestJudgeAccuracyEscalationAllowsWeakenTierAfterSaturatedWindow(t *testing.T) {
	task, tr := accuracyFixture(t)
	version := task.Meta.Version(generation.MetaSkillJudgeSystemPrompt,
		generation.DefaultMetaArtifacts()[generation.MetaSkillJudgeSystemPrompt])
	seed := func(v string, dropMissed int) {
		t.Helper()
		if err := tr.logJudgeAccuracy(judgeAccuracyRecord{
			JudgeVersion: v, Pairs: 2, Correct: 2 - dropMissed,
			ByClass: map[string][2]int{
				"imperative-drop": {1 - dropMissed, 1},
				"safety-drop":     {1, 1},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}

	if task.weakenTierUnlocked(version) {
		t.Fatal("weaken tier unlocked with no history")
	}
	for i := 0; i < judgeEscalationWindow-1; i++ {
		seed(version, 0)
	}
	if task.weakenTierUnlocked(version) {
		t.Fatal("weaken tier unlocked below the saturation window")
	}
	seed(version, 0)
	if !task.weakenTierUnlocked(version) {
		t.Fatal("weaken tier locked after a fully saturated window")
	}
	// Version scoping: a revised judge re-earns the tier from scratch.
	if task.weakenTierUnlocked("revised-judge") {
		t.Fatal("weaken tier unlocked for a judge version with no history")
	}
	// A drop-tier miss re-locks — the frontier moved back down.
	seed(version, 1)
	if task.weakenTierUnlocked(version) {
		t.Fatal("weaken tier stayed unlocked past a drop-tier miss")
	}
	for i := 0; i < judgeEscalationWindow; i++ {
		seed(version, 0)
	}
	if !task.weakenTierUnlocked(version) {
		t.Fatal("weaken tier locked despite a fresh saturated window")
	}

	// End-to-end: an unlocked run replays weaken pairs through the identical
	// verdict path and ledgers them under their own ByClass keys.
	task.verdictFn = func(_ context.Context, _, _, _ string) (judgeVerdict, error) {
		return judgeVerdict{Pass: false}, nil // catch everything
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	recs, err := tr.recentJudgeAccuracy(1)
	if err != nil || len(recs) != 1 {
		t.Fatalf("ledger read failed: %+v err=%v", recs, err)
	}
	if ct := recs[0].ByClass["imperative-weaken"]; ct[1] == 0 {
		t.Fatalf("escalated run carried no weaken pairs: %+v", recs[0].ByClass)
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
func TestIsCharterCaseDeterministicAboutQuarterProportionStraddlesBothPoolBoundary(t *testing.T) {
	charter := 0
	for i := 0; i < 400; i++ {
		rec := SkillValidationCaseRecord{SkillName: "sk", ID: fmt.Sprintf("case-%d", i), RequiredSubstrings: []string{"x"}}
		first := isCharterCase(rec)
		if first != isCharterCase(rec) {
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
		if isCharterCase(rec) {
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
func TestJudgeAccuracyIntervalDefaultsAndAllowsEnvOverride(t *testing.T) {
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

// Infra verdict errors must not poison the accuracy ledger: after a short
// consecutive-error abort the lane writes nothing (or only scored pairs).
func TestJudgeAccuracyTaskRunSkipsLedgerOnConsecutiveVerdictErrors(t *testing.T) {
	task, tr := accuracyFixture(t)
	calls := 0
	task.verdictFn = func(_ context.Context, _, _, _ string) (judgeVerdict, error) {
		calls++
		return judgeVerdict{}, fmt.Errorf("judge unavailable")
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != judgeAccuracyAbortAfterErrors {
		t.Fatalf("calls = %d, want abort after %d", calls, judgeAccuracyAbortAfterErrors)
	}
	recs, err := tr.recentJudgeAccuracy(5)
	if err != nil || len(recs) != 0 {
		t.Fatalf("infra-only storm must not ledger: %+v err=%v", recs, err)
	}
}

func TestJudgeAccuracyProbeUsableRejectsErrorStormRows(t *testing.T) {
	storm := judgeAccuracyRecord{
		Pairs: 24, Correct: 0,
		ByClass: map[string][2]int{"fake-tool": {0, 3}, "section-drop": {0, 3}},
		Misses: []judgeMissExhibit{
			{Skill: "sk", Degradation: "section-drop", Verdict: "error"},
			{Skill: "sk", Degradation: "fake-tool", Verdict: "error"},
		},
	}
	if judgeAccuracyProbeUsable(storm) {
		t.Fatal("all-error storm row treated as usable probe evidence")
	}
	real := judgeAccuracyRecord{
		Pairs: 4, Correct: 3,
		Misses: []judgeMissExhibit{{Skill: "sk", Degradation: "fake-tool", Verdict: "passed_defect"}},
	}
	if !judgeAccuracyProbeUsable(real) {
		t.Fatal("real miss row rejected")
	}
	if !judgeMissCountsAsFuel(real.Misses[0]) || judgeMissCountsAsFuel(storm.Misses[0]) {
		t.Fatal("fuel filter wrong")
	}
}

// Storm rows must not starve the recent window — otherwise L3 / weaken-tier /
// meta evidence only see infra noise and never the healthy runs behind it.
func TestRecentJudgeAccuracySkipsUnusableStormRows(t *testing.T) {
	_, tr := accuracyFixture(t)
	storm := judgeAccuracyRecord{
		JudgeVersion: "v-storm", Pairs: 24, Correct: 0,
		ByClass: map[string][2]int{"fake-tool": {0, 12}},
		Misses:  []judgeMissExhibit{{Skill: "sk", Degradation: "fake-tool", Verdict: "error"}},
	}
	healthy := judgeAccuracyRecord{
		JudgeVersion: "v-ok", Pairs: 4, Correct: 4,
		ByClass: map[string][2]int{"fake-tool": {4, 4}},
	}
	for _, rec := range []judgeAccuracyRecord{healthy, storm, storm, storm} {
		if err := tr.logJudgeAccuracy(rec); err != nil {
			t.Fatal(err)
		}
	}
	recs, err := tr.recentJudgeAccuracy(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].JudgeVersion != "v-ok" {
		t.Fatalf("recent window = %+v, want only the healthy run", recs)
	}
}

func TestThinPairsToCanaryKeepsFirstOfEachClass(t *testing.T) {
	pairs := []judgeBenchPair{
		{Skill: "a", Degradation: "fake-tool"},
		{Skill: "b", Degradation: "fake-tool"},
		{Skill: "a", Degradation: "step-reorder"},
		{Skill: "b", Degradation: "step-reorder"},
	}
	got := thinPairsToCanary(pairs)
	if len(got) != 2 {
		t.Fatalf("canary = %+v, want one pair per class", got)
	}
	if got[0].Skill != "a" || got[0].Degradation != "fake-tool" {
		t.Fatalf("first canary pair = %+v", got[0])
	}
	if got[1].Skill != "a" || got[1].Degradation != "step-reorder" {
		t.Fatalf("second canary pair = %+v", got[1])
	}
}

func addAccuracySkill(t *testing.T, task *JudgeAccuracyTask, name string) {
	t.Helper()
	body := "# 스킬\n\n## When to Use\n" + fmt.Sprintf("%0400d", 0) + "\n\n## Procedure\n필수 절차 문구를 빠짐없이 지켜 진행한다.\n\n## Verification\n검증."
	path := filepath.Join(t.TempDir(), name+".md")
	if err := os.WriteFile(path, []byte("---\nname: "+name+"\nversion: 1.0.0\n---\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
	e := skills.SkillEntry{}
	e.Skill.Name = name
	e.Skill.FilePath = path
	task.Evolver.catalog.Register(e)
}

// Once the highest planted rung saturates, the lane must not replay every
// catalog pair — one per class is enough to notice a regression.
func TestJudgeAccuracyRunThinsToCanaryWhenReorderCeilingSaturated(t *testing.T) {
	task, tr := accuracyFixture(t)
	addAccuracySkill(t, task, "sk2")
	addAccuracySkill(t, task, "sk3")
	version := task.Meta.Version(generation.MetaSkillJudgeSystemPrompt,
		generation.DefaultMetaArtifacts()[generation.MetaSkillJudgeSystemPrompt])
	for i := 0; i < judgeEscalationWindow; i++ {
		if err := tr.logJudgeAccuracy(judgeAccuracyRecord{
			JudgeVersion: version, Pairs: 2, Correct: 2,
			ByClass: map[string][2]int{
				"step-reorder":          {1, 1},
				"contradiction-example": {1, 1},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if !task.probeCeilingSaturated(version) {
		t.Fatal("ceiling not saturated after seeded reorder window")
	}
	task.verdictFn = func(_ context.Context, _, _, _ string) (judgeVerdict, error) {
		return judgeVerdict{Pass: false}, nil
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	recs, err := tr.recentJudgeAccuracy(1)
	if err != nil || len(recs) != 1 {
		t.Fatalf("ledger = %+v err=%v", recs, err)
	}
	rec := recs[0]
	if rec.Pairs < 2 {
		t.Fatalf("canary too thin: %+v", rec)
	}
	for cls, ct := range rec.ByClass {
		if ct[1] > 1 {
			t.Fatalf("class %s scored %d pairs after canary thin: %+v", cls, ct[1], rec.ByClass)
		}
	}
}
