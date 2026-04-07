package session

import (
	"fmt"
	"testing"
	"time"
)

func TestKeyCacheGetMiss(t *testing.T) {
	c := NewKeyCache()
	_, ok := c.Get("nonexistent")
	if ok {
		t.Error("expected miss for nonexistent key")
	}
}

func TestKeyCachePutAndGet(t *testing.T) {
	c := NewKeyCache()
	c.Put("run-1", "session-abc")

	key, ok := c.Get("run-1")
	if !ok {
		t.Fatal("expected hit")
	}
	if key != "session-abc" {
		t.Errorf("got %q, want %q", key, "session-abc")
	}
}

func TestKeyCachePutMissWithTTL(t *testing.T) {
	c := NewKeyCache()
	c.PutMiss("run-miss")

	// Should return empty string as a "known miss" within TTL.
	key, ok := c.Get("run-miss")
	if !ok {
		t.Fatal("expected known miss (hit with empty key)")
	}
	if key != "" {
		t.Errorf("expected empty key for miss, got %q", key)
	}
}

func TestKeyCacheMissExpires(t *testing.T) {
	c := NewKeyCache()

	// Manually insert an expired miss entry.
	c.mu.Lock()
	elem := c.order.PushBack("expired")
	c.items["expired"] = &keyCacheItem{
		entry: &keyCacheEntry{
			isHit:     false,
			expiresAt: time.Now().Add(-1 * time.Second),
		},
		element: elem,
	}
	c.mu.Unlock()

	_, ok := c.Get("expired")
	if ok {
		t.Error("expected expired miss to report as cache miss")
	}
}

func TestKeyCacheLRUEviction(t *testing.T) {
	c := NewKeyCache()

	// Fill cache to limit.
	for i := 0; i < KeyCacheLimit; i++ {
		c.Put(fmt.Sprintf("run-%d", i), fmt.Sprintf("key-%d", i))
	}
	if c.Len() != KeyCacheLimit {
		t.Fatalf("expected %d entries, got %d", KeyCacheLimit, c.Len())
	}

	// Adding one more should evict the oldest (run-0).
	c.Put("run-new", "key-new")
	if c.Len() != KeyCacheLimit {
		t.Fatalf("expected %d entries after eviction, got %d", KeyCacheLimit, c.Len())
	}

	// run-0 should be evicted.
	_, ok := c.Get("run-0")
	if ok {
		t.Error("expected run-0 to be evicted")
	}

	// run-new should be present.
	key, ok := c.Get("run-new")
	if !ok || key != "key-new" {
		t.Error("expected run-new to be present")
	}
}

func TestKeyCacheUpdateExisting(t *testing.T) {
	c := NewKeyCache()
	c.Put("run-1", "old-key")
	c.Put("run-1", "new-key")

	key, ok := c.Get("run-1")
	if !ok || key != "new-key" {
		t.Errorf("expected updated key, got %q", key)
	}
	if c.Len() != 1 {
		t.Errorf("expected 1 entry, got %d", c.Len())
	}
}

func TestKeyCacheClear(t *testing.T) {
	c := NewKeyCache()
	c.Put("a", "1")
	c.Put("b", "2")
	c.Clear()

	if c.Len() != 0 {
		t.Errorf("expected empty cache after clear, got %d", c.Len())
	}
}
