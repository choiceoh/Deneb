package web

import (
	"testing"
	"time"
)

func TestFetchCacheReturnsHitOrMiss(t *testing.T) {
	c := newFetchCacheWithTTL(8, time.Minute)

	// Miss on empty cache.
	if _, ok := c.Get("https://example.com"); ok {
		t.Fatal("expected miss on empty cache")
	}

	c.Put("https://example.com", "hello")
	got, ok := c.Get("https://example.com")
	if !ok || got != "hello" {
		t.Fatalf("got %q ok=%v, want hit with 'hello'", got, ok)
	}

	// Different key is a miss.
	if _, ok := c.Get("https://other.com"); ok {
		t.Fatal("expected miss for different key")
	}
}

func TestFetchCacheExpiresAfterTTL(t *testing.T) {
	c := newFetchCacheWithTTL(8, 10*time.Millisecond)

	c.Put("https://example.com", "data")
	if _, ok := c.Get("https://example.com"); !ok {
		t.Fatal("expected hit before TTL")
	}

	time.Sleep(20 * time.Millisecond)
	if _, ok := c.Get("https://example.com"); ok {
		t.Fatal("expected miss after TTL expiry")
	}
}

func TestFetchCache_Eviction(t *testing.T) {
	c := newFetchCacheWithTTL(3, time.Minute)

	c.Put("https://a.com", "a")
	c.Put("https://b.com", "b")
	c.Put("https://c.com", "c")

	// All three should be present.
	for _, url := range []string{"https://a.com", "https://b.com", "https://c.com"} {
		if _, ok := c.Get(url); !ok {
			t.Fatalf("expected hit for %s", url)
		}
	}

	// Adding a 4th should evict the oldest (a.com).
	// Note: Gets above promote each key; final order is still a,b,c (oldest=a).
	c.Put("https://d.com", "d")
	if _, ok := c.Get("https://a.com"); ok {
		t.Fatal("expected a.com to be evicted")
	}
	if _, ok := c.Get("https://d.com"); !ok {
		t.Fatal("expected hit for d.com")
	}
}

func TestFetchCache_GetPromotesLRU(t *testing.T) {
	c := newFetchCacheWithTTL(3, time.Minute)
	c.Put("https://a.com", "a")
	c.Put("https://b.com", "b")
	c.Put("https://c.com", "c")

	// Touch a so it becomes newest; b is then the oldest.
	if _, ok := c.Get("https://a.com"); !ok {
		t.Fatal("expected hit for a.com")
	}
	c.Put("https://d.com", "d")
	if _, ok := c.Get("https://b.com"); ok {
		t.Fatal("expected b.com to be evicted after a was promoted")
	}
	if _, ok := c.Get("https://a.com"); !ok {
		t.Fatal("expected a.com to survive after promote-on-Get")
	}
}
