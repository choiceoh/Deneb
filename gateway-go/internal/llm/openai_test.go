package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompleteOpenAI_OmitsAuthorizationHeaderWhenAPIKeyEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization header = %q, want empty", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	got, err := client.CompleteOpenAI(context.Background(), ChatRequest{
		Model:     "local-model",
		Messages:  []Message{NewTextMessage("user", "hello")},
		MaxTokens: 16,
	})
	if err != nil {
		t.Fatalf("CompleteOpenAI error: %v", err)
	}
	if got != "ok" {
		t.Fatalf("CompleteOpenAI = %q, want %q", got, "ok")
	}
}
