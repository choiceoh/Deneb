package chat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

func richMsg(role, blocks string) ChatMessage {
	return ChatMessage{Role: role, Content: json.RawMessage(blocks)}
}

// A tool turn is persisted as an assistant tool_use followed by a user-role
// tool_result. The tool_result message must not surface as a chat bubble, while
// the surrounding turn (including the assistant's text) is preserved in order.
func TestStripToolResultBlocks_DropsToolResultOnlyMessage(t *testing.T) {
	msgs := []ChatMessage{
		toolctx.NewTextChatMessage("user", "리눅스 프로세스 보여줘", 0),
		richMsg("assistant", `[{"type":"text","text":"확인해볼게요"},{"type":"tool_use","id":"t1","name":"exec","input":{"command":"ps aux"}}]`),
		richMsg("user", `[{"type":"tool_result","tool_use_id":"t1","content":"root 1 ... ps aux output ..."}]`),
		toolctx.NewTextChatMessage("assistant", "정상 프로세스입니다.", 0),
	}
	out := stripToolResultBlocksForDisplay(msgs)

	if len(out) != 3 {
		t.Fatalf("tool_result-only message should be dropped, got %d messages", len(out))
	}
	for _, m := range out {
		if strings.Contains(string(m.Content), "tool_result") {
			t.Fatalf("tool_result leaked into display: %s", m.Content)
		}
		if strings.Contains(string(m.Content), "ps aux output") {
			t.Fatalf("raw tool output leaked into display: %s", m.Content)
		}
	}
	if out[0].TextContent() != "리눅스 프로세스 보여줘" || out[2].TextContent() != "정상 프로세스입니다." {
		t.Fatalf("surrounding messages not preserved in order: %+v", out)
	}
	if got := out[1].TextContent(); got != "확인해볼게요" {
		t.Fatalf("assistant text block lost, got %q", got)
	}
}

// A message that mixes a text block with a tool_result keeps the text and loses
// only the tool_result.
func TestStripToolResultBlocks_KeepsOtherBlocksInMixedMessage(t *testing.T) {
	msgs := []ChatMessage{
		richMsg("user", `[{"type":"text","text":"여기 결과"},{"type":"tool_result","tool_use_id":"t9","content":"secret stdout"}]`),
	}
	out := stripToolResultBlocksForDisplay(msgs)

	if len(out) != 1 {
		t.Fatalf("mixed message must be kept, got %d", len(out))
	}
	if strings.Contains(string(out[0].Content), "tool_result") || strings.Contains(string(out[0].Content), "secret stdout") {
		t.Fatalf("tool_result not stripped from mixed message: %s", out[0].Content)
	}
	if got := out[0].TextContent(); got != "여기 결과" {
		t.Fatalf("text block lost from mixed message, got %q", got)
	}
}

// Plain-string content and non-tool_result blocks (thinking, tool_use) pass
// through byte-for-byte — the strip is scoped to tool_result only.
func TestStripToolResultBlocks_LeavesPlainAndNonToolResultBlocks(t *testing.T) {
	thinking := richMsg("assistant", `[{"type":"thinking","thinking":"고민중"},{"type":"tool_use","id":"t1","name":"exec","input":{}}]`)
	msgs := []ChatMessage{
		toolctx.NewTextChatMessage("user", "안녕", 0),
		thinking,
	}
	out := stripToolResultBlocksForDisplay(msgs)

	if len(out) != 2 {
		t.Fatalf("no message should be dropped, got %d", len(out))
	}
	if out[0].TextContent() != "안녕" {
		t.Fatalf("plain user message altered: %q", out[0].TextContent())
	}
	if string(out[1].Content) != string(thinking.Content) {
		t.Fatalf("non-tool_result blocks must be untouched:\ngot:  %s\nwant: %s", out[1].Content, thinking.Content)
	}
}
