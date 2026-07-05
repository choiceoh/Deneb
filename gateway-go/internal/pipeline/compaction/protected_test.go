package compaction

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// fetchToolsCallMsg is an assistant message that called fetch_tools with id.
func fetchToolsCallMsg(t *testing.T, id string) llm.Message {
	t.Helper()
	blocks := []llm.ContentBlock{{Type: "tool_use", ID: id, Name: "fetch_tools"}}
	raw, err := json.Marshal(blocks)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return llm.Message{Role: "assistant", Content: raw}
}

// toolResultMsgFor is toolResultMsg with an explicit tool_use_id.
func toolResultMsgFor(t *testing.T, toolUseID, content string) llm.Message {
	t.Helper()
	blocks := []llm.ContentBlock{{Type: "tool_result", ToolUseID: toolUseID, Content: content}}
	raw, err := json.Marshal(blocks)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return llm.Message{Role: "user", Content: raw}
}

func toolResultContentFor(t *testing.T, raw json.RawMessage) string {
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

// A fetch_tools schema payload past the turn threshold must survive Tier 2b
// stubbing while a plain oversized tool result beside it is stubbed. Stubbing
// the schema forces an identical re-fetch (production 2026-07-05: 20% of
// fetch_tools calls were same-input same-output repeats).
func TestTruncateOldToolResults_ProtectsFetchToolsSchema(t *testing.T) {
	schema := strings.Repeat("도구 스키마 ", 100)
	plain := strings.Repeat("x", 500)
	messages := []llm.Message{
		fetchToolsCallMsg(t, "ft_1"),
		toolResultMsgFor(t, "ft_1", schema),
		toolResultMsgFor(t, "other_1", plain),
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		assistantMsg(t, "a4"),
		assistantMsg(t, "a5"),
	}
	out, stubbed := TruncateOldToolResults(messages, 4, 256)
	if stubbed != 1 {
		t.Fatalf("stubbed = %d, want 1 (only the unprotected result)", stubbed)
	}
	if got := toolResultContentFor(t, out[1].Content); got != schema {
		t.Errorf("fetch_tools schema was stubbed: %q", got[:min(40, len(got))])
	}
	if got := toolResultContentFor(t, out[2].Content); got == plain {
		t.Errorf("unprotected oversized result survived")
	}
}

// MicroCompact must not strip code fences out of a protected fetch_tools
// result (schemas carry fenced JSON), while still pruning fences elsewhere.
func TestMicroCompact_ProtectsFetchToolsSchema(t *testing.T) {
	fenced := "설명\n```json\n{\"name\":\"exec\"}\n```"
	messages := []llm.Message{
		fetchToolsCallMsg(t, "ft_1"),
		toolResultMsgFor(t, "ft_1", fenced),
		toolResultMsgFor(t, "other_1", fenced),
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		assistantMsg(t, "a4"),
		assistantMsg(t, "a5"),
	}
	out, pruned := MicroCompact(messages, 4)
	if pruned != 1 {
		t.Fatalf("pruned = %d, want 1 (only the unprotected result)", pruned)
	}
	if got := toolResultContentFor(t, out[1].Content); got != fenced {
		t.Errorf("fetch_tools schema fences were stripped: %q", got)
	}
	if got := toolResultContentFor(t, out[2].Content); strings.Contains(got, "```") {
		t.Errorf("unprotected result kept its fences: %q", got)
	}
}

func TestProtectedToolResultIDs_OnlyFetchTools(t *testing.T) {
	messages := []llm.Message{
		fetchToolsCallMsg(t, "ft_1"),
		func() llm.Message {
			blocks := []llm.ContentBlock{{Type: "tool_use", ID: "ex_1", Name: "exec"}}
			raw, _ := json.Marshal(blocks)
			return llm.Message{Role: "assistant", Content: raw}
		}(),
	}
	ids := protectedToolResultIDs(messages)
	if !ids["ft_1"] || ids["ex_1"] || len(ids) != 1 {
		t.Fatalf("protected ids = %v, want only ft_1", ids)
	}
}
