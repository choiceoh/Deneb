package genesis

import (
	"strings"
	"testing"
	"time"
)

// seedBaselineLabel appends a resolved-watch lifecycle entry carrying a
// baseline-test verdict — the only thing that produces an e-process label.
func seedBaselineLabel(t *testing.T, tr *Tracker, entryType string, reachable, disagree bool) {
	t.Helper()
	appendFunnel(t, tr.logPath, LifecycleLogEntry{
		Type:      entryType,
		SkillName: "sk",
		CreatedAt: time.Now().UnixMilli(),
		BaselineTest: &rollbackBaselineTest{
			RejectReachable: reachable,
			Disagreement:    disagree,
		},
	})
}

// The defect: "라벨 0/20" reads as "filling up" when the truth is that every
// resolved watch was structurally incapable of producing a label. Live
// 2026-07-25 the row said 축적 중 while 2 of 2 resolutions were unfair.
func TestEProcessLadderRowNamesUnfairLabelsInsteadOfImplyingAccumulation(t *testing.T) {
	tr := newTestTracker(t)
	seedBaselineLabel(t, tr, "evolve_confirmed", false, false)
	seedBaselineLabel(t, tr, "evolve_confirmed", false, false)

	row := tr.ladderEProcessRow()
	if row.State != ladderStateGrowing {
		t.Fatalf("state = %q, want growing", row.State)
	}
	if !strings.Contains(row.Detail, "무효 2건") {
		t.Fatalf("detail must name the unfair labels, got %q", row.Detail)
	}
	if !strings.Contains(row.Detail, "라벨 0/") {
		t.Fatalf("detail must still carry the fair-label count, got %q", row.Detail)
	}
}

// Agreement on a pure-confirm population is trivially ~1.0, so a row with
// labels but zero fair rollbacks is blocked on a different input than the
// count implies — say so rather than letting the number read as progress.
func TestEProcessLadderRowFlagsPureConfirmPopulation(t *testing.T) {
	tr := newTestTracker(t)
	seedBaselineLabel(t, tr, "evolve_confirmed", true, false)

	row := tr.ladderEProcessRow()
	if !strings.Contains(row.Detail, "공정 롤백 0건") {
		t.Fatalf("detail must flag the pure-confirm population, got %q", row.Detail)
	}
}

// A healthy row must stay quiet: no unfair labels and a real rollback present
// means the count alone is an honest progress signal.
func TestEProcessLadderRowStaysTerseWhenEvidenceIsHealthy(t *testing.T) {
	tr := newTestTracker(t)
	seedBaselineLabel(t, tr, "evolve_rolled_back", true, false)

	row := tr.ladderEProcessRow()
	if strings.Contains(row.Detail, "무효") || strings.Contains(row.Detail, "공정 롤백 0건") {
		t.Fatalf("healthy evidence must not carry a caveat, got %q", row.Detail)
	}
}

// The rollback half of Ready has no path by waiting when no evolve has ever
// regressed — the threshold's own comment concedes it is "structurally
// near-unreachable". A row that omits that invites the reader to wait for
// evidence that is not coming.
func TestEProcessLadderRowNamesTheUnreachableRollbackGate(t *testing.T) {
	tr := newTestTracker(t)
	seedBaselineLabel(t, tr, "evolve_confirmed", false, false)

	row := tr.ladderEProcessRow()
	if !strings.Contains(row.Detail, "롤백 이력 0건") {
		t.Fatalf("detail must name the empty rollback history, got %q", row.Detail)
	}
	for _, want := range []string{"rsi-backtest", "DENEB_EPROCESS_OWNS_ROLLBACK"} {
		if !strings.Contains(row.Detail, want) {
			t.Errorf("detail must point at the actual decision path %q, got %q", want, row.Detail)
		}
	}
}

// Once a rollback exists the gate is reachable again, so the caveat must go —
// otherwise it becomes permanent noise the reader learns to skip.
func TestEProcessLadderRowDropsTheCaveatOnceARollbackExists(t *testing.T) {
	tr := newTestTracker(t)
	seedBaselineLabel(t, tr, "evolve_rolled_back", true, false)

	row := tr.ladderEProcessRow()
	if strings.Contains(row.Detail, "롤백 이력 0건") {
		t.Fatalf("a ledger with a rollback must not claim an empty history, got %q", row.Detail)
	}
}

// RollbacksEver counts rollbacks whether or not they carried a baseline test —
// an unlabeled rollback still proves the gate is reachable.
func TestEProcessReadinessCountsUnlabeledRollbacks(t *testing.T) {
	tr := newTestTracker(t)
	appendFunnel(t, tr.logPath, LifecycleLogEntry{
		Type: "evolve_rolled_back", SkillName: "sk", CreatedAt: time.Now().UnixMilli(),
	})
	r := tr.eProcessCutoverReadiness()
	if r.RollbacksEver != 1 {
		t.Fatalf("RollbacksEver = %d, want 1 (no baseline test required)", r.RollbacksEver)
	}
	if r.Labels != 0 || r.FairRollbacks != 0 {
		t.Fatalf("an unlabeled rollback must not become a label: %+v", r)
	}
}
