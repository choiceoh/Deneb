package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// completeJSONServer returns an httptest server answering /chat/completions
// with the given non-streaming JSON body.
func completeJSONServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
}

// A non-streaming refusal arrives on message.refusal with content null.
// completeOpenAI used to read only content, returning "" with a nil error —
// background callers (wiki dreamer/verify/merge) then treated the refusal as
// a successful empty result. It must surface as an explicit error instead.
func TestCompleteOpenAI_RefusalSurfacedAsError(t *testing.T) {
	server := completeJSONServer(`{"choices":[{"message":{"content":null,"refusal":"I cannot help with that."}}]}`)
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	out, err := client.Complete(context.Background(), ChatRequest{
		Model:     "test-model",
		Messages:  []Message{NewTextMessage("user", "do something disallowed")},
		MaxTokens: 50,
	})
	if err == nil {
		t.Fatalf("Complete = (%q, nil), want refusal error (empty success hides the refusal)", out)
	}
	if !strings.Contains(err.Error(), "refused") {
		t.Errorf("error = %q, want it to mention the refusal", err)
	}
}

// Normal content path still decodes (guards the response-struct change).
func TestCompleteOpenAI_NormalContent(t *testing.T) {
	server := completeJSONServer(`{"choices":[{"message":{"content":"ok"}}]}`)
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	out, err := client.Complete(context.Background(), ChatRequest{
		Model:     "test-model",
		Messages:  []Message{NewTextMessage("user", "hi")},
		MaxTokens: 50,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if out != "ok" {
		t.Errorf("Complete = %q, want %q", out, "ok")
	}
}

// Some OpenAI-compatible servers stream tool calls without an id. The
// translator must synthesize one at finish emission: tool_use↔tool_result
// pairing and the echo-back to the provider both require a non-empty id.
func TestStreamChat_MissingToolCallID_Synthesized(t *testing.T) {
	chunks := []map[string]any{
		{"id": "c", "model": "m", "choices": []map[string]any{{"index": 0, "delta": map[string]any{
			"tool_calls": []map[string]any{{"index": 0, "type": "function",
				"function": map[string]string{"name": "read", "arguments": `{"path":"f.go"}`}}}}}}},
		{"id": "c", "model": "m", "choices": []map[string]any{{"index": 0, "delta": map[string]any{
			"tool_calls": []map[string]any{{"index": 1, "type": "function",
				"function": map[string]string{"name": "grep", "arguments": `{"pattern":"x"}`}}}}}}},
		{"id": "c", "model": "m", "choices": []map[string]any{{"index": 0,
			"delta": map[string]string{}, "finish_reason": "tool_calls"}}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", mustJSON(c))
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	events, err := client.StreamChat(context.Background(), ChatRequest{
		Model:     "test-model",
		Messages:  []Message{NewTextMessage("user", "go")},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	ids := map[string]string{} // name -> id
	for _, b := range assembleSingleBlock(events) {
		if b.Type == "tool_use" {
			ids[b.Name] = b.ID
		}
	}
	if len(ids) != 2 {
		t.Fatalf("got %d tool calls, want 2: %+v", len(ids), ids)
	}
	if ids["read"] == "" || ids["grep"] == "" {
		t.Errorf("synthesized ids missing: %+v (empty id breaks tool_result pairing)", ids)
	}
	if ids["read"] == ids["grep"] {
		t.Errorf("ids must be distinct, both %q", ids["read"])
	}
}
