package localai

import (
	"context"
	"sync"
	"testing"
	"time"
)


func TestResponseCache(t *testing.T) {
	cache := newResponseCache(5*time.Minute, 100)

	req := SimpleRequest("sys", "hello", 100, PriorityNormal, "test")

	// Miss.
	if _, ok := cache.Get(&req, 0); ok {
		t.Fatal("expected cache miss")
	}

	// Put + hit.
	cache.Put(&req, "world")
	text, ok := cache.Get(&req, 0)
	if !ok || text != "world" {
		t.Fatalf("got ok=%v text=%q, want cache hit with 'world'", ok, text)
	}

	// Different request = miss.
	req2 := SimpleRequest("sys", "different", 100, PriorityNormal, "test")
	if _, ok := cache.Get(&req2, 0); ok {
		t.Fatal("expected cache miss for different request")
	}
}

func TestResponseCacheExpiry(t *testing.T) {
	cache := newResponseCache(10*time.Millisecond, 100)

	req := SimpleRequest("sys", "hello", 100, PriorityNormal, "test")
	cache.Put(&req, "world")

	time.Sleep(20 * time.Millisecond)
	if _, ok := cache.Get(&req, 0); ok {
		t.Fatal("expected expired cache entry to miss")
	}
}

func TestResponseCacheEviction(t *testing.T) {
	cache := newResponseCache(5*time.Minute, 3)

	for i := range 5 {
		req := SimpleRequest("sys", string(rune('a'+i)), 100, PriorityNormal, "test")
		cache.Put(&req, "val")
	}

	if cache.Len() > 3 {
		t.Fatalf("got %d, want max 3 entries", cache.Len())
	}
}

func TestPriorityQueue(t *testing.T) {
	q := newRequestQueue()

	bg := &queueEntry{
		req:        &Request{Priority: PriorityBackground, CallerTag: "bg"},
		resultCh:   make(chan submitResult, 1),
		enqueuedAt: time.Now(),
	}
	crit := &queueEntry{
		req:        &Request{Priority: PriorityCritical, CallerTag: "crit"},
		resultCh:   make(chan submitResult, 1),
		enqueuedAt: time.Now().Add(time.Second), // enqueued later
	}

	q.Push(bg)
	q.Push(crit)

	// Critical should come out first despite being enqueued later.
	done := make(chan struct{})
	close(done) // non-blocking pop
	first := q.PopWait(done)
	if first == nil || first.req.CallerTag != "crit" {
		t.Fatalf("got %v, want critical first", first)
	}
}

func TestQueueDropOldestBackground(t *testing.T) {
	q := newRequestQueue()

	// Add 3 entries: 1 normal + 2 background.
	normal := &queueEntry{
		req:        &Request{Priority: PriorityNormal, CallerTag: "normal"},
		resultCh:   make(chan submitResult, 1),
		enqueuedAt: time.Now(),
	}
	bg1Ch := make(chan submitResult, 1)
	bg1 := &queueEntry{
		req:        &Request{Priority: PriorityBackground, CallerTag: "bg1"},
		resultCh:   bg1Ch,
		enqueuedAt: time.Now(),
	}
	bg2 := &queueEntry{
		req:        &Request{Priority: PriorityBackground, CallerTag: "bg2"},
		resultCh:   make(chan submitResult, 1),
		enqueuedAt: time.Now().Add(time.Second),
	}
	q.Push(normal)
	q.Push(bg1)
	q.Push(bg2)

	// Drop with max depth 2. Should drop bg1 (oldest background).
	dropped := q.DropOldestBackground(2)
	if !dropped {
		t.Fatal("expected a drop")
	}

	// bg1's resultCh should have an error.
	select {
	case res := <-bg1Ch:
		if res.err != ErrQueueFull {
			t.Fatalf("got %v, want ErrQueueFull", res.err)
		}
	default:
		t.Fatal("bg1 should have received error")
	}

	if q.Len() != 2 {
		t.Fatalf("got %d, want 2 remaining", q.Len())
	}
}


func TestCacheKey_DifferentMaxTokens(t *testing.T) {
	r1 := SimpleRequest("sys", "hello", 100, PriorityNormal, "test")
	r2 := SimpleRequest("sys", "hello", 200, PriorityNormal, "test")

	k1 := cacheKey(&r1)
	k2 := cacheKey(&r2)
	if k1 == k2 {
		t.Fatal("different maxTokens should produce different cache keys")
	}
}

func TestSubmit_UnhealthyRejectsBackground(t *testing.T) {
	// Create a hub with no actual local AI server.
	cfg := Config{}
	h := &Hub{
		cfg:   cfg.withDefaults(),
		queue: newRequestQueue(),
		cache: newResponseCache(0, 0),
		Stats: &HubStats{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.ctx = ctx
	h.cancel = cancel
	h.budgetCond = sync.NewCond(&h.budgetMu)
	// healthy defaults to false.

	req := SimpleRequest("sys", "test", 100, PriorityBackground, "test")
	_, err := h.Submit(context.Background(), req)
	if err != ErrUnhealthy {
		t.Fatalf("got %v, want ErrUnhealthy for background on unhealthy hub", err)
	}

	h.cancel()
}
