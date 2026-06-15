package chat

import (
	"strings"
	"testing"
)

func TestBuildHindsightRetainItem(t *testing.T) {
	item, ok := buildHindsightRetainItem(
		RunParams{
			SessionKey: "telegram:1",
			Message:    "회상 개선 계속하자",
			Delivery:   &DeliveryContext{Channel: "telegram"},
		},
		"이어서 진행하겠습니다",
	)
	if !ok {
		t.Fatal("expected a retain item for a complete turn")
	}
	if !strings.Contains(item.Content, "User: 회상 개선 계속하자") {
		t.Fatalf("user message missing from content: %q", item.Content)
	}
	if !strings.Contains(item.Content, "Assistant: 이어서 진행하겠습니다") {
		t.Fatalf("assistant reply missing from content: %q", item.Content)
	}
	if item.DocumentID != "telegram:1" {
		t.Fatalf("document_id should be the session key, got %q", item.DocumentID)
	}
	if item.Metadata["channel"] != "telegram" {
		t.Fatalf("channel metadata missing: %v", item.Metadata)
	}
	if item.Metadata["source"] != "deneb" {
		t.Fatalf("source metadata missing: %v", item.Metadata)
	}
}

func TestBuildHindsightRetainItemSkipsEmptyTurns(t *testing.T) {
	if _, ok := buildHindsightRetainItem(RunParams{Message: "   "}, "reply"); ok {
		t.Fatal("blank user message should not produce a retain item")
	}
	if _, ok := buildHindsightRetainItem(RunParams{Message: "hi"}, "   "); ok {
		t.Fatal("blank assistant reply should not produce a retain item")
	}
}

// TestBuildHindsightRetainItemSkipsFocusedChat confirms the memory-off toggle is
// symmetric: a SkipRecall turn that would otherwise be storable produces no
// retain item, so focused chat does not pollute the work-memory bank.
func TestBuildHindsightRetainItemSkipsFocusedChat(t *testing.T) {
	if _, ok := buildHindsightRetainItem(RunParams{Message: "데코레이터가 뭐야", SkipRecall: true}, "데코레이터는 함수를 감싸는 함수입니다"); ok {
		t.Fatal("SkipRecall (focused chat) turn must not produce a retain item")
	}
	// Sanity: the same turn without SkipRecall IS storable.
	if _, ok := buildHindsightRetainItem(RunParams{Message: "데코레이터가 뭐야"}, "데코레이터는 함수를 감싸는 함수입니다"); !ok {
		t.Fatal("non-focused turn should produce a retain item")
	}

	// chat: session is gated on the session key, not just the flag: even with
	// SkipRecall unset, a 챗봇 turn must not be written to the 업무 memory bank.
	if _, ok := buildHindsightRetainItem(RunParams{SessionKey: "chat:main", Message: "데코레이터가 뭐야"}, "함수를 감싸는 함수"); ok {
		t.Fatal("chat: session turn must not produce a retain item (session-key gate)")
	}
	// 업무 (client:) session with the flag unset IS storable.
	if _, ok := buildHindsightRetainItem(RunParams{SessionKey: "client:main", Message: "탑솔라 견적"}, "정리했습니다"); !ok {
		t.Fatal("client: session turn should produce a retain item")
	}
}

func TestRetainTurnToHindsightNilClientNoPanic(t *testing.T) {
	// A nil client (Hindsight unconfigured) must be a safe no-op.
	retainTurnToHindsight(nil, RunParams{SessionKey: "telegram:1", Message: "hi"}, "hello", nil)
}
