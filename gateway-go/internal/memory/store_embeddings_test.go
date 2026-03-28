package memory

import (
	"context"
	"testing"
)

func TestLoadEmbeddings_CacheSnapshotAndCopyOnWrite(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	id1, err := s.InsertFact(ctx, Fact{Content: "fact 1", Category: CategoryContext, Importance: 0.8})
	if err != nil {
		t.Fatalf("InsertFact #1: %v", err)
	}
	if err := s.StoreEmbedding(ctx, id1, []float32{1, 2, 3}, "test-model"); err != nil {
		t.Fatalf("StoreEmbedding #1: %v", err)
	}

	snapshot, err := s.LoadEmbeddings(ctx)
	if err != nil {
		t.Fatalf("LoadEmbeddings initial: %v", err)
	}
	if got := len(snapshot); got != 1 {
		t.Fatalf("expected 1 embedding in snapshot, got %d", got)
	}

	id2, err := s.InsertFact(ctx, Fact{Content: "fact 2", Category: CategoryContext, Importance: 0.8})
	if err != nil {
		t.Fatalf("InsertFact #2: %v", err)
	}
	if err := s.StoreEmbedding(ctx, id2, []float32{4, 5, 6}, "test-model"); err != nil {
		t.Fatalf("StoreEmbedding #2: %v", err)
	}

	if _, exists := snapshot[id2]; exists {
		t.Fatalf("old snapshot should not be mutated by copy-on-write update")
	}

	fresh, err := s.LoadEmbeddings(ctx)
	if err != nil {
		t.Fatalf("LoadEmbeddings after update: %v", err)
	}
	if got := len(fresh); got != 2 {
		t.Fatalf("expected 2 embeddings after update, got %d", got)
	}
}

func TestLoadEmbeddingsForMerge_FiltersActiveAndDepth(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	idLow, _ := s.InsertFact(ctx, Fact{Content: "low depth", Category: CategoryContext, Importance: 0.8})
	idHigh, _ := s.InsertFact(ctx, Fact{Content: "high depth", Category: CategoryContext, Importance: 0.8})
	idInactive, _ := s.InsertFact(ctx, Fact{Content: "inactive", Category: CategoryContext, Importance: 0.8})

	if err := s.StoreEmbedding(ctx, idLow, []float32{0.1, 0.2}, "m"); err != nil {
		t.Fatalf("StoreEmbedding low: %v", err)
	}
	if err := s.StoreEmbedding(ctx, idHigh, []float32{0.3, 0.4}, "m"); err != nil {
		t.Fatalf("StoreEmbedding high: %v", err)
	}
	if err := s.StoreEmbedding(ctx, idInactive, []float32{0.5, 0.6}, "m"); err != nil {
		t.Fatalf("StoreEmbedding inactive: %v", err)
	}

	if _, err := s.db.ExecContext(ctx, `UPDATE facts SET merge_depth = 3 WHERE id = ?`, idHigh); err != nil {
		t.Fatalf("set merge_depth: %v", err)
	}
	if err := s.DeactivateFact(ctx, idInactive); err != nil {
		t.Fatalf("DeactivateFact: %v", err)
	}

	embeddings, depths, err := s.LoadEmbeddingsForMerge(ctx, 3)
	if err != nil {
		t.Fatalf("LoadEmbeddingsForMerge: %v", err)
	}

	if got := len(embeddings); got != 1 {
		t.Fatalf("expected only 1 eligible embedding, got %d", got)
	}
	if _, ok := embeddings[idLow]; !ok {
		t.Fatalf("expected low-depth active fact to be present")
	}
	if _, ok := embeddings[idHigh]; ok {
		t.Fatalf("high-depth fact should be excluded")
	}
	if _, ok := embeddings[idInactive]; ok {
		t.Fatalf("inactive fact should be excluded")
	}
	if depth := depths[idLow]; depth != 0 {
		t.Fatalf("expected depth 0 for low-depth fact, got %d", depth)
	}
}
