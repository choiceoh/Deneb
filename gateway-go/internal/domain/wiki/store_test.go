package wiki

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestStore_WriteAndReadPage(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	page := NewPage("DGX Spark", "기술", []string{"하드웨어", "NVIDIA"})
	page.Meta.Importance = 0.9
	page.Body = "# DGX Spark\n\n## 요약\nNVIDIA DGX Spark."

	if err := store.WritePage("기술/dgx-spark.md", page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	// Read back.
	got := testutil.Must(store.ReadPage("기술/dgx-spark.md"))
	if got.Meta.Title != "DGX Spark" {
		t.Errorf("title = %q", got.Meta.Title)
	}
	if got.Meta.Importance != 0.9 {
		t.Errorf("importance = %f", got.Meta.Importance)
	}

	// Verify index was updated.
	idx := store.Index()
	entry, ok := idx.Entries["기술/dgx-spark.md"]
	if !ok {
		t.Fatal("page not in index")
	}
	if entry.Title != "DGX Spark" {
		t.Errorf("index title = %q", entry.Title)
	}
}

// TestStore_PathNormalization verifies that a page written with a bare path
// (no .md extension) resolves to the same .md file on every access path —
// ReadPage, ListPages, and the master index. This guards the duplicate-page
// regression: before normalization, a bare path wrote an extensionless sibling
// that ListPages (which filters on .md) dropped, so search and index never saw
// it and the dreamer kept re-creating the same page.
func TestStore_PathNormalization(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	page := NewPage("스파이럴 테스트베드", "프로젝트", []string{"자동화"})
	page.Meta.Importance = 0.85
	page.Body = "# 스파이럴 테스트베드\n\n본문"

	// Write WITHOUT the .md extension — the bug trigger.
	if err := store.WritePage("프로젝트/스파이럴-테스트베드", page); err != nil {
		t.Fatalf("WritePage(bare): %v", err)
	}

	// Only the .md file must exist on disk; no extensionless sibling.
	mdPath := filepath.Join(dir, "wiki", "프로젝트", "스파이럴-테스트베드.md")
	if _, err := os.Stat(mdPath); err != nil {
		t.Fatalf("expected .md file on disk: %v", err)
	}
	barePath := filepath.Join(dir, "wiki", "프로젝트", "스파이럴-테스트베드")
	if _, err := os.Stat(barePath); !os.IsNotExist(err) {
		t.Errorf("extensionless sibling should not exist (err=%v)", err)
	}

	// ReadPage resolves with or without the extension.
	if _, err := store.ReadPage("프로젝트/스파이럴-테스트베드"); err != nil {
		t.Errorf("ReadPage(bare): %v", err)
	}
	if _, err := store.ReadPage("프로젝트/스파이럴-테스트베드.md"); err != nil {
		t.Errorf("ReadPage(.md): %v", err)
	}

	// ListPages sees exactly one page (the .md file), not zero or two.
	pages := testutil.Must(store.ListPages("프로젝트"))
	if len(pages) != 1 {
		t.Fatalf("ListPages(프로젝트) = %d, want 1 (%v)", len(pages), pages)
	}
	if !strings.HasSuffix(pages[0], ".md") {
		t.Errorf("listed page %q lacks .md", pages[0])
	}

	// The master index is keyed by the normalized .md path.
	if _, ok := store.Index().Entries["프로젝트/스파이럴-테스트베드.md"]; !ok {
		t.Errorf("index missing normalized key; entries=%v", store.Index().Entries)
	}

	// Re-writing with the .md form must update in place, not create a second page.
	page.Meta.Importance = 0.90
	if err := store.WritePage("프로젝트/스파이럴-테스트베드.md", page); err != nil {
		t.Fatalf("WritePage(.md update): %v", err)
	}
	if pages := testutil.Must(store.ListPages("프로젝트")); len(pages) != 1 {
		t.Errorf("after re-write ListPages = %d, want 1 (%v)", len(pages), pages)
	}
}

func TestStore_DeletePage(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	page := NewPage("임시", "결정", nil)
	page.Body = "# 임시"
	if err := store.WritePage("결정/temp.md", page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	if err := store.DeletePage("결정/temp.md"); err != nil {
		t.Fatalf("DeletePage: %v", err)
	}

	// File should be gone.
	abs := filepath.Join(dir, "wiki", "결정/temp.md")
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Error("file should be deleted")
	}

	// Index should be updated.
	idx := store.Index()
	if _, ok := idx.Entries["결정/temp.md"]; ok {
		t.Error("deleted page still in index")
	}
}

func TestStore_ListPages(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	// Write pages in different categories.
	for _, tc := range []struct {
		path  string
		title string
		cat   string
	}{
		{"기술/go.md", "Go", "기술"},
		{"기술/rust.md", "Rust", "기술"},
		{"사람/alice.md", "Alice", "사람"},
	} {
		p := NewPage(tc.title, tc.cat, nil)
		p.Body = "# " + tc.title
		if err := store.WritePage(tc.path, p); err != nil {
			t.Fatalf("WritePage(%s): %v", tc.path, err)
		}
	}

	// List all.
	all := testutil.Must(store.ListPages(""))
	if len(all) != 3 {
		t.Errorf("ListPages('') = %d pages, want 3", len(all))
	}

	// List by category.
	tech := testutil.Must(store.ListPages("기술"))
	if len(tech) != 2 {
		t.Errorf("ListPages(기술) = %d pages, want 2", len(tech))
	}
}

func TestStore_Search(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	// Write a searchable page.
	page := NewPage("DGX Spark", "기술", []string{"NVIDIA"})
	page.Body = "# DGX Spark\n\n## 요약\nNVIDIA DGX Spark는 128GB 통합 메모리를 가진 로컬 서버입니다."
	if err := store.WritePage("기술/dgx-spark.md", page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	// Search for content.
	ctx := context.Background()
	results := testutil.Must(store.Search(ctx, "NVIDIA", 10))
	if len(results) == 0 {
		t.Error("Search('NVIDIA') returned no results")
	}

	// Search for non-existent content.
	results = testutil.Must(store.Search(ctx, "nonexistent_xyz_12345", 10))
	if len(results) != 0 {
		t.Errorf("Search(nonexistent) returned %d results", len(results))
	}
}

func TestStore_BacklinkMaintenance(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	// Write page B first (target of backlink).
	pageB := NewPage("Page B", "기술", nil)
	pageB.Body = "# Page B"
	if err := store.WritePage("기술/b.md", pageB); err != nil {
		t.Fatalf("WritePage(B): %v", err)
	}

	// Write page A with related pointing to B.
	pageA := NewPage("Page A", "기술", nil)
	pageA.Meta.Related = []string{"기술/b.md"}
	pageA.Body = "# Page A"
	if err := store.WritePage("기술/a.md", pageA); err != nil {
		t.Fatalf("WritePage(A): %v", err)
	}

	// Verify B now has a backlink to A.
	gotB := testutil.Must(store.ReadPage("기술/b.md"))
	found := false
	for _, r := range gotB.Meta.Related {
		if r == "기술/a.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("B.Related = %v, want to contain 기술/a.md", gotB.Meta.Related)
	}
}

func TestStore_BacklinkCleanupOnDelete(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	// Write B, then A referencing B.
	pageB := NewPage("B", "기술", nil)
	pageB.Body = "# B"
	_ = store.WritePage("기술/b.md", pageB)

	pageA := NewPage("A", "기술", nil)
	pageA.Meta.Related = []string{"기술/b.md"}
	pageA.Body = "# A"
	_ = store.WritePage("기술/a.md", pageA)

	// Delete A — B should lose the backlink.
	if err := store.DeletePage("기술/a.md"); err != nil {
		t.Fatalf("DeletePage: %v", err)
	}

	gotB, _ := store.ReadPage("기술/b.md")
	for _, r := range gotB.Meta.Related {
		if r == "기술/a.md" {
			t.Errorf("B still has backlink to deleted A: %v", gotB.Meta.Related)
		}
	}
}

func TestStore_Stats(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	page := NewPage("Test", "기술", nil)
	page.Body = "# Test\n\nContent."
	if err := store.WritePage("기술/test.md", page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	stats := store.Stats()
	if stats.TotalPages != 1 {
		t.Errorf("TotalPages = %d, want 1", stats.TotalPages)
	}
	if stats.TotalBytes == 0 {
		t.Error("TotalBytes = 0")
	}
	if stats.CategoryCount["기술"] != 1 {
		t.Errorf("CategoryCount[기술] = %d, want 1", stats.CategoryCount["기술"])
	}
}

// TestAppendDiaryTo_RedactsSecret ensures diary entries are scrubbed before
// they land on disk. Diary files are the primary input to the Wiki Dreamer;
// redacting here stops secrets from entering the wiki synthesis pipeline.
func TestAppendDiaryTo_RedactsSecret(t *testing.T) {
	dir := t.TempDir()
	diaryDir := filepath.Join(dir, "diary")

	token := "sk-ant-" + strings.Repeat("Z", 40) // synthetic
	entry := "사용자가 ANTHROPIC_API_KEY=" + token + " 를 설정함"
	if err := AppendDiaryTo(diaryDir, entry); err != nil {
		t.Fatalf("AppendDiaryTo: %v", err)
	}

	// Locate today's diary file.
	entries, err := os.ReadDir(diaryDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 diary file, got %d", len(entries))
	}
	data := testutil.Must(os.ReadFile(filepath.Join(diaryDir, entries[0].Name())))
	body := string(data)
	if strings.Contains(body, token) {
		t.Fatalf("diary file contains raw token: %q", body)
	}
	// Korean surrounding text must survive.
	if !strings.Contains(body, "사용자가") {
		t.Errorf("Korean prose lost: %q", body)
	}
}

// TestStore_AppendLog_RedactsSecret ensures the audit log does not persist
// secret patterns (page titles / details can echo user content).
func TestStore_AppendLog_RedactsSecret(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	token := "github_pat_11" + strings.Repeat("Z", 60)
	if err := store.AppendLog("create", "페이지 본문에 "+token+" 포함됨"); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}

	data := testutil.Must(os.ReadFile(filepath.Join(dir, "wiki", "log.md")))
	body := string(data)
	if strings.Contains(body, token) {
		t.Fatalf("log.md contains raw token: %q", body)
	}
}
