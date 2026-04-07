package llm

import (
	"encoding/json"
	"testing"
)

func TestNormalizeMessages_NoOp(t *testing.T) {
	// Empty and single-message cases return input as-is.
	if got := NormalizeMessages(nil); got != nil {
		t.Fatalf("nil input: got %v", got)
	}
	msgs := []Message{NewTextMessage("user", "hello")}
	if got := NormalizeMessages(msgs); len(got) != 1 {
		t.Fatalf("single message: got %d", len(got))
	}
}

func TestNormalizeMessages_AlternatingUnchanged(t *testing.T) {
	msgs := []Message{
		NewTextMessage("user", "hello"),
		NewTextMessage("assistant", "hi"),
		NewTextMessage("user", "bye"),
	}
	got := NormalizeMessages(msgs)
	if len(got) != 3 {
		t.Fatalf("alternating: want 3, got %d", len(got))
	}
}

func TestNormalizeMessages_MergesConsecutiveUser(t *testing.T) {
	msgs := []Message{
		NewTextMessage("user", "first"),
		NewTextMessage("user", "second"),
		NewTextMessage("assistant", "response"),
	}
	got := NormalizeMessages(msgs)
	if len(got) != 2 {
		t.Fatalf("want 2 messages, got %d", len(got))
	}
	if got[0].Role != "user" {
		t.Fatalf("first message role: want user, got %s", got[0].Role)
	}

	// Merged content should be a block array with both text blocks.
	var blocks []ContentBlock
	if err := json.Unmarshal(got[0].Content, &blocks); err != nil {
		t.Fatalf("unmarshal merged content: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Text != "first" || blocks[1].Text != "second" {
		t.Fatalf("block texts: %q, %q", blocks[0].Text, blocks[1].Text)
	}
}

func TestNormalizeMessages_MergesToolResultBlocks(t *testing.T) {
	// Simulate parallel tool execution producing consecutive tool_result messages.
	blocks1 := []ContentBlock{
		{Type: "tool_result", ToolUseID: "t1", Content: "result1"},
	}
	blocks2 := []ContentBlock{
		{Type: "tool_result", ToolUseID: "t2", Content: "result2"},
	}
	msgs := []Message{
		NewTextMessage("assistant", "calling tools"),
		NewBlockMessage("user", blocks1),
		NewBlockMessage("user", blocks2),
		NewTextMessage("assistant", "done"),
	}
	got := NormalizeMessages(msgs)
	if len(got) != 3 {
		t.Fatalf("want 3 messages, got %d", len(got))
	}

	// The two user messages should be merged into one with both tool_result blocks.
	var merged []ContentBlock
	if err := json.Unmarshal(got[1].Content, &merged); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(merged) != 2 {
		t.Fatalf("want 2 tool_result blocks, got %d", len(merged))
	}
	if merged[0].ToolUseID != "t1" || merged[1].ToolUseID != "t2" {
		t.Fatalf("tool IDs: %s, %s", merged[0].ToolUseID, merged[1].ToolUseID)
	}
}

func TestNormalizeMessages_MixedTextAndBlocks(t *testing.T) {
	// Tool result message followed by a plain text restoration message.
	blocks := []ContentBlock{
		{Type: "tool_result", ToolUseID: "t1", Content: "result"},
	}
	msgs := []Message{
		NewTextMessage("assistant", "call"),
		NewBlockMessage("user", blocks),
		NewTextMessage("user", "restored context"),
	}
	got := NormalizeMessages(msgs)
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}

	var merged []ContentBlock
	if err := json.Unmarshal(got[1].Content, &merged); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(merged) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(merged))
	}
	if merged[0].Type != "tool_result" {
		t.Fatalf("first block type: want tool_result, got %s", merged[0].Type)
	}
	if merged[1].Type != "text" || merged[1].Text != "restored context" {
		t.Fatalf("second block: type=%s text=%q", merged[1].Type, merged[1].Text)
	}
}

func TestNormalizeMessages_ThreeConsecutive(t *testing.T) {
	msgs := []Message{
		NewTextMessage("user", "a"),
		NewTextMessage("user", "b"),
		NewTextMessage("user", "c"),
		NewTextMessage("assistant", "reply"),
	}
	got := NormalizeMessages(msgs)
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(got[0].Content, &blocks); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(blocks) != 3 {
		t.Fatalf("want 3 blocks, got %d", len(blocks))
	}
}

func TestContentToBlocks_EmptyInput(t *testing.T) {
	if got := contentToBlocks(nil); got != nil {
		t.Fatalf("nil: got %v", got)
	}
	if got := contentToBlocks(json.RawMessage(`""`)); got != nil {
		t.Fatalf("empty string: got %v", got)
	}
	if got := contentToBlocks(json.RawMessage(`[]`)); got != nil {
		t.Fatalf("empty array: got %v", got)
	}
}
