package queue

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"testing"
	"time"
)

func TestRecentMessageIDCache_PeekAndCheck(t *testing.T) {
	cache := NewRecentMessageIDCache()

	// Not present initially.
	if cache.peek("key1") {
		t.Error("expected key1 not found initially")
	}

	// Add key.
	cache.check("key1")

	// Now present.
	if !cache.peek("key1") {
		t.Error("expected key1 found after check")
	}
}

func TestRecentMessageIDCache_Clear(t *testing.T) {
	cache := NewRecentMessageIDCache()
	cache.check("key1")
	cache.check("key2")
	cache.clear()

	if cache.peek("key1") {
		t.Error("expected key1 not found after clear")
	}
	if cache.peek("key2") {
		t.Error("expected key2 not found after clear")
	}
}

func TestBuildRecentMessageIDKey(t *testing.T) {
	run := types.FollowupRun{
		MessageID:            "msg123",
		OriginatingChannel:   "telegram",
		OriginatingTo:        "bot1",
		OriginatingAccountID: "acc1",
		OriginatingThreadID:  "thread1",
	}

	key := buildRecentMessageIDKey(run, "session:main")
	if key == "" {
		t.Fatal("expected non-empty key")
	}
	// Should contain all parts.
	expected := "queue|session:main|telegram|bot1|acc1|thread1|msg123"
	if key != expected {
		t.Errorf("got %q, want %q", key, expected)
	}
}

func TestBuildRecentMessageIDKey_EmptyMessageID(t *testing.T) {
	run := types.FollowupRun{
		MessageID:          "",
		OriginatingChannel: "telegram",
	}
	key := buildRecentMessageIDKey(run, "session:main")
	if key != "" {
		t.Errorf("expected empty key for empty messageID, got %q", key)
	}
}

func TestBuildRecentMessageIDKey_WhitespaceMessageID(t *testing.T) {
	run := types.FollowupRun{
		MessageID:          "  ",
		OriginatingChannel: "telegram",
	}
	key := buildRecentMessageIDKey(run, "session:main")
	if key != "" {
		t.Errorf("expected empty key for whitespace messageID, got %q", key)
	}
}

func TestIsRunAlreadyQueued_ByMessageID(t *testing.T) {
	run := types.FollowupRun{
		MessageID:          "msg1",
		OriginatingChannel: "telegram",
		OriginatingTo:      "bot",
	}
	items := []types.FollowupRun{
		{MessageID: "msg1", OriginatingChannel: "telegram", OriginatingTo: "bot"},
	}

	if !isRunAlreadyQueued(run, items, false) {
		t.Error("expected run to be detected as queued by messageID")
	}
}

func TestIsRunAlreadyQueued_DifferentRouting(t *testing.T) {
	run := types.FollowupRun{
		MessageID:          "msg1",
		OriginatingChannel: "telegram",
		OriginatingTo:      "bot1",
	}
	items := []types.FollowupRun{
		{MessageID: "msg1", OriginatingChannel: "telegram", OriginatingTo: "bot2"},
	}

	if isRunAlreadyQueued(run, items, false) {
		t.Error("expected run not queued with different routing")
	}
}

func TestIsRunAlreadyQueued_ByPromptFallback(t *testing.T) {
	run := types.FollowupRun{
		Prompt:             "hello",
		OriginatingChannel: "telegram",
		OriginatingTo:      "bot",
	}
	items := []types.FollowupRun{
		{Prompt: "hello", OriginatingChannel: "telegram", OriginatingTo: "bot"},
	}

	// Without fallback.
	if isRunAlreadyQueued(run, items, false) {
		t.Error("expected not queued without prompt fallback")
	}

	// With fallback.
	if !isRunAlreadyQueued(run, items, true) {
		t.Error("expected queued with prompt fallback")
	}
}

func TestIsRunAlreadyQueued_EmptyItems(t *testing.T) {
	run := types.FollowupRun{MessageID: "msg1"}
	if isRunAlreadyQueued(run, nil, false) {
		t.Error("expected not queued in empty items")
	}
}

func TestRecentMessageIDCache_Expiry(t *testing.T) {
	cache := NewRecentMessageIDCache()

	// Manually insert an expired entry.
	cache.mu.Lock()
	cache.entries["expired"] = dedupeEntry{seenAt: time.Now().Add(-recentMessageIDTTL - time.Second)}
	cache.mu.Unlock()

	if cache.peek("expired") {
		t.Error("expected expired entry to not be found")
	}
}
