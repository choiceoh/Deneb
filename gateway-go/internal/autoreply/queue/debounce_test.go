package queue

import (
	"context"
	"sync"
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
