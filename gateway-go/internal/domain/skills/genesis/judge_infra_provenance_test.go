package genesis

import (
	"testing"
	"time"
)

// An outage-born exhibit stays MINEABLE — it is a good body the loop lost to a
// failed judge call, and recovering it is this lane's purpose — but it must not
// count as judge miscalibration, which is a claim about the judge.
func TestJudgeMiscalibrationExhibitsExcludeOutagesButKeepVerdicts(t *testing.T) {
	all := []falseRejectExhibit{
		{Skill: "a", RejectReason: "held-out selection rejected: no gain"},
		{Skill: "b", RejectReason: "judge error", Infrastructure: true},
		{Skill: "c", RejectReason: "flip gate rejected: regressed 1 case"},
	}
	got := judgeMiscalibrationExhibits(all)
	if len(got) != 2 {
		t.Fatalf("miscalibration exhibits = %d, want 2", len(got))
	}
	for _, e := range got {
		if e.Infrastructure {
			t.Errorf("outage exhibit %q leaked into the miscalibration set", e.Skill)
		}
	}
	// The full set is untouched: the recovery path still sees all three.
	if len(all) != 3 {
		t.Fatalf("filtering must not mutate the source slice (len=%d)", len(all))
	}
}

func TestJudgeMiscalibrationExhibitsHandlesEmptyAndAllInfra(t *testing.T) {
	if got := judgeMiscalibrationExhibits(nil); len(got) != 0 {
		t.Fatalf("nil input = %d exhibits, want 0", len(got))
	}
	allInfra := []falseRejectExhibit{
		{Skill: "a", Infrastructure: true},
		{Skill: "b", Infrastructure: true},
	}
	if got := judgeMiscalibrationExhibits(allInfra); len(got) != 0 {
		t.Fatalf("all-outage input = %d exhibits, want 0", len(got))
	}
}

// The L1 headline counts candidate verdicts. A judge outage in the same window
// must land in its own counter instead of inflating 기각 — and must not become
// the "last rejection reason" the operator reads either.
func TestEvolutionHealthSplitsInfrastructureRejections(t *testing.T) {
	tr := newTestTracker(t)
	now := time.Now().UnixMilli()
	dayMs := int64(86_400_000)

	appendFunnel(t, tr.logPath, LifecycleLogEntry{
		Type: "evolve_rejected", SkillName: "contract-review", CreatedAt: now - 3*dayMs,
		Reason: "judge error",
	})
	appendFunnel(t, tr.logPath, LifecycleLogEntry{
		Type: "evolve_rejected", SkillName: "system-health-check", CreatedAt: now - 2*dayMs,
		Reason: "held-out selection rejected: candidate did not improve validation score",
	})

	h := tr.EvolutionHealth()
	if h.EvolveRejected7d != 1 {
		t.Fatalf("EvolveRejected7d = %d, want 1 (only the verdict)", h.EvolveRejected7d)
	}
	if h.EvolveRejectedInfra7d != 1 {
		t.Fatalf("EvolveRejectedInfra7d = %d, want 1 — outages must stay visible", h.EvolveRejectedInfra7d)
	}
	if h.LastRejectedReason == "judge error" {
		t.Error("an outage must not become the headline's last rejection reason")
	}
}
