package embedindex

import (
	"context"
	"testing"
)

// fakeEmbedder returns a deterministic 64-dim rune-histogram vector: identical
// text → identical vector (cosine 1.0), distinct text → lower. 64 dims give
// enough spread that different Korean strings aren't near-parallel.
type fakeEmbedder struct{ healthy bool }

func (f fakeEmbedder) IsHealthy() bool { return f.healthy }
func (f fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, 64)
		for _, r := range t {
			v[int(r)%64]++
		}
		out[i] = v
	}
	return out, nil
}

func TestIndexRefreshLoadsItemsAndSearchRanksExactMatch(t *testing.T) {
	ix := New("test", fakeEmbedder{healthy: true}, "")
	defer ix.Close()
	items := []Item{
		{ID: "a", Hash: ContentHash("금호타이어 곡성 납기 지연 검토"), Text: "금호타이어 곡성 납기 지연 검토"},
		{ID: "b", Hash: ContentHash("당진 해저케이블 포설 공정"), Text: "당진 해저케이블 포설 공정"},
	}
	if err := ix.Warm(context.Background(), func() []Item { return items }); err != nil {
		t.Fatalf("Warm: %v", err)
	}

	hits := ix.Search(context.Background(), "금호타이어 곡성 납기 지연 검토", 5)
	if len(hits) == 0 {
		t.Fatal("expected hits")
	}
	if hits[0].ID != "a" {
		t.Errorf("top hit = %q, want a (exact-text match)", hits[0].ID)
	}
	// Batch: index-aligned, degraded (nil) slot for a too-short query.
	batch := ix.SearchBatch(context.Background(), []string{"금호타이어 곡성 납기 지연 검토", "x"}, 5)
	if len(batch) != 2 {
		t.Fatalf("batch len %d", len(batch))
	}
	if len(batch[0]) == 0 || batch[1] != nil {
		t.Errorf("batch shape wrong: [0]=%v [1]=%v", batch[0], batch[1])
	}
}

func TestDisabledEmbedderReturnsNilSearchResults(t *testing.T) {
	ix := New("test", fakeEmbedder{healthy: false}, "")
	defer ix.Close()
	if ix.Enabled() {
		t.Error("should be disabled when embedder unhealthy")
	}
	if hits := ix.Search(context.Background(), "anything long enough", 5); hits != nil {
		t.Errorf("expected nil hits when disabled, got %v", hits)
	}
}

// TestReembedEvictsVectorWhenItemRemoved: dropping an item removes its vector.
// Guards the content-hash cache key + vanished-entry cleanup.
func TestReembedEvictsVectorWhenItemRemoved(t *testing.T) {
	ix := New("test", fakeEmbedder{healthy: true}, "")
	defer ix.Close()
	v1 := []Item{{ID: "a", Hash: ContentHash("first"), Text: "first version text"}}
	if err := ix.Warm(context.Background(), func() []Item { return v1 }); err != nil {
		t.Fatalf("initial Warm: %v", err)
	}
	if got := ix.Search(context.Background(), "first version text", 3); len(got) == 0 {
		t.Fatal("expected a hit after first embed")
	}
	if err := ix.Warm(context.Background(), func() []Item { return nil }); err != nil {
		t.Fatalf("drop Warm: %v", err)
	}
	if got := ix.Search(context.Background(), "first version text", 3); len(got) != 0 {
		t.Errorf("expected no hits after item dropped, got %d", len(got))
	}
}
