package wiki

import (
	"strings"
	"testing"
)

// TestPruneDeadWikiLinks_RepointsRenamedTargetsAndLeavesAmbiguousProse: the
// 2026-07-19 folder rename swept Related but not bodies, so a third of the
// wiki's inline links point at retired paths — each one a missing graph edge
// and a dead end for a reader. Only unambiguous targets are rewritten; an
// unresolvable link is counted, never deleted (removing it would erase the
// author's claim that a relationship exists).
func TestPruneDeadWikiLinks_RepointsRenamedTargetsAndLeavesAmbiguousProse(t *testing.T) {
	s, _ := newVerifyStore(t)
	writePageT(t, s, "프로젝트/pl2-tha-epc-001/대표.md", "대한전선 당진", "프로젝트", "본문")
	writePageT(t, s, "인물/김부장.md", "김부장", "인물", "본문")
	writePageT(t, s, "업무/노트.md", "노트", "업무", strings.Join([]string{
		"구 경로 링크: [[프로젝트/대한전선-당진/대표.md]]",       // renamed → repointed by unique basename? no: repointed via title/basename
		"별칭 유지: [[프로젝트/대한전선-당진/대표.md|당진 프로젝트]]", // alias must survive
		"평문 인물 링크: [[김부장]]",                     // resolves to a live page — left as prose
		"죽은 링크: [[기타/사라진-페이지]]",                 // unresolvable — kept, counted
	}, "\n"))

	stats, err := s.PruneDeadWikiLinks()
	if err != nil {
		t.Fatalf("PruneDeadWikiLinks: %v", err)
	}

	page, _ := s.ReadPage("업무/노트.md")
	if strings.Contains(page.Body, "프로젝트/대한전선-당진/대표.md") {
		t.Errorf("renamed target not repointed:\n%s", page.Body)
	}
	if !strings.Contains(page.Body, "[[프로젝트/pl2-tha-epc-001/대표.md|당진 프로젝트]]") {
		t.Errorf("alias lost during repoint:\n%s", page.Body)
	}
	if !strings.Contains(page.Body, "[[김부장]]") {
		t.Errorf("live prose-form link was rewritten:\n%s", page.Body)
	}
	if !strings.Contains(page.Body, "[[기타/사라진-페이지]]") {
		t.Errorf("unresolvable link was deleted:\n%s", page.Body)
	}
	if stats.Removed == 0 {
		t.Error("unresolvable link not counted")
	}

	// Idempotent: a second sweep changes nothing.
	before := page.Body
	if _, err := s.PruneDeadWikiLinks(); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	after, _ := s.ReadPage("업무/노트.md")
	if after.Body != before {
		t.Errorf("sweep is not idempotent:\n%s\n---\n%s", before, after.Body)
	}
}
