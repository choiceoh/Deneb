package toolctx

import (
	"encoding/json"
	"strings"
	"testing"
)

func richMsg(role, blocks string) ChatMessage {
	return ChatMessage{Role: role, Content: json.RawMessage(blocks)}
}

func TestTransliterateAssistantTextForDisplay(t *testing.T) {
	msgs := []ChatMessage{
		// User input keeps its Hanja verbatim (not assistant prose).
		NewTextChatMessage("user", "報告書 보내줘", 0),
		// Plain-string assistant content → transliterated.
		NewTextChatMessage("assistant", "報告書를 첨부합니다. 契約 진행하죠.", 0),
		// Block-array assistant: text block converted, tool_use args untouched.
		richMsg("assistant", `[{"type":"text","text":"見積書 확인했습니다"},{"type":"tool_use","id":"t1","name":"exec","input":{"command":"報告"}}]`),
		// tool_result (user-role) data left intact.
		richMsg("user", `[{"type":"tool_result","tool_use_id":"t1","content":"報告 raw output"}]`),
	}
	out := TransliterateAssistantTextForDisplay(msgs)

	if got := mustText(t, out[0]); got != "報告書 보내줘" {
		t.Errorf("user message must be untouched, got %q", got)
	}
	if got := mustText(t, out[1]); got != "보고서를 첨부합니다. 계약 진행하죠." {
		t.Errorf("assistant prose = %q, want transliterated", got)
	}
	if !strings.Contains(string(out[2].Content), `"견적서 확인했습니다"`) {
		t.Errorf("assistant text block not transliterated: %s", out[2].Content)
	}
	if !strings.Contains(string(out[2].Content), `"command":"報告"`) {
		t.Errorf("tool_use input must stay verbatim: %s", out[2].Content)
	}
	if !strings.Contains(string(out[3].Content), "報告 raw output") {
		t.Errorf("tool_result data must stay verbatim: %s", out[3].Content)
	}
}

func mustText(t *testing.T, m ChatMessage) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(m.Content, &s); err != nil {
		t.Fatalf("content not a plain string: %s", m.Content)
	}
	return s
}

// A tool turn is persisted as an assistant tool_use followed by a user-role
// tool_result. The tool_result message must not surface as a chat bubble, while
// the surrounding turn (including the assistant's text) is preserved in order.
func TestStripToolResultBlocks_DropsToolResultOnlyMessage(t *testing.T) {
	msgs := []ChatMessage{
		NewTextChatMessage("user", "리눅스 프로세스 보여줘", 0),
		richMsg("assistant", `[{"type":"text","text":"확인해볼게요"},{"type":"tool_use","id":"t1","name":"exec","input":{"command":"ps aux"}}]`),
		richMsg("user", `[{"type":"tool_result","tool_use_id":"t1","content":"root 1 ... ps aux output ..."}]`),
		NewTextChatMessage("assistant", "정상 프로세스입니다.", 0),
	}
	out := StripToolResultBlocksForDisplay(msgs)

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
	out := StripToolResultBlocksForDisplay(msgs)

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
		NewTextChatMessage("user", "안녕", 0),
		thinking,
	}
	out := StripToolResultBlocksForDisplay(msgs)

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

// A user message carrying an appended link-enrichment block is trimmed back to
// the typed text; a message that merely mentions a --- divider stays intact.
func TestStripLinkEnrichmentForDisplay_StripsAppendedBlock(t *testing.T) {
	typed := "이 링크 요약해줘 https://example.com"
	enriched := typed + "\n\n---\n" + LinkEnrichmentHeader + "\n\npage dump here\n---"
	msgs := []ChatMessage{
		NewTextChatMessage("user", enriched, 0),
		NewTextChatMessage("user", "구분선은 ---로 씁니다", 0),
		NewTextChatMessage("assistant", enriched, 0), // non-user roles untouched
	}
	out := StripLinkEnrichmentForDisplay(msgs)

	if got := out[0].TextContent(); got != typed {
		t.Fatalf("enriched user message not stripped to typed text, got %q", got)
	}
	if got := out[1].TextContent(); got != "구분선은 ---로 씁니다" {
		t.Fatalf("plain message must be untouched, got %q", got)
	}
	if got := out[2].TextContent(); got != enriched {
		t.Fatalf("assistant message must be untouched, got %q", got)
	}
}

// The baked "[<RFC3339>] " wall-clock prefix (run_exec.go persist site) must
// come off user bubbles, while user-typed brackets and non-user roles survive.
func TestStripUserMessageTimestamp(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"baked timestamp", "[2026-06-12T15:30:00+09:00] 안녕", "안녕"},
		{"utc timestamp", "[2026-06-12T06:30:00Z] hello", "hello"},
		{"user-typed bracket", "[중요] 회의 메모", "[중요] 회의 메모"},
		{"pre-policy plain", "타임스탬프 없는 메시지", "타임스탬프 없는 메시지"},
		{"bracket without close", "[2026-06-12T15:30:00+09:00 잘림", "[2026-06-12T15:30:00+09:00 잘림"},
		{"date only is not rfc3339", "[2026-06-12] 메모", "[2026-06-12] 메모"},
		{"timestamp then typed bracket", "[2026-06-12T15:30:00+09:00] [중요] 공지", "[중요] 공지"},
	}
	for _, c := range cases {
		if got := StripUserMessageTimestamp(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestStripUserMessageTimestampsForDisplay(t *testing.T) {
	stamped := "[2026-06-12T15:30:00+09:00] 오늘 일정 알려줘"
	blocks := `[{"type":"text","text":"[2026-06-12T15:30:00+09:00] block"}]`
	msgs := []ChatMessage{
		NewTextChatMessage("user", stamped, 0),
		NewTextChatMessage("assistant", stamped, 0), // non-user roles untouched
		richMsg("user", blocks),                     // block content untouched
	}
	out := StripUserMessageTimestampsForDisplay(msgs)

	if got := out[0].TextContent(); got != "오늘 일정 알려줘" {
		t.Fatalf("user bubble not stripped, got %q", got)
	}
	if got := out[1].TextContent(); got != stamped {
		t.Fatalf("assistant message must be untouched, got %q", got)
	}
	if string(out[2].Content) != blocks {
		t.Fatalf("block content must be untouched, got %s", out[2].Content)
	}
}
