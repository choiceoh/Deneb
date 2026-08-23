package wiki

import (
	"path/filepath"
	"testing"
)

func TestPersonBaseNameStripsRankAndQualifier(t *testing.T) {
	cases := map[string]string{
		"이영민":              "이영민",
		"이영민 차장":           "이영민",
		"이영민-팀장":           "이영민",
		"김익환 부장 (GS풍력발전)":  "김익환",
		"박상수-부장(한국전기안전공사)": "박상수",
		"오선택 전무 (기획조정실장)":  "오선택",
		"부장":               "", // a rank alone names no one
	}
	for title, want := range cases {
		if got := personBaseName(title); got != want {
			t.Errorf("personBaseName(%q) = %q, want %q", title, got, want)
		}
	}
}

// Two pages for one name are grouped with the evidence that tells the operator
// whether they are one person; a lone page is never reported.
func TestDuplicatePersonGroupsReportsCollisionsWithEvidence(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	write := func(rel, title string, emails []string, archived bool) {
		t.Helper()
		p := NewPage(title, "인물", nil)
		p.Meta.Emails = emails
		p.Meta.Archived = archived
		if err := s.WritePage(rel, p); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write("인물/선향란.md", "선향란", []string{"shr@topsolar.kr"}, false)
	write("인물/선향란-과장.md", "선향란 과장", []string{"shr@topsolar.kr"}, false)
	write("인물/강동민.md", "강동민", []string{"kdm@topsolar.kr"}, false)
	write("인물/강동민-대리.md", "강동민 대리", nil, true) // archived → not a live duplicate

	groups := s.DuplicatePersonGroups(0)
	if len(groups) != 1 {
		t.Fatalf("groups = %+v, want only 선향란", groups)
	}
	g := groups[0]
	if g.Name != "선향란" || len(g.Pages) != 2 {
		t.Fatalf("group = %+v", g)
	}
	if g.Pages[0].PagePath != "인물/선향란-과장.md" || g.Pages[1].PagePath != "인물/선향란.md" {
		t.Errorf("pages not sorted deterministically: %+v", g.Pages)
	}
	for _, p := range g.Pages {
		if len(p.Domains) != 1 || p.Domains[0] != "topsolar.kr" {
			t.Errorf("%s domains = %v — the operator needs this evidence", p.PagePath, p.Domains)
		}
	}
}

// The scan is advisory: it must never produce a Fix, because merging two people
// is exactly the 2026-07-28 over-merge incident.
func TestDetectDuplicatePersonPagesProposesNoFix(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	for rel, title := range map[string]string{
		"인물/이영민.md":    "이영민",
		"인물/이영민-차장.md": "이영민 차장",
	} {
		p := NewPage(title, "인물", nil)
		p.Meta.Emails = []string{"ylee@kr.kpmg.com"}
		if err := s.WritePage(rel, p); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	wd := &WikiDreamer{store: s}
	findings := wd.detectDuplicatePersonPages()
	if len(findings) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
	f := findings[0]
	if f.Type != "person-duplicate" || f.Fix != nil {
		t.Fatalf("finding = %+v — merging must stay manual", f)
	}
	if f.PageA == "" || f.PageB == "" {
		t.Errorf("finding names %q/%q; both pages must be identified", f.PageA, f.PageB)
	}
}
