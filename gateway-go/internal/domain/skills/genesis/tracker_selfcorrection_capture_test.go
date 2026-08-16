package genesis

import (
	"strings"
	"testing"
	"time"

	rsilifecycle "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/lifecycle"
)

func TestSelfCorrectionReopenRejectedUnlessAppliedCooledAndFreshlyRecurring(t *testing.T) {
	now := time.Now()
	src := "failure-cluster:deadbeef"
	old := now.Add(-30 * 24 * time.Hour).UnixMilli() // older than the 14d cooldown
	recent := now.Add(-1 * time.Hour).UnixMilli()
	impactTwin := func(status string, createdAt, updatedAt, checkedAt int64) []SelfCorrectionCandidateRecord {
		return []SelfCorrectionCandidateRecord{{
			Source: src, Status: SelfCorrectionStatusApplied,
			CreatedAt: createdAt, UpdatedAt: updatedAt,
			ImpactResult: &rsilifecycle.ImpactResult{Status: status, CheckedAt: checkedAt},
		}}
	}

	cases := []struct {
		name        string
		existing    []SelfCorrectionCandidateRecord
		freshLastAt int64
		wantBlocked bool
	}{
		{"no matching candidate → allow first capture", nil, recent, false},
		{"different source → allow", []SelfCorrectionCandidateRecord{{Source: "other:1", Status: SelfCorrectionStatusProposed, CreatedAt: old}}, recent, false},
		{"live proposed twin → block", []SelfCorrectionCandidateRecord{{Source: src, Status: SelfCorrectionStatusProposed, CreatedAt: old}}, recent, true},
		{"accepted twin → block", []SelfCorrectionCandidateRecord{{Source: src, Status: SelfCorrectionStatusAccepted, CreatedAt: old}}, recent, true},
		{"rejected → block (operator ruled)", []SelfCorrectionCandidateRecord{{Source: src, Status: selfCorrectionStatusRejected, CreatedAt: old}}, recent, true},
		{"superseded → block", []SelfCorrectionCandidateRecord{{Source: src, Status: selfCorrectionStatusSuperseded, CreatedAt: old}}, recent, true},
		{"applied + cooled + fresh recurrence → REOPEN", []SelfCorrectionCandidateRecord{{Source: src, Status: SelfCorrectionStatusApplied, CreatedAt: old}}, recent, false},
		{"applied but not cooled → block", []SelfCorrectionCandidateRecord{{Source: src, Status: SelfCorrectionStatusApplied, CreatedAt: recent}}, now.UnixMilli(), true},
		{"applied recently despite old capture → block", []SelfCorrectionCandidateRecord{{Source: src, Status: SelfCorrectionStatusApplied, CreatedAt: old, UpdatedAt: recent}}, now.UnixMilli(), true},
		{"applied + cooled but no fresh recurrence → block", []SelfCorrectionCandidateRecord{{Source: src, Status: SelfCorrectionStatusApplied, CreatedAt: old}}, old - 1000, true},
		{"no effect + recurrence after verdict → standing veto", impactTwin(selfCorrectionImpactNoEffect, recent, recent, recent), recent + 1, true},
		{"regressed + recurrence after verdict → standing veto", impactTwin(selfCorrectionImpactRegressed, recent, recent, recent), recent + 1, true},
		{"no effect but recurrence predates verdict → block", impactTwin(selfCorrectionImpactNoEffect, old, recent, recent), recent - 1, true},
		{"verified impact keeps cooldown → block", impactTwin(selfCorrectionImpactVerified, old, recent, recent), recent + 1, true},
		{"malformed impact timestamp cannot waive cooldown", impactTwin(selfCorrectionImpactNoEffect, recent, recent, 0), now.UnixMilli(), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := selfCorrectionReopenBlocked(tc.existing, src, tc.freshLastAt, now); got != tc.wantBlocked {
				t.Fatalf("blocked = %v, want %v", got, tc.wantBlocked)
			}
		})
	}

	t.Run("newest applied wins over older rejected → reopen", func(t *testing.T) {
		existing := []SelfCorrectionCandidateRecord{
			{Source: src, Status: selfCorrectionStatusRejected, CreatedAt: now.Add(-60 * 24 * time.Hour).UnixMilli()},
			{Source: src, Status: SelfCorrectionStatusApplied, CreatedAt: old},
		}
		if selfCorrectionReopenBlocked(existing, src, recent, now) {
			t.Fatal("newest applied+cooled+fresh should reopen")
		}
	})

	// Reopen cap: a signature that already produced selfCorrectionReopenCap
	// prior candidates is permanently blocked even when the newest is applied,
	// cooled, and freshly recurring. Each twin has a distinct CreatedAt so they
	// survive dedup; all share the same source prefix.
	t.Run("reopen cap exceeded → permanent block", func(t *testing.T) {
		existing := make([]SelfCorrectionCandidateRecord, 0, selfCorrectionReopenCap+1)
		for i := 0; i <= selfCorrectionReopenCap; i++ {
			age := old - int64(i)*int64(24*time.Hour/time.Millisecond)
			existing = append(existing, SelfCorrectionCandidateRecord{
				Source: src, Status: SelfCorrectionStatusApplied, CreatedAt: age,
			})
		}
		if !selfCorrectionReopenBlocked(existing, src, recent, now) {
			t.Fatalf("expected permanent block with %d source twins (cap=%d), got allow",
				len(existing), selfCorrectionReopenCap)
		}
	})

	// Just under the cap: the applied+cooled+fresh path still re-opens.
	t.Run("at cap (not exceeded) → still reopens", func(t *testing.T) {
		existing := make([]SelfCorrectionCandidateRecord, 0, selfCorrectionReopenCap)
		for i := 0; i < selfCorrectionReopenCap; i++ {
			age := old - int64(i)*int64(24*time.Hour/time.Millisecond)
			existing = append(existing, SelfCorrectionCandidateRecord{
				Source: src, Status: SelfCorrectionStatusApplied, CreatedAt: age,
			})
		}
		if selfCorrectionReopenBlocked(existing, src, recent, now) {
			t.Fatalf("expected reopen at exactly cap=%d twins, got block", selfCorrectionReopenCap)
		}
	})
}

func seedClusterFailures(t *testing.T, tr *Tracker, skill, errMsg string, n int) {
	t.Helper()
	base := time.Now().UnixMilli()
	for i := 0; i < n; i++ {
		if err := tr.RecordUsage(UsageRecord{
			SkillName:  skill,
			SessionKey: "client:main",
			Success:    false,
			ErrorMsg:   errMsg,
			Source:     UsageSourceReal,
			UsedAt:     base + int64(i+1)*1000,
		}); err != nil {
			t.Fatalf("RecordUsage %d: %v", i, err)
		}
	}
}

func TestPromoteFailureClusterCandidates(t *testing.T) {
	tr := newTestTracker(t)
	seedClusterFailures(t, tr, "deploy-helper", "context deadline exceeded while deploying", 3)

	promoted, err := tr.PromoteFailureClusterCandidates()
	if err != nil {
		t.Fatalf("PromoteFailureClusterCandidates: %v", err)
	}
	if promoted < 1 {
		t.Fatalf("promoted = %d, want >= 1", promoted)
	}
	cands, err := tr.RecentSelfCorrectionCandidates("deploy-helper", SelfCorrectionStatusProposed, 10)
	if err != nil {
		t.Fatalf("RecentSelfCorrectionCandidates: %v", err)
	}
	if len(cands) < 1 {
		t.Fatalf("expected a proposed cluster candidate, got %+v", cands)
	}
	if !strings.HasPrefix(cands[0].Source, failureClusterSource+":") {
		t.Fatalf("source = %q, want %q prefix", cands[0].Source, failureClusterSource)
	}
	if cands[0].Scope != "skill" {
		t.Fatalf("scope = %q, want skill", cands[0].Scope)
	}
	for _, want := range []string{
		"shadowRoute=origin:workflow",
		"intervention:workflow",
		"confidence:medium",
		"reasons:execution_or_sequence_signal",
		"harnessPrimary:" + HarnessDimensionOrchestration,
	} {
		if !strings.Contains(cands[0].Evidence, want) {
			t.Fatalf("candidate evidence missing %q: %s", want, cands[0].Evidence)
		}
	}

	// Idempotent: a live proposed twin blocks a second promotion in the same window.
	promoted2, err := tr.PromoteFailureClusterCandidates()
	if err != nil {
		t.Fatalf("second PromoteFailureClusterCandidates: %v", err)
	}
	if promoted2 != 0 {
		t.Fatalf("second promote = %d, want 0 (dedup)", promoted2)
	}
}

func TestPromoteFailureClusterCandidates_BelowThreshold(t *testing.T) {
	tr := newTestTracker(t)
	seedClusterFailures(t, tr, "deploy-helper", "context deadline exceeded while deploying", 1) // < threshold(2)

	promoted, err := tr.PromoteFailureClusterCandidates()
	if err != nil {
		t.Fatalf("PromoteFailureClusterCandidates: %v", err)
	}
	if promoted != 0 {
		t.Fatalf("promoted = %d, want 0 below support threshold", promoted)
	}
}
