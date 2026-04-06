package reranker

import (
	"context"
	"encoding/json"
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

	rr := NewReranker(RerankConfig{
		APIKey: "test-key",
		URL:    srv.URL + "/v1/rerank",
	})
	results, err := rr.Rerank(context.Background(), "test query", []string{"doc0", "doc1", "doc2"}, 3)
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
	rr := NewReranker(RerankConfig{APIKey: "test-key"})
	results, err := rr.Rerank(context.Background(), "query", nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil, got %v", results)
	}
}

func TestReranker_ValidWithoutAPIKey(t *testing.T) {
	rr := NewReranker(RerankConfig{})
	if rr == nil {
		t.Error("expected valid reranker without API key (local server mode)")
	}
}

func TestReranker_NoAuthHeaderWithoutAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("expected no Authorization header, got %q", auth)
		}
		resp := rerankResponse{
			Results: []RerankResult{{Index: 0, RelevanceScore: 0.9}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	rr := NewReranker(RerankConfig{URL: srv.URL + "/v1/rerank"})
	results, err := rr.Rerank(context.Background(), "query", []string{"doc"}, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestReranker_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	rr := NewReranker(RerankConfig{
		APIKey: "test-key",
		URL:    srv.URL + "/v1/rerank",
	})
	_, err := rr.Rerank(context.Background(), "query", []string{"doc"}, 1)
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
