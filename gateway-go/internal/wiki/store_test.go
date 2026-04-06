package wiki

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewStore_CreatesDirs(t *testing.T) {
	dir := t.TempDir()
	wikiDir := filepath.Join(dir, "wiki")
	diaryDir := filepath.Join(dir, "diary")

	store, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	// Verify category directories exist.
	for _, cat := range Categories {
		catDir := filepath.Join(wikiDir, cat)
		info, err := os.Stat(catDir)
		if err != nil {
			t.Errorf("category dir %q not created: %v", cat, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("category %q is not a directory", cat)
		}
	}

	// Verify index.md exists.
	if _, err := os.Stat(filepath.Join(wikiDir, "index.md")); err != nil {
		t.Error("index.md not created")
	}
}

func TestStore_WriteAndReadPage(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	page := NewPage("DGX Spark", "기술", []string{"하드웨어", "NVIDIA"})
	page.Meta.Importance = 0.9
	page.Body = "# DGX Spark\n\n## 요약\nNVIDIA DGX Spark."

	if err := store.WritePage("기술/dgx-spark.md", page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	// Read back.
	got, err := store.ReadPage("기술/dgx-spark.md")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if got.Meta.Title != "DGX Spark" {
		t.Errorf("title = %q", got.Meta.Title)
	}
	if got.Meta.Importance != 0.9 {
		t.Errorf("importance = %f", got.Meta.Importance)
	}

	// Verify index was updated.
	idx := store.GetIndex()
	entry, ok := idx.Entries["기술/dgx-spark.md"]
	if !ok {
		t.Fatal("page not in index")
	}
	if entry.Title != "DGX Spark" {
		t.Errorf("index title = %q", entry.Title)
	}
}

func TestStore_DeletePage(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
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
	idx := store.GetIndex()
	if _, ok := idx.Entries["결정/temp.md"]; ok {
		t.Error("deleted page still in index")
	}
}

func TestStore_ListPages(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
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
	all, err := store.ListPages("")
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("ListPages('') = %d pages, want 3", len(all))
	}

	// List by category.
	tech, err := store.ListPages("기술")
	if err != nil {
		t.Fatalf("ListPages(기술): %v", err)
	}
	if len(tech) != 2 {
		t.Errorf("ListPages(기술) = %d pages, want 2", len(tech))
	}
}

func TestStore_Search(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	// Write a searchable page.
	page := NewPage("DGX Spark", "기술", []string{"NVIDIA"})
	page.Body = "# DGX Spark\n\n## 요약\nNVIDIA DGX Spark는 128GB 통합 메모리를 가진 로컬 서버입니다."
	if err := store.WritePage("기술/dgx-spark.md", page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	// Search for content.
	ctx := context.Background()
	results, err := store.Search(ctx, "NVIDIA", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Error("Search('NVIDIA') returned no results")
	}

	// Search for non-existent content.
	results, err = store.Search(ctx, "nonexistent_xyz_12345", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Search(nonexistent) returned %d results", len(results))
	}
}

func TestStore_Stats(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
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
