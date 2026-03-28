package queue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDebouncerImmediateFlush(t *testing.T) {
	var mu sync.Mutex
	var flushed [][]string

	ctx := context.Background()
	d := NewDebouncer(ctx, DebouncerConfig[string]{
		DebounceMs: 0, // immediate
		BuildKey:   func(s string) string { return "key" },
		OnFlush: func(_ context.Context, items []string) error {
			mu.Lock()
			flushed = append(flushed, items)
			mu.Unlock()
			return nil
		},
	})
	defer d.Close()

	d.Enqueue("hello")
	d.Enqueue("world")

	// With debounce=0, each item should flush immediately.
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if len(flushed) != 2 {
		t.Errorf("expected 2 flushes, got %d", len(flushed))
	}
	mu.Unlock()
}

func TestDebouncerBatching(t *testing.T) {
	var mu sync.Mutex
	var flushed [][]string

	ctx := context.Background()
	d := NewDebouncer(ctx, DebouncerConfig[string]{
		DebounceMs: 100,
		BuildKey:   func(s string) string { return "key" },
		OnFlush: func(_ context.Context, items []string) error {
			mu.Lock()
			flushed = append(flushed, items)
			mu.Unlock()
			return nil
		},
	})
	defer d.Close()

	d.Enqueue("a")
	d.Enqueue("b")
	d.Enqueue("c")

	// Should not have flushed yet.
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if len(flushed) != 0 {
		t.Errorf("expected 0 flushes before debounce, got %d", len(flushed))
	}
	mu.Unlock()

	// Wait for debounce to fire.
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	if len(flushed) != 1 {
		t.Errorf("expected 1 flush after debounce, got %d", len(flushed))
	} else if len(flushed[0]) != 3 {
		t.Errorf("expected batch of 3, got %d", len(flushed[0]))
	}
	mu.Unlock()
}

func TestDebouncerFlushKey(t *testing.T) {
	var mu sync.Mutex
	var flushed [][]string

	ctx := context.Background()
	d := NewDebouncer(ctx, DebouncerConfig[string]{
		DebounceMs: 500,
		BuildKey:   func(s string) string { return "key" },
		OnFlush: func(_ context.Context, items []string) error {
			mu.Lock()
			flushed = append(flushed, items)
			mu.Unlock()
			return nil
		},
	})
	defer d.Close()

	d.Enqueue("a")
	d.Enqueue("b")
	d.FlushKey("key")

	mu.Lock()
	if len(flushed) != 1 {
		t.Errorf("expected 1 flush after FlushKey, got %d", len(flushed))
	} else if len(flushed[0]) != 2 {
		t.Errorf("expected batch of 2, got %d", len(flushed[0]))
	}
	mu.Unlock()
}

func TestDebouncerRetryUntilSuccess(t *testing.T) {
	var attempts atomic.Int32
	var mu sync.Mutex
	var flushed [][]string

	d := NewDebouncer(context.Background(), DebouncerConfig[string]{
		DebounceMs: 5,
		BuildKey:   func(_ string) string { return "retry-key" },
		OnFlush: func(_ context.Context, items []string) error {
			attempt := attempts.Add(1)
			if attempt <= 2 {
				return errors.New("temporary failure")
			}
			mu.Lock()
			flushed = append(flushed, append([]string(nil), items...))
			mu.Unlock()
			return nil
		},
	})
	defer d.Close()

	d.Enqueue("one")
	d.Enqueue("two")

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if attempts.Load() >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := attempts.Load(); got != 3 {
		t.Fatalf("expected 3 attempts (2 retries then success), got %d", got)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(flushed) != 1 {
		t.Fatalf("expected single successful flush batch, got %d", len(flushed))
	}
	if len(flushed[0]) != 2 || flushed[0][0] != "one" || flushed[0][1] != "two" {
		t.Fatalf("unexpected flushed batch: %#v", flushed[0])
	}
}

func TestDebouncerStopsAfterMaxRetries(t *testing.T) {
	var attempts atomic.Int32
	d := NewDebouncer(context.Background(), DebouncerConfig[string]{
		DebounceMs: 5,
		BuildKey:   func(_ string) string { return "hard-fail" },
		OnFlush: func(_ context.Context, _ []string) error {
			attempts.Add(1)
			return errors.New("always failing")
		},
	})
	defer d.Close()

	d.Enqueue("x")

	deadline := time.Now().Add(800 * time.Millisecond)
	for time.Now().Before(deadline) {
		if attempts.Load() >= 4 { // initial + maxDebounceRetries(3)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := attempts.Load(); got != 4 {
		t.Fatalf("expected 4 total attempts, got %d", got)
	}

	// After max retries, queue should drop the failed batch and not keep retrying.
	time.Sleep(60 * time.Millisecond)
	if got := attempts.Load(); got != 4 {
		t.Fatalf("expected retries to stop at 4 attempts, got %d", got)
	}
}
