package chat

import (
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
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
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || total != 1 {
		t.Fatalf("expected 1 message, got %d (total=%d)", len(msgs), total)
	}
	if inner.loadCount != 1 {
		t.Fatalf("expected 1 inner load, got %d", inner.loadCount)
	}

	// Second load: cache hit.
	msgs, total, err = cache.Load("s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || total != 1 {
		t.Fatalf("expected 1 message on cache hit, got %d", len(msgs))
	}
	if inner.loadCount != 1 {
		t.Fatalf("expected still 1 inner load, got %d", inner.loadCount)
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
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || total != 2 {
		t.Fatalf("expected 2 messages after write-through, got %d (total=%d)", len(msgs), total)
	}
	if inner.loadCount != 1 {
		t.Fatalf("expected 1 inner load, got %d", inner.loadCount)
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
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 || total != 0 {
		t.Fatalf("expected empty after delete, got %d messages", len(msgs))
	}
	if inner.loadCount != 2 {
		t.Fatalf("expected 2 inner loads (initial + post-delete), got %d", inner.loadCount)
	}
}

func TestCachedTranscriptStore_AppendBeforeLoad(t *testing.T) {
	inner := newCountingStore()
	cache := NewCachedTranscriptStore(inner, 5*time.Second)

	// Append via cache before any Load call.
	cache.Append("s1", NewTextChatMessage("user", "first", 0))

	// Load must be served from the seeded cache entry — no inner Load call.
	msgs, total, err := cache.Load("s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || total != 1 {
		t.Fatalf("expected 1 message after append-seed, got %d (total=%d)", len(msgs), total)
	}
	if inner.loadCount != 0 {
		t.Fatalf("expected 0 inner loads (cache should be seeded by Append), got %d", inner.loadCount)
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
		t.Fatalf("expected 2 inner loads after TTL expiry, got %d", inner.loadCount)
	}
}

func TestCachedTranscriptStore_LimitSlicing(t *testing.T) {
	inner := newCountingStore()
	cache := NewCachedTranscriptStore(inner, 5*time.Second)

	for range 10 {
		inner.Append("s1", NewTextChatMessage("user", "msg", 0))
	}

	msgs, total, err := cache.Load("s1", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages with limit, got %d", len(msgs))
	}
	if total != 10 {
		t.Fatalf("expected total=10, got %d", total)
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
