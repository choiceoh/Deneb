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

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// anthropicCapture records the inbound HTTP request a single time and serves
// a minimal Anthropic SSE stream so streamChatAnthropic completes cleanly.
type anthropicCapture struct {
	mu      sync.Mutex
	headers http.Header
	body    map[string]any
	path    string
}

func (c *anthropicCapture) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		var parsed map[string]any
		if err := json.NewDecoder(r.Body).Decode(&parsed); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		c.mu.Lock()
		c.headers = r.Header.Clone()
		c.body = parsed
		c.path = r.URL.Path
		c.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		// Anthropic-style SSE — two named events plus message_stop.
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", mustJSON(map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":    "msg-x",
				"model": "glm-4.7",
				"usage": map[string]int{"input_tokens": 1, "output_tokens": 0},
			},
		}))
		flusher.Flush()
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", mustJSON(map[string]any{
			"type": "message_stop",
		}))
		flusher.Flush()
	}
}

// TestStreamChatAnthropic_RequestShape verifies the wire format Z.AI's
// Anthropic mirror expects: POST /v1/messages, x-api-key + Authorization
// headers, anthropic-version pinned, system + thinking surfaced as
// top-level fields.
func TestStreamChatAnthropic_RequestShape(t *testing.T) {
	cap := &anthropicCapture{}
	server := httptest.NewServer(cap.handler(t))
	defer server.Close()

	client := NewClient(server.URL, "zai-key", WithAPIType(APITypeAnthropic))
	events, err := client.StreamChat(context.Background(), ChatRequest{
		Model:     "glm-4.7",
		System:    SystemString("you are a helper"),
		Messages:  []Message{NewTextMessage("user", "hi")},
		MaxTokens: 256,
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
	defer cap.mu.Unlock()

	if cap.path != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", cap.path)
	}
	if got := cap.headers.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want 2023-06-01", got)
	}
	if got := cap.headers.Get("x-api-key"); got != "zai-key" {
		t.Errorf("x-api-key = %q, want zai-key", got)
	}
	if got := cap.headers.Get("Authorization"); got != "Bearer zai-key" {
		t.Errorf("Authorization = %q, want Bearer zai-key", got)
	}
	if got := cap.headers.Get("anthropic-beta"); !strings.Contains(got, "interleaved-thinking-2025-05-14") {
		t.Errorf("anthropic-beta = %q, want interleaved-thinking flag", got)
	}

	// Body shape: system is top-level, thinking is structured object,
	// and messages survive verbatim.
	if cap.body["system"] != "you are a helper" {
		t.Errorf("system = %v, want plain string passed through", cap.body["system"])
	}
	thinking, ok := cap.body["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking missing or wrong shape: %v", cap.body["thinking"])
	}
	if thinking["type"] != "enabled" {
		t.Errorf("thinking.type = %v, want enabled", thinking["type"])
	}
	if int(thinking["budget_tokens"].(float64)) != 4096 {
		t.Errorf("thinking.budget_tokens = %v, want 4096", thinking["budget_tokens"])
	}
	if cap.body["max_tokens"].(float64) != 256 {
		t.Errorf("max_tokens = %v, want 256", cap.body["max_tokens"])
	}
}

// TestStreamChatAnthropic_NoThinkingObjectWhenDisabled confirms we don't
// attach the structured thinking object when thinking is off — sending
// budget=0 trips a 400 on Anthropic.
func TestStreamChatAnthropic_NoThinkingObjectWhenDisabled(t *testing.T) {
	cap := &anthropicCapture{}
	server := httptest.NewServer(cap.handler(t))
	defer server.Close()

	client := NewClient(server.URL, "k", WithAPIType(APITypeAnthropic))
	events, err := client.StreamChat(context.Background(), ChatRequest{
		Model:     "glm-4.7",
		Messages:  []Message{NewTextMessage("user", "hi")},
		MaxTokens: 64,
	})
	testutil.NoError(t, err)
	for range events {
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()
	if _, present := cap.body["thinking"]; present {
		t.Errorf("thinking field unexpectedly present: %v", cap.body["thinking"])
	}
}

// TestForwardAnthropicStream_PassesThroughEvents confirms Anthropic SSE
// events flow to the consumer unchanged (no translation), with `ping` and
// empty events filtered out.
func TestForwardAnthropicStream_PassesThroughEvents(t *testing.T) {
	in := make(chan StreamEvent, 8)
	out := make(chan StreamEvent, 8)
	in <- StreamEvent{Type: "message_start", Payload: json.RawMessage(`{}`)}
	in <- StreamEvent{Type: "ping", Payload: json.RawMessage(`{}`)}
	in <- StreamEvent{Type: "content_block_start", Payload: json.RawMessage(`{"index":0}`)}
	in <- StreamEvent{Type: "", Payload: json.RawMessage(`should be dropped`)}
	in <- StreamEvent{Type: "content_block_stop", Payload: json.RawMessage(`{"index":0}`)}
	close(in)

	c := NewClient("http://invalid", "")
	c.forwardAnthropicStream(context.Background(), in, out)
	close(out)

	var got []string
	for ev := range out {
		got = append(got, ev.Type)
	}
	want := []string{"message_start", "content_block_start", "content_block_stop", "message_stop"}
	if len(got) != len(want) {
		t.Fatalf("got %d events %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestStreamChatAnthropic_PreservesThinkingSignature verifies that the
// Anthropic native request body carries thinking blocks with their
// signature intact when the agent loop replays prior reasoning. This is
// the hard requirement for interleaved-thinking round-trip — Anthropic
// rejects thinking blocks that lack a signature.
func TestStreamChatAnthropic_PreservesThinkingSignature(t *testing.T) {
	cap := &anthropicCapture{}
	server := httptest.NewServer(cap.handler(t))
	defer server.Close()

	prior := NewBlockMessage("assistant", []ContentBlock{
		{Type: "thinking", Thinking: "let me check the search tool", Signature: "sig_abc123"},
		{Type: "tool_use", ID: "t1", Name: "search", Input: json.RawMessage(`{"q":"x"}`)},
	})

	client := NewClient(server.URL, "k", WithAPIType(APITypeAnthropic))
	events, err := client.StreamChat(context.Background(), ChatRequest{
		Model:     "glm-4.7",
		Messages:  []Message{prior},
		MaxTokens: 64,
		Thinking: &ThinkingConfig{
			Type:         "enabled",
			BudgetTokens: 1024,
			Interleaved:  true,
		},
	})
	testutil.NoError(t, err)
	for range events {
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()
	msgs, _ := cap.body["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	first, _ := msgs[0].(map[string]any)
	content, _ := first["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("got %d content blocks, want 2", len(content))
	}
	thinkingBlock, _ := content[0].(map[string]any)
	if thinkingBlock["type"] != "thinking" {
		t.Errorf("first block type = %v, want thinking", thinkingBlock["type"])
	}
	if thinkingBlock["signature"] != "sig_abc123" {
		t.Errorf("signature = %v, want sig_abc123 round-trip", thinkingBlock["signature"])
	}
	if thinkingBlock["thinking"] != "let me check the search tool" {
		t.Errorf("thinking = %v, want preserved", thinkingBlock["thinking"])
	}
}
