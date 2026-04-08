package web

import (
	"testing"
	"time"
)

func TestFetchCache_HitMiss(t *testing.T) {
	c := NewFetchCacheWithTTL(8, time.Minute)

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

func TestFetchCache_TTLExpiry(t *testing.T) {
	c := NewFetchCacheWithTTL(8, 10*time.Millisecond)

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
	c := NewFetchCacheWithTTL(3, time.Minute)

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
	c.Put("https://d.com", "d")
	if _, ok := c.Get("https://a.com"); ok {
		t.Fatal("expected a.com to be evicted")
	}
	if _, ok := c.Get("https://d.com"); !ok {
		t.Fatal("expected hit for d.com")
	}
}

func TestFetchCache_UpdateExisting(t *testing.T) {
	c := NewFetchCacheWithTTL(3, time.Minute)

	c.Put("https://a.com", "v1")
	c.Put("https://a.com", "v2")

	got, ok := c.Get("https://a.com")
	if !ok || got != "v2" {
		t.Fatalf("got %q, want updated value 'v2'", got)
	}
}
