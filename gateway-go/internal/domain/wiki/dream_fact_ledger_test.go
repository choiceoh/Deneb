package wiki

import (
	"os"
	"strings"
	"testing"
)

// The fact ledger records every applied mutation with its page and detail,
// and the verify-fix recorder feeds the measured DreamReport counters
// (improvement-ideas 5.6) — the numbers the weekly digest reports.
func TestVerifyFixRecorderFeedsLedgerAndCounters(t *testing.T) {
	s, wd := newVerifyStore(t)
	writePageT(t, s, "프로젝트/a.md", "탑솔라", "프로젝트", "AAA 본문")
	writePageT(t, s, "프로젝트/b.md", "탑솔라", "프로젝트", "BBB 본문")

	var merged int
	rec := func(f verifyFinding) {
		if f.Fix.Kind == "merge" {
			merged++
			wd.recordDreamFact("verify", "merged", f.PageA, f.PageB, f.Detail)
		}
	}
	n := wd.applyVerifyFixes([]verifyFinding{{
		Type:   "duplicate",
		PageA:  "프로젝트/a.md",
		PageB:  "프로젝트/b.md",
		Detail: "동일 제목 중복",
		Fix:    &verifyFix{Kind: "merge"},
	}}, rec)
	if n != 1 || merged != 1 {
		t.Fatalf("applied=%d merged=%d, want 1/1", n, merged)
	}

	ledger := wd.LoadDreamFactLedger()
	if len(ledger) != 1 {
		t.Fatalf("ledger entries = %d, want 1", len(ledger))
	}
	rec0 := ledger[0]
	if rec0.Phase != "verify" || rec0.Op != "merged" || rec0.Page != "프로젝트/a.md" ||
		rec0.RefPage != "프로젝트/b.md" || rec0.Detail != "동일 제목 중복" || rec0.AtMs == 0 {
		t.Fatalf("record = %+v", rec0)
	}
}

func TestRecordDreamFactSkipsEmptyAndNilStore(t *testing.T) {
	wd := &WikiDreamer{} // no store — must be a silent no-op
	wd.recordDreamFact("synthesize", "learned", "업무/x.md", "", "")
	wd.recordDreamFact("synthesize", "learned", "  ", "", "") // blank page skips
	if got := wd.LoadDreamFactLedger(); got != nil {
		t.Fatalf("nil-store dreamer must record nothing, got %+v", got)
	}

	s, wd2 := newVerifyStore(t)
	wd2.recordDreamFact("synthesize", "learned", "", "ref", "blank page never records")
	if got := wd2.LoadDreamFactLedger(); len(got) != 0 {
		t.Fatalf("blank page must not record, got %+v", got)
	}
	// Corrupt trailing lines are skipped on load, valid ones survive.
	raw := "not-json\n{\"atMs\":1,\"phase\":\"verify\",\"op\":\"expired\",\"page\":\"업무/y.md\"}\n"
	if err := os.WriteFile(s.Dir()+"/"+dreamFactLedgerFile, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	got := wd2.LoadDreamFactLedger()
	if len(got) != 1 || got[0].Op != "expired" || !strings.HasPrefix(got[0].Page, "업") {
		t.Fatalf("corrupt-tolerant load = %+v", got)
	}
}
