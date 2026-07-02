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
