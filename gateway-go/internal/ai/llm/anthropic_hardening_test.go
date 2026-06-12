// anthropic_hardening_test.go — Anthropic-path hardening (the Anthropic-mode
// sibling of the OpenAI-path #2270 fixes): Complete()'s error/empty-result
// guards, tool_choice vocabulary translation, the required-max_tokens default,
// adaptive thinking serialization, and redacted_thinking data round-trip.
package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// anthropicSSEServer returns a test server that answers every request with the
// given pre-formatted SSE body.
func anthropicSSEServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
}

// TestCompleteViaStream_TextHappyPath guards the baseline: text deltas are
// concatenated and returned with a nil error when the stream ends cleanly.
func TestCompleteViaStream_TextHappyPath(t *testing.T) {
	srv := anthropicSSEServer(t, ""+
		"event: content_block_delta\n"+
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"안녕"}}`+"\n\n"+
		"event: content_block_delta\n"+
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"하세요"}}`+"\n\n"+
		"event: message_stop\n"+
		`data: {"type":"message_stop"}`+"\n\n")
	defer srv.Close()

	client := NewClient(srv.URL, "k", WithAPIMode(APIModeAnthropic), WithRetry(0, 0, 0))
	out, err := client.Complete(context.Background(), ChatRequest{
		Model: "m", MaxTokens: 64,
		Messages: []Message{NewTextMessage("user", "hi")},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if out != "안녕하세요" {
		t.Errorf("out = %q, want 안녕하세요", out)
	}
}

// TestCompleteViaStream_ErrorEventSurfaces verifies a mid-stream error event
// fails the call instead of returning the partial text as a success.
func TestCompleteViaStream_ErrorEventSurfaces(t *testing.T) {
	srv := anthropicSSEServer(t, ""+
		"event: content_block_delta\n"+
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`+"\n\n"+
		"event: error\n"+
		`data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`+"\n\n")
	defer srv.Close()

	client := NewClient(srv.URL, "k", WithAPIMode(APIModeAnthropic), WithRetry(0, 0, 0))
	out, err := client.Complete(context.Background(), ChatRequest{
		Model: "m", MaxTokens: 64,
		Messages: []Message{NewTextMessage("user", "hi")},
	})
	if err == nil {
		t.Fatalf("Complete = (%q, nil), want error for mid-stream error event", out)
	}
	if !strings.Contains(err.Error(), "stream error event") {
		t.Errorf("error = %v, want stream error event", err)
	}
}

// TestCompleteViaStream_PrematureEOFSurfaces verifies a connection cut without
// message_stop (premature_end synthesized by the forwarder) fails the call.
func TestCompleteViaStream_PrematureEOFSurfaces(t *testing.T) {
	srv := anthropicSSEServer(t, ""+
		"event: content_block_delta\n"+
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"cut"}}`+"\n\n")
	defer srv.Close()

	client := NewClient(srv.URL, "k", WithAPIMode(APIModeAnthropic), WithRetry(0, 0, 0))
	_, err := client.Complete(context.Background(), ChatRequest{
		Model: "m", MaxTokens: 64,
		Messages: []Message{NewTextMessage("user", "hi")},
	})
	if err == nil || !strings.Contains(err.Error(), "premature_end") {
		t.Fatalf("err = %v, want premature_end stream error", err)
	}
}

// TestCompleteViaStream_ThinkingBurnedBudgetIsError verifies the empty-content
// guard: a turn whose whole output budget went to the thinking channel must
// not read as a successful empty result.
func TestCompleteViaStream_ThinkingBurnedBudgetIsError(t *testing.T) {
	srv := anthropicSSEServer(t, ""+
		"event: content_block_delta\n"+
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"...깊은 고민..."}}`+"\n\n"+
		"event: message_delta\n"+
		`data: {"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":64}}`+"\n\n"+
		"event: message_stop\n"+
		`data: {"type":"message_stop"}`+"\n\n")
	defer srv.Close()

	client := NewClient(srv.URL, "k", WithAPIMode(APIModeAnthropic), WithRetry(0, 0, 0))
	out, err := client.Complete(context.Background(), ChatRequest{
		Model: "m", MaxTokens: 64,
		Messages: []Message{NewTextMessage("user", "hi")},
	})
	if err == nil {
		t.Fatalf("Complete = (%q, nil), want error for thinking-burned budget", out)
	}
	if !strings.Contains(err.Error(), "reasoning consumed the output budget") {
		t.Errorf("error = %v, want output-budget diagnostic", err)
	}
}

// TestCompleteViaStream_EmptyCleanStreamStaysNilError pins the preserved
// behavior: a clean stream with no text, no thinking, and no max_tokens stop
// still returns ("", nil) — mirroring completeOpenAI's empty-success path.
func TestCompleteViaStream_EmptyCleanStreamStaysNilError(t *testing.T) {
	srv := anthropicSSEServer(t, ""+
		"event: message_delta\n"+
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}`+"\n\n"+
		"event: message_stop\n"+
		`data: {"type":"message_stop"}`+"\n\n")
	defer srv.Close()

	client := NewClient(srv.URL, "k", WithAPIMode(APIModeAnthropic), WithRetry(0, 0, 0))
	out, err := client.Complete(context.Background(), ChatRequest{
		Model: "m", MaxTokens: 64,
		Messages: []Message{NewTextMessage("user", "hi")},
	})
	if err != nil || out != "" {
		t.Fatalf("Complete = (%q, %v), want (\"\", nil)", out, err)
	}
}

// TestTranslateAnthropicToolChoice covers the OpenAI→Anthropic vocabulary map
// and the pass-through cases.
func TestTranslateAnthropicToolChoice(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want any
	}{
		{"nil", nil, nil},
		{"empty string", "  ", nil},
		{"auto", "auto", map[string]any{"type": "auto"}},
		{"none", "NONE", map[string]any{"type": "none"}},
		{"required", "required", map[string]any{"type": "any"}},
		{"any", "any", map[string]any{"type": "any"}},
		{"bare tool name", "get_weather", map[string]any{"type": "tool", "name": "get_weather"}},
		{
			"openai function shape",
			map[string]any{"type": "function", "function": map[string]any{"name": "search"}},
			map[string]any{"type": "tool", "name": "search"},
		},
		{
			"anthropic shape passes through",
			map[string]any{"type": "tool", "name": "search"},
			map[string]any{"type": "tool", "name": "search"},
		},
		{
			"anthropic auto object passes through",
			map[string]any{"type": "auto", "disable_parallel_tool_use": true},
			map[string]any{"type": "auto", "disable_parallel_tool_use": true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := translateAnthropicToolChoice(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("translateAnthropicToolChoice(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestBuildAnthropicRequestBody_MaxTokensDefault verifies a zero MaxTokens is
// replaced by the wire default — max_tokens is required (>0) on the Anthropic
// API, unlike the OpenAI path where omitempty defers to the server default.
func TestBuildAnthropicRequestBody_MaxTokensDefault(t *testing.T) {
	body, err := buildAnthropicRequestBody(ChatRequest{
		Model:    "m",
		Messages: []Message{NewTextMessage("user", "hi")},
	})
	if err != nil {
		t.Fatalf("buildAnthropicRequestBody: %v", err)
	}
	var got struct {
		MaxTokens int `json:"max_tokens"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.MaxTokens != defaultAnthropicMaxTokens {
		t.Errorf("max_tokens = %d, want %d", got.MaxTokens, defaultAnthropicMaxTokens)
	}
}

// TestBuildAnthropicRequestBody_AdaptiveThinking verifies the adaptive type
// serializes alone, without a budget_tokens field (Opus 4.7+ rejects one).
func TestBuildAnthropicRequestBody_AdaptiveThinking(t *testing.T) {
	body, err := buildAnthropicRequestBody(ChatRequest{
		Model: "claude-opus-4-8", MaxTokens: 1024,
		Messages: []Message{NewTextMessage("user", "hi")},
		Thinking: &ThinkingConfig{Type: "adaptive"},
	})
	if err != nil {
		t.Fatalf("buildAnthropicRequestBody: %v", err)
	}
	var got struct {
		Thinking map[string]any `json:"thinking"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Thinking["type"] != "adaptive" {
		t.Errorf("thinking.type = %v, want adaptive", got.Thinking["type"])
	}
	if _, has := got.Thinking["budget_tokens"]; has {
		t.Errorf("thinking carries budget_tokens %v; must be omitted for adaptive", got.Thinking["budget_tokens"])
	}
}

// TestBuildAnthropicRequestBody_ToolChoiceTranslated verifies the translation
// is applied on the wire, not just in the helper.
func TestBuildAnthropicRequestBody_ToolChoiceTranslated(t *testing.T) {
	body, err := buildAnthropicRequestBody(ChatRequest{
		Model: "m", MaxTokens: 64,
		Messages:   []Message{NewTextMessage("user", "hi")},
		ToolChoice: "required",
	})
	if err != nil {
		t.Fatalf("buildAnthropicRequestBody: %v", err)
	}
	var got struct {
		ToolChoice map[string]any `json:"tool_choice"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ToolChoice["type"] != "any" {
		t.Errorf(`tool_choice = %v, want {"type":"any"}`, got.ToolChoice)
	}
}

// TestMarshalAnthropicBlocks_RedactedThinkingDataRoundTrip verifies the
// encrypted payload of a redacted_thinking block survives serialization —
// Anthropic rejects an echoed redacted block without its data.
func TestMarshalAnthropicBlocks_RedactedThinkingDataRoundTrip(t *testing.T) {
	raw, err := marshalAnthropicBlocks([]ContentBlock{
		{Type: "redacted_thinking", Data: "EncryptedOpaquePayload=="},
		{Type: "text", Text: "answer"},
	})
	if err != nil {
		t.Fatalf("marshalAnthropicBlocks: %v", err)
	}
	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if blocks[0]["data"] != "EncryptedOpaquePayload==" {
		t.Errorf("redacted_thinking data = %v, want round-tripped payload", blocks[0]["data"])
	}
}
