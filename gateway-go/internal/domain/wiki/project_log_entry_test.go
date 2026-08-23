package wiki

import (
	"strings"
	"testing"
)

// TestRotateProjectLog_CountsEntriesNotSubHeadings: a log entry routinely
// carries its own "## 요지"/"## 리스크" sub-headings (347 of 601 H2 lines across
// the live logs are not entry headers). Counting those as sections made
// "newest 20" mean about ten real entries and could split one entry across the
// archive boundary.
func TestRotateProjectLog_CountsEntriesNotSubHeadings(t *testing.T) {
	s, _ := newVerifyStore(t)
	const project = "pl2-kia-epc-001"

	var b strings.Builder
	b.WriteString("# 진행 로그\n")
	for i := range LogKeepSections {
		b.WriteString("\n## [2026-08-" + twoDigits(i+1) + "] 회의 | 항목 " + twoDigits(i+1) + "\n\n본문\n")
		// Every entry carries two sub-headings — 3× the H2 count of entries.
		b.WriteString("\n## 요지\n\n요지 본문\n")
		b.WriteString("\n## 리스크\n\n리스크 본문\n")
	}
	writePageT(t, s, LogPagePath(project), project+" 진행 로그", "프로젝트", b.String())

	// 20 entries (60 H2 lines) — at the keep limit, so nothing rotates.
	moved, err := s.RotateProjectLog(project)
	if err != nil {
		t.Fatalf("RotateProjectLog: %v", err)
	}
	if moved != 0 {
		t.Fatalf("rotated %d sections at exactly the entry limit, want 0", moved)
	}

	// One more entry pushes exactly one ENTRY out, sub-headings included.
	if err := s.UpdatePage(LogPagePath(project), func(cur *Page) (*Page, error) {
		cur.Body += "\n## [2026-08-21] 발주 | 신규\n\n본문\n\n## 요지\n\n새 요지\n"
		return cur, nil
	}); err != nil {
		t.Fatalf("append entry: %v", err)
	}
	moved, err = s.RotateProjectLog(project)
	if err != nil {
		t.Fatalf("RotateProjectLog after append: %v", err)
	}
	if moved != 1 {
		t.Fatalf("rotated %d, want 1 entry", moved)
	}

	archive, _ := s.ReadPage(LogArchivePath(project))
	if archive == nil {
		t.Fatal("archive page missing")
	}
	// The rotated entry kept its sub-headings — the split-entry failure mode.
	if !strings.Contains(archive.Body, "[2026-08-01] 회의 | 항목 01") ||
		!strings.Contains(archive.Body, "요지 본문") {
		t.Errorf("rotated entry lost its sub-sections:\n%s", archive.Body)
	}
	logPage, _ := s.ReadPage(LogPagePath(project))
	if strings.Contains(logPage.Body, "[2026-08-01] 회의 | 항목 01") {
		t.Error("rotated entry still in the live log")
	}
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
