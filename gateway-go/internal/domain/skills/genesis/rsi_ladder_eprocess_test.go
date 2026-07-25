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
