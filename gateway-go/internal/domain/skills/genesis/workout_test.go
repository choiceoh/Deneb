package genesis

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

func workoutFixtures(t *testing.T) (*Tracker, *skills.Catalog) {
	t.Helper()
	tr := newTestTracker(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte("# contract-review\n\n- always write the answer file"), 0o644); err != nil {
		t.Fatalf("seed skill: %v", err)
	}
	catalog := skills.NewCatalog(nil)
	catalog.Register(skills.SkillEntry{Skill: skills.Skill{Name: "contract-review", Dir: dir, FilePath: path}})
	if err := tr.RecordSkillValidationCase(SkillValidationCaseRecord{
		SkillName: "contract-review", ID: "case-1",
		Replay: SkillReplayCaseRecord{
			Input:         "계약서 검토해줘",
			RequiredTools: []string{"fs"},
		},
	}); err != nil {
		t.Fatalf("seed case: %v", err)
	}
	return tr, catalog
}

// A failing workout replay records quarantined evidence: Source=workout, never
// real usage, surfacing as its own cluster kind.
func TestSkillWorkoutTask_RecordsQuarantinedFailures(t *testing.T) {
	tr, catalog := workoutFixtures(t)
	task := &SkillWorkoutTask{
		Tracker: tr, Catalog: catalog,
		replay: func(_ context.Context, body string, _ SkillValidationCaseRecord) (skillReplayTrace, error) {
			if !strings.Contains(body, "answer file") {
				t.Errorf("replay should receive the current skill body, got %q", body)
			}
			return skillReplayTrace{}, nil // no tool calls → RequiredTools assertion fails
		},
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	records, err := jsonlstore.Load[UsageRecord](tr.usagePath)
	if err != nil || len(records) != 1 {
		t.Fatalf("want 1 workout record, got %d (err=%v)", len(records), err)
	}
	rec := records[0]
	if rec.Source != UsageSourceWorkout || rec.Success || !strings.HasPrefix(rec.SessionKey, workoutSessionPrefix) {
		t.Fatalf("workout record shape wrong: %+v", rec)
	}
	if isRealUsageRecord(rec) {
		t.Fatal("workout records must never count as real usage")
	}

	tr.mu.Lock()
	clusters := tr.computeFailureEvidenceClustersLocked(time.Now(), 0)
	tr.mu.Unlock()
	if len(clusters) != 1 || clusters[0].Kind != FailureClusterKindWorkout || clusters[0].Skill != "contract-review" {
		t.Fatalf("workout failure should cluster under its own kind: %+v", clusters)
	}
}

// A passing replay records nothing, and an executor error ends the cycle
// cleanly without records.
func TestSkillWorkoutTask_PassAndExecutorErrorPaths(t *testing.T) {
	tr, catalog := workoutFixtures(t)
	pass := &SkillWorkoutTask{
		Tracker: tr, Catalog: catalog,
		replay: func(_ context.Context, _ string, _ SkillValidationCaseRecord) (skillReplayTrace, error) {
			return traceFromEmittedCalls([]emittedToolCall{{Name: "fs"}}), nil
		},
	}
	if err := pass.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if records, _ := jsonlstore.Load[UsageRecord](tr.usagePath); len(records) != 0 {
		t.Fatalf("passing workout must record nothing, got %+v", records)
	}

	broken := &SkillWorkoutTask{
		Tracker: tr, Catalog: catalog,
		replay: func(_ context.Context, _ string, _ SkillValidationCaseRecord) (skillReplayTrace, error) {
			return skillReplayTrace{}, context.DeadlineExceeded
		},
	}
	if err := broken.Run(context.Background()); err != nil {
		t.Fatalf("executor failure must fail open, got %v", err)
	}
	if records, _ := jsonlstore.Load[UsageRecord](tr.usagePath); len(records) != 0 {
		t.Fatalf("failed executor must not record evidence, got %+v", records)
	}
}

// The lane rotates fairly (least-recently-exercised first) and never
// re-records a defect already evidenced inside the window.
func TestSkillWorkoutTask_RotationAndDedup(t *testing.T) {
	tr, catalog := workoutFixtures(t)
	failing := func(_ context.Context, _ string, _ SkillValidationCaseRecord) (skillReplayTrace, error) {
		return skillReplayTrace{}, nil
	}
	task := &SkillWorkoutTask{Tracker: tr, Catalog: catalog, replay: failing}

	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("run1: %v", err)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("run2: %v", err)
	}
	records, _ := jsonlstore.Load[UsageRecord](tr.usagePath)
	if len(records) != 1 {
		t.Fatalf("second cycle must dedup the already-evidenced defect, got %d records", len(records))
	}
	trace := records[0].FailureTrace
	if trace == nil || trace.Signature != "terminal=heldout-assertion|mechanism=skill-behavior-drift" ||
		!strings.Contains(trace.CausalStatus, "synthetic workout") {
		t.Fatalf("workout failure needs the stable explicit trace, got %+v", trace)
	}

	// Rotation: the exercised skill has lastAt set; a fresh skill must sort first.
	lastAt, seen := tr.WorkoutActivity(7 * 24 * time.Hour)
	if lastAt["contract-review"] == 0 || !seen["contract-review"]["case-1"] {
		t.Fatalf("workout activity should report exercised skill + failed case: %v %v", lastAt, seen)
	}
}

// The liveness summary rolls up in-window workout records for the status view.
func TestWorkoutActivitySummarize(t *testing.T) {
	tr, catalog := workoutFixtures(t)
	task := &SkillWorkoutTask{
		Tracker: tr, Catalog: catalog,
		replay: func(_ context.Context, _ string, _ SkillValidationCaseRecord) (skillReplayTrace, error) {
			return skillReplayTrace{}, nil // fails the RequiredTools assertion
		},
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	s := tr.WorkoutActivitySummarize()
	if s.SkillsExercised != 1 || s.DistinctFailures != 1 || s.LastRunAt == 0 {
		t.Fatalf("summary should report 1 skill / 1 failure / a timestamp, got %+v", s)
	}

	// No workout records → zero-valued summary (never-run lane reads clean).
	empty := newTestTracker(t)
	if got := empty.WorkoutActivitySummarize(); got.SkillsExercised != 0 || got.LastRunAt != 0 {
		t.Fatalf("empty lane must summarize to zero, got %+v", got)
	}
}
