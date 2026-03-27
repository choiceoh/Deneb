package vega

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReranker_Rerank(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rerank" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("unexpected auth header: %s", got)
		}

		var req rerankRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if req.Query != "test query" {
			t.Errorf("unexpected query: %s", req.Query)
		}
		if len(req.Documents) != 3 {
			t.Errorf("expected 3 documents, got %d", len(req.Documents))
		}

		resp := rerankResponse{
			Results: []RerankResult{
				{Index: 2, RelevanceScore: 0.95},
				{Index: 0, RelevanceScore: 0.80},
				{Index: 1, RelevanceScore: 0.30},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	reranker := NewReranker(RerankConfig{
		APIKey: "test-key",
		URL:    srv.URL + "/v1/rerank",
	})
	results, err := reranker.Rerank(context.Background(), "test query", []string{"doc0", "doc1", "doc2"}, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Results should be sorted by descending relevance score.
	if results[0].Index != 2 || results[0].RelevanceScore != 0.95 {
		t.Errorf("first result: got index=%d score=%f, want index=2 score=0.95",
			results[0].Index, results[0].RelevanceScore)
	}
	if results[1].Index != 0 || results[1].RelevanceScore != 0.80 {
		t.Errorf("second result: got index=%d score=%f, want index=0 score=0.80",
			results[1].Index, results[1].RelevanceScore)
	}
}

func TestReranker_EmptyDocs(t *testing.T) {
	reranker := NewReranker(RerankConfig{APIKey: "test-key"})
	results, err := reranker.Rerank(context.Background(), "query", nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil, got %v", results)
	}
}

func TestReranker_NilWithoutAPIKey(t *testing.T) {
	reranker := NewReranker(RerankConfig{})
	if reranker != nil {
		t.Error("expected nil reranker without API key")
	}
}

func TestReranker_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	reranker := NewReranker(RerankConfig{
		APIKey: "test-key",
		URL:    srv.URL + "/v1/rerank",
	})
	_, err := reranker.Rerank(context.Background(), "query", []string{"doc"}, 1)
	if err == nil {
		t.Fatal("expected error for server 500")
	}
}

func TestGetJinaAPIKey(t *testing.T) {
	t.Setenv("JINA_API_KEY", "jina_test_key_123")
	got := GetJinaAPIKey()
	if got != "jina_test_key_123" {
		t.Errorf("expected jina_test_key_123, got %q", got)
	}
}

func TestGetJinaAPIKey_Empty(t *testing.T) {
	t.Setenv("JINA_API_KEY", "")
	got := GetJinaAPIKey()
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input    string
		maxChars int
		want     string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hello"},
		{"안녕하세요", 3, "안녕하"},
	}

	for _, tt := range tests {
		got := truncateString(tt.input, tt.maxChars)
		if got != tt.want {
			t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxChars, got, tt.want)
		}
	}
}

func TestRerankResults_Fallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "broken", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	eb := &EnhancedBackend{
		reranker: NewReranker(RerankConfig{
			APIKey: "test-key",
			URL:    srv.URL + "/v1/rerank",
		}),
		logger: slog.Default(),
	}

	input := []SearchResult{
		{ProjectID: 1, Content: "first"},
		{ProjectID: 2, Content: "second"},
	}

	got := eb.rerankResults(context.Background(), "query", input)

	if len(got) != 2 {
		t.Fatalf("expected 2 results, got %d", len(got))
	}
	if got[0].ProjectID != 1 || got[1].ProjectID != 2 {
		t.Errorf("expected original order, got %+v", got)
	}
}
