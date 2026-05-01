package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStreamChat_AnthropicMode_BuildsMessagesEndpointRequest verifies the
// Anthropic client posts to /v1/messages with x-api-key, anthropic-version,
// and an Anthropic-format JSON body.
func TestStreamChat_AnthropicMode_BuildsMessagesEndpointRequest(t *testing.T) {
	var (
		gotPath    string
		gotMethod  string
		gotAPIKey  string
		gotVersion string
		gotBeta    string
		gotBody    map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		gotBeta = r.Header.Get("anthropic-beta")

		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "secret-key", WithAPIMode(APIModeAnthropic))

	events, err := client.StreamChat(context.Background(), ChatRequest{
		Model:     "glm-5.1",
		MaxTokens: 1024,
		System:    SystemString("be helpful"),
		Messages: []Message{
			NewTextMessage("user", "hello"),
		},
		Thinking: &ThinkingConfig{Type: "enabled", BudgetTokens: 4096, Interleaved: true},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	for range events {
	}

	if gotPath != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", gotPath)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotAPIKey != "secret-key" {
		t.Errorf("x-api-key = %q, want secret-key", gotAPIKey)
	}
	if gotVersion == "" {
		t.Errorf("anthropic-version header missing")
	}
	if !strings.Contains(gotBeta, "interleaved-thinking-2025-05-14") {
		t.Errorf("anthropic-beta = %q, want interleaved-thinking flag", gotBeta)
	}
	if gotBody["model"] != "glm-5.1" {
		t.Errorf("body.model = %v, want glm-5.1", gotBody["model"])
	}
	if gotBody["max_tokens"].(float64) != 1024 {
		t.Errorf("body.max_tokens = %v, want 1024", gotBody["max_tokens"])
	}
	if gotBody["system"] != "be helpful" {
		t.Errorf("body.system = %v, want \"be helpful\"", gotBody["system"])
	}
	thinking, _ := gotBody["thinking"].(map[string]any)
	if thinking == nil || thinking["type"] != "enabled" {
		t.Errorf("body.thinking = %v, want {type:enabled,budget_tokens:4096}", gotBody["thinking"])
	}
}

// TestStreamChat_AnthropicMode_ForwardsSSEEventsAsIs verifies Anthropic SSE
// event names and payloads pass through to the consumer unchanged (the
// internal StreamEvent contract is already Anthropic-style).
func TestStreamChat_AnthropicMode_ForwardsSSEEventsAsIs(t *testing.T) {
	body := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"m1","model":"glm-5.1","usage":{"input_tokens":7,"output_tokens":0}}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		"",
		"event: ping",
		`data: {"type":"ping"}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "k", WithAPIMode(APIModeAnthropic))

	events, err := client.StreamChat(context.Background(), ChatRequest{
		Model:     "glm-5.1",
		MaxTokens: 256,
		Messages:  []Message{NewTextMessage("user", "hi")},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	var seen []string
	for ev := range events {
		seen = append(seen, ev.Type)
	}
	want := []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	}
	if len(seen) != len(want) {
		t.Fatalf("event types = %v, want %v", seen, want)
	}
	for i, w := range want {
		if seen[i] != w {
			t.Errorf("event[%d] = %q, want %q", i, seen[i], w)
		}
	}
}

// TestSanitizeAnthropicContent ensures the request builder backfills the
// fields z.ai's Anthropic-compat validator requires. Without this, an
// empty `text` block or a `tool_use` block with no `input` triggers a
// 500: "sequence item 0: expected str instance, NoneType found".
func TestSanitizeAnthropicContent(t *testing.T) {
	cases := []struct {
		name string
		in   json.RawMessage
		want string // substring required in serialized output
	}{
		{
			name: "nil → empty text block",
			in:   nil,
			want: `[{"type":"text","text":""}]`,
		},
		{
			name: "literal null → empty text block",
			in:   json.RawMessage(`null`),
			want: `[{"type":"text","text":""}]`,
		},
		{
			name: "plain string → wrapped text block",
			in:   json.RawMessage(`"hi"`),
			want: `[{"type":"text","text":"hi"}]`,
		},
		{
			name: "tool_use without input gets {}",
			in:   json.RawMessage(`[{"type":"tool_use","id":"t1","name":"fs"}]`),
			want: `"input":{}`,
		},
		{
			name: "tool_use with explicit null input → {}",
			in:   json.RawMessage(`[{"type":"tool_use","id":"t1","name":"fs","input":null}]`),
			want: `"input":{}`,
		},
		{
			name: "empty text block keeps text:\"\"",
			in:   json.RawMessage(`[{"type":"text","text":""}]`),
			want: `"text":""`,
		},
		{
			name: "tool_result with empty content keeps content:\"\"",
			in:   json.RawMessage(`[{"type":"tool_result","tool_use_id":"t1","content":""}]`),
			want: `"content":""`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sanitizeAnthropicContent(tc.in)
			if err != nil {
				t.Fatalf("sanitize: %v", err)
			}
			if !strings.Contains(string(got), tc.want) {
				t.Errorf("got %s, want substring %s", string(got), tc.want)
			}
		})
	}
}

// TestWithAPIMode_NormalizesValues confirms common aliases map to the right
// constant and unknown values default to OpenAI.
func TestWithAPIMode_NormalizesValues(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{APIModeAnthropic, APIModeAnthropic},
		{"anthropic-messages", APIModeAnthropic},
		{"  ANTHROPIC  ", APIModeAnthropic},
		{APIModeOpenAI, APIModeOpenAI},
		{"", APIModeOpenAI},
		{"unknown", APIModeOpenAI},
	}
	for _, tc := range cases {
		c := NewClient("http://x", "", WithAPIMode(tc.in))
		if c.apiMode != tc.want {
			t.Errorf("WithAPIMode(%q) → %q, want %q", tc.in, c.apiMode, tc.want)
		}
	}
}
