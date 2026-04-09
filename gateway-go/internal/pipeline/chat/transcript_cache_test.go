package chat

import (
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// countingStore wraps MemoryTranscriptStore and counts Load calls.
type countingStore struct {
	*MemoryTranscriptStore
	loadCount int
}

func newCountingStore() *countingStore {
	return &countingStore{MemoryTranscriptStore: NewMemoryTranscriptStore()}
}

func (s *countingStore) Load(sessionKey string, limit int) ([]ChatMessage, int, error) {
	s.loadCount++
	return s.MemoryTranscriptStore.Load(sessionKey, limit)
}

func TestCachedTranscriptStore_CacheHit(t *testing.T) {
	inner := newCountingStore()
	cache := NewCachedTranscriptStore(inner, 5*time.Second)

	inner.Append("s1", NewTextChatMessage("user", "hello", 0))

	// First load: cache miss.
	msgs, total, err := cache.Load("s1", 0)
	testutil.NoError(t, err)
	if len(msgs) != 1 || total != 1 {
		t.Fatalf("got %d (total=%d), want 1 message", len(msgs), total)
	}
	if inner.loadCount != 1 {
		t.Fatalf("got %d, want 1 inner load", inner.loadCount)
	}

	// Second load: cache hit.
	msgs, total, err = cache.Load("s1", 0)
	testutil.NoError(t, err)
	if len(msgs) != 1 || total != 1 {
		t.Fatalf("got %d, want 1 message on cache hit", len(msgs))
	}
	if inner.loadCount != 1 {
		t.Fatalf("got %d, want still 1 inner load", inner.loadCount)
	}
}

func TestCachedTranscriptStore_WriteThrough(t *testing.T) {
	inner := newCountingStore()
	cache := NewCachedTranscriptStore(inner, 5*time.Second)

	inner.Append("s1", NewTextChatMessage("user", "first", 0))

	// Prime cache.
	cache.Load("s1", 0)

	// Append via cache (write-through).
	cache.Append("s1", NewTextChatMessage("assistant", "second", 0))

	// Load should return 2 messages without hitting inner store again.
	msgs, total, err := cache.Load("s1", 0)
	testutil.NoError(t, err)
	if len(msgs) != 2 || total != 2 {
		t.Fatalf("got %d (total=%d), want 2 messages after write-through", len(msgs), total)
	}
	if inner.loadCount != 1 {
		t.Fatalf("got %d, want 1 inner load", inner.loadCount)
	}
}

func TestCachedTranscriptStore_InvalidateOnDelete(t *testing.T) {
	inner := newCountingStore()
	cache := NewCachedTranscriptStore(inner, 5*time.Second)

	inner.Append("s1", NewTextChatMessage("user", "msg", 0))
	cache.Load("s1", 0)

	cache.Delete("s1")

	// After delete, cache should be cleared; load returns empty.
	msgs, total, err := cache.Load("s1", 0)
	testutil.NoError(t, err)
	if len(msgs) != 0 || total != 0 {
		t.Fatalf("got %d messages, want empty after delete", len(msgs))
	}
	if inner.loadCount != 2 {
		t.Fatalf("got %d, want 2 inner loads (initial + post-delete)", inner.loadCount)
	}
}

func TestCachedTranscriptStore_TTLExpiry(t *testing.T) {
	inner := newCountingStore()
	cache := NewCachedTranscriptStore(inner, 1*time.Millisecond)

	inner.Append("s1", NewTextChatMessage("user", "msg", 0))
	cache.Load("s1", 0)
	time.Sleep(5 * time.Millisecond)

	// After TTL, should re-read from inner.
	cache.Load("s1", 0)
	if inner.loadCount != 2 {
		t.Fatalf("got %d, want 2 inner loads after TTL expiry", inner.loadCount)
	}
}

func TestCachedTranscriptStore_LimitSlicing(t *testing.T) {
	inner := newCountingStore()
	cache := NewCachedTranscriptStore(inner, 5*time.Second)

	for range 10 {
		inner.Append("s1", NewTextChatMessage("user", "msg", 0))
	}

	msgs, total, err := cache.Load("s1", 3)
	testutil.NoError(t, err)
	if len(msgs) != 3 {
		t.Fatalf("got %d, want 3 messages with limit", len(msgs))
	}
	if total != 10 {
		t.Fatalf("got %d, want total=10", total)
	}
}

func TestCachedTranscriptStore_ReturnsCopy(t *testing.T) {
	inner := newCountingStore()
	cache := NewCachedTranscriptStore(inner, 5*time.Second)

	inner.Append("s1", NewTextChatMessage("user", "original", 0))
	msgs, _, _ := cache.Load("s1", 0)

	// Mutate returned slice.
	msgs[0].Content = toolctx.MarshalJSONString("MUTATED")

	// Reload — should still see original.
	msgs2, _, _ := cache.Load("s1", 0)
	if msgs2[0].TextContent() != "original" {
		t.Fatalf("cache was corrupted: got %q, want %q", msgs2[0].TextContent(), "original")
	}
}
