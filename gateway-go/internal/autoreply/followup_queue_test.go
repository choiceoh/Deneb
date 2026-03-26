package autoreply

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFollowupQueueState_EnqueueAndDrain(t *testing.T) {
	var drained atomic.Int32
	var drainedItems []QueueItem
	var mu sync.Mutex

	q := NewFollowupQueueState("session-1", FollowupQueueConfig{
		MaxItems:   10,
		DebounceMs: 50, // short debounce for testing
	})

	drainFn := func(ctx context.Context, sessionKey string, items []QueueItem) {
		mu.Lock()
		drainedItems = append(drainedItems, items...)
		mu.Unlock()
		drained.Add(1)
	}

	// Enqueue two items quickly.
	q.Enqueue(QueueItem{Text: "msg1"}, drainFn)
	q.Enqueue(QueueItem{Text: "msg2"}, drainFn)

	// Wait for debounce.
	time.Sleep(150 * time.Millisecond)

	if drained.Load() != 1 {
		t.Errorf("expected 1 drain, got %d", drained.Load())
	}
	mu.Lock()
	if len(drainedItems) != 2 {
		t.Errorf("expected 2 drained items, got %d", len(drainedItems))
	}
	mu.Unlock()
}

func TestFollowupQueueState_CapEnforcement(t *testing.T) {
	q := NewFollowupQueueState("session-1", FollowupQueueConfig{
		MaxItems:   3,
		DebounceMs: 1000, // long debounce
	})

	noop := func(ctx context.Context, sessionKey string, items []QueueItem) {}

	q.Enqueue(QueueItem{Text: "msg1"}, noop)
	q.Enqueue(QueueItem{Text: "msg2"}, noop)
	q.Enqueue(QueueItem{Text: "msg3"}, noop)
	q.Enqueue(QueueItem{Text: "msg4"}, noop) // should drop msg1

	if q.Len() != 3 {
		t.Errorf("expected len=3 after overflow, got %d", q.Len())
	}

	q.Cancel()
}

func TestFollowupQueueState_Cancel(t *testing.T) {
	q := NewFollowupQueueState("session-1", FollowupQueueConfig{
		MaxItems:   10,
		DebounceMs: 500,
	})

	var drained atomic.Int32
	drainFn := func(ctx context.Context, sessionKey string, items []QueueItem) {
		drained.Add(1)
	}

	q.Enqueue(QueueItem{Text: "msg1"}, drainFn)
	q.Cancel()

	time.Sleep(100 * time.Millisecond)

	if q.Len() != 0 {
		t.Errorf("expected len=0 after cancel, got %d", q.Len())
	}
}

func TestFollowupQueueState_ConcurrentEnqueue(t *testing.T) {
	q := NewFollowupQueueState("session-1", FollowupQueueConfig{
		MaxItems:   100,
		DebounceMs: 100,
	})

	var wg sync.WaitGroup
	noop := func(ctx context.Context, sessionKey string, items []QueueItem) {}

	// Enqueue from 10 goroutines concurrently.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				q.Enqueue(QueueItem{Text: "msg"}, noop)
			}
		}(i)
	}

	wg.Wait()
	// Should not panic or deadlock. Cancel to clean up.
	q.Cancel()
}

func TestFollowupQueueRegistry_GetOrCreate(t *testing.T) {
	r := NewFollowupQueueRegistry(DefaultFollowupQueueConfig())

	q1 := r.GetOrCreate("session-1")
	q2 := r.GetOrCreate("session-1")

	if q1 != q2 {
		t.Error("expected same queue for same session key")
	}

	q3 := r.GetOrCreate("session-2")
	if q1 == q3 {
		t.Error("expected different queues for different sessions")
	}

	if r.Count() != 2 {
		t.Errorf("expected count=2, got %d", r.Count())
	}
}

func TestFollowupQueueRegistry_Remove(t *testing.T) {
	r := NewFollowupQueueRegistry(DefaultFollowupQueueConfig())

	r.GetOrCreate("session-1")
	r.Remove("session-1")

	if r.Count() != 0 {
		t.Errorf("expected count=0 after remove, got %d", r.Count())
	}
}

func TestFollowupQueueRegistry_CancelAll(t *testing.T) {
	r := NewFollowupQueueRegistry(DefaultFollowupQueueConfig())

	r.GetOrCreate("session-1")
	r.GetOrCreate("session-2")
	r.CancelAll()

	if r.Count() != 0 {
		t.Errorf("expected count=0 after CancelAll, got %d", r.Count())
	}
}
