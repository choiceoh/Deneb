package ffi

import (
	"math"
	"testing"
)

func TestMemoryCosineSimilarity_Identical(t *testing.T) {
	a := []float64{1.0, 2.0, 3.0}
	b := []float64{1.0, 2.0, 3.0}
	sim := MemoryCosineSimilarity(a, b)
	if math.Abs(sim-1.0) > 1e-9 {
		t.Errorf("expected ~1.0, got %f", sim)
	}
}

func TestMemoryCosineSimilarity_Orthogonal(t *testing.T) {
	a := []float64{1.0, 0.0}
	b := []float64{0.0, 1.0}
	sim := MemoryCosineSimilarity(a, b)
	if math.Abs(sim) > 1e-9 {
		t.Errorf("expected ~0.0, got %f", sim)
	}
}

func TestMemoryCosineSimilarity_Opposite(t *testing.T) {
	a := []float64{1.0, 0.0}
	b := []float64{-1.0, 0.0}
	sim := MemoryCosineSimilarity(a, b)
	if math.Abs(sim-(-1.0)) > 1e-9 {
		t.Errorf("expected ~-1.0, got %f", sim)
	}
}

func TestMemoryCosineSimilarity_Empty(t *testing.T) {
	sim := MemoryCosineSimilarity(nil, nil)
	if sim != 0.0 {
		t.Errorf("expected 0.0, got %f", sim)
	}
}

func TestMemoryBm25RankToScore(t *testing.T) {
	tests := []struct {
		rank float64
		want float64
	}{
		{0, 1.0},
		{1, 0.5},
		{9, 0.1},
	}
	for _, tt := range tests {
		score := MemoryBm25RankToScore(tt.rank)
		if math.Abs(score-tt.want) > 1e-9 {
			t.Errorf("Bm25RankToScore(%f) = %f, want %f", tt.rank, score, tt.want)
		}
	}
}

func TestMemoryBuildFtsQuery_Basic(t *testing.T) {
	query, err := MemoryBuildFtsQuery("hello world test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query == "" {
		t.Fatal("expected non-empty query")
	}
	t.Logf("FTS query: %s", query)
}

func TestMemoryBuildFtsQuery_Empty(t *testing.T) {
	query, err := MemoryBuildFtsQuery("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query != "" {
		t.Errorf("expected empty, got %q", query)
	}
}

func TestMemoryExtractKeywords_Basic(t *testing.T) {
	keywords, err := MemoryExtractKeywords("machine learning algorithms for natural language processing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keywords) == 0 {
		t.Fatal("expected at least one keyword")
	}
	t.Logf("Keywords: %v", keywords)
}

func TestMemoryExtractKeywords_Empty(t *testing.T) {
	keywords, err := MemoryExtractKeywords("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keywords) != 0 {
		t.Errorf("expected empty, got %v", keywords)
	}
}

func TestMemoryMergeHybridResults_Empty(t *testing.T) {
	// Minimal valid merge params with empty results.
	params := `{"vector":[],"keyword":[],"vectorWeight":0.7,"textWeight":0.3}`
	results, err := MemoryMergeHybridResults(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results == nil {
		t.Fatal("expected non-nil results")
	}
	t.Logf("Merge results: %s", string(results))
}
