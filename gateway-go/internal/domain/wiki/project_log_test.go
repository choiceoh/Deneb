package wiki

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func newLogStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), ""))
	t.Cleanup(func() { store.Close() })
	return store
}

// TestRotateProjectLog: sections beyond the newest LogKeepSections move to
// 로그-보관.md (archived), newest sections stay in place and in order.
func TestRotateProjectLog(t *testing.T) {
	store := newLogStore(t)

	page := NewPage("영산고 진행 로그", "프로젝트", nil)
	var b strings.Builder
	b.WriteString("# 영산고 진행 로그\n")
	total := LogKeepSections + 7
	for i := 1; i <= total; i++ {
		fmt.Fprintf(&b, "\n## 2026-06-%02d 사건 %d\n\n내용 %d\n", i, i, i)
	}
	page.Body = b.String()
	if err := store.WritePage(LogPagePath("영산고"), page); err != nil {
		t.Fatal(err)
	}

	moved, err := store.RotateProjectLog("영산고")
	if err != nil {
		t.Fatalf("RotateProjectLog: %v", err)
	}
	if moved != 7 {
		t.Fatalf("moved = %d, want 7", moved)
	}

	logPage := testutil.Must(store.ReadPage(LogPagePath("영산고")))
	_, kept := logPage.SplitByH2()
	if len(kept) != LogKeepSections {
		t.Fatalf("kept sections = %d, want %d", len(kept), LogKeepSections)
	}
	// Oldest kept section is #8; newest is the last one.
	if kept[0].Heading != "2026-06-08 사건 8" || kept[len(kept)-1].Heading != fmt.Sprintf("2026-06-%02d 사건 %d", total, total) {
		t.Errorf("kept range wrong: first=%q last=%q", kept[0].Heading, kept[len(kept)-1].Heading)
	}

	archive := testutil.Must(store.ReadPage(LogArchivePath("영산고")))
	if !archive.Meta.Archived {
		t.Error("archive page must be archived")
	}
	_, archived := archive.SplitByH2()
	if len(archived) != 7 || archived[0].Heading != "2026-06-01 사건 1" {
		t.Errorf("archived sections = %d (first %q), want 7 starting at 사건 1",
			len(archived), archived[0].Heading)
	}

	// Under the cap: no-op.
	if moved, err := store.RotateProjectLog("영산고"); err != nil || moved != 0 {
		t.Errorf("second rotation = (%d, %v), want no-op", moved, err)
	}
	// Missing log: no-op, no error.
	if moved, err := store.RotateProjectLog("없는-프로젝트"); err != nil || moved != 0 {
		t.Errorf("missing log rotation = (%d, %v), want no-op", moved, err)
	}
}

// TestTrimRotatedLogSections_KeepsConcurrentAppend: the trim mutate rebuilds
// from the page's CURRENT body — a section appended between the rotation's
// pre-read and the trim (here simulated by a body that grew a tail section
// after the overflow snapshot) must survive; the stale-snapshot rebuild lost
// it silently.
func TestTrimRotatedLogSections_KeepsConcurrentAppend(t *testing.T) {
	var b strings.Builder
	b.WriteString("서문\n")
	for i := 1; i <= 4; i++ {
		fmt.Fprintf(&b, "\n## 사건 %d\n\n내용 %d\n", i, i)
	}
	snapshot := &Page{Body: b.String()}
	_, sections := snapshot.SplitByH2()
	overflow := sections[:2] // 사건 1, 사건 2 were archived

	// Meanwhile the live page gained 사건 5 (the race window append).
	cur := &Page{Body: b.String() + "\n## 사건 5\n\n내용 5\n"}
	got := trimRotatedLogSections(cur, overflow)
	if got == nil {
		t.Fatal("trim skipped despite a matching head")
	}
	_, kept := got.SplitByH2()
	if len(kept) != 3 || kept[0].Heading != "사건 3" || kept[2].Heading != "사건 5" {
		t.Fatalf("kept = %+v, want [사건 3, 사건 4, 사건 5]", kept)
	}
	if !strings.Contains(got.Body, "서문") {
		t.Errorf("preamble lost:\n%s", got.Body)
	}

	// Head mismatch (log rewritten concurrently) → skip, never guess.
	rewritten := &Page{Body: "## 완전히 다른 섹션\n\n내용\n"}
	if trimRotatedLogSections(rewritten, overflow) != nil {
		t.Error("trim must bail when the head no longer matches the archived overflow")
	}
	// Deleted concurrently → skip.
	if trimRotatedLogSections(nil, overflow) != nil {
		t.Error("trim must bail on a deleted page")
	}
}

// TestIsProjectLogPage guards the review-exclusion rule for log slots.
func TestIsProjectLogPage(t *testing.T) {
	cases := map[string]bool{
		"프로젝트/영산고/로그.md":    true,
		"프로젝트/영산고/로그-보관.md": true,
		"프로젝트/영산고/대표.md":    false,
		"프로젝트/영산고/상세.md":    false,
		"프로젝트/거래/로그.md":     false, // reserved bucket, not a project
		"업무/로그.md":          false,
	}
	for p, want := range cases {
		if got := IsProjectLogPage(p); got != want {
			t.Errorf("IsProjectLogPage(%q) = %v, want %v", p, got, want)
		}
	}
}

// TestFindSimilarPages_CodeSignal: two 대표페이지 sharing a frozen project code
// are the same project regardless of naming.
func TestFindSimilarPages_CodeSignal(t *testing.T) {
	store := newLogStore(t)
	a := NewPage("기아 화성 국유지", "프로젝트", nil)
	a.Meta.Code = "pl3-kia-mod-001"
	a.Body = "# a"
	if err := store.WritePage("프로젝트/기아-화성/대표.md", a); err != nil {
		t.Fatal(err)
	}

	hits := store.FindSimilarPages(t.Context(), SimilarQuery{
		Path:  "프로젝트/기아-오토랜드-화성/대표.md",
		Code:  "pl3-kia-mod-001",
		Title: "기아 오토랜드 화성 모듈",
	}, 3)
	if len(hits) == 0 || hits[0].Path != "프로젝트/기아-화성/대표.md" || hits[0].Reason != "code" {
		t.Fatalf("hits = %+v, want the code-matched 대표페이지 first", hits)
	}
}
