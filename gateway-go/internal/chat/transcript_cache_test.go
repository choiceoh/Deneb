package chat

import (
	"testing"
	"time"
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

	inner.Append("s1", ChatMessage{Role: "user", Content: "hello"})

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

	inner.Append("s1", ChatMessage{Role: "user", Content: "first"})

	// Prime cache.
	cache.Load("s1", 0)

	// Append via cache (write-through).
	cache.Append("s1", ChatMessage{Role: "assistant", Content: "second"})

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

	inner.Append("s1", ChatMessage{Role: "user", Content: "msg"})
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

func TestCachedTranscriptStore_TTLExpiry(t *testing.T) {
	inner := newCountingStore()
	cache := NewCachedTranscriptStore(inner, 1*time.Millisecond)

	inner.Append("s1", ChatMessage{Role: "user", Content: "msg"})
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

	for i := 0; i < 10; i++ {
		inner.Append("s1", ChatMessage{Role: "user", Content: "msg"})
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

	inner.Append("s1", ChatMessage{Role: "user", Content: "original"})
	msgs, _, _ := cache.Load("s1", 0)

	// Mutate returned slice.
	msgs[0].Content = "MUTATED"

	// Reload — should still see original.
	msgs2, _, _ := cache.Load("s1", 0)
	if msgs2[0].Content != "original" {
		t.Fatalf("cache was corrupted: got %q, want %q", msgs2[0].Content, "original")
	}
}
