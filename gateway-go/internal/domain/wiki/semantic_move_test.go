package wiki

import (
	"context"
	"path/filepath"
	"testing"
)

// TestSemanticRefresh_ReusesVectorsForMovedPages: relocation arrives as a new
// path with no cache entry, so a path-keyed refresh re-embeds byte-identical
// text and rewrites the whole 180MB cache. Chunk offsets are body-relative, so
// the cached vectors transfer exactly — and relocation comes in batches (deal-
// ledger normalization, orphan-mail refiles, layout repairs).
func TestSemanticRefresh_ReusesVectorsForMovedPages(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	emb := &countingEmbedder{fakeEmbedder: fakeEmbedder{healthy: true, fingerprint: "model-a:2", dimensions: 2}}
	mustWrite(t, store, "기타/거래/고려전선.md", &Page{
		Meta: Frontmatter{ID: "koryo", Title: "고려전선", Category: "기타"},
		Body: "TFR-CV 150SQ 발주 건. gpu 언급 포함.",
	})
	store.SetEmbedder(emb)
	if err := store.WarmSemanticIndex(context.Background()); err != nil {
		t.Fatalf("warm: %v", err)
	}
	warmed := emb.calls
	if warmed == 0 {
		t.Fatal("warm embedded nothing — fixture is wrong")
	}

	if err := store.MovePage("기타/거래/고려전선.md", "프로젝트/거래/고려전선.md"); err != nil {
		t.Fatalf("MovePage: %v", err)
	}
	if err := store.WarmSemanticIndex(context.Background()); err != nil {
		t.Fatalf("warm after move: %v", err)
	}

	if emb.calls != warmed {
		t.Errorf("moved page re-embedded: %d embed calls before, %d after", warmed, emb.calls)
	}
	if status := store.SemanticStatus(); !status.Healthy || status.Pending != 0 {
		t.Errorf("semantic index incomplete after the move: %+v", status)
	}
	if hits := store.searchSemanticWithVec(context.Background(), []float32{0, 1}, 5); len(hits) == 0 {
		t.Error("moved page has no searchable vector")
	}
}
