package wiki

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func newVerifyStore(t *testing.T) (*Store, *WikiDreamer) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(dir, dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	wd := &WikiDreamer{store: s, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return s, wd
}

func writePageT(t *testing.T, s *Store, rel, title, category, body string) {
	t.Helper()
	p := NewPage(title, category, nil)
	p.Body = body
	if err := s.WritePage(rel, p); err != nil {
		t.Fatalf("WritePage %q: %v", rel, err)
	}
}

func TestRecategorizedPath_SwapsLeadingCategoryDirNoopOnSameOrInvalid(t *testing.T) {
	cases := []struct {
		path, newCat, want string
	}{
		{"기타/김부장.md", "인물", "인물/김부장.md"},
		{"기타/김부장.md", "기타", ""},              // same category → no move
		{"기타/김부장.md", "엉뚱", ""},              // invalid category → no move
		{"김부장.md", "인물", ""},                 // no category segment → skip
		{"프로젝트/거래/x.md", "업무", "업무/거래/x.md"}, // only the leading dir swaps
	}
	for _, c := range cases {
		if got := recategorizedPath(c.path, c.newCat); got != c.want {
			t.Errorf("recategorizedPath(%q, %q) = %q, want %q", c.path, c.newCat, got, c.want)
		}
	}
}

func TestApplyVerifyFixes_MoveRelocatesPageAndUpdatesCategory(t *testing.T) {
	s, wd := newVerifyStore(t)
	writePageT(t, s, "기타/김부장.md", "김부장", "기타", "사람인데 기타로 잘못 분류됨")

	n := wd.applyVerifyFixes([]verifyFinding{{
		Type:  "misclassified",
		PageA: "기타/김부장.md",
		Fix:   &verifyFix{Kind: "move", NewPath: "인물/김부장.md"},
	}}, nil)
	if n != 1 {
		t.Fatalf("applied = %d, want 1", n)
	}
	if p, _ := s.ReadPage("기타/김부장.md"); p != nil {
		t.Error("source still present after auto-move")
	}
	moved, _ := s.ReadPage("인물/김부장.md")
	if moved == nil || moved.Meta.Category != "인물" {
		t.Errorf("page not moved to 인물 with updated category: %+v", moved)
	}
}

func TestApplyVerifyFixes_MergeFoldsPagePreservingBothBodies(t *testing.T) {
	s, wd := newVerifyStore(t)
	writePageT(t, s, "프로젝트/a.md", "탑솔라", "프로젝트", "AAA 본문")
	writePageT(t, s, "프로젝트/b.md", "탑솔라", "프로젝트", "BBB 본문")

	n := wd.applyVerifyFixes([]verifyFinding{{
		Type:  "duplicate",
		PageA: "프로젝트/a.md", // keep
		PageB: "프로젝트/b.md", // fold
		Fix:   &verifyFix{Kind: "merge"},
	}}, nil)
	if n != 1 {
		t.Fatalf("applied = %d, want 1", n)
	}
	if p, _ := s.ReadPage("프로젝트/b.md"); p != nil {
		t.Error("folded page still present after auto-merge")
	}
	keep, _ := s.ReadPage("프로젝트/a.md")
	if keep == nil {
		t.Fatal("keeper page vanished")
	}
	// Zero-loss: both bodies survive in the keeper.
	for _, must := range []string{"AAA 본문", "BBB 본문", "병합된 중복 문서"} {
		if !strings.Contains(keep.Body, must) {
			t.Errorf("merged body missing %q:\n%s", must, keep.Body)
		}
	}
}

func TestApplyVerifyFixes_IgnoresAdvisoryAndCapsAppliedFixCount(t *testing.T) {
	s, wd := newVerifyStore(t)
	// One advisory finding (no Fix) must be ignored entirely.
	advisory := verifyFinding{Type: "misclassified", PageA: "기타/keep.md", Detail: "low-confidence"}
	writePageT(t, s, "기타/keep.md", "keep", "기타", "stays put")

	// Cap+1 high-confidence moves — the cap must hold, leaving exactly one behind.
	var names []string
	for i := 0; i <= maxAutoVerifyFixes; i++ {
		names = append(names, fmt.Sprintf("p%02d", i))
	}
	var findings []verifyFinding
	for _, name := range names {
		writePageT(t, s, "기타/"+name+".md", name, "기타", "move me")
		findings = append(findings, verifyFinding{
			Type:  "misclassified",
			PageA: "기타/" + name + ".md",
			Fix:   &verifyFix{Kind: "move", NewPath: "인물/" + name + ".md"},
		})
	}
	findings = append([]verifyFinding{advisory}, findings...)

	n := wd.applyVerifyFixes(findings, nil)
	if n != maxAutoVerifyFixes {
		t.Fatalf("applied = %d, want %d (cap)", n, maxAutoVerifyFixes)
	}
	// The advisory page is untouched.
	if p, _ := s.ReadPage("기타/keep.md"); p == nil {
		t.Error("advisory (no-Fix) page was wrongly touched")
	}
	// Exactly one of the move-sources remains (cap left it behind).
	remaining := 0
	for _, name := range names {
		if p, _ := s.ReadPage("기타/" + name + ".md"); p != nil {
			remaining++
		}
	}
	if remaining != 1 {
		t.Errorf("remaining un-moved sources = %d, want 1 (cap+1 - cap)", remaining)
	}
}

// TestFoldDuplicate_RefusesDifferentMessageIDsAllowsSameIDAcrossBuckets: the shared
// merge chokepoint is the last-line guard — even if a caller decides two
// 메일분석 pages are duplicates, different Message IDs mean two different Gmail
// messages (메일 1통 = 1페이지) and the fold is refused with both pages left
// intact. A same-ID fold (one mail relocated across buckets) still succeeds.
func TestFoldDuplicate_RefusesDifferentMessageIDsAllowsSameIDAcrossBuckets(t *testing.T) {
	s, _ := newVerifyStore(t)
	keep := "프로젝트/해밀고흥솔라팜-모듈/메일분석/19eaa3e4371576a3.md"
	fold := "프로젝트/해밀고흥솔라팜-모듈/메일분석/19eaa3aa72de312b.md"
	writePageT(t, s, keep, "Re: 견적", "프로젝트", "> Message ID: `19eaa3e4371576a3`\nA 본문")
	writePageT(t, s, fold, "Re: 견적", "프로젝트", "> Message ID: `19eaa3aa72de312b`\nB 본문")

	if err := s.FoldDuplicate(keep, fold); err == nil {
		t.Error("FoldDuplicate folded two distinct mail analyses (different Message IDs)")
	}
	// Both pages survive untouched — the guard fires before any write/delete.
	if p, _ := s.ReadPage(fold); p == nil {
		t.Error("fold page was destroyed despite the guard rejecting the fold")
	}
	if p, _ := s.ReadPage(keep); p == nil {
		t.Error("keep page vanished")
	}
	if p, _ := s.ReadPage(keep); p != nil && strings.Contains(p.Body, "병합된 중복 문서") {
		t.Error("keep page absorbed the fold body despite the guard")
	}

	// Same msgID in two buckets IS one mail — that fold must still succeed.
	uKeep := "프로젝트/해밀고흥솔라팜-모듈/메일분석/aaaa1111bbbb2222.md"
	uFold := "프로젝트/메일분석/aaaa1111bbbb2222.md"
	writePageT(t, s, uKeep, "FW: 계약", "프로젝트", "> Message ID: `aaaa1111bbbb2222`\n프로젝트 슬롯")
	writePageT(t, s, uFold, "FW: 계약", "프로젝트", "> Message ID: `aaaa1111bbbb2222`\n미연결 버킷")
	if err := s.FoldDuplicate(uKeep, uFold); err != nil {
		t.Errorf("same-mail fold across buckets rejected: %v", err)
	}
	if p, _ := s.ReadPage(uFold); p != nil {
		t.Error("same-mail duplicate should have been folded away")
	}
}

func TestExactDupFinding_PicksHigherImportancePageAsKeeperWithMergeFix(t *testing.T) {
	entries := map[string]IndexEntry{
		"프로젝트/low.md":  {Title: "탑솔라", Importance: 0.3},
		"프로젝트/high.md": {Title: "탑솔라", Importance: 0.8},
	}

	// pathA is the low-importance one; the keeper should flip to the high one.
	f := exactDupFinding(entries, "프로젝트/low.md", "프로젝트/high.md", "동일한 제목")
	if f.PageA != "프로젝트/high.md" || f.PageB != "프로젝트/low.md" {
		t.Errorf("keep/fold = %q/%q, want high kept, low folded", f.PageA, f.PageB)
	}
	if f.Fix == nil || f.Fix.Kind != "merge" {
		t.Errorf("expected a merge Fix, got %+v", f.Fix)
	}
}
