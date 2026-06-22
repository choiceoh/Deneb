package rpc

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPoolConcurrencyLimit(t *testing.T) {
	pool := NewWorkerPool(2)
	var maxConcurrent atomic.Int64
	var running atomic.Int64
	var wg sync.WaitGroup

	for range 6 {
		wg.Add(1)
		pool.Submit(func() {
			defer wg.Done()
			cur := running.Add(1)
			// Track peak concurrency
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			running.Add(-1)
		})
	}

	wg.Wait()

	if got := maxConcurrent.Load(); got > 2 {
		t.Errorf("got %d, want max concurrency ≤2", got)
	}
}

func TestWorkerPoolStats(t *testing.T) {
	pool := NewWorkerPool(4)
	var wg sync.WaitGroup

	for range 3 {
		wg.Add(1)
		pool.Submit(func() {
			defer wg.Done()
			time.Sleep(5 * time.Millisecond)
		})
	}

	wg.Wait()

	// Synchronize on the pool's own bookkeeping, not the user WaitGroup.
	// Each task's deferred wg.Done() fires when the task body returns, which
	// unblocks wg.Wait() *before* the pool's goroutine wrapper records its
	// accounting: active--/done++ run in the wrapper's defer, after task()
	// returns (see Submit in workerpool.go). Reading Stats() right after
	// wg.Wait() can therefore observe Done<3 / Active>0. Stats() also samples
	// Active before Done, so a settle requires both fields, not just Done.
	// Poll until the pool reports a settled snapshot, bounded so a genuine
	// hang fails the test instead of spinning forever.
	var stats WorkerPoolStats
	deadline := time.Now().Add(time.Second)
	for {
		stats = pool.Stats()
		if (stats.Done == 3 && stats.Active == 0) || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}

	if stats.Done != 3 {
		t.Errorf("got %d, want 3 done", stats.Done)
	}
	if stats.MaxSize != 4 {
		t.Errorf("got %d, want max size 4", stats.MaxSize)
	}
	if stats.Active != 0 {
		t.Errorf("got %d, want 0 active after completion", stats.Active)
	}
}

func TestWorkerPoolSubmitContextCanceledWhileQueued(t *testing.T) {
	pool := NewWorkerPool(1)
	blocked := make(chan struct{})
	release := make(chan struct{})
	pool.Submit(func() {
		close(blocked)
		<-release
	})
	<-blocked

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	if ok := pool.SubmitContext(ctx, func() {}); ok {
		t.Fatal("expected queued submit to fail after context cancellation")
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("queued submit ignored cancellation for %v", elapsed)
	}

	close(release)
}
