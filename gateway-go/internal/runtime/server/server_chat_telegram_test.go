package server

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

// TestSplitTelegramTarget covers the proactive-relay path that splits
// "<chat>:thread:<id>" target strings: a forum-topic target must yield both
// parts so the relayed cron / dream / mail-poll message lands in the right
// topic instead of leaking into General.
func TestSplitTelegramTarget(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		wantChat   string
		wantThread string
	}{
		{name: "1:1 chat", target: "7074071666", wantChat: "7074071666"},
		{name: "supergroup chat", target: "-1001234567890", wantChat: "-1001234567890"},
		{name: "forum topic", target: "-1001234567890:thread:42", wantChat: "-1001234567890", wantThread: "42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotChat, gotThread := splitTelegramTarget(tt.target)
			if gotChat != tt.wantChat || gotThread != tt.wantThread {
				t.Fatalf("splitTelegramTarget(%q) = (%q, %q), want (%q, %q)",
					tt.target, gotChat, gotThread, tt.wantChat, tt.wantThread)
			}
		})
	}
}

// TestDeliveryThreadID locks in the forum-topic routing of outbound replies:
// an empty / missing ThreadID must yield 0 (Bot API "no thread"), a numeric
// string must parse, and a malformed value must degrade to 0 rather than
// surface a parse error to the user — silent fallback to General is safer
// than dropping the reply because of a corrupt delivery context.
func TestDeliveryThreadID(t *testing.T) {
	tests := []struct {
		name string
		dc   *chat.DeliveryContext
		want int64
	}{
		{name: "nil context", dc: nil, want: 0},
		{name: "empty thread", dc: &chat.DeliveryContext{}, want: 0},
		{name: "valid thread", dc: &chat.DeliveryContext{ThreadID: "42"}, want: 42},
		{name: "malformed thread", dc: &chat.DeliveryContext{ThreadID: "not-a-number"}, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deliveryThreadID(tt.dc); got != tt.want {
				t.Fatalf("deliveryThreadID = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestTelegramDedupKey_DifferentLongMessagesCollide covers the regression
// where the dedup key was a fixed-length byte prefix of the reply text. Two
// different long replies that shared an opening header (common for dashboard
// embeds, report skeletons, error envelopes) collapsed into the same bucket
// and the second one was silently dropped for the full 10-second TTL.
// A content hash makes the key independent of length and shared prefix.
func TestTelegramDedupKey_DifferentLongMessagesCollide(t *testing.T) {
	commonHeader := strings.Repeat("# 대시보드\n\n---\n\n", 20)
	a := commonHeader + "첫 번째 본문: 서비스 상태 요약입니다."
	b := commonHeader + "두 번째 본문: 장애 발생 보고입니다."

	keyA := telegramDedupKey("chat-1", a)
	keyB := telegramDedupKey("chat-1", b)
	if keyA == keyB {
		t.Fatalf("expected distinct keys for different long bodies sharing a prefix, got %q", keyA)
	}
}

// TestTelegramDedupKey_Stable verifies that the same (chat, text) always
// hashes to the same key so legitimate duplicate suppression keeps working.
func TestTelegramDedupKey_Stable(t *testing.T) {
	text := "안녕하세요, 대시보드 업데이트입니다."
	if telegramDedupKey("chat-1", text) != telegramDedupKey("chat-1", text) {
		t.Fatalf("dedup key should be deterministic")
	}
}

// TestTelegramDedupKey_ChatScoped ensures the chat ID is part of the key so
// the same text to two different chats is NOT considered a duplicate.
func TestTelegramDedupKey_ChatScoped(t *testing.T) {
	text := "동일 메시지"
	if telegramDedupKey("chat-1", text) == telegramDedupKey("chat-2", text) {
		t.Fatalf("dedup key should be scoped by chat ID")
	}
}

func TestResolveTelegramSecretRefs_BotTokenRefWins(t *testing.T) {
	cfg := &telegram.Config{
		BotToken:    "plaintext-token",
		BotTokenRef: "op://Deneb/Telegram/bot_token",
	}
	got := resolveTelegramSecretRefsWith(
		context.Background(),
		cfg,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(_ context.Context, ref string) (string, error) {
			if ref != "op://Deneb/Telegram/bot_token" {
				t.Fatalf("ref = %q", ref)
			}
			return "resolved-token\n", nil
		},
	)
	if got.BotToken != "resolved-token" {
		t.Fatalf("BotToken = %q, want resolved-token", got.BotToken)
	}
}
