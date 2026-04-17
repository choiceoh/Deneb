package server

import (
	"strings"
	"testing"
)

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
