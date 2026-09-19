package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// captureServer answers /chat/completions with body and records each request.
func captureServer(t *testing.T, body string) (*httptest.Server, func() []map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var reqs []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		reqs = append(reqs, req)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), reqs...)
	}
}

func firstTokenRequest() ChatRequest {
	return ChatRequest{
		Model:     "tiny",
		Messages:  []Message{NewTextMessage("user", "worth it?")},
		MaxTokens: 50, // CompleteFirstToken forces 1
	}
}

func TestCompleteFirstTokenAsksForLogprobsAndReadsTopAlternatives(t *testing.T) {
	srv, requests := captureServer(t, `{"choices":[{"message":{"content":"YES"},"finish_reason":"length",
		"logprobs":{"content":[{"token":"YES","logprob":-0.1,"top_logprobs":[
			{"token":"YES","logprob":-0.10536},{"token":"NO","logprob":-2.302585},{"token":"**","logprob":-6}]}]}}],
		"usage":{"prompt_tokens":100,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":40}}}`)

	got, err := NewClient(srv.URL, "k").CompleteFirstToken(context.Background(), firstTokenRequest(), 10)
	if err != nil {
		t.Fatalf("CompleteFirstToken: %v (a length stop is the request here, not a truncation)", err)
	}
	if got.Text != "YES" {
		t.Errorf("Text = %q, want YES", got.Text)
	}
	if len(got.Top) != 3 || got.Top[0].Token != "YES" || got.Top[1].Token != "NO" || got.Top[1].Logprob != -2.302585 {
		t.Errorf("Top = %#v", got.Top)
	}
	if got.Usage.InputTokens != 60 || got.Usage.CacheReadInputTokens != 40 || got.Usage.OutputTokens != 1 {
		t.Errorf("Usage = %#v, want cached tokens split out like the stream path", got.Usage)
	}

	reqs := requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d", len(reqs))
	}
	req := reqs[0]
	if req["max_tokens"] != float64(1) || req["logprobs"] != true || req["top_logprobs"] != float64(10) || req["stream"] != false {
		t.Errorf("request = %#v, want max_tokens 1, logprobs, top_logprobs 10, non-streaming", req)
	}
}

// Logprobs ride only on CompleteFirstToken: every other Complete body must go
// out as before, with no logprob keys at all.
func TestCompleteLeavesLogprobFieldsOff(t *testing.T) {
	srv, requests := captureServer(t, `{"choices":[{"message":{"content":"ok"}}]}`)
	if _, err := NewClient(srv.URL, "k").Complete(context.Background(), firstTokenRequest()); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	req := requests()[0]
	for _, key := range []string{"logprobs", "top_logprobs"} {
		if _, ok := req[key]; ok {
			t.Errorf("Complete request carries %q: %#v", key, req)
		}
	}
	if req["max_tokens"] != float64(50) {
		t.Errorf("Complete max_tokens = %#v, want the caller's 50", req["max_tokens"])
	}
}

// OpenRouter's hosted models answer without logprobs: the token still comes
// back, and the caller decides from the text.
func TestCompleteFirstTokenWithoutLogprobsKeepsText(t *testing.T) {
	srv, _ := captureServer(t, `{"choices":[{"message":{"content":"NO"},"finish_reason":"length"}]}`)
	got, err := NewClient(srv.URL, "k").CompleteFirstToken(context.Background(), firstTokenRequest(), 10)
	if err != nil || got.Text != "NO" || got.Top != nil {
		t.Fatalf("CompleteFirstToken = %#v/%v, want text NO with nil Top", got, err)
	}
}

func TestCompleteFirstTokenFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{"blank token", `{"choices":[{"message":{"content":" "},"finish_reason":"length"}]}`, "empty first token"},
		{"token spent on reasoning", `{"choices":[{"message":{"content":null,"reasoning":"Let"},"finish_reason":"length"}]}`, "reasoning"},
		{"refusal", `{"choices":[{"message":{"content":null,"refusal":"no"}}]}`, "refused"},
		{"no choices", `{"choices":[]}`, "no choices"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := captureServer(t, tc.body)
			got, err := NewClient(srv.URL, "k").CompleteFirstToken(context.Background(), firstTokenRequest(), 5)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("CompleteFirstToken = %#v/%v, want error containing %q", got, err, tc.want)
			}
		})
	}
}
