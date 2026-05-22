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

func TestRetainTurnToHindsightNilClientNoPanic(t *testing.T) {
	// A nil client (Hindsight unconfigured) must be a safe no-op.
	retainTurnToHindsight(nil, RunParams{SessionKey: "telegram:1", Message: "hi"}, "hello", nil)
}
