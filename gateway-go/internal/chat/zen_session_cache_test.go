package chat

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

func TestSessionCache_PromptHitMiss(t *testing.T) {
	sc := NewSessionCache()
	key := PromptCacheKey("telegram", 10, "/workspace", "anthropic")

	// Miss on empty cache.
	if _, ok := sc.GetPrompt(key); ok {
		t.Fatal("expected miss on empty cache")
	}

	// Set and hit.
	prompt := json.RawMessage(`"hello"`)
	sc.SetPrompt(key, prompt)

	got, ok := sc.GetPrompt(key)
	if !ok {
		t.Fatal("expected hit after set")
	}
	if string(got) != string(prompt) {
		t.Fatalf("got %s, want %s", got, prompt)
	}

	// Different key misses.
	other := PromptCacheKey("discord", 10, "/workspace", "anthropic")
	if _, ok := sc.GetPrompt(other); ok {
		t.Fatal("expected miss for different key")
	}
}

func TestSessionCache_PromptTTLExpiry(t *testing.T) {
	sc := NewSessionCache()
	key := "test-ttl"

	sc.mu.Lock()
	sc.prompts[key] = &promptCacheEntry{
		prompt:    json.RawMessage(`"expired"`),
		expiresAt: time.Now().Add(-1 * time.Second),
	}
	sc.mu.Unlock()

	if _, ok := sc.GetPrompt(key); ok {
		t.Fatal("expected miss for expired entry")
	}
}

func TestSessionCache_ContextMessageCountInvalidation(t *testing.T) {
	sc := NewSessionCache()

	msgs := []llm.Message{
		llm.NewTextMessage("user", "hello"),
		llm.NewTextMessage("assistant", "hi"),
	}
	sc.SetContext("session-1", msgs, 2)

	// Hit with same count.
	got, ok := sc.GetContext("session-1", 2)
	if !ok {
		t.Fatal("expected hit with matching count")
	}
	if len(got) != 2 {
		t.Fatalf("got %d messages, want 2", len(got))
	}

	// Miss with different count (new messages added).
	if _, ok := sc.GetContext("session-1", 3); ok {
		t.Fatal("expected miss when message count differs")
	}
}

func TestSessionCache_ContextCopyIsolation(t *testing.T) {
	sc := NewSessionCache()

	msgs := []llm.Message{llm.NewTextMessage("user", "hello")}
	sc.SetContext("session-1", msgs, 1)

	got, ok := sc.GetContext("session-1", 1)
	if !ok {
		t.Fatal("expected hit")
	}

	// Mutate returned slice — should not affect cache.
	got[0] = llm.NewTextMessage("user", "mutated")

	got2, ok := sc.GetContext("session-1", 1)
	if !ok {
		t.Fatal("expected hit on second get")
	}
	if string(got2[0].Content) == string(got[0].Content) {
		t.Fatal("cache should be isolated from caller mutations")
	}
}

func TestSessionCache_InvalidateContext(t *testing.T) {
	sc := NewSessionCache()

	msgs := []llm.Message{llm.NewTextMessage("user", "hello")}
	sc.SetContext("session-1", msgs, 1)

	sc.InvalidateContext("session-1")

	if _, ok := sc.GetContext("session-1", 1); ok {
		t.Fatal("expected miss after invalidation")
	}
}

func TestSessionCache_InvalidateAllPrompts(t *testing.T) {
	sc := NewSessionCache()

	sc.SetPrompt("key1", json.RawMessage(`"a"`))
	sc.SetPrompt("key2", json.RawMessage(`"b"`))

	sc.InvalidateAllPrompts()

	if _, ok := sc.GetPrompt("key1"); ok {
		t.Fatal("expected miss after invalidate all")
	}
	if _, ok := sc.GetPrompt("key2"); ok {
		t.Fatal("expected miss after invalidate all")
	}
}
