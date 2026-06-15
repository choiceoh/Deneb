package wiki

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeEmbedder maps text to a 2-dim vector by concept markers, so a query can
// match a page that shares the concept but none of the query's keywords.
// dim0 = "risk" cluster {위험, 차질, 우려}; dim1 = mentions "gpu".
type fakeEmbedder struct{ healthy bool }

func (f fakeEmbedder) IsHealthy() bool { return f.healthy }

func (f fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		lt := strings.ToLower(t)
		var risk, gpu float32
		if strings.Contains(t, "위험") || strings.Contains(t, "차질") || strings.Contains(t, "우려") {
			risk = 1
		}
		if strings.Contains(lt, "gpu") {
			gpu = 1
		}
		out[i] = []float32{risk, gpu}
	}
	return out, nil
}

func TestSearchHybrid(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Keyword page: contains the query terms verbatim.
	mustWrite(t, store, "프로젝트/risk.md", &Page{
		Meta: Frontmatter{ID: "risk", Title: "위험 평가 보고서", Category: "프로젝트", Summary: "분기 리스크 점검"},
		Body: "정기 점검.",
	})
	// Semantic page: about risk (차질/우려) but shares NO query keyword.
	mustWrite(t, store, "프로젝트/supply.md", &Page{
		Meta: Frontmatter{ID: "supply", Title: "공급 현황", Category: "프로젝트", Summary: "납품 일정"},
		Body: "원자재 차질 우려 있음.",
	})
	// Unrelated page: different concept entirely.
	mustWrite(t, store, "운영시스템/gpu.md", &Page{
		Meta: Frontmatter{ID: "gpu", Title: "GPU 서버", Category: "운영시스템", Summary: "추론 성능"},
		Body: "GPU 추론 운영.",
	})

	ctx := context.Background()
	const query = "위험 평가 보고"

	contains := func(rs []SearchResult, path string) bool {
		for _, r := range rs {
			if r.Path == path {
				return true
			}
		}
		return false
	}

	// BM25-only (no embedder): keyword page found, semantic page missed.
	bm25, err := store.Search(ctx, query, 10)
	if err != nil {
		t.Fatalf("Search (bm25): %v", err)
	}
	if !contains(bm25, "프로젝트/risk.md") {
		t.Errorf("bm25 should find the keyword page; got %+v", bm25)
	}
	if contains(bm25, "프로젝트/supply.md") {
		t.Errorf("bm25 should NOT find the no-keyword semantic page; got %+v", bm25)
	}

	// Hybrid (healthy embedder): now the semantic page surfaces too, and the
	// unrelated GPU page stays out (cosine 0, no keyword).
	store.SetEmbedder(fakeEmbedder{healthy: true})
	store.sem.syncRefresh = true // deterministic: embed pages inline, not in a goroutine
	hybrid, err := store.Search(ctx, query, 10)
	if err != nil {
		t.Fatalf("Search (hybrid): %v", err)
	}
	if !contains(hybrid, "프로젝트/risk.md") {
		t.Errorf("hybrid should still find the keyword page; got %+v", hybrid)
	}
	if !contains(hybrid, "프로젝트/supply.md") {
		t.Errorf("hybrid should surface the semantic page; got %+v", hybrid)
	}
	if contains(hybrid, "운영시스템/gpu.md") {
		t.Errorf("hybrid should not surface the unrelated page; got %+v", hybrid)
	}

	// Unhealthy embedder degrades to BM25 (semantic page missed again).
	store.SetEmbedder(fakeEmbedder{healthy: false})
	degraded, err := store.Search(ctx, query, 10)
	if err != nil {
		t.Fatalf("Search (degraded): %v", err)
	}
	if contains(degraded, "프로젝트/supply.md") {
		t.Errorf("unhealthy embedder should fall back to BM25; got %+v", degraded)
	}
}

func TestSuggestRelatedAndEnrich(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	store.SetEmbedder(fakeEmbedder{healthy: true})
	store.sem.syncRefresh = true // deterministic: embed pages inline, not in a goroutine

	// Two risk-concept pages (no related links), one unrelated GPU page.
	mustWrite(t, store, "프로젝트/risk1.md", &Page{
		Meta: Frontmatter{ID: "risk1", Title: "위험 평가", Category: "프로젝트", Summary: "분기 점검"},
		Body: "정기 위험 점검.",
	})
	mustWrite(t, store, "프로젝트/risk2.md", &Page{
		Meta: Frontmatter{ID: "risk2", Title: "공급 현황", Category: "프로젝트", Summary: "납품"},
		Body: "원자재 차질 우려.",
	})
	mustWrite(t, store, "운영시스템/gpu.md", &Page{
		Meta: Frontmatter{ID: "gpu", Title: "GPU 서버", Category: "운영시스템", Summary: "추론"},
		Body: "GPU 운영.",
	})

	ctx := context.Background()
	sugg := store.SuggestRelated(ctx, "프로젝트/risk1.md", 3)
	if len(sugg) != 1 || sugg[0] != "프로젝트/risk2.md" {
		t.Fatalf("SuggestRelated = %v, want [프로젝트/risk2.md]", sugg)
	}

	// Dreamer enrichment wires the suggestion onto the zero-related page.
	wd := NewWikiDreamer(store, nil, "", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if added := wd.enrichRelatedLinks(ctx); added == 0 {
		t.Fatal("enrichRelatedLinks added no links")
	}
	page, err := store.ReadPage("프로젝트/risk1.md")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if len(page.Meta.Related) == 0 {
		t.Errorf("risk1 should have gained a related link, got none")
	}
}

// TestRefreshAsync_BackgroundAndSingleFlight exercises the real async path
// (syncRefresh off): a request triggers a background re-embed that populates
// vectors without blocking the caller, and a second trigger over unchanged
// pages re-embeds nothing (single-flight + content-hash skip).
func TestRefreshAsync_BackgroundAndSingleFlight(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mustWrite(t, store, "프로젝트/supply.md", &Page{
		Meta: Frontmatter{ID: "supply", Title: "공급 현황", Category: "프로젝트", Summary: "납품 일정"},
		Body: "원자재 차질 우려 있음.",
	})
	emb := &countingEmbedder{fakeEmbedder: fakeEmbedder{healthy: true}}
	store.SetEmbedder(emb) // syncRefresh stays false → real background path

	store.sem.refreshAsync(store)
	waitRefresh(t, store.sem)

	store.sem.mu.Lock()
	n := len(store.sem.vecs)
	store.sem.mu.Unlock()
	if n == 0 {
		t.Fatal("background refresh embedded no vectors")
	}
	embedded := emb.calls

	// Re-trigger: nothing changed, so no further embedding happens.
	store.sem.refreshAsync(store)
	waitRefresh(t, store.sem)
	if emb.calls != embedded {
		t.Errorf("re-embedded unchanged pages: %d → %d Embed calls", embedded, emb.calls)
	}
}

// waitRefresh polls the single-flight flag until the background refresh started
// by refreshAsync finishes (it is set synchronously before the goroutine spawns).
func waitRefresh(t *testing.T, si *semanticIndex) {
	t.Helper()
	for range 400 {
		if !si.refreshing.Load() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("background refresh did not complete in time")
}

// countingEmbedder wraps fakeEmbedder and counts Embed calls so tests can
// assert the persisted cache prevents re-embedding after a restart.
type countingEmbedder struct {
	fakeEmbedder
	calls int
}

func (c *countingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	c.calls++
	return c.fakeEmbedder.Embed(ctx, texts)
}

// TestSemanticCache_PersistsAcrossRestart guards the embedding cache: the
// gateway restarts every few minutes in production, and an in-memory-only
// index re-embedded the entire wiki on the first semantic query of every
// boot. A second Store over the same dir must hydrate vectors from disk and
// only embed the query itself.
func TestSemanticCache_PersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	wikiDir := filepath.Join(dir, "wiki")
	store, err := NewStore(wikiDir, filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mustWrite(t, store, "프로젝트/supply.md", &Page{
		Meta: Frontmatter{ID: "supply", Title: "공급 현황", Category: "프로젝트", Summary: "납품 일정"},
		Body: "원자재 차질 우려 있음.",
	})

	emb1 := &countingEmbedder{fakeEmbedder: fakeEmbedder{healthy: true}}
	store.SetEmbedder(emb1)
	store.sem.syncRefresh = true // deterministic: count page embeds inline
	if got := store.searchSemantic(context.Background(), "납기 지연 위험 우려", 5); len(got) == 0 {
		t.Fatalf("semantic search returned no hits on first run")
	}
	if emb1.calls < 2 { // at least one page batch + the query
		t.Fatalf("expected page embedding on first run, got %d calls", emb1.calls)
	}

	// "Restart": fresh Store + fresh embedder over the same wiki dir.
	store2, err := NewStore(wikiDir, filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore (restart): %v", err)
	}
	emb2 := &countingEmbedder{fakeEmbedder: fakeEmbedder{healthy: true}}
	store2.SetEmbedder(emb2)
	store2.sem.syncRefresh = true // deterministic: cache hydration should skip page embeds
	if got := store2.searchSemantic(context.Background(), "납기 지연 위험 우려", 5); len(got) == 0 {
		t.Fatalf("semantic search returned no hits after restart")
	}
	if emb2.calls != 1 { // only the query — page vectors came from the cache
		t.Errorf("expected cache hydration to skip page embedding, got %d Embed calls", emb2.calls)
	}
}
