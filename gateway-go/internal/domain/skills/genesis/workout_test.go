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
