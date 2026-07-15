package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// history returns a minimal two-turn transcript history for append tests.
func appendTestHistory() []llm.Message {
	return []llm.Message{
		llm.NewTextMessage("user", "[2026-07-05T10:00:00+09:00] 이전 질문"),
		llm.NewTextMessage("assistant", "이전 답변"),
	}
}

func messagesContain(msgs []llm.Message, needle string) bool {
	for _, m := range msgs {
		if strings.Contains(m.Content.String(), needle) {
			return true
		}
	}
	return false
}

// AppendCurrentMessage (deferred-enrichment / ephemeral path) must append the
// current message as a NEW last user message after the loaded history.
func TestAssembleTurnMessagesAppendCurrentMessageCreatesNewUserTurn(t *testing.T) {
	params := runParams{
		SessionKey:           "client:main",
		Message:              "[2026-07-05T10:05:00+09:00] 링크 요약해줘",
		AppendCurrentMessage: true,
	}
	prep := prepResult{Messages: appendTestHistory()}
	msgs := assembleTurnMessages(context.Background(), params, runDeps{}, prep, modelResolution{})

	if len(msgs) != 3 {
		t.Fatalf("expected history+1 messages, got %d", len(msgs))
	}
	last := msgs[len(msgs)-1]
	if last.Role != "user" || !strings.Contains(last.Content.String(), "링크 요약해줘") {
		t.Fatalf("last message must be the appended current user message, got role=%s content=%s", last.Role, last.Content)
	}
	// The previous turn's user message must be untouched (the replace-last
	// attachment branch corrupts it if the switch arms are misordered).
	if !messagesContain(msgs[:2], "이전 질문") {
		t.Fatal("history must be untouched")
	}
}

// AppendCurrentMessage + attachments must APPEND a multimodal message, not
// replace the previous turn's last user message.
func TestAssembleTurnMessages_AppendCurrentMessageWithAttachments(t *testing.T) {
	params := runParams{
		SessionKey:           "client:main",
		Message:              "[2026-07-05T10:05:00+09:00] 스크린샷이랑 링크 봐줘",
		AppendCurrentMessage: true,
		Attachments: []ChatAttachment{
			{Type: "image", MimeType: "image/png", Data: "aGVsbG8="},
		},
	}
	prep := prepResult{Messages: appendTestHistory()}
	msgs := assembleTurnMessages(context.Background(), params, runDeps{}, prep, modelResolution{})

	if len(msgs) != 3 {
		t.Fatalf("expected history+1 messages, got %d", len(msgs))
	}
	if !messagesContain(msgs[:2], "이전 질문") {
		t.Fatal("previous user message must not be replaced by the attachment merge")
	}
	last := msgs[len(msgs)-1]
	if last.Role != "user" || !strings.Contains(last.Content.String(), "스크린샷이랑 링크 봐줘") {
		t.Fatalf("last message must carry the current text, got: %s", last.Content)
	}
	if !strings.Contains(last.Content.String(), "image") {
		t.Fatalf("last message must be multimodal (attachment blocks), got: %s", last.Content)
	}
}

// Without the flag, a persisted turn keeps the exact legacy behavior: the
// message is expected to already be in history and nothing is appended.
func TestAssembleTurnMessagesPersistedTurnPreservesHistory(t *testing.T) {
	params := runParams{SessionKey: "client:main", Message: "이전 질문"}
	prep := prepResult{Messages: appendTestHistory()}
	msgs := assembleTurnMessages(context.Background(), params, runDeps{}, prep, modelResolution{})
	if len(msgs) != 2 {
		t.Fatalf("persisted turn must not append, got %d messages", len(msgs))
	}
}
