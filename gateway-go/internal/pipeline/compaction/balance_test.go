package compaction

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func tbToolUse(id, name string) llm.ContentBlock {
	return llm.ContentBlock{Type: "tool_use", ID: id, Name: name, Input: json.RawMessage(`{}`)}
}
func tbToolResult(id, content string) llm.ContentBlock {
	return llm.ContentBlock{Type: "tool_result", ToolUseID: id, Content: content}
}
func tbText(s string) llm.ContentBlock {
	return llm.ContentBlock{Type: "text", Text: s}
}

// assertBalanced fails if any tool_use lacks a matching tool_result or vice
// versa — the invariant Anthropic's /v1/messages requires.
func assertBalanced(t *testing.T, messages []llm.Message) {
	t.Helper()
	uses, results := map[string]bool{}, map[string]bool{}
	for _, m := range messages {
		blocks, ok := decodeBlocks(m.Content)
		if !ok {
			continue
		}
		for _, b := range blocks {
			switch b.Type {
			case "tool_use":
				uses[b.ID] = true
			case "tool_result":
				results[b.ToolUseID] = true
			}
		}
	}
	for id := range uses {
		if !results[id] {
			t.Errorf("orphaned tool_use id=%q (no matching tool_result)", id)
		}
	}
	for id := range results {
		if !uses[id] {
			t.Errorf("orphaned tool_result tool_use_id=%q (no matching tool_use)", id)
		}
	}
}

func TestBalanceToolBlocks_OrphanedToolUseStubbed(t *testing.T) {
	// Assistant called a tool but its tool_result message was dropped by a tier.
	msgs := []llm.Message{
		llm.NewTextMessage("user", "do it"),
		llm.NewBlockMessage("assistant", []llm.ContentBlock{tbText("working on it"), tbToolUse("A", "read")}),
	}
	out := BalanceToolBlocks(msgs)
	assertBalanced(t, out)

	blocks, ok := decodeBlocks(out[1].Content)
	if !ok {
		t.Fatal("assistant message lost its block content")
	}
	if len(blocks) != 2 || blocks[0].Type != "text" || blocks[0].Text != "working on it" {
		t.Fatalf("text block not preserved: %+v", blocks)
	}
	if blocks[1].Type != "text" || !strings.Contains(blocks[1].Text, "omitted") {
		t.Errorf("orphaned tool_use not stubbed to text: %+v", blocks[1])
	}
}

func TestBalanceToolBlocks_OrphanedToolResultStubbed(t *testing.T) {
	// tool_result survived but its tool_use (assistant turn) was dropped.
	msgs := []llm.Message{
		llm.NewBlockMessage("user", []llm.ContentBlock{tbToolResult("B", "result text")}),
		llm.NewTextMessage("assistant", "ok"),
	}
	out := BalanceToolBlocks(msgs)
	assertBalanced(t, out)

	blocks, _ := decodeBlocks(out[0].Content)
	if len(blocks) != 1 || blocks[0].Type != "text" || !strings.Contains(blocks[0].Text, "omitted") {
		t.Errorf("orphaned tool_result not stubbed to text: %+v", blocks)
	}
}

func TestBalanceToolBlocks_BalancedPairUnchanged(t *testing.T) {
	msgs := []llm.Message{
		llm.NewBlockMessage("assistant", []llm.ContentBlock{tbToolUse("A", "read")}),
		llm.NewBlockMessage("user", []llm.ContentBlock{tbToolResult("A", "ok")}),
	}
	before := []string{string(msgs[0].Content), string(msgs[1].Content)}
	out := BalanceToolBlocks(msgs)
	assertBalanced(t, out)
	// A balanced input must be a byte-for-byte no-op (prompt-cache stability).
	for i := range out {
		if string(out[i].Content) != before[i] {
			t.Errorf("message %d was rewritten on a balanced input:\n got  %s\n want %s", i, out[i].Content, before[i])
		}
	}
}

func TestBalanceToolBlocks_StringContentUntouched(t *testing.T) {
	msgs := []llm.Message{
		llm.NewTextMessage("user", "hello"),
		llm.NewTextMessage("assistant", "hi"),
	}
	before := []string{string(msgs[0].Content), string(msgs[1].Content)}
	out := BalanceToolBlocks(msgs)
	for i := range out {
		if string(out[i].Content) != before[i] {
			t.Errorf("plain text message %d was modified", i)
		}
	}
}
