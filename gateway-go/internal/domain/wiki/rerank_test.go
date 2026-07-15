package wiki

import (
	"context"
	"testing"
)

type fakeReranker struct {
	calls int
}

func (f *fakeReranker) Identity() string { return "fake-reranker" }

func (f *fakeReranker) Rerank(_ context.Context, _ string, documents []string) ([]float64, error) {
	f.calls++
	return []float64{0.1, 0.9}, nil
}

type descendingReranker struct{}

func (descendingReranker) Identity() string { return "descending-reranker" }

func (descendingReranker) Rerank(_ context.Context, _ string, documents []string) ([]float64, error) {
	scores := make([]float64, len(documents))
	for i := range scores {
		scores[i] = float64(i)
	}
	return scores, nil
}

func TestModelRerankRunsOnlyForAmbiguousCandidates(t *testing.T) {
	rr := &fakeReranker{}
	store := &Store{reranker: rr}
	strong := []SearchResult{{Path: "a", Score: 0.9}, {Path: "b", Score: 0.3}}
	_, _, diagnostics := store.applyModelRerank(context.Background(), "query", strong, false)
	if rr.calls != 0 || diagnostics.Eligible {
		t.Fatalf("strong ranking called reranker: calls=%d diagnostics=%+v", rr.calls, diagnostics)
	}
	ambiguous := []SearchResult{{Path: "a", Score: 0.50}, {Path: "b", Score: 0.49}}
	_, _, diagnostics = store.applyModelRerank(context.Background(), "query", ambiguous, false)
	if rr.calls != 1 || !diagnostics.Applied || ambiguous[0].Path != "b" {
		t.Fatalf("ambiguous rerank = calls=%d diagnostics=%+v results=%+v", rr.calls, diagnostics, ambiguous)
	}
}

func TestModelRerankKeepsUntouchedTailBehindCandidateWindow(t *testing.T) {
	store := &Store{reranker: descendingReranker{}}
	results := make([]SearchResult, rerankCandidateLimit+1)
	for i := range results {
		results[i] = SearchResult{Path: string(rune('a' + i)), Score: 0.5 - float64(i)/100}
	}
	tailPath := results[rerankCandidateLimit].Path
	_, _, diagnostics := store.applyModelRerank(context.Background(), "query", results, true)
	if !diagnostics.Applied || results[len(results)-1].Path != tailPath {
		t.Fatalf("tail interleaved after candidate rerank: diagnostics=%+v results=%+v", diagnostics, results)
	}
}
