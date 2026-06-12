// cache_reconcile_test.go — fallback-attempt cache_control reconciliation and
// the thinking-block guard on trailing markers.
package chat

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func markedSystemBlocks(t *testing.T) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal([]llm.ContentBlock{
		{Type: "text", Text: "static prompt", CacheControl: &llm.CacheControl{Type: "ephemeral"}},
		{Type: "text", Text: "semi-static prompt", CacheControl: &llm.CacheControl{Type: "ephemeral"}},
	})
	if err != nil {
		t.Fatalf("marshal system blocks: %v", err)
	}
	return raw
}

// countCacheMarkers returns how many content blocks across messages carry a
// cache_control marker.
func countCacheMarkers(t *testing.T, messages []llm.Message) int {
	t.Helper()
	n := 0
	for _, m := range messages {
		for _, b := range llm.ContentToBlocks(m.Content) {
			if b.CacheControl != nil {
				n++
			}
		}
	}
	return n
}

// TestWithTrailingCacheControl_SkipsTrailingThinkingBlock verifies the marker
// walks back past a trailing thinking block — Anthropic rejects cache_control
// on thinking/redacted_thinking with HTTP 400.
func TestWithTrailingCacheControl_SkipsTrailingThinkingBlock(t *testing.T) {
	content, _ := json.Marshal([]llm.ContentBlock{
		{Type: "text", Text: "answer"},
		{Type: "thinking", Thinking: "reasoning cut by max_tokens"},
	})
	got := withTrailingCacheControl(llm.Message{Role: "assistant", Content: content})

	var blocks []llm.ContentBlock
	if err := json.Unmarshal(got.Content, &blocks); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if blocks[0].CacheControl == nil {
		t.Error("text block missing marker; should receive the walked-back marker")
	}
	if blocks[1].CacheControl != nil {
		t.Error("thinking block carries cache_control; Anthropic 400s on this")
	}
}

// TestWithTrailingCacheControl_ThinkingOnlyMessageUnchanged verifies a message
// made solely of thinking blocks (a turn cut mid-reasoning) gets no marker.
func TestWithTrailingCacheControl_ThinkingOnlyMessageUnchanged(t *testing.T) {
	content, _ := json.Marshal([]llm.ContentBlock{
		{Type: "thinking", Thinking: "only reasoning, no answer"},
	})
	msg := llm.Message{Role: "assistant", Content: content}
	got := withTrailingCacheControl(msg)
	if !bytes.Equal(got.Content, content) {
		t.Errorf("thinking-only message rewritten: %s", got.Content)
	}
}

// TestStripMessageCacheMarkersHook verifies markers are removed from a
// per-request copy without mutating the input.
func TestStripMessageCacheMarkersHook(t *testing.T) {
	markedContent, _ := json.Marshal([]llm.ContentBlock{
		{Type: "text", Text: "hello", CacheControl: &llm.CacheControl{Type: "ephemeral"}},
	})
	input := []llm.Message{
		llm.NewTextMessage("user", "plain"),
		{Role: "assistant", Content: markedContent},
	}

	out := stripMessageCacheMarkersHook(input)

	if got := countCacheMarkers(t, out); got != 0 {
		t.Errorf("output carries %d cache markers, want 0", got)
	}
	// Input untouched (per-request copy semantics).
	if !bytes.Contains(input[1].Content, []byte("cache_control")) {
		t.Error("input message mutated; hook must operate on a copy")
	}
	// Untouched message passes through byte-identical.
	if !bytes.Equal(out[0].Content, input[0].Content) {
		t.Error("marker-free message rewritten")
	}
}

// TestStripMessageCacheMarkersHook_NoMarkersReturnsInputSlice pins the
// fast path: nothing to strip → the input slice itself comes back.
func TestStripMessageCacheMarkersHook_NoMarkersReturnsInputSlice(t *testing.T) {
	input := []llm.Message{llm.NewTextMessage("user", "plain")}
	out := stripMessageCacheMarkersHook(input)
	if &out[0] != &input[0] {
		t.Error("marker-free input copied; expected pass-through")
	}
}

// TestReconcileFallbackCacheMarkers_RejectingProviderStrips verifies a
// fallback onto a marker-rejecting provider (Kimi) strips the system markers
// AND neutralizes markers the inherited hook chain attaches — otherwise every
// fallback attempt 400s.
func TestReconcileFallbackCacheMarkers_RejectingProviderStrips(t *testing.T) {
	cfg := agent.AgentConfig{
		System: markedSystemBlocks(t),
		// Original provider was Anthropic-mode (zai): trailing hook installed.
		BeforeAPICall: agent.ComposeBeforeAPICall(buildTrailingCacheHook(llm.APIModeAnthropic)),
	}
	fbClient := llm.NewClient("http://127.0.0.1:1", "", llm.WithAPIMode(llm.APIModeAnthropic))

	reconcileFallbackCacheMarkers(&cfg, runDeps{}, "zai", "glm-5-turbo",
		"kimi", "kimi-for-coding", fbClient, discardLogger())

	if bytes.Contains(cfg.System, []byte("cache_control")) {
		t.Error("system markers survived; Kimi rejects them with 400")
	}
	msgs := []llm.Message{
		llm.NewTextMessage("user", "q1"),
		llm.NewTextMessage("assistant", "a1"),
		llm.NewTextMessage("user", "q2"),
	}
	out := cfg.BeforeAPICall(msgs)
	if got := countCacheMarkers(t, out); got != 0 {
		t.Errorf("hook chain leaves %d markers on messages, want 0", got)
	}
}

// TestReconcileFallbackCacheMarkers_AnthropicFallbackGetsTrailingHook verifies
// a fallback from an OpenAI-mode main onto an accepting Anthropic-mode
// provider installs the trailing-marker hook so the attempt runs cached.
func TestReconcileFallbackCacheMarkers_AnthropicFallbackGetsTrailingHook(t *testing.T) {
	cfg := agent.AgentConfig{System: markedSystemBlocks(t)} // vllm main: no hook
	fbClient := llm.NewClient("http://127.0.0.1:1", "", llm.WithAPIMode(llm.APIModeAnthropic))

	reconcileFallbackCacheMarkers(&cfg, runDeps{}, "vllm", "deepseek-v4-flash",
		"mimo-plan", "mimo-v2", fbClient, discardLogger())

	if cfg.BeforeAPICall == nil {
		t.Fatal("trailing-marker hook not installed for Anthropic-mode fallback")
	}
	msgs := []llm.Message{
		llm.NewTextMessage("user", "q1"),
		llm.NewTextMessage("assistant", "a1"),
		llm.NewTextMessage("user", "q2"),
	}
	out := cfg.BeforeAPICall(msgs)
	if got := countCacheMarkers(t, out); got != trailingCacheCount {
		t.Errorf("messages carry %d markers, want %d", got, trailingCacheCount)
	}
	if countCacheMarkers(t, msgs) != 0 {
		t.Error("input messages mutated; hook must operate on a copy")
	}
}

// TestReconcileFallbackCacheMarkers_OpenAIFallbackUntouched verifies an
// OpenAI-mode fallback changes nothing: its converter drops cache_control
// during translation, so the config keeps the original policy.
func TestReconcileFallbackCacheMarkers_OpenAIFallbackUntouched(t *testing.T) {
	system := markedSystemBlocks(t)
	cfg := agent.AgentConfig{System: system}
	fbClient := llm.NewClient("http://127.0.0.1:1", "", llm.WithAPIMode(llm.APIModeOpenAI))

	reconcileFallbackCacheMarkers(&cfg, runDeps{}, "vllm", "deepseek-v4-flash",
		"vllm", "qwen3.6", fbClient, discardLogger())

	if cfg.BeforeAPICall != nil {
		t.Error("hook installed for OpenAI-mode fallback; expected no change")
	}
	if !bytes.Equal(cfg.System, system) {
		t.Error("system rewritten for OpenAI-mode fallback; expected no change")
	}
}
