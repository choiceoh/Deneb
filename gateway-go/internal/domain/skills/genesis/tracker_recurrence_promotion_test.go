package genesis

import (
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

func TestPromoteTargetRecurrence_CapturesOnceAtThreshold(t *testing.T) {
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

func TestPromoteTargetRecurrence_ReviewedSignatureStaysBlocked(t *testing.T) {
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
		ID:     cands[0].ID,
		Status: SelfCorrectionStatusRejected,
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

func TestPromoteTargetRecurrence_BelowThresholdStaysQuiet(t *testing.T) {
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
