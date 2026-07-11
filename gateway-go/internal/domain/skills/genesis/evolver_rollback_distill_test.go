package genesis

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

// A rollback must leave re-proposal defenses behind: the regressing body as a
// rejected edit, and the failure evidence as a hard-frontier held-out case.
func TestRollbackSkill_PersistsEvidence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	file := filepath.Join(dir, "SKILL.md")
	original := "---\nname: sk\nversion: 1.0.0\n---\n\n# sk\n\noriginal body\n"
	evolved := "---\nname: sk\nversion: 1.0.1\n---\n\n# sk\n\nregressed body\n"
	if err := os.WriteFile(file, []byte(evolved), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := backupSkillVersion(file, original); err != nil {
		t.Fatal(err)
	}

	tracker, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	// Failure evidence with concrete tool fragments — distillable.
	if err := tracker.RecordUsage(UsageRecord{
		SkillName: "sk", SessionKey: "client:t", Success: false, ErrorMsg: "boom",
		FailureTrace: &UsageFailureTrace{
			Signature: "wiki search returns empty", ToolName: "wiki",
			ToolInput: `{"query":"단가"}`, AgentMechanism: "skipped the recall step",
		},
	}); err != nil {
		t.Fatal(err)
	}

	catalog := skills.NewCatalog(slog.Default())
	catalog.Register(skills.SkillEntry{Skill: skills.Skill{Name: "sk", Version: "1.0.1", FilePath: file}})
	e := NewEvolver(nil, catalog, tracker, "m", slog.Default())

	e.RollbackSkill("sk")

	restored, err := os.ReadFile(file)
	if err != nil || string(restored) != original {
		t.Fatalf("rollback did not restore backup: %v", err)
	}
	rejected, _ := tracker.RecentRejectedSkillEdits("sk", 5)
	if len(rejected) != 1 || !strings.Contains(rejected[0].CandidateBody, "regressed body") || rejected[0].Source != "rollback" {
		t.Fatalf("regressing body not recorded as rejected edit: %+v", rejected)
	}
	cases, _ := tracker.RecentSkillValidationCases("sk", 5)
	if len(cases) != 1 || cases[0].Source != "post-rollback" || cases[0].FrontierTier != "hard" {
		t.Fatalf("rollback evidence not distilled: %+v", cases)
	}
	if len(cases[0].Replay.ExpectedToolCalls) != 1 || cases[0].Replay.ExpectedToolCalls[0].Name != "wiki" {
		t.Fatalf("distilled case lost the tool evidence: %+v", cases[0].Replay)
	}
}

// Traces without concrete tool evidence must be quietly skipped by the
// weak-case guard — no case, no error noise.
func TestRollbackSkill_WeakEvidenceSkipsDistillation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tracker, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.RecordUsage(UsageRecord{SkillName: "sk", SessionKey: "client:t", Success: false, ErrorMsg: "vague"}); err != nil {
		t.Fatal(err)
	}
	e := NewEvolver(nil, skills.NewCatalog(slog.Default()), tracker, "m", slog.Default())
	e.distillRollbackValidationCase("sk")
	cases, _ := tracker.RecentSkillValidationCases("sk", 5)
	if len(cases) != 0 {
		t.Fatalf("weak evidence should not distill: %+v", cases)
	}
}
