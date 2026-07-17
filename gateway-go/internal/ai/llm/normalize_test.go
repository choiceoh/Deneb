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
	if err := json.Unmarshal(got[0].Content.Bytes(), &blocks); err != nil {
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
	// Simulate multiple tool calls in a single turn producing consecutive
	// tool_result messages.
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
	if err := json.Unmarshal(got[1].Content.Bytes(), &merged); err != nil {
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
	if err := json.Unmarshal(got[1].Content.Bytes(), &merged); err != nil {
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
	if err := json.Unmarshal(got[0].Content.Bytes(), &blocks); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(blocks) != 3 {
		t.Fatalf("want 3 blocks, got %d", len(blocks))
	}
}

func TestOrderToolResultsFirst_SteerMergeShape(t *testing.T) {
	// The live-bisected kimi failure shape: a steering user text merged in
	// front of the turn's tool_results ([text, result, result]). The results
	// must move to the front, both relative orders preserved.
	msgs := []Message{
		{Role: "assistant", Content: marshalBlocks([]ContentBlock{
			{Type: "tool_use", ID: "call_a", Name: "web"},
			{Type: "tool_use", ID: "call_b", Name: "web"},
		})},
		{Role: "user", Content: marshalBlocks([]ContentBlock{
			{Type: "text", Text: "steer: do it differently"},
			{Type: "tool_result", ToolUseID: "call_a", Content: "result A"},
			{Type: "tool_result", ToolUseID: "call_b", Content: "result B"},
		})},
	}
	got := OrderToolResultsFirst(msgs)
	if len(got) != 2 {
		t.Fatalf("want 2 messages, got %d", len(got))
	}
	blocks := ContentToBlocks(got[1].Content)
	if len(blocks) != 3 {
		t.Fatalf("want 3 blocks, got %d", len(blocks))
	}
	if blocks[0].ToolUseID != "call_a" || blocks[1].ToolUseID != "call_b" {
		t.Fatalf("tool_results not first / order lost: %+v", blocks)
	}
	if blocks[2].Type != "text" || blocks[2].Text != "steer: do it differently" {
		t.Fatalf("text block lost: %+v", blocks[2])
	}
	// Input slice must be untouched.
	orig := ContentToBlocks(msgs[1].Content)
	if orig[0].Type != "text" {
		t.Fatalf("input mutated: %+v", orig)
	}
}

func TestOrderToolResultsFirst_OrderedInputUntouched(t *testing.T) {
	// Already-ordered messages pass through byte-identical (cache stability):
	// same slice back, no re-marshal.
	msgs := []Message{
		NewTextMessage("user", "hello"),
		{Role: "assistant", Content: marshalBlocks([]ContentBlock{
			{Type: "tool_use", ID: "call_a", Name: "web"},
		})},
		{Role: "user", Content: marshalBlocks([]ContentBlock{
			{Type: "tool_result", ToolUseID: "call_a", Content: "result"},
			{Type: "text", Text: "and a follow-up"},
		})},
	}
	got := OrderToolResultsFirst(msgs)
	if &got[0] != &msgs[0] {
		t.Fatal("ordered input should return the same slice")
	}
	for i := range msgs {
		if string(got[i].Content.Bytes()) != string(msgs[i].Content.Bytes()) {
			t.Fatalf("message %d content changed", i)
		}
	}
}

func TestOrderToolResultsFirst_AssistantUntouched(t *testing.T) {
	// Only user messages are rewritten — an assistant message with text before
	// tool_use is the normal shape and must never be reordered.
	msgs := []Message{
		{Role: "assistant", Content: marshalBlocks([]ContentBlock{
			{Type: "text", Text: "let me check"},
			{Type: "tool_use", ID: "call_a", Name: "web"},
			{Type: "tool_result", ToolUseID: "call_x", Content: "weird persisted block"},
		})},
	}
	got := OrderToolResultsFirst(msgs)
	if string(got[0].Content.Bytes()) != string(msgs[0].Content.Bytes()) {
		t.Fatal("assistant message was rewritten")
	}
}

func TestOrderToolResultsFirst_OnlyOffendingMessageRewritten(t *testing.T) {
	// A mixed history: clean messages keep their exact bytes; only the
	// offending message is re-marshaled.
	clean := Message{Role: "user", Content: marshalBlocks([]ContentBlock{
		{Type: "tool_result", ToolUseID: "call_0", Content: "ok"},
	})}
	dirty := Message{Role: "user", Content: marshalBlocks([]ContentBlock{
		{Type: "text", Text: "steer"},
		{Type: "tool_result", ToolUseID: "call_1", Content: "late"},
	})}
	msgs := []Message{clean, NewTextMessage("assistant", "mid"), dirty}
	got := OrderToolResultsFirst(msgs)
	if string(got[0].Content.Bytes()) != string(clean.Content.Bytes()) {
		t.Fatal("clean message bytes changed")
	}
	blocks := ContentToBlocks(got[2].Content)
	if blocks[0].Type != "tool_result" || blocks[1].Type != "text" {
		t.Fatalf("dirty message not reordered: %+v", blocks)
	}
}
