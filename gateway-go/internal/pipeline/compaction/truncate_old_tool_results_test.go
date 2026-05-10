package compaction

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// toolResultMsg + textMsg helpers come from polaris_test.go (same package).

func assistantMsg(t *testing.T, text string) llm.Message {
	t.Helper()
	blocks := []llm.ContentBlock{{Type: "text", Text: text}}
	raw, err := json.Marshal(blocks)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return llm.Message{Role: "assistant", Content: raw}
}

func firstToolResultContent(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, b := range blocks {
		if b.Type == "tool_result" {
			return b.Content
		}
	}
	return ""
}

const stubPlaceholder = "[older tool output cleared to save context]"

func TestTruncateOldToolResults_StubsOversizedContent(t *testing.T) {
	long := strings.Repeat("a", 500)
	messages := []llm.Message{
		assistantMsg(t, "a1"),
		toolResultMsg(long),
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		assistantMsg(t, "a4"),
		assistantMsg(t, "a5"),
	}
	out, stubbed := TruncateOldToolResults(messages, 4, 256)
	if stubbed != 1 {
		t.Errorf("stubbed = %d, want 1", stubbed)
	}
	if got := firstToolResultContent(t, out[1].Content); got != stubPlaceholder {
		t.Errorf("content = %q, want placeholder", got)
	}
}

func TestTruncateOldToolResults_PreservesShortContent(t *testing.T) {
	short := strings.Repeat("a", 100)
	messages := []llm.Message{
		assistantMsg(t, "a1"),
		toolResultMsg(short),
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		assistantMsg(t, "a4"),
		assistantMsg(t, "a5"),
	}
	out, stubbed := TruncateOldToolResults(messages, 4, 256)
	if stubbed != 0 {
		t.Errorf("stubbed = %d, want 0", stubbed)
	}
	if got := firstToolResultContent(t, out[1].Content); got != short {
		t.Errorf("short content modified: %q", got)
	}
}

func TestTruncateOldToolResults_ProtectsRecentTurns(t *testing.T) {
	long := strings.Repeat("z", 500)
	// tool_result sits AFTER a3 — among the last 4 assistant turns, must be preserved.
	messages := []llm.Message{
		assistantMsg(t, "a1"),
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		toolResultMsg(long),
		assistantMsg(t, "a4"),
	}
	out, stubbed := TruncateOldToolResults(messages, 4, 256)
	if stubbed != 0 {
		t.Errorf("stubbed = %d, want 0 (within protected tail)", stubbed)
	}
	if got := firstToolResultContent(t, out[3].Content); got != long {
		t.Errorf("recent content modified")
	}
}

func TestTruncateOldToolResults_NoOpWhenInsufficientTurns(t *testing.T) {
	long := strings.Repeat("a", 500)
	messages := []llm.Message{
		assistantMsg(t, "a1"),
		toolResultMsg(long),
		assistantMsg(t, "a2"),
	}
	_, stubbed := TruncateOldToolResults(messages, 4, 256)
	if stubbed != 0 {
		t.Errorf("stubbed = %d, want 0 (only 2 assistant turns)", stubbed)
	}
}

func TestTruncateOldToolResults_HandlesMultipleBlocksInMessage(t *testing.T) {
	long := strings.Repeat("a", 500)
	short := "ok"
	blocks := []llm.ContentBlock{
		{Type: "tool_result", ToolUseID: "t1", Content: long},
		{Type: "tool_result", ToolUseID: "t2", Content: short},
		{Type: "tool_result", ToolUseID: "t3", Content: long},
	}
	raw, _ := json.Marshal(blocks)
	messages := []llm.Message{
		assistantMsg(t, "a1"),
		{Role: "user", Content: raw},
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		assistantMsg(t, "a4"),
		assistantMsg(t, "a5"),
	}
	out, stubbed := TruncateOldToolResults(messages, 4, 256)
	if stubbed != 2 {
		t.Errorf("stubbed = %d, want 2", stubbed)
	}
	var got []llm.ContentBlock
	if err := json.Unmarshal(out[1].Content, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got[0].Content != stubPlaceholder {
		t.Errorf("block 0 not stubbed: %q", got[0].Content)
	}
	if got[1].Content != short {
		t.Errorf("block 1 (short) modified: %q", got[1].Content)
	}
	if got[2].Content != stubPlaceholder {
		t.Errorf("block 2 not stubbed: %q", got[2].Content)
	}
}

func TestTruncateOldToolResults_NonToolResultBlocksUntouched(t *testing.T) {
	long := strings.Repeat("a", 500)
	blocks := []llm.ContentBlock{
		{Type: "text", Text: long},
		{Type: "tool_use", ID: "t1", Name: "exec", Input: json.RawMessage(`{"cmd":"echo"}`)},
	}
	raw, _ := json.Marshal(blocks)
	messages := []llm.Message{
		assistantMsg(t, "a1"),
		{Role: "assistant", Content: raw},
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		assistantMsg(t, "a4"),
		assistantMsg(t, "a5"),
		assistantMsg(t, "a6"),
	}
	_, stubbed := TruncateOldToolResults(messages, 4, 256)
	if stubbed != 0 {
		t.Errorf("stubbed = %d, want 0 (no tool_result blocks)", stubbed)
	}
}

func TestTruncateOldToolResults_CJKRunesNotBytes(t *testing.T) {
	// 한글 200자 ≈ 600 bytes. Threshold 256 RUNES → not stubbed.
	korean200 := strings.Repeat("가", 200)
	messages := []llm.Message{
		assistantMsg(t, "a1"),
		toolResultMsg(korean200),
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		assistantMsg(t, "a4"),
		assistantMsg(t, "a5"),
	}
	out, stubbed := TruncateOldToolResults(messages, 4, 256)
	if stubbed != 0 {
		t.Errorf("stubbed = %d, want 0 (200 runes < 256)", stubbed)
	}
	if got := firstToolResultContent(t, out[1].Content); got != korean200 {
		t.Errorf("Korean short content modified")
	}

	// 한글 300자 → over rune threshold.
	korean300 := strings.Repeat("나", 300)
	messages2 := []llm.Message{
		assistantMsg(t, "a1"),
		toolResultMsg(korean300),
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		assistantMsg(t, "a4"),
		assistantMsg(t, "a5"),
	}
	out2, stubbed2 := TruncateOldToolResults(messages2, 4, 256)
	if stubbed2 != 1 {
		t.Errorf("stubbed = %d, want 1 (300 runes > 256)", stubbed2)
	}
	if got := firstToolResultContent(t, out2[1].Content); got != stubPlaceholder {
		t.Errorf("Korean long content not stubbed: %q", got)
	}
}

func TestTruncateOldToolResults_DoesNotMutateInput(t *testing.T) {
	long := strings.Repeat("x", 500)
	messages := []llm.Message{
		assistantMsg(t, "a1"),
		toolResultMsg(long),
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		assistantMsg(t, "a4"),
		assistantMsg(t, "a5"),
	}
	originalContent := string(messages[1].Content)
	_, _ = TruncateOldToolResults(messages, 4, 256)
	if string(messages[1].Content) != originalContent {
		t.Errorf("input mutated; placeholder leaked into source slice")
	}
}
