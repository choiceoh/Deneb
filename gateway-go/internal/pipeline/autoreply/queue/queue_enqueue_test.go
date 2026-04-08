package queue

import (
	"fmt"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"testing"
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


func TestRecentMessageIDCache_CapacityEviction(t *testing.T) {
	cache := NewRecentMessageIDCache()

	// Fill to capacity.
	for i := 0; i < recentMessageIDMaxSize; i++ {
		cache.check(fmt.Sprintf("key%d", i))
	}

	// The first key should still be present (at capacity, not over).
	if !cache.peek("key0") {
		t.Error("expected key0 found at capacity")
	}

	// Adding one more triggers a clear, so old keys disappear.
	cache.check("overflow")
	if cache.peek("key0") {
		t.Error("expected key0 evicted after overflow")
	}
	if !cache.peek("overflow") {
		t.Error("expected overflow key present after reset")
	}
}
