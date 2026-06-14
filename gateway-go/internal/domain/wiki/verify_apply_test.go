package wiki

import (
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

func TestRecategorizedPath(t *testing.T) {
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

func TestApplyVerifyFixes_Move(t *testing.T) {
	s, wd := newVerifyStore(t)
	writePageT(t, s, "기타/김부장.md", "김부장", "기타", "사람인데 기타로 잘못 분류됨")

	n := wd.applyVerifyFixes([]VerifyFinding{{
		Type:  "misclassified",
		PageA: "기타/김부장.md",
		Fix:   &VerifyFix{Kind: "move", NewPath: "인물/김부장.md"},
	}})
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

func TestApplyVerifyFixes_Merge(t *testing.T) {
	s, wd := newVerifyStore(t)
	writePageT(t, s, "프로젝트/a.md", "탑솔라", "프로젝트", "AAA 본문")
	writePageT(t, s, "프로젝트/b.md", "탑솔라", "프로젝트", "BBB 본문")

	n := wd.applyVerifyFixes([]VerifyFinding{{
		Type:  "duplicate",
		PageA: "프로젝트/a.md", // keep
		PageB: "프로젝트/b.md", // fold
		Fix:   &VerifyFix{Kind: "merge"},
	}})
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

func TestApplyVerifyFixes_SkipsAdvisoryAndCaps(t *testing.T) {
	s, wd := newVerifyStore(t)
	// One advisory finding (no Fix) must be ignored entirely.
	advisory := VerifyFinding{Type: "misclassified", PageA: "기타/keep.md", Detail: "low-confidence"}
	writePageT(t, s, "기타/keep.md", "keep", "기타", "stays put")

	// Six high-confidence moves — the cap (5) must hold, leaving exactly one behind.
	var findings []VerifyFinding
	for _, name := range []string{"p0", "p1", "p2", "p3", "p4", "p5"} {
		writePageT(t, s, "기타/"+name+".md", name, "기타", "move me")
		findings = append(findings, VerifyFinding{
			Type:  "misclassified",
			PageA: "기타/" + name + ".md",
			Fix:   &VerifyFix{Kind: "move", NewPath: "인물/" + name + ".md"},
		})
	}
	findings = append([]VerifyFinding{advisory}, findings...)

	n := wd.applyVerifyFixes(findings)
	if n != maxAutoVerifyFixes {
		t.Fatalf("applied = %d, want %d (cap)", n, maxAutoVerifyFixes)
	}
	// The advisory page is untouched.
	if p, _ := s.ReadPage("기타/keep.md"); p == nil {
		t.Error("advisory (no-Fix) page was wrongly touched")
	}
	// Exactly one of the six move-sources remains (cap left it behind).
	remaining := 0
	for _, name := range []string{"p0", "p1", "p2", "p3", "p4", "p5"} {
		if p, _ := s.ReadPage("기타/" + name + ".md"); p != nil {
			remaining++
		}
	}
	if remaining != 1 {
		t.Errorf("remaining un-moved sources = %d, want 1 (6 - cap 5)", remaining)
	}
}

func TestExactDupFinding_KeepsHigherImportance(t *testing.T) {
	idx := NewIndex()
	idx.Entries["프로젝트/low.md"] = IndexEntry{Title: "탑솔라", Importance: 0.3}
	idx.Entries["프로젝트/high.md"] = IndexEntry{Title: "탑솔라", Importance: 0.8}

	// pathA is the low-importance one; the keeper should flip to the high one.
	f := exactDupFinding(idx, "프로젝트/low.md", "프로젝트/high.md", "동일한 제목")
	if f.PageA != "프로젝트/high.md" || f.PageB != "프로젝트/low.md" {
		t.Errorf("keep/fold = %q/%q, want high kept, low folded", f.PageA, f.PageB)
	}
	if f.Fix == nil || f.Fix.Kind != "merge" {
		t.Errorf("expected a merge Fix, got %+v", f.Fix)
	}
}
