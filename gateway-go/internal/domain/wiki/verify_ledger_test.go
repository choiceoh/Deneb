package wiki

import (
	"testing"
	"time"
)

func advisory(typ, a, b, detail string) verifyFinding {
	return verifyFinding{Type: typ, PageA: a, PageB: b, Detail: detail}
}

// A finding seen for the first time is reported; the same finding next cycle
// folds into the repeat count — the dream notification announces news, not the
// standing backlog (measured before #4376: 6,632 lines / 1,779 unique over 14d).
func TestReconcileVerifyFindings_ReportsOnlyFirstTimeFindings(t *testing.T) {
	t.Parallel()
	now := time.Now()
	led := verifyLedger{Version: 1, Entries: map[string]verifyLedgerEntry{}}
	findings := []verifyFinding{
		advisory("stale_deadline", "프로젝트/a/대표.md", "", "기한 지남 (2026-07-01)"),
		advisory("duplicate", "프로젝트/x.md", "프로젝트/y.md", "유사한 제목"),
	}

	led, fresh, repeats := reconcileVerifyFindings(led, findings, now)
	if len(fresh) != 2 || repeats != 0 {
		t.Fatalf("cycle 1: fresh=%d repeats=%d, want 2/0", len(fresh), repeats)
	}

	// Cycle 2: same findings — Detail may re-render differently (dates,
	// distances) but the key holds, so nothing is re-announced.
	findings[0].Detail = "기한 지남 (2026-07-02)"
	led, fresh, repeats = reconcileVerifyFindings(led, findings, now.Add(8*time.Hour))
	if len(fresh) != 0 || repeats != 2 {
		t.Fatalf("cycle 2: fresh=%d repeats=%d, want 0/2", len(fresh), repeats)
	}
	if e := led.Entries[verifyFindingKey(findings[0])]; e.Count != 2 {
		t.Errorf("repeat count = %d, want 2", e.Count)
	}
}

// An entry whose finding stops appearing is dropped — the issue was resolved
// (page merged/archived, deadline updated) — so a genuine recurrence later
// counts as new again instead of being silently folded forever.
func TestReconcileVerifyFindings_ResolvedFindingsRearmAsNew(t *testing.T) {
	t.Parallel()
	now := time.Now()
	f := advisory("duplicate", "프로젝트/x.md", "프로젝트/y.md", "유사한 제목")
	led, _, _ := reconcileVerifyFindings(verifyLedger{Entries: map[string]verifyLedgerEntry{}}, []verifyFinding{f}, now)

	// Resolved: the finding does not re-appear.
	led, fresh, repeats := reconcileVerifyFindings(led, nil, now.Add(8*time.Hour))
	if len(led.Entries) != 0 || len(fresh) != 0 || repeats != 0 {
		t.Fatalf("resolved finding must leave the ledger: entries=%d", len(led.Entries))
	}

	// Recurs a week later: announced as new.
	_, fresh, repeats = reconcileVerifyFindings(led, []verifyFinding{f}, now.Add(7*24*time.Hour))
	if len(fresh) != 1 || repeats != 0 {
		t.Fatalf("recurrence: fresh=%d repeats=%d, want 1/0", len(fresh), repeats)
	}
}

// Auto-fixed findings are applied, never announced — they must not occupy the
// ledger either, or their keys would suppress a later ADVISORY finding on the
// same pair.
func TestReconcileVerifyFindings_IgnoresAutoFixedFindings(t *testing.T) {
	t.Parallel()
	fixed := verifyFinding{Type: "duplicate", PageA: "a.md", PageB: "b.md", Fix: &verifyFix{Kind: "merge"}}
	led, fresh, repeats := reconcileVerifyFindings(verifyLedger{Entries: map[string]verifyLedgerEntry{}},
		[]verifyFinding{fixed}, time.Now())
	if len(led.Entries) != 0 || len(fresh) != 0 || repeats != 0 {
		t.Fatalf("auto-fixed finding leaked into the ledger/report: entries=%d fresh=%d", len(led.Entries), len(fresh))
	}
}
