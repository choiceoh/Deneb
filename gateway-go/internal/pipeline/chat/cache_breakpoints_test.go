package chat

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestBuildTrailingCacheHook_NonAnthropicReturnsNil(t *testing.T) {
	if h := buildTrailingCacheHook(llm.APIModeOpenAI); h != nil {
		t.Errorf("expected nil hook for OpenAI, got non-nil")
	}
	if h := buildTrailingCacheHook(""); h != nil {
		t.Errorf("expected nil hook for empty mode, got non-nil")
	}
}

func TestBuildTrailingCacheHook_AttachesTrailingMarkers(t *testing.T) {
	hook := buildTrailingCacheHook(llm.APIModeAnthropic)
	if hook == nil {
		t.Fatal("expected non-nil hook for Anthropic mode")
	}
	msgs := []llm.Message{
		llm.NewTextMessage("user", "hello"),
		llm.NewTextMessage("assistant", "hi"),
		llm.NewTextMessage("user", "what is the weather"),
	}
	out := hook(msgs)
	if len(out) != len(msgs) {
		t.Fatalf("expected %d messages, got %d", len(msgs), len(out))
	}
	// Last 2 messages (assistant + user) should carry ephemeral cache_control.
	for _, idx := range []int{1, 2} {
		blocks := decodeOrFail(t, out[idx].Content)
		if len(blocks) == 0 {
			t.Fatalf("msg[%d]: empty blocks", idx)
		}
		last := blocks[len(blocks)-1]
		if last.CacheControl == nil || last.CacheControl.Type != "ephemeral" {
			t.Errorf("msg[%d]: expected ephemeral cache_control, got %+v", idx, last.CacheControl)
		}
	}
	// Earlier message untouched.
	blocks0 := decodeOrFail(t, out[0].Content)
	if len(blocks0) > 0 && blocks0[len(blocks0)-1].CacheControl != nil {
		t.Errorf("msg[0]: unexpected cache_control on non-trailing message")
	}
}

func TestBuildTrailingCacheHook_DoesNotMutateInput(t *testing.T) {
	hook := buildTrailingCacheHook(llm.APIModeAnthropic)
	original := []llm.Message{
		llm.NewTextMessage("user", "a"),
		llm.NewTextMessage("assistant", "b"),
		llm.NewTextMessage("user", "c"),
	}
	snapshots := make([]string, len(original))
	for i, m := range original {
		snapshots[i] = string(m.Content)
	}
	_ = hook(original)
	for i, m := range original {
		if string(m.Content) != snapshots[i] {
			t.Errorf("msg[%d] content mutated: before=%s after=%s", i, snapshots[i], m.Content)
		}
	}
}

func TestBuildTrailingCacheHook_SkipsSystemMessages(t *testing.T) {
	hook := buildTrailingCacheHook(llm.APIModeAnthropic)
	msgs := []llm.Message{
		llm.NewTextMessage("user", "u"),
		llm.NewTextMessage("system", "noise"),
		llm.NewTextMessage("assistant", "a"),
	}
	out := hook(msgs)
	// user (0) + assistant (2) are the last 2 non-system.
	for _, idx := range []int{0, 2} {
		blocks := decodeOrFail(t, out[idx].Content)
		if blocks[len(blocks)-1].CacheControl == nil {
			t.Errorf("msg[%d]: expected cache_control on non-system trailing message", idx)
		}
	}
	blocks := decodeOrFail(t, out[1].Content)
	if len(blocks) > 0 && blocks[len(blocks)-1].CacheControl != nil {
		t.Errorf("system message marked unexpectedly")
	}
}

func TestBuildTrailingCacheHook_BlockContentMarksLastBlock(t *testing.T) {
	hook := buildTrailingCacheHook(llm.APIModeAnthropic)
	blocks := []llm.ContentBlock{
		{Type: "text", Text: "first"},
		{Type: "text", Text: "second"},
	}
	msg := llm.NewBlockMessage("user", blocks)
	out := hook([]llm.Message{msg})
	got := decodeOrFail(t, out[0].Content)
	if len(got) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(got))
	}
	if got[0].CacheControl != nil {
		t.Errorf("first block should not be marked")
	}
	if got[1].CacheControl == nil || got[1].CacheControl.Type != "ephemeral" {
		t.Errorf("last block should carry ephemeral marker, got %+v", got[1].CacheControl)
	}
}

func TestBuildTrailingCacheHook_FewerMessagesThanLimit(t *testing.T) {
	hook := buildTrailingCacheHook(llm.APIModeAnthropic)
	msgs := []llm.Message{llm.NewTextMessage("user", "alone")}
	out := hook(msgs)
	blocks := decodeOrFail(t, out[0].Content)
	if blocks[len(blocks)-1].CacheControl == nil {
		t.Errorf("single message should still get marker (within limit)")
	}
}

func TestBuildTrailingCacheHook_EmptyMessagesNoOp(t *testing.T) {
	hook := buildTrailingCacheHook(llm.APIModeAnthropic)
	if out := hook(nil); out != nil {
		t.Errorf("nil input should pass through")
	}
	if out := hook([]llm.Message{}); len(out) != 0 {
		t.Errorf("empty slice should pass through")
	}
}

func TestBuildTrailingCacheHook_ToolResultBlockIsMarkable(t *testing.T) {
	hook := buildTrailingCacheHook(llm.APIModeAnthropic)
	msg := llm.NewBlockMessage("user", []llm.ContentBlock{
		{Type: "tool_result", ToolUseID: "tool_1", Content: "result text"},
	})
	out := hook([]llm.Message{msg})
	got := decodeOrFail(t, out[0].Content)
	if got[0].CacheControl == nil || got[0].CacheControl.Type != "ephemeral" {
		t.Errorf("tool_result block should accept ephemeral marker, got %+v", got[0].CacheControl)
	}
}

func TestPickTrailingCacheTargets_AscendingOrder(t *testing.T) {
	msgs := []llm.Message{
		llm.NewTextMessage("user", "1"),
		llm.NewTextMessage("assistant", "2"),
		llm.NewTextMessage("user", "3"),
		llm.NewTextMessage("assistant", "4"),
	}
	idx := pickTrailingCacheTargets(msgs, 3)
	if len(idx) != 3 {
		t.Fatalf("expected 3 indices, got %d", len(idx))
	}
	for i := 1; i < len(idx); i++ {
		if idx[i] <= idx[i-1] {
			t.Errorf("indices not ascending: %v", idx)
		}
	}
	// Should pick the LAST 3.
	want := []int{1, 2, 3}
	for i, v := range want {
		if idx[i] != v {
			t.Errorf("idx[%d]: want %d got %d (full=%v)", i, v, idx[i], idx)
		}
	}
}

func decodeOrFail(t *testing.T, raw json.RawMessage) []llm.ContentBlock {
	t.Helper()
	blocks, ok := decodeMessageBlocks(raw)
	if !ok {
		t.Fatalf("decode failed for content: %s", raw)
	}
	return blocks
}
