package wiki

import (
	"strings"
	"testing"
	"time"
)

// The rules-revision prompt must speak in kinds a synthesis rule can act on:
// category, and for projects the folder slot the layout rules name.
func TestPageKind_BucketsByCategoryAndProjectSlot(t *testing.T) {
	t.Parallel()
	for path, want := range map[string]string{
		"인물/김성훈.md":                      "인물",
		"사용자/오선택-사용자모델.md":               "사용자",
		"프로젝트/pl2-ymn-epc-001/대표.md":     "프로젝트/대표",
		"프로젝트/pl2-ymn-epc-001/로그.md":     "프로젝트/로그",
		"프로젝트/nde-ztt/메일분석/abc@x.md":     "프로젝트/메일분석",
		"프로젝트/nde-ztt/회의록/2026-07-01.md": "프로젝트/회의록",
		"프로젝트/거래/한화.md":                  "프로젝트/기타",
		"기타/맥코넬-의원.md":                   "기타",
	} {
		if got := pageKind(path); got != want {
			t.Errorf("pageKind(%q) = %q, want %q", path, got, want)
		}
	}
}

// Exposure is not use: a page only ever injected must not count as recalled,
// or the evidence would tell the rules that everything works.
func TestUtilityByKind_CountsObservedUseNotExposure(t *testing.T) {
	t.Parallel()
	entries := map[string]IndexEntry{
		"프로젝트/a/대표.md":        {Title: "A"},
		"프로젝트/b/대표.md":        {Title: "B"},
		"프로젝트/c/대표.md":        {Title: "C"},
		"인물/김甲.md":            {Title: "김갑"},
		"프로젝트/a/메일분석/m1@x.md": {Title: "m1"},
	}
	recalls := map[string]RecallUsage{
		"프로젝트/a/대표.md":        {Reads: 3},   // observed use
		"프로젝트/b/대표.md":        {Injects: 5}, // exposure only — not use
		"프로젝트/a/메일분석/m1@x.md": {Injects: 9},
	}
	groups := utilityByKind(entries, recalls)
	byKind := map[string]utilityGroup{}
	for _, g := range groups {
		byKind[g.Kind] = g
	}
	if g := byKind["프로젝트/대표"]; g.Written != 3 || g.Recalled != 1 {
		t.Errorf("프로젝트/대표 = %+v, want written=3 recalled=1 (inject-only must not count)", g)
	}
	if g := byKind["프로젝트/메일분석"]; g.Written != 1 || g.Recalled != 0 {
		t.Errorf("프로젝트/메일분석 = %+v, want written=1 recalled=0", g)
	}
	if g := byKind["인물"]; g.Written != 1 || g.Recalled != 0 {
		t.Errorf("인물 = %+v, want written=1 recalled=0", g)
	}
	// Most-written first, so the prompt leads with the kinds that dominate the
	// wiki rather than a long tail.
	if groups[0].Kind != "프로젝트/대표" {
		t.Errorf("groups[0] = %q, want the most-written kind first: %+v", groups[0].Kind, groups)
	}
}

// No ledger traffic → no evidence block. Teaching the rules from an empty
// ledger would read as "nothing is ever recalled".
func TestRenderUtilityEvidence_EmptyLedgerYieldsNoBlock(t *testing.T) {
	t.Parallel()
	s := missStore(t)
	wd := &WikiDreamer{store: s}
	if got := wd.renderUtilityEvidence(time.Now()); got != "" {
		t.Errorf("empty ledger produced an evidence block: %q", got)
	}
}

// With traffic, the block reports each kind's recall rate and appends the
// unanswered-demand line — the two halves of "what should we write more of".
func TestRenderUtilityEvidence_ReportsRatesAndDemand(t *testing.T) {
	t.Parallel()
	s := missStore(t)
	// The distribution is written-vs-recalled, so the page has to exist: a hit
	// on a page the index does not hold contributes no denominator.
	if err := s.WritePage("인물/김성훈.md", &Page{Meta: Frontmatter{Title: "김성훈"}, Body: "본문"}); err != nil {
		t.Fatalf("write page: %v", err)
	}
	if err := s.RecordRecallEvents([]RecallEvent{{Path: "인물/김성훈.md", Event: RecallEventRead}}); err != nil {
		t.Fatalf("record hit: %v", err)
	}
	if err := s.RecordRecallMiss(RecallMiss{Query: "완도 관산포 계약 기억나?"}); err != nil {
		t.Fatalf("record miss: %v", err)
	}
	wd := &WikiDreamer{store: s}
	got := wd.renderUtilityEvidence(time.Now())
	if got == "" {
		t.Fatal("expected an evidence block once the ledger has traffic")
	}
	if !strings.Contains(got, "인물: 보유 1쪽, 실제 회상 1쪽 (100%)") {
		t.Errorf("recall rate line missing/incorrect: %q", got)
	}
	if !strings.Contains(got, "답하지 못한 질문 주제(수요)") || !strings.Contains(got, "완도") {
		t.Errorf("demand line missing from evidence block: %q", got)
	}
}
