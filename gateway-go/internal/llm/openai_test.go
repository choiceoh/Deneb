package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMergeJSONFields(t *testing.T) {
	base := []byte(`{"model":"test","stream":true}`)
	extra := map[string]any{
		"chat_completion_extra_params": map[string]any{"enable_thinking": false},
	}
	got, err := mergeJSONFields(base, extra)
	if err != nil {
		t.Fatalf("mergeJSONFields error: %v", err)
	}
	s := string(got)
	if !strings.Contains(s, `"enable_thinking":false`) {
		t.Errorf("expected enable_thinking:false in result, got: %s", s)
	}
	if !strings.Contains(s, `"model":"test"`) {
		t.Errorf("expected original fields preserved, got: %s", s)
	}
}

func TestComplete_OmitsAuthorizationHeaderWhenAPIKeyEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization header = %q, want empty", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	got, err := client.Complete(context.Background(), ChatRequest{
		Model:     "local-model",
		Messages:  []Message{NewTextMessage("user", "hello")},
		MaxTokens: 16,
	})
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if got != "ok" {
		t.Fatalf("CompleteOpenAI = %q, want %q", got, "ok")
	}
}
