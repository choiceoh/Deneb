package wikiwork

import (
	"testing"
	"time"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
)

// The grace contract: a fresh dead link waits, a healed link is forgotten (its
// grace restarts if it dies again), and only a link dead across the whole
// window is condemned — and then leaves the ledger, since it no longer exists.
func TestReconcileDeadLinksGraceWindow(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -(wikiDeadLinkGraceDays + 1)).UnixMilli()
	recent := now.AddDate(0, 0, -3).UnixMilli()

	ledger := map[string]int64{
		"업무/보고.md\x00프로젝트/사라진/대표.md": old,    // past grace → condemn
		"업무/보고.md\x00인물/임은진":         recent, // inside grace → keep waiting
		"업무/노트.md\x00기타/치유됨":         old,    // no longer dead → forget
	}
	current := []wiki.DeadWikiLink{
		{Page: "업무/보고.md", Target: "프로젝트/사라진/대표.md"},
		{Page: "업무/보고.md", Target: "인물/임은진"},
		{Page: "업무/메모.md", Target: "프로젝트/신규-부패"}, // first seen now
	}

	next, condemned := reconcileDeadLinks(ledger, current, now)

	if len(condemned) != 1 || !condemned["업무/보고.md"]["프로젝트/사라진/대표.md"] {
		t.Fatalf("condemned = %+v, want only the past-grace link", condemned)
	}
	if _, ok := next["업무/보고.md\x00프로젝트/사라진/대표.md"]; ok {
		t.Error("condemned link still in the ledger")
	}
	if got := next["업무/보고.md\x00인물/임은진"]; got != recent {
		t.Errorf("waiting link's first-seen changed: %d != %d", got, recent)
	}
	if _, ok := next["업무/노트.md\x00기타/치유됨"]; ok {
		t.Error("healed link not forgotten")
	}
	if got := next["업무/메모.md\x00프로젝트/신규-부패"]; got != now.UnixMilli() {
		t.Errorf("new dead link not stamped now: %d", got)
	}
}

// A nil ledger (first run ever, or pre-upgrade state file) must behave as
// empty, not panic.
func TestReconcileDeadLinksNilLedger(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	next, condemned := reconcileDeadLinks(nil, []wiki.DeadWikiLink{{Page: "p.md", Target: "x/y"}}, now)
	if len(condemned) != 0 {
		t.Fatalf("fresh link condemned immediately: %+v", condemned)
	}
	if len(next) != 1 {
		t.Fatalf("ledger = %+v, want the one fresh entry", next)
	}
}
