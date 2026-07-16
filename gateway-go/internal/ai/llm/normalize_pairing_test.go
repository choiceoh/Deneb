package llm

import (
	"testing"
)

func blocksMsg(role string, blocks ...ContentBlock) Message {
	return Message{Role: role, Content: marshalBlocks(blocks)}
}

func toolUse(id, name string) ContentBlock {
	return ContentBlock{Type: "tool_use", ID: id, Name: name}
}

func toolResult(id, content string) ContentBlock {
	return ContentBlock{Type: "tool_result", ToolUseID: id, Content: content}
}

func pairingBlocks(t *testing.T, m Message) []ContentBlock {
	t.Helper()
	return ContentToBlocks(m.Content)
}

func TestRepairToolPairing_HealthyHistoryUntouched(t *testing.T) {
	msgs := []Message{
		NewTextMessage("user", "질문"),
		blocksMsg("assistant", ContentBlock{Type: "text", Text: "찾아볼게"}, toolUse("web:1", "web")),
		blocksMsg("user", toolResult("web:1", "결과")),
		NewTextMessage("assistant", "답"),
	}
	got := RepairToolPairing(msgs)
	if len(got) != 4 {
		t.Fatalf("healthy history changed: %d messages", len(got))
	}
	for i := range msgs {
		if string(got[i].Content.Bytes()) != string(msgs[i].Content.Bytes()) {
			t.Fatalf("message %d content changed", i)
		}
	}
}

// The live failure shape (kimi 400 on client:main): an assistant message with
// parallel tool_use blocks whose result message was pruned from history.
func TestRepairToolPairing_SynthesizesResultsForForwardOrphans(t *testing.T) {
	msgs := []Message{
		NewTextMessage("user", "검색해줘"),
		blocksMsg("assistant", toolUse("web:24", "web"), toolUse("web:25", "web")),
		NewTextMessage("assistant", "…그렇습니다"), // the pruned gap left same-role adjacency
	}
	got := RepairToolPairing(msgs)
	if len(got) != 4 {
		t.Fatalf("want inserted result message, got %d messages", len(got))
	}
	if got[2].Role != "user" {
		t.Fatalf("inserted message role = %s, want user", got[2].Role)
	}
	blocks := pairingBlocks(t, got[2])
	if len(blocks) != 2 {
		t.Fatalf("synthetic results = %d, want 2", len(blocks))
	}
	seen := map[string]bool{}
	for _, b := range blocks {
		if b.Type != "tool_result" || !b.IsError || b.Content == "" {
			t.Fatalf("synthetic block malformed: %+v", b)
		}
		seen[b.ToolUseID] = true
	}
	if !seen["web:24"] || !seen["web:25"] {
		t.Fatalf("synthetic ids = %v", seen)
	}
}

func TestRepairToolPairing_DropsReverseOrphanResults(t *testing.T) {
	// The originating assistant tool_use was pruned; its floating result must
	// go (strict backends reject a result without its use), while sibling
	// text blocks survive.
	msgs := []Message{
		blocksMsg("user", toolResult("gone:1", "고아 결과"), ContentBlock{Type: "text", Text: "그리고 질문"}),
		NewTextMessage("assistant", "답"),
	}
	got := RepairToolPairing(msgs)
	if len(got) != 2 {
		t.Fatalf("messages = %d, want 2", len(got))
	}
	blocks := pairingBlocks(t, got[0])
	if len(blocks) != 1 || blocks[0].Type != "text" || blocks[0].Text != "그리고 질문" {
		t.Fatalf("kept blocks = %+v", blocks)
	}
}

func TestRepairToolPairing_ResultBeforeUseIsOrphanBothWays(t *testing.T) {
	// A result that PRECEDES its use satisfies no strict backend: the early
	// result is dropped and the later use gets a synthetic result.
	msgs := []Message{
		blocksMsg("user", toolResult("x:1", "이른 결과")),
		blocksMsg("assistant", toolUse("x:1", "web")),
		NewTextMessage("user", "다음"),
	}
	got := RepairToolPairing(msgs)
	// The stripped-empty user message is dropped entirely:
	// assistant, synthetic user, user(다음)
	if len(got) != 3 {
		t.Fatalf("messages = %d, want 3", len(got))
	}
	if got[0].Role != "assistant" {
		t.Fatalf("first message role = %s, want assistant (empty user dropped)", got[0].Role)
	}
	synth := pairingBlocks(t, got[1])
	if len(synth) != 1 || synth[0].ToolUseID != "x:1" {
		t.Fatalf("synthetic = %+v", synth)
	}
}

func TestRepairToolPairing_ThenNormalizeMergesInsertedMessage(t *testing.T) {
	// The synthetic result message lands before an existing user message;
	// NormalizeMessages must merge them into one strict-legal user turn.
	msgs := []Message{
		blocksMsg("assistant", toolUse("a:1", "wiki"), toolUse("a:2", "wiki")),
		blocksMsg("user", toolResult("a:2", "실결과")),
	}
	got := NormalizeMessages(RepairToolPairing(msgs))
	if len(got) != 2 {
		t.Fatalf("messages = %d, want 2 after merge", len(got))
	}
	blocks := pairingBlocks(t, got[1])
	if len(blocks) != 2 {
		t.Fatalf("merged result blocks = %d, want 2", len(blocks))
	}
	ids := map[string]bool{}
	for _, b := range blocks {
		ids[b.ToolUseID] = true
	}
	if !ids["a:1"] || !ids["a:2"] {
		t.Fatalf("merged ids = %v", ids)
	}
}
