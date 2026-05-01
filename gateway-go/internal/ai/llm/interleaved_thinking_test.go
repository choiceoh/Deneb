package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// captureRequest is a tiny test handler that records the inbound HTTP request
// (headers + body) and returns a minimal SSE stream so StreamChat completes
// without errors. Tests inspect the captured request to verify wire shape.
type captureRequest struct {
	mu      sync.Mutex
	headers http.Header
	body    []byte
}

func (c *captureRequest) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		c.mu.Lock()
		c.headers = r.Header.Clone()
		c.body = body
		c.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id":    "chatcmpl-x",
			"model": "test-model",
			"choices": []map[string]any{
				{"index": 0, "delta": map[string]string{"role": "assistant", "content": "ok"}, "finish_reason": "stop"},
			},
			"usage": map[string]int{
				"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2,
			},
		}))
		flusher.Flush()
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}
}

// TestStreamChat_InterleavedThinkingBetaHeader verifies that turning on
// interleaved thinking attaches the Anthropic beta header. Other providers
// silently ignore the header, so this is safe everywhere.
func TestStreamChat_InterleavedThinkingBetaHeader(t *testing.T) {
	cap := &captureRequest{}
	server := httptest.NewServer(cap.handler(t))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	events, err := client.StreamChat(context.Background(), ChatRequest{
		Model:     "test-model",
		Messages:  []Message{NewTextMessage("user", "hi")},
		MaxTokens: 8,
		Thinking: &ThinkingConfig{
			Type:         "enabled",
			BudgetTokens: 4096,
			Interleaved:  true,
		},
	})
	testutil.NoError(t, err)
	for range events {
	}

	cap.mu.Lock()
	got := cap.headers.Get("anthropic-beta")
	cap.mu.Unlock()
	if !strings.Contains(got, "interleaved-thinking-2025-05-14") {
		t.Fatalf("anthropic-beta header = %q, want it to contain interleaved-thinking-2025-05-14", got)
	}
}

// TestStreamChat_NoBetaHeaderWhenInterleavedOff verifies that the beta
// header is NOT set when interleaved is disabled, so non-Anthropic
// providers don't see noise on every request.
func TestStreamChat_NoBetaHeaderWhenInterleavedOff(t *testing.T) {
	cap := &captureRequest{}
	server := httptest.NewServer(cap.handler(t))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	events, err := client.StreamChat(context.Background(), ChatRequest{
		Model:     "test-model",
		Messages:  []Message{NewTextMessage("user", "hi")},
		MaxTokens: 8,
		Thinking: &ThinkingConfig{
			Type:         "enabled",
			BudgetTokens: 4096,
			Interleaved:  false,
		},
	})
	testutil.NoError(t, err)
	for range events {
	}

	cap.mu.Lock()
	got := cap.headers.Get("anthropic-beta")
	cap.mu.Unlock()
	if got != "" {
		t.Fatalf("anthropic-beta header = %q, want empty", got)
	}
}

// TestStreamChat_BetaHeadersPassThrough verifies caller-supplied beta flags
// are joined into the anthropic-beta header.
func TestStreamChat_BetaHeadersPassThrough(t *testing.T) {
	cap := &captureRequest{}
	server := httptest.NewServer(cap.handler(t))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	events, err := client.StreamChat(context.Background(), ChatRequest{
		Model:       "test-model",
		Messages:    []Message{NewTextMessage("user", "hi")},
		MaxTokens:   8,
		BetaHeaders: []string{"prompt-caching-2024-07-31", "interleaved-thinking-2025-05-14"},
		Thinking: &ThinkingConfig{
			Type:         "enabled",
			BudgetTokens: 4096,
			Interleaved:  true,
		},
	})
	testutil.NoError(t, err)
	for range events {
	}

	cap.mu.Lock()
	got := cap.headers.Get("anthropic-beta")
	cap.mu.Unlock()
	// Both flags present, no duplicates.
	parts := strings.Split(got, ",")
	if len(parts) != 2 {
		t.Fatalf("anthropic-beta = %q, want 2 unique flags, got %d", got, len(parts))
	}
	if !strings.Contains(got, "prompt-caching-2024-07-31") {
		t.Errorf("missing prompt-caching flag in %q", got)
	}
	if !strings.Contains(got, "interleaved-thinking-2025-05-14") {
		t.Errorf("missing interleaved-thinking flag in %q", got)
	}
}

// TestConvertMessages_PreservesThinkingWhenInterleaved verifies that prior
// assistant `thinking` blocks survive the OpenAI conversion as
// `reasoning_content` so reasoning context flows across tool boundaries.
func TestConvertMessages_PreservesThinkingWhenInterleaved(t *testing.T) {
	c := NewClient("http://invalid", "")
	messages := []Message{
		NewBlockMessage("assistant", []ContentBlock{
			{Type: "thinking", Thinking: "I should call the search tool first."},
			{Type: "text", Text: "Calling search."},
			{Type: "tool_use", ID: "t1", Name: "search", Input: json.RawMessage(`{"q":"x"}`)},
		}),
	}

	withInterleaved := c.convertMessagesToOpenAI(messages, true)
	if len(withInterleaved) != 1 {
		t.Fatalf("got %d messages, want 1", len(withInterleaved))
	}
	if withInterleaved[0].ReasoningContent != "I should call the search tool first." {
		t.Errorf("reasoning_content = %q, want preserved thinking",
			withInterleaved[0].ReasoningContent)
	}

	withoutInterleaved := c.convertMessagesToOpenAI(messages, false)
	if withoutInterleaved[0].ReasoningContent != "" {
		t.Errorf("interleaved=false should drop thinking, got %q",
			withoutInterleaved[0].ReasoningContent)
	}
}

// TestConvertMessages_DoesNotLeakUserThinking confirms thinking blocks on
// non-assistant messages (defensive: the wire never carries these, but
// belt-and-braces) never bleed into reasoning_content.
func TestConvertMessages_DoesNotLeakUserThinking(t *testing.T) {
	c := NewClient("http://invalid", "")
	messages := []Message{
		NewBlockMessage("user", []ContentBlock{
			{Type: "thinking", Thinking: "user-side junk"},
			{Type: "text", Text: "hello"},
		}),
	}
	got := c.convertMessagesToOpenAI(messages, true)
	if len(got) != 1 {
		t.Fatalf("got %d messages, want 1", len(got))
	}
	if got[0].ReasoningContent != "" {
		t.Errorf("user reasoning_content = %q, want empty", got[0].ReasoningContent)
	}
}
