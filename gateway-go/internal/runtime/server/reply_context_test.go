package server

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

func TestExtractReplyContext_NoReply(t *testing.T) {
	msg := &telegram.Message{
		MessageID: 1,
		Chat:      telegram.Chat{ID: 100},
		Text:      "hello",
	}
	rc := ExtractReplyContext(msg, 999)
	if rc != nil {
		t.Fatal("expected nil for non-reply message")
	}
}

func TestExtractReplyContext_TextReply(t *testing.T) {
	msg := &telegram.Message{
		MessageID: 2,
		Chat:      telegram.Chat{ID: 100},
		Text:      "yes I agree",
		ReplyToMessage: &telegram.Message{
			MessageID: 1,
			From:      &telegram.User{ID: 42, FirstName: "Alice", LastName: "Kim"},
			Text:      "Do you agree?",
		},
	}
	rc := ExtractReplyContext(msg, 999)
	if rc == nil {
		t.Fatal("expected non-nil reply context")
	}
	if rc.ReplyToID != "tg-100-1" {
		t.Errorf("ReplyToID = %q, want %q", rc.ReplyToID, "tg-100-1")
	}
	if rc.ReplyToBody != "Do you agree?" {
		t.Errorf("ReplyToBody = %q, want %q", rc.ReplyToBody, "Do you agree?")
	}
	if rc.ReplyToSender != "Alice Kim" {
		t.Errorf("ReplyToSender = %q, want %q", rc.ReplyToSender, "Alice Kim")
	}
	if rc.IsBot {
		t.Error("expected IsBot=false for non-bot sender")
	}
}

func TestExtractReplyContext_MediaCaption(t *testing.T) {
	msg := &telegram.Message{
		MessageID: 3,
		Chat:      telegram.Chat{ID: 100},
		Text:      "nice photo",
		ReplyToMessage: &telegram.Message{
			MessageID: 2,
			From:      &telegram.User{ID: 42, FirstName: "Bob"},
			Caption:   "sunset at the beach",
		},
	}
	rc := ExtractReplyContext(msg, 999)
	if rc == nil {
		t.Fatal("expected non-nil reply context")
	}
	if rc.ReplyToBody != "sunset at the beach" {
		t.Errorf("ReplyToBody = %q, want caption text", rc.ReplyToBody)
	}
}

func TestExtractReplyContext_BotReply(t *testing.T) {
	msg := &telegram.Message{
		MessageID: 4,
		Chat:      telegram.Chat{ID: 100},
		Text:      "tell me more",
		ReplyToMessage: &telegram.Message{
			MessageID: 3,
			From:      &telegram.User{ID: 999, IsBot: true, FirstName: "Deneb"},
			Text:      "Here is some info about Go.",
		},
	}
	rc := ExtractReplyContext(msg, 999)
	if rc == nil {
		t.Fatal("expected non-nil reply context")
	}
	if !rc.IsBot {
		t.Error("expected IsBot=true for bot sender")
	}
	if rc.ReplyToSender != "Deneb" {
		t.Errorf("ReplyToSender = %q, want %q", rc.ReplyToSender, "Deneb")
	}
}

func TestExtractReplyContext_LongBodyTruncated(t *testing.T) {
	longText := strings.Repeat("가", maxReplyQuoteLen+100)
	msg := &telegram.Message{
		MessageID: 5,
		Chat:      telegram.Chat{ID: 100},
		Text:      "summary?",
		ReplyToMessage: &telegram.Message{
			MessageID: 4,
			From:      &telegram.User{ID: 42, FirstName: "Eve"},
			Text:      longText,
		},
	}
	rc := ExtractReplyContext(msg, 999)
	if rc == nil {
		t.Fatal("expected non-nil reply context")
	}
	if len(rc.ReplyToBody) > maxReplyQuoteLen+10 {
		t.Errorf("body not truncated: len=%d", len(rc.ReplyToBody))
	}
	if !strings.HasSuffix(rc.ReplyToBody, "…") {
		t.Error("expected truncation suffix '…'")
	}
}

func TestFormatReplyPrefix_WithSender(t *testing.T) {
	rc := &ReplyContext{
		ReplyToBody:   "Original message",
		ReplyToSender: "Alice",
	}
	prefix := FormatReplyPrefix(rc)
	if !strings.Contains(prefix, "[Alice에 대한 답장]") {
		t.Errorf("missing sender header, got: %q", prefix)
	}
	if !strings.Contains(prefix, "> Original message") {
		t.Errorf("missing quoted text, got: %q", prefix)
	}
}

func TestFormatReplyPrefix_MultilineQuote(t *testing.T) {
	rc := &ReplyContext{
		ReplyToBody:   "line one\nline two\nline three",
		ReplyToSender: "Bob",
	}
	prefix := FormatReplyPrefix(rc)
	if !strings.Contains(prefix, "> line one\n") {
		t.Error("missing first quoted line")
	}
	if !strings.Contains(prefix, "> line two\n") {
		t.Error("missing second quoted line")
	}
}
