package genesis

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

func TestEvaluateAblationMeasuresSkillLiftAcrossRepeatedTrials(t *testing.T) {
	tracker := newTestTracker(t)
	testCase := behaviorTestCase()
	if err := tracker.RecordSkillValidationCase(testCase); err != nil {
		t.Fatalf("record case: %v", err)
	}
	engine := NewSkillValidationEngine(tracker, nil)
	var withCalls, withoutCalls int
	result, err := engine.evaluateAblationWith(
		context.Background(), "topsolar-db", "# Skill\nRun the dashboard tool.", "test-model", 3,
		func(_ context.Context, body string, _ SkillValidationCaseRecord) (skillReplayTrace, error) {
			if strings.TrimSpace(body) == "" {
				withoutCalls++
				return skillReplayTrace{}, nil
			}
			withCalls++
			return traceFromEmittedCalls([]emittedToolCall{{Name: "exec", Args: `python3 topsolar.py dashboard`}}), nil
		},
	)
	if err != nil {
		t.Fatalf("evaluate ablation: %v", err)
	}
	if !result.Evaluated || result.Trials != 3 || result.CaseCount != 1 {
		t.Fatalf("unexpected ablation shape: %+v", result)
	}
	if result.WithSkillScore != 100 || result.WithoutSkillScore != 0 || result.Lift != 100 {
		t.Fatalf("expected +100 skill lift, got %+v", result)
	}
	if withCalls != 3 || withoutCalls != 3 {
		t.Fatalf("expected three paired trials, got with=%d without=%d", withCalls, withoutCalls)
	}
}

func TestEvaluateAblationReportsReplayErrorAndBoundsTrials(t *testing.T) {
	tracker := newTestTracker(t)
	if err := tracker.RecordSkillValidationCase(behaviorTestCase()); err != nil {
		t.Fatalf("record case: %v", err)
	}
	engine := NewSkillValidationEngine(tracker, nil)
	result, err := engine.evaluateAblationWith(
		context.Background(), "topsolar-db", "body", "test-model", 99,
		func(context.Context, string, SkillValidationCaseRecord) (skillReplayTrace, error) {
			return skillReplayTrace{}, errors.New("executor unavailable")
		},
	)
	if err == nil {
		t.Fatal("replay error should be observable to stop the bounded task cycle")
	}
	if result.Evaluated {
		t.Fatalf("failed replay must not produce a measurement: %+v", result)
	}
	if got := normalizeSkillAblationTrials(99); got != maxSkillAblationTrials {
		t.Fatalf("trials max = %d, want %d", got, maxSkillAblationTrials)
	}
	if got := normalizeSkillAblationTrials(1); got != minSkillAblationTrials {
		t.Fatalf("trials min = %d, want %d", got, minSkillAblationTrials)
	}
}

func TestTrackerSkillAblationSummaryUsesLatestRunPerSkill(t *testing.T) {
	tracker := newTestTracker(t)
	record := func(skill string, withScore, withoutScore float64, createdAt int64) {
		t.Helper()
		if err := tracker.RecordSkillAblation(SkillAblationRecord{
			SkillName: skill, Evaluated: true, CaseCount: 1, Trials: 3,
			WithSkillPassed: int(withScore), WithSkillTotal: 100,
			WithoutSkillPassed: int(withoutScore), WithoutSkillTotal: 100,
			WithSkillScore: withScore, WithoutSkillScore: withoutScore,
			Lift: withScore - withoutScore, CreatedAt: createdAt,
		}); err != nil {
			t.Fatalf("record ablation: %v", err)
		}
	}
	record("alpha", 80, 90, 100)
	record("alpha", 100, 75, 200)
	record("beta", 66, 66, 300)

	summary, err := tracker.SkillAblationSummary("")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.Runs != 3 || summary.SkillsMeasured != 2 || summary.LatestSkill != "beta" || summary.LastRunAt != 300 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if len(summary.NoLiftSkills) != 1 || summary.NoLiftSkills[0] != "beta" {
		t.Fatalf("latest positive alpha run should clear its old no-lift result: %+v", summary.NoLiftSkills)
	}
	alpha, err := tracker.SkillAblationSummary("alpha")
	if err != nil || alpha.Runs != 2 || alpha.LatestLift != 25 || len(alpha.NoLiftSkills) != 0 {
		t.Fatalf("unexpected alpha summary: %+v (err=%v)", alpha, err)
	}
}

func TestSkillAblationTaskRecordsOneBoundedComparison(t *testing.T) {
	tracker := newTestTracker(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: alpha\n---\n\n# Alpha\n\nUse exec."), 0o644); err != nil {
		t.Fatalf("seed skill: %v", err)
	}
	catalog := skills.NewCatalog(nil)
	catalog.Register(skills.SkillEntry{Skill: skills.Skill{Name: "alpha", FilePath: path}})
	task := &SkillAblationTask{
		Tracker: tracker,
		Catalog: catalog,
		evaluate: func(_ context.Context, name, body string, trials int) (SkillAblationRecord, error) {
			if name != "alpha" || !strings.Contains(body, "Use exec") || trials != defaultSkillAblationTrials {
				t.Fatalf("unexpected evaluation input: name=%q body=%q trials=%d", name, body, trials)
			}
			return SkillAblationRecord{
				Evaluated: true, Model: "test-model", CaseCount: 1, Trials: trials,
				WithSkillPassed: 3, WithSkillTotal: 3, WithoutSkillPassed: 1, WithoutSkillTotal: 3,
				WithSkillScore: 100, WithoutSkillScore: 100.0 / 3, Lift: 200.0 / 3,
			}, nil
		},
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	records, err := tracker.RecentSkillAblations("alpha", 5)
	if err != nil || len(records) != 1 {
		t.Fatalf("expected one persisted comparison, got %+v (err=%v)", records, err)
	}
	if records[0].CreatedAt == 0 || records[0].SkillName != "alpha" {
		t.Fatalf("unexpected persisted comparison: %+v", records[0])
	}
}

func TestSkillAblationTaskStopsCycleAfterExecutorError(t *testing.T) {
	tracker := newTestTracker(t)
	catalog := skills.NewCatalog(nil)
	for _, name := range []string{"alpha", "beta"} {
		dir := t.TempDir()
		path := filepath.Join(dir, "SKILL.md")
		if err := os.WriteFile(path, []byte("# "+name+"\n\nUse exec."), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		catalog.Register(skills.SkillEntry{Skill: skills.Skill{Name: name, FilePath: path}})
	}
	calls := 0
	task := &SkillAblationTask{
		Tracker: tracker,
		Catalog: catalog,
		evaluate: func(context.Context, string, string, int) (SkillAblationRecord, error) {
			calls++
			return SkillAblationRecord{}, errors.New("executor unavailable")
		},
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("scheduled task should fail open: %v", err)
	}
	if calls != 1 {
		t.Fatalf("executor failure must stop the bounded cycle, calls=%d", calls)
	}
	records, err := tracker.RecentSkillAblations("", 5)
	if err != nil || len(records) != 0 {
		t.Fatalf("failed executor must not persist a measurement: %+v (err=%v)", records, err)
	}
}

func TestBuildReplayExecutorPromptHasExplicitNoSkillControl(t *testing.T) {
	withSystem, withUser := buildReplayExecutorPrompt("# Procedure\nUse exec.", SkillReplayCaseRecord{Input: "run it"})
	withoutSystem, withoutUser := buildReplayExecutorPrompt("", SkillReplayCaseRecord{Input: "run it"})
	if !strings.Contains(withUser, "## SKILL") || strings.Contains(withoutUser, "## SKILL") {
		t.Fatalf("skill section should appear only in treatment: with=%q without=%q", withUser, withoutUser)
	}
	if withSystem == withoutSystem || !strings.Contains(withoutSystem, "별도의 SKILL 문서를 제공받지 않은") {
		t.Fatalf("control prompt must explicitly declare no skill: %q", withoutSystem)
	}
}
