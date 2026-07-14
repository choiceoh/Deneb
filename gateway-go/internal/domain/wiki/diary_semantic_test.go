package wiki

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestDiarySemanticParaphraseReturnsMatch proves the diary semantic index
// catches a hit that BM25 misses: a diary entry phrased "차질 우려" is found by
// the query "위험" (the fakeEmbedder's risk cluster) even though they share no
// keyword. BM25 over the same query returns nothing, so semantic is the only
// path that surfaces it.
func TestDiarySemanticParaphraseReturnsMatch(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	// Two diary entries: one about a risk (no "위험" keyword), one about GPU.
	if err := store.AppendDiary("금호 곡성 납기에 차질 우려가 있다"); err != nil {
		t.Fatalf("AppendDiary: %v", err)
	}
	if err := store.AppendDiary("GPU 추론 서버를 재기동했다"); err != nil {
		t.Fatalf("AppendDiary: %v", err)
	}

	store.SetEmbedder(fakeEmbedder{healthy: true})
	if err := store.WarmDiarySemantic(context.Background()); err != nil {
		t.Fatalf("WarmDiarySemantic: %v", err)
	}

	// BM25 for "위험 요인 점검" finds nothing (no keyword overlap with the entries).
	const riskQuery = "위험 요인 점검" // risk concept, none of the entries' keywords
	bm25, err := store.SearchDiary(context.Background(), riskQuery, 5)
	if err != nil {
		t.Fatalf("SearchDiary: %v", err)
	}
	if len(bm25) != 0 {
		t.Fatalf("BM25 baseline should miss the paraphrase, got %d hits", len(bm25))
	}

	// Semantic finds the risk entry via the concept, not the keyword.
	batch := store.SearchDiarySemanticBatch(context.Background(), []string{riskQuery}, 5)
	if len(batch) != 1 {
		t.Fatalf("batch len %d, want 1", len(batch))
	}
	if len(batch[0]) == 0 {
		t.Fatal("semantic should surface the risk entry BM25 missed")
	}
	top := batch[0][0]
	if want := "차질 우려"; !strings.Contains(top.Content, want) {
		t.Errorf("top semantic hit = %q, want the risk entry (%q)", top.Content, want)
	}
	if top.Score <= 0 {
		t.Errorf("expected positive cosine, got %v", top.Score)
	}
}

// TestDiarySemanticDegradesWithoutEmbedder: no embedder → empty batch, BM25 only.
func TestDiarySemanticDegradesWithoutEmbedder(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()
	if err := store.AppendDiary("아무 내용이나 충분히 길게 적은 일기 항목"); err != nil {
		t.Fatalf("AppendDiary: %v", err)
	}
	// No SetEmbedder → semantic disabled.
	if batch := store.SearchDiarySemanticBatch(context.Background(), []string{"아무 내용"}, 5); batch != nil {
		t.Errorf("expected nil batch without embedder, got %v", batch)
	}
}
