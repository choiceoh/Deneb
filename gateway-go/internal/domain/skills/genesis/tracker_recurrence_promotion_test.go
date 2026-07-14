package genesis

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func seedRecurrence(t *testing.T, tr *Tracker, failures int) {
	t.Helper()
	audit := HarnessEditAudit{
		TargetSignature:        "terminal=timeout|mechanism=bounded-execution",
		EditedSurface:          "Procedure",
		ExpectedBehaviorChange: "add bounded execution",
		RegressionRisk:         "may reject slow but valid commands",
	}
	evolveAt := time.Now().UnixMilli()
	if err := tr.LogEvolveWithAudit("deploy-helper", "1.0.1", "timeout recovery", audit); err != nil {
		t.Fatalf("LogEvolveWithAudit: %v", err)
	}
	for i := 0; i < failures; i++ {
		if err := tr.RecordUsage(UsageRecord{
			SkillName:  "deploy-helper",
			SessionKey: "client:main",
			Success:    false,
			ErrorMsg:   "context deadline exceeded while deploying",
			Source:     UsageSourceReal,
			// Strictly after the evolve; spaced so each failure is distinct.
			UsedAt: evolveAt + int64(i+1)*1000,
		}); err != nil {
			t.Fatalf("RecordUsage %d: %v", i, err)
		}
	}
}

func TestPromoteTargetRecurrenceCandidatesCapturesOnceAtThresholdThenIdempotent(t *testing.T) {
	tr := newTestTracker(t)
	seedRecurrence(t, tr, 2)

	promoted, err := tr.PromoteTargetRecurrenceCandidates()
	if err != nil {
		t.Fatalf("PromoteTargetRecurrenceCandidates: %v", err)
	}
	if promoted != 1 {
		t.Fatalf("promoted = %d, want 1", promoted)
	}
	cands, err := tr.RecentSelfCorrectionCandidates("deploy-helper", SelfCorrectionStatusProposed, 10)
	if err != nil {
		t.Fatalf("RecentSelfCorrectionCandidates: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("expected one proposed candidate, got %+v", cands)
	}
	cand := cands[0]
	if !strings.HasPrefix(cand.Source, targetRecurrenceSource+":") {
		t.Fatalf("source = %q, want %q prefix", cand.Source, targetRecurrenceSource)
	}
	if cand.Scope != "skill" || !strings.Contains(cand.Evidence, "recurrences(window)=2") {
		t.Fatalf("unexpected candidate content: %+v", cand)
	}

	// Idempotent: the same signature never re-promotes, even while proposed.
	promoted, err = tr.PromoteTargetRecurrenceCandidates()
	if err != nil {
		t.Fatalf("second promote: %v", err)
	}
	if promoted != 0 {
		t.Fatalf("re-promotion = %d, want 0", promoted)
	}
}

func TestPromoteTargetRecurrenceCandidatesStaysBlockedAfterOperatorRejection(t *testing.T) {
	tr := newTestTracker(t)
	seedRecurrence(t, tr, 2)
	if _, err := tr.PromoteTargetRecurrenceCandidates(); err != nil {
		t.Fatalf("promote: %v", err)
	}
	cands, err := tr.RecentSelfCorrectionCandidates("deploy-helper", "", 10)
	if err != nil || len(cands) != 1 {
		t.Fatalf("candidates: %v %+v", err, cands)
	}
	if _, err := tr.RecordSelfCorrectionReview(SelfCorrectionCandidateRecord{
		ID:       cands[0].ID,
		Status:   SelfCorrectionStatusRejected,
		Reviewer: "test",
	}); err != nil {
		t.Fatalf("review: %v", err)
	}
	promoted, err := tr.PromoteTargetRecurrenceCandidates()
	if err != nil {
		t.Fatalf("promote after review: %v", err)
	}
	if promoted != 0 {
		t.Fatalf("rejected signature re-promoted (%d), want 0 — operator verdict must stick", promoted)
	}
}

func TestPromoteTargetRecurrenceCandidatesReturnsEmptyBelowThreshold(t *testing.T) {
	tr := newTestTracker(t)
	seedRecurrence(t, tr, 1)

	promoted, err := tr.PromoteTargetRecurrenceCandidates()
	if err != nil {
		t.Fatalf("PromoteTargetRecurrenceCandidates: %v", err)
	}
	if promoted != 0 {
		t.Fatalf("single recurrence promoted (%d), want 0 — one flake must not fire", promoted)
	}
}

// TestPromoteTargetRecurrenceCandidatesLoadsNestedGenesisSkillPath guards the
// target-path contract: a genesis skill lives at <root>/genesis/<category>/<name>/SKILL.md,
// and the promoted candidate must point there — the earlier naive root+name
// join recorded phantom targets (~/.deneb/skills/<name>/SKILL.md) that sent the
// consuming session to a nonexistent file.
func TestPromoteTargetRecurrenceCandidatesLoadsNestedGenesisSkillPath(t *testing.T) {
	tr := newTestTracker(t)
	skillMD := filepath.Join(tr.skillsRoot, "genesis", "productivity", "deploy-helper", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillMD), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillMD, []byte("---\nname: deploy-helper\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedRecurrence(t, tr, 2)

	if _, err := tr.PromoteTargetRecurrenceCandidates(); err != nil {
		t.Fatalf("PromoteTargetRecurrenceCandidates: %v", err)
	}
	cands, err := tr.RecentSelfCorrectionCandidates("deploy-helper", SelfCorrectionStatusProposed, 10)
	if err != nil || len(cands) != 1 {
		t.Fatalf("candidates: %v %+v", err, cands)
	}
	cand := cands[0]
	if len(cand.TargetFiles) != 2 || cand.TargetFiles[0] != skillMD {
		t.Fatalf("TargetFiles = %v, want first entry %q", cand.TargetFiles, skillMD)
	}
	if strings.Contains(cand.Evidence, skillTargetMissingNote) {
		t.Fatalf("evidence flags a missing skill file although it exists: %q", cand.Evidence)
	}
}

// TestPromoteTargetRecurrence_MissingSkillFileDropsTargetAndNotes guards the
// archived/removed case: when the skill has no SKILL.md anywhere under the
// managed root, the candidate must NOT carry a guessed path — only the
// validation-cases ledger — and the evidence must say why.
func TestPromoteTargetRecurrence_MissingSkillFileDropsTargetAndNotes(t *testing.T) {
	tr := newTestTracker(t)
	seedRecurrence(t, tr, 2)

	if _, err := tr.PromoteTargetRecurrenceCandidates(); err != nil {
		t.Fatalf("PromoteTargetRecurrenceCandidates: %v", err)
	}
	cands, err := tr.RecentSelfCorrectionCandidates("deploy-helper", SelfCorrectionStatusProposed, 10)
	if err != nil || len(cands) != 1 {
		t.Fatalf("candidates: %v %+v", err, cands)
	}
	cand := cands[0]
	for _, target := range cand.TargetFiles {
		if strings.Contains(target, "SKILL.md") {
			t.Fatalf("candidate carries a guessed SKILL.md target for a skill absent on disk: %v", cand.TargetFiles)
		}
	}
	if !strings.Contains(cand.Evidence, skillTargetMissingNote) {
		t.Fatalf("evidence lacks the missing-skill note: %q", cand.Evidence)
	}
}
