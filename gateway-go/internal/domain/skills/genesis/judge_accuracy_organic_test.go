package genesis

import (
	"log/slog"
	"testing"
	"time"
)

// Organic false-accept mining: only baseline-confirmed rollbacks count (PACE
// precondition — a baseline-blind rollback is a disagreement label, not P3
// food), and each label is attributed to the judge version whose acceptance
// shipped the evolve.
func TestOrganicFalseAcceptsIncludesOnlyBaselineConfirmedRollbacks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	rollback := func(skill string, baselineConfirmed bool, withTest bool) {
		t.Helper()
		if withTest {
			tr.mu.Lock()
			tr.pendingBaselineTest[skill] = &rollbackBaselineTest{Reject: baselineConfirmed, Disagreement: !baselineConfirmed}
			tr.mu.Unlock()
		}
		if err := tr.logEvolveRolledBack(skill); err != nil {
			t.Fatal(err)
		}
	}

	// sk-a: judge v-live accepted, baseline-confirmed rollback → a label.
	if err := tr.logEvolveWithProvenance("sk-a", "1.1", "d", HarnessEditAudit{},
		&evolveProvenance{JudgeArtifactVersion: "v-live"}); err != nil {
		t.Fatal(err)
	}
	rollback("sk-a", true, true)

	// sk-b: rollback where the e-process did NOT reject (baseline-blind
	// threshold fire) → excluded.
	if err := tr.logEvolveWithProvenance("sk-b", "1.1", "d", HarnessEditAudit{},
		&evolveProvenance{JudgeArtifactVersion: "v-live"}); err != nil {
		t.Fatal(err)
	}
	rollback("sk-b", false, true)

	// sk-c: rollback with no baseline test at all (legacy entry) → excluded.
	if err := tr.logEvolveWithProvenance("sk-c", "1.1", "d", HarnessEditAudit{},
		&evolveProvenance{JudgeArtifactVersion: "v-live"}); err != nil {
		t.Fatal(err)
	}
	rollback("sk-c", false, false)

	// sk-d: provenance-free evolve (legacy) then confirmed rollback →
	// included with an empty JudgeVersion (consumer filters it out).
	if err := tr.LogEvolveWithAudit("sk-d", "1.1", "d", HarnessEditAudit{}); err != nil {
		t.Fatal(err)
	}
	rollback("sk-d", true, true)

	got := tr.organicFalseAccepts(30*24*time.Hour, 10)
	if len(got) != 2 {
		t.Fatalf("want 2 labels (sk-a, sk-d), got %+v", got)
	}
	// Newest first: sk-d then sk-a.
	if got[0].Skill != "sk-d" || got[0].JudgeVersion != "" {
		t.Fatalf("newest label must be provenance-free sk-d: %+v", got[0])
	}
	if got[1].Skill != "sk-a" || got[1].JudgeVersion != "v-live" {
		t.Fatalf("sk-a label must carry the accepting judge version: %+v", got[1])
	}

	// The limit caps newest-first.
	if capped := tr.organicFalseAccepts(30*24*time.Hour, 1); len(capped) != 1 || capped[0].Skill != "sk-d" {
		t.Fatalf("limit=1 must keep the newest label: %+v", capped)
	}

	// A window that excludes everything yields nothing.
	if none := tr.organicFalseAccepts(time.Millisecond, 10); len(none) != 0 {
		time.Sleep(2 * time.Millisecond)
		if none = tr.organicFalseAccepts(time.Millisecond, 10); len(none) != 0 {
			t.Fatalf("sub-ms window must exclude all labels: %+v", none)
		}
	}
}
