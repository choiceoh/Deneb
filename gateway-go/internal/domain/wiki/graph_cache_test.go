package wiki

import (
	"context"
	"path/filepath"
	"testing"
)

func TestLoadGraphCorpusCachesUntilWrite(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	page := NewPage("Alpha", "프로젝트", nil)
	page.Body = "alpha body"
	if err := store.WritePage("프로젝트/alpha.md", page); err != nil {
		t.Fatalf("write: %v", err)
	}

	ctx := context.Background()
	first, err := store.loadGraphCorpus(ctx)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if len(first.recs) == 0 {
		t.Fatal("expected at least one graph record")
	}
	store.graphMu.RLock()
	cachedPtr := store.graphCache
	store.graphMu.RUnlock()
	if cachedPtr == nil {
		t.Fatal("expected graphCache to be populated")
	}

	second, err := store.loadGraphCorpus(ctx)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if len(second.recs) != len(first.recs) {
		t.Fatalf("cached corpus size = %d, want %d", len(second.recs), len(first.recs))
	}
	store.graphMu.RLock()
	samePtr := store.graphCache == cachedPtr
	store.graphMu.RUnlock()
	if !samePtr {
		t.Fatal("warm load should reuse the same graphCache pointer")
	}

	page2 := NewPage("Beta", "프로젝트", nil)
	page2.Body = "beta body"
	if err := store.WritePage("프로젝트/beta.md", page2); err != nil {
		t.Fatalf("write beta: %v", err)
	}
	store.graphMu.RLock()
	cleared := store.graphCache == nil
	store.graphMu.RUnlock()
	if !cleared {
		t.Fatal("write should invalidate graphCache")
	}

	third, err := store.loadGraphCorpus(ctx)
	if err != nil {
		t.Fatalf("third load: %v", err)
	}
	if len(third.recs) < len(first.recs)+1 {
		t.Fatalf("after write corpus size = %d, want >= %d", len(third.recs), len(first.recs)+1)
	}
}

func corpusHasPath(c graphCorpus, relPath string) bool {
	for _, r := range c.recs {
		if r.relPath == relPath {
			return true
		}
	}
	return false
}

// TestLoadGraphCorpus_DiscardStaleBuildAfterInvalidation guards the race where a
// corpus build started before wiki_forget/delete finishes after the mutation and
// reinstalls a snapshot that still contains the removed page — graph boost would
// then surface a forgotten fact in recall.
func TestLoadGraphCorpus_DiscardStaleBuildAfterInvalidation(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	const forgetPath = "사람/홍길동.md"
	for _, spec := range []struct {
		path, title, body string
	}{
		{"프로젝트/alpha.md", "Alpha", "alpha body"},
		{forgetPath, "홍길동", "secret fact"},
	} {
		page := NewPage(spec.title, "프로젝트", nil)
		page.Body = spec.body
		if err := store.WritePage(spec.path, page); err != nil {
			t.Fatalf("write %s: %v", spec.path, err)
		}
	}

	ctx := context.Background()
	warm, err := store.loadGraphCorpus(ctx)
	if err != nil {
		t.Fatalf("warm load: %v", err)
	}
	if !corpusHasPath(warm, forgetPath) {
		t.Fatal("warm corpus missing forget target")
	}

	store.graphMu.RLock()
	buildGen := store.graphGen
	store.graphMu.RUnlock()

	if _, err := store.Forget(forgetPath, "privacy test"); err != nil {
		t.Fatalf("Forget: %v", err)
	}

	// Simulate the pre-fix publish path: a build that captured buildGen before
	// Forget must not reinstall its snapshot once graphGen has advanced.
	stale := warm
	store.graphMu.Lock()
	if store.graphGen == buildGen {
		store.graphCache = &stale
	}
	store.graphMu.Unlock()

	got, err := store.loadGraphCorpus(ctx)
	if err != nil {
		t.Fatalf("post-forget load: %v", err)
	}
	if corpusHasPath(got, forgetPath) {
		t.Fatalf("forgotten page %q still in graph corpus", forgetPath)
	}

	store.graphMu.RLock()
	cached := store.graphCache
	store.graphMu.RUnlock()
	if cached != nil && corpusHasPath(*cached, forgetPath) {
		t.Fatalf("graphCache still holds forgotten page %q", forgetPath)
	}
}

func TestWikiSearchSkipRerankOption(t *testing.T) {
	// Behavioral smoke: SearchWithOptions with SkipRerank must not attempt
	// model rerank (API still available for lean callers; agent knowledge
	// adapter uses the default gated path instead).
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	page := NewPage("Contract", "프로젝트", []string{"계약"})
	page.Body = "계약 일정과 납기"
	if err := store.WritePage("프로젝트/contract.md", page); err != nil {
		t.Fatal(err)
	}
	report, err := store.SearchWithOptions(context.Background(), "계약", 5, QueryOptions{SkipRerank: true})
	if err != nil {
		t.Fatalf("SearchWithOptions: %v", err)
	}
	if report.Diagnostics.Rerank.Attempted {
		t.Fatalf("SkipRerank should not attempt model rerank: %+v", report.Diagnostics.Rerank)
	}
}
