package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestMergeJSONFields(t *testing.T) {
	base := []byte(`{"model":"test","stream":true}`)
	extra := map[string]any{
		"timeout": 30.0,
	}
	got := testutil.Must(mergeJSONFields(base, extra))
	s := string(got)
	if !strings.Contains(s, `"timeout":30`) {
		t.Errorf("expected timeout:30 in result, got: %s", s)
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
	testutil.NoError(t, err)
	if got != "ok" {
		t.Fatalf("CompleteOpenAI = %q, want %q", got, "ok")
	}
}
