package wiki

import (
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
)

var (
	callMethod    = rpctest.Call
	mustOK        = rpctest.MustOK
	mustErr       = rpctest.MustErr
	extractResult = rpctest.Result
)

// newTestStore creates a wiki.Store backed by a temp directory.
func newTestStore(t *testing.T) *wiki.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// seedPage writes a test page into the store and fails the test on error.
func seedPage(t *testing.T, store *wiki.Store, path, title, category, body string, tags []string) {
	t.Helper()
	page := wiki.NewPage(title, category, tags)
	page.Body = body
	if err := store.WritePage(path, page); err != nil {
		t.Fatalf("seed WritePage(%q): %v", path, err)
	}
}

// methodsWithStore returns the handler map backed by a real store.
func methodsWithStore(t *testing.T) (map[string]rpcutil.HandlerFunc, *wiki.Store) {
	t.Helper()
	store := newTestStore(t)
	m := Methods(Deps{Store: store})
	if m == nil {
		t.Fatal("Methods returned nil with non-nil store")
	}
	return m, store
}

// ─── Methods registration ───────────────────────────────────────────────────

func TestMethods_nilStore_returnsNil(t *testing.T) {
	m := Methods(Deps{Store: nil})
	if m != nil {
		t.Errorf("expected nil, got %d handlers", len(m))
	}
}

func TestMethods_registersAllHandlers(t *testing.T) {
	m, _ := methodsWithStore(t)
	expected := []string{
		"wiki.search",
		"wiki.read",
		"wiki.write",
		"wiki.delete",
		"wiki.list",
		"wiki.index",
		"wiki.stats",
	}
	if len(m) != len(expected) {
		t.Errorf("expected %d handlers, got %d", len(expected), len(m))
	}
	for _, name := range expected {
		if _, ok := m[name]; !ok {
			t.Errorf("missing handler %q", name)
		}
	}
}

// ─── wiki.search ────────────────────────────────────────────────────────────

func TestSearch_missingQuery(t *testing.T) {
	m, _ := methodsWithStore(t)
	resp := callMethod(m, "wiki.search", map[string]any{})
	mustErr(t, resp)
}

func TestSearch_emptyQuery(t *testing.T) {
	m, _ := methodsWithStore(t)
	resp := callMethod(m, "wiki.search", map[string]any{"query": ""})
	mustErr(t, resp)
}

func TestSearch_defaultLimit(t *testing.T) {
	m, store := methodsWithStore(t)
	seedPage(t, store, "기술/golang.md", "Go Language", "기술", "Go is a statically typed language.", []string{"go"})

	resp := callMethod(m, "wiki.search", map[string]any{"query": "Go"})
	mustOK(t, resp)
	result := extractResult(t, resp)
	results, ok := result["results"].([]any)
	if !ok {
		t.Fatalf("expected results array, got %T", result["results"])
	}
	if len(results) == 0 {
		t.Error("expected at least one search result")
	}
}

func TestSearch_customLimit(t *testing.T) {
	m, store := methodsWithStore(t)
	seedPage(t, store, "기술/a.md", "Alpha", "기술", "alpha content", nil)
	seedPage(t, store, "기술/b.md", "Beta", "기술", "beta content", nil)

	resp := callMethod(m, "wiki.search", map[string]any{"query": "content", "limit": 1})
	mustOK(t, resp)
	result := extractResult(t, resp)
	results := result["results"].([]any)
	if len(results) > 1 {
		t.Errorf("expected at most 1 result, got %d", len(results))
	}
}

func TestSearch_noResults(t *testing.T) {
	m, _ := methodsWithStore(t)
	resp := callMethod(m, "wiki.search", map[string]any{"query": "nonexistent_xyz"})
	mustOK(t, resp)
	result := extractResult(t, resp)
	// Results may be nil (no matches) or an empty slice.
	if results, ok := result["results"].([]any); ok && len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestSearch_zeroLimitDefaultsTo10(t *testing.T) {
	m, store := methodsWithStore(t)
	seedPage(t, store, "기술/test.md", "Test", "기술", "searchable body", nil)

	// limit=0 should default to 10 (not error).
	resp := callMethod(m, "wiki.search", map[string]any{"query": "searchable", "limit": 0})
	mustOK(t, resp)
}

func TestSearch_negativeLimitDefaultsTo10(t *testing.T) {
	m, store := methodsWithStore(t)
	seedPage(t, store, "기술/test.md", "Test", "기술", "searchable body", nil)

	resp := callMethod(m, "wiki.search", map[string]any{"query": "searchable", "limit": -5})
	mustOK(t, resp)
}

// ─── wiki.read ──────────────────────────────────────────────────────────────

func TestRead_missingPath(t *testing.T) {
	m, _ := methodsWithStore(t)
	resp := callMethod(m, "wiki.read", map[string]any{})
	mustErr(t, resp)
}

func TestRead_emptyPath(t *testing.T) {
	m, _ := methodsWithStore(t)
	resp := callMethod(m, "wiki.read", map[string]any{"path": ""})
	mustErr(t, resp)
}

func TestRead_pageNotFound(t *testing.T) {
	m, _ := methodsWithStore(t)
	resp := callMethod(m, "wiki.read", map[string]any{"path": "기술/no-such-page.md"})
	mustErr(t, resp)
}

func TestRead_existingPage(t *testing.T) {
	m, store := methodsWithStore(t)
	seedPage(t, store, "기술/dgx-spark.md", "DGX Spark", "기술", "NVIDIA DGX Spark overview.", []string{"gpu"})

	resp := callMethod(m, "wiki.read", map[string]any{"path": "기술/dgx-spark.md"})
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["path"] != "기술/dgx-spark.md" {
		t.Errorf("path mismatch: %v", result["path"])
	}
	if result["body"] != "NVIDIA DGX Spark overview." {
		t.Errorf("body mismatch: %v", result["body"])
	}
	meta, ok := result["meta"].(map[string]any)
	if !ok {
		t.Fatalf("expected meta map, got %T", result["meta"])
	}
	if meta["Title"] != "DGX Spark" {
		t.Errorf("title mismatch: %v", meta["Title"])
	}
}

func TestRead_withSection(t *testing.T) {
	m, store := methodsWithStore(t)
	body := "## Overview\nHigh-level view.\n\n## Details\nDetailed info here."
	seedPage(t, store, "기술/test.md", "Test Page", "기술", body, nil)

	resp := callMethod(m, "wiki.read", map[string]any{"path": "기술/test.md", "section": "Details"})
	mustOK(t, resp)
	result := extractResult(t, resp)

	section, ok := result["section"].(string)
	if !ok {
		t.Fatalf("expected section string, got %T", result["section"])
	}
	if section != "Detailed info here." {
		t.Errorf("section mismatch: %q", section)
	}
}

func TestRead_withMissingSection(t *testing.T) {
	m, store := methodsWithStore(t)
	seedPage(t, store, "기술/test.md", "Test Page", "기술", "no sections here", nil)

	resp := callMethod(m, "wiki.read", map[string]any{"path": "기술/test.md", "section": "Nonexistent"})
	mustOK(t, resp)
	result := extractResult(t, resp)

	// Section should be empty string when not found.
	if result["section"] != "" {
		t.Errorf("expected empty section, got %q", result["section"])
	}
}

// ─── wiki.write ─────────────────────────────────────────────────────────────

func TestWrite_missingPath(t *testing.T) {
	m, _ := methodsWithStore(t)
	resp := callMethod(m, "wiki.write", map[string]any{"title": "Test"})
	mustErr(t, resp)
}

func TestWrite_missingTitle(t *testing.T) {
	m, _ := methodsWithStore(t)
	resp := callMethod(m, "wiki.write", map[string]any{"path": "기술/test.md"})
	mustErr(t, resp)
}

func TestWrite_missingBoth(t *testing.T) {
	m, _ := methodsWithStore(t)
	resp := callMethod(m, "wiki.write", map[string]any{})
	mustErr(t, resp)
}

func TestWrite_success(t *testing.T) {
	m, store := methodsWithStore(t)
	resp := callMethod(m, "wiki.write", map[string]any{
		"path":  "기술/new-page.md",
		"title": "New Page",
		"body":  "Some content.",
		"tags":  []string{"test"},
	})
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["ok"] != true {
		t.Errorf("expected ok=true: %v", result)
	}
	if result["path"] != "기술/new-page.md" {
		t.Errorf("path mismatch: %v", result["path"])
	}

	// Verify the page was actually written.
	page, err := store.ReadPage("기술/new-page.md")
	if err != nil {
		t.Fatalf("ReadPage after write: %v", err)
	}
	if page.Meta.Title != "New Page" {
		t.Errorf("title mismatch: %q", page.Meta.Title)
	}
	if page.Body != "Some content." {
		t.Errorf("body mismatch: %q", page.Body)
	}
}

func TestWrite_withCategoryAndImportance(t *testing.T) {
	m, store := methodsWithStore(t)
	resp := callMethod(m, "wiki.write", map[string]any{
		"path":       "사람/user.md",
		"title":      "User Profile",
		"category":   "사람",
		"body":       "User details.",
		"tags":       []string{"profile"},
		"importance": 0.85,
	})
	mustOK(t, resp)

	page, err := store.ReadPage("사람/user.md")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if page.Meta.Category != "사람" {
		t.Errorf("category mismatch: %q", page.Meta.Category)
	}
	if page.Meta.Importance != 0.85 {
		t.Errorf("importance mismatch: %v", page.Meta.Importance)
	}
}

func TestWrite_overwriteExisting(t *testing.T) {
	m, store := methodsWithStore(t)

	// Write initial page.
	seedPage(t, store, "기술/overwrite.md", "Original", "기술", "original body", nil)

	// Overwrite via RPC.
	resp := callMethod(m, "wiki.write", map[string]any{
		"path":  "기술/overwrite.md",
		"title": "Updated",
		"body":  "updated body",
	})
	mustOK(t, resp)

	page, err := store.ReadPage("기술/overwrite.md")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if page.Meta.Title != "Updated" {
		t.Errorf("title should be updated: %q", page.Meta.Title)
	}
	if page.Body != "updated body" {
		t.Errorf("body should be updated: %q", page.Body)
	}
}

// ─── wiki.delete ────────────────────────────────────────────────────────────

func TestDelete_missingPath(t *testing.T) {
	m, _ := methodsWithStore(t)
	resp := callMethod(m, "wiki.delete", map[string]any{})
	mustErr(t, resp)
}

func TestDelete_emptyPath(t *testing.T) {
	m, _ := methodsWithStore(t)
	resp := callMethod(m, "wiki.delete", map[string]any{"path": ""})
	mustErr(t, resp)
}

func TestDelete_existingPage(t *testing.T) {
	m, store := methodsWithStore(t)
	seedPage(t, store, "기술/to-delete.md", "Deletable", "기술", "to be deleted", nil)

	resp := callMethod(m, "wiki.delete", map[string]any{"path": "기술/to-delete.md"})
	mustOK(t, resp)
	result := extractResult(t, resp)
	if result["ok"] != true {
		t.Errorf("expected ok=true: %v", result)
	}
	if result["path"] != "기술/to-delete.md" {
		t.Errorf("path mismatch: %v", result["path"])
	}

	// Verify the page is gone.
	_, err := store.ReadPage("기술/to-delete.md")
	if err == nil {
		t.Error("page should not exist after deletion")
	}
}

func TestDelete_nonexistentPage(t *testing.T) {
	m, _ := methodsWithStore(t)
	// Deleting a non-existent file succeeds silently because os.Remove only
	// returns an error when the file exists but cannot be removed. The store's
	// DeletePage guards os.IsNotExist, so missing files are a no-op.
	resp := callMethod(m, "wiki.delete", map[string]any{"path": "기술/ghost.md"})
	mustOK(t, resp)
	result := extractResult(t, resp)
	if result["ok"] != true {
		t.Errorf("expected ok=true: %v", result)
	}
}

// ─── wiki.list ──────────────────────────────────────────────────────────────

func TestList_emptyStore(t *testing.T) {
	m, _ := methodsWithStore(t)
	resp := callMethod(m, "wiki.list", map[string]any{})
	mustOK(t, resp)
	result := extractResult(t, resp)

	pages, ok := result["pages"].([]any)
	if !ok {
		// nil pages is valid when the store is empty.
		if result["pages"] != nil {
			t.Fatalf("expected pages array or nil, got %T", result["pages"])
		}
		pages = nil
	}
	count := result["count"].(float64)
	if int(count) != len(pages) {
		t.Errorf("count mismatch: count=%v, pages=%d", count, len(pages))
	}
}

func TestList_allPages(t *testing.T) {
	m, store := methodsWithStore(t)
	seedPage(t, store, "기술/a.md", "A", "기술", "body a", nil)
	seedPage(t, store, "사람/b.md", "B", "사람", "body b", nil)

	resp := callMethod(m, "wiki.list", map[string]any{})
	mustOK(t, resp)
	result := extractResult(t, resp)

	count := result["count"].(float64)
	if int(count) < 2 {
		t.Errorf("expected at least 2 pages, got %v", count)
	}
}

func TestList_filteredByCategory(t *testing.T) {
	m, store := methodsWithStore(t)
	seedPage(t, store, "기술/x.md", "X", "기술", "tech page", nil)
	seedPage(t, store, "사람/y.md", "Y", "사람", "people page", nil)

	resp := callMethod(m, "wiki.list", map[string]any{"category": "기술"})
	mustOK(t, resp)
	result := extractResult(t, resp)

	pages := result["pages"].([]any)
	count := result["count"].(float64)
	if int(count) != len(pages) {
		t.Errorf("count/pages mismatch: count=%v, len=%d", count, len(pages))
	}

	// All returned pages should be in the 기술 category.
	for _, p := range pages {
		path := p.(string)
		if len(path) < 3 || path[:len("기술")] != "기술" {
			t.Errorf("page %q not in 기술 category", path)
		}
	}
}

func TestList_nonexistentCategory(t *testing.T) {
	m, _ := methodsWithStore(t)
	// Listing a non-existent category returns empty results (filepath.Walk
	// skips errors on the missing directory and returns nil pages).
	resp := callMethod(m, "wiki.list", map[string]any{"category": "nonexistent_category"})
	mustOK(t, resp)
	result := extractResult(t, resp)
	count := result["count"].(float64)
	if count != 0 {
		t.Errorf("expected 0 pages for nonexistent category, got %v", count)
	}
}

func TestList_nilParams(t *testing.T) {
	m, _ := methodsWithStore(t)
	// BindHandler requires a JSON body; nil params yields INVALID_REQUEST.
	resp := callMethod(m, "wiki.list", nil)
	mustErr(t, resp)
}

// ─── wiki.index ─────────────────────────────────────────────────────────────

func TestIndex_emptyStore(t *testing.T) {
	m, _ := methodsWithStore(t)
	resp := callMethod(m, "wiki.index", map[string]any{})
	mustOK(t, resp)
	result := extractResult(t, resp)

	totalPages := result["totalPages"].(float64)
	if totalPages != 0 {
		t.Errorf("expected 0 totalPages in empty store, got %v", totalPages)
	}
}

func TestIndex_withPages(t *testing.T) {
	m, store := methodsWithStore(t)
	seedPage(t, store, "기술/go.md", "Go", "기술", "Go lang", []string{"language"})
	seedPage(t, store, "사람/alice.md", "Alice", "사람", "Alice info", nil)

	resp := callMethod(m, "wiki.index", map[string]any{})
	mustOK(t, resp)
	result := extractResult(t, resp)

	totalPages := result["totalPages"].(float64)
	if int(totalPages) < 2 {
		t.Errorf("expected at least 2 totalPages, got %v", totalPages)
	}

	entries, ok := result["entries"].(map[string]any)
	if !ok {
		t.Fatalf("expected entries map, got %T", result["entries"])
	}
	if _, ok := entries["기술/go.md"]; !ok {
		t.Error("missing entry for 기술/go.md")
	}
	if _, ok := entries["사람/alice.md"]; !ok {
		t.Error("missing entry for 사람/alice.md")
	}
}

func TestIndex_filterByCategory(t *testing.T) {
	m, store := methodsWithStore(t)
	seedPage(t, store, "기술/go.md", "Go", "기술", "Go lang", nil)
	seedPage(t, store, "사람/bob.md", "Bob", "사람", "Bob info", nil)

	resp := callMethod(m, "wiki.index", map[string]any{"category": "기술"})
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["category"] != "기술" {
		t.Errorf("expected category=기술: %v", result["category"])
	}

	entries := result["entries"].(map[string]any)
	totalPages := result["totalPages"].(float64)
	if int(totalPages) != len(entries) {
		t.Errorf("totalPages/entries mismatch: %v vs %d", totalPages, len(entries))
	}

	// Should only contain 기술 category pages.
	for path := range entries {
		entry := entries[path].(map[string]any)
		if entry["Category"] != "기술" {
			t.Errorf("entry %q has wrong category: %v", path, entry["Category"])
		}
	}
}

func TestIndex_filterEmptyCategory(t *testing.T) {
	m, store := methodsWithStore(t)
	seedPage(t, store, "기술/go.md", "Go", "기술", "Go lang", nil)

	// Filter by category with no matching pages.
	resp := callMethod(m, "wiki.index", map[string]any{"category": "사람"})
	mustOK(t, resp)
	result := extractResult(t, resp)

	totalPages := result["totalPages"].(float64)
	if totalPages != 0 {
		t.Errorf("expected 0 pages for empty category, got %v", totalPages)
	}
}

func TestIndex_nilParams(t *testing.T) {
	m, _ := methodsWithStore(t)
	// BindHandler requires a JSON body; nil params yields INVALID_REQUEST.
	resp := callMethod(m, "wiki.index", nil)
	mustErr(t, resp)
}

// ─── wiki.stats ─────────────────────────────────────────────────────────────

func TestStats_emptyStore(t *testing.T) {
	m, _ := methodsWithStore(t)
	resp := callMethod(m, "wiki.stats", map[string]any{})
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["TotalPages"].(float64) != 0 {
		t.Errorf("expected 0 total pages: %v", result)
	}
}

func TestStats_withPages(t *testing.T) {
	m, store := methodsWithStore(t)
	seedPage(t, store, "기술/a.md", "A", "기술", "content a", nil)
	seedPage(t, store, "기술/b.md", "B", "기술", "content b", nil)
	seedPage(t, store, "사람/c.md", "C", "사람", "content c", nil)

	resp := callMethod(m, "wiki.stats", map[string]any{})
	mustOK(t, resp)
	result := extractResult(t, resp)

	totalPages := result["TotalPages"].(float64)
	if int(totalPages) < 3 {
		t.Errorf("expected at least 3 total pages, got %v", totalPages)
	}

	totalBytes := result["TotalBytes"].(float64)
	if totalBytes <= 0 {
		t.Errorf("expected positive total bytes, got %v", totalBytes)
	}

	catCount, ok := result["CategoryCount"].(map[string]any)
	if !ok {
		t.Fatalf("expected CategoryCount map, got %T", result["CategoryCount"])
	}
	if catCount["기술"] == nil {
		t.Error("missing 기술 category count")
	}
}

func TestStats_nilParams(t *testing.T) {
	m, _ := methodsWithStore(t)
	// BindHandler requires a JSON body; nil params yields INVALID_REQUEST.
	resp := callMethod(m, "wiki.stats", nil)
	mustErr(t, resp)
}

func TestStats_withEmptyParams(t *testing.T) {
	m, _ := methodsWithStore(t)
	resp := callMethod(m, "wiki.stats", map[string]any{})
	mustOK(t, resp)
}
