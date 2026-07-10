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
		if !pool.Submit(context.Background(), func() {
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
		}) {
			t.Fatal("background submission unexpectedly canceled")
		}
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
		if !pool.Submit(context.Background(), func() {
			defer wg.Done()
			time.Sleep(5 * time.Millisecond)
		}) {
			t.Fatal("background submission unexpectedly canceled")
		}
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
	if stats.CanceledBeforeStart != 0 || stats.SaturationEvents != 0 || stats.PeakQueued != 0 || stats.Saturated {
		t.Errorf("unexpected idle health flags: %+v", stats)
	}
}

func TestWorkerPoolStatsTracksCanceledWaiter(t *testing.T) {
	pool := NewWorkerPool(1)
	started := make(chan struct{})
	release := make(chan struct{})
	if !pool.Submit(context.Background(), func() {
		close(started)
		<-release
	}) {
		t.Fatal("first submission unexpectedly canceled")
	}
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	submitted := make(chan bool, 1)
	go func() {
		submitted <- pool.Submit(ctx, func() {})
	}()
	deadline := time.Now().Add(time.Second)
	for pool.Stats().Queued != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if stats := pool.Stats(); stats.Queued != 1 || !stats.Saturated {
		t.Fatalf("expected a saturated waiter before cancellation: %+v", stats)
	}
	cancel()
	if <-submitted {
		t.Fatal("canceled waiter should not acquire a worker slot")
	}
	stats := pool.Stats()
	if stats.CanceledBeforeStart != 1 {
		t.Fatalf("canceledBeforeStart = %d, want 1", stats.CanceledBeforeStart)
	}
	if stats.Queued != 0 {
		t.Fatalf("queued = %d after cancellation, want 0", stats.Queued)
	}
	if stats.SaturationEvents != 1 || stats.PeakQueued != 1 {
		t.Fatalf("unexpected saturation accounting: %+v", stats)
	}

	close(release)
}

func TestWorkerPoolDoesNotStartCanceledWaiterWhenCapacityReturns(t *testing.T) {
	pool := NewWorkerPool(1)
	release := make(chan struct{})
	finished := make(chan struct{})
	if !pool.Submit(context.Background(), func() {
		<-release
		close(finished)
	}) {
		t.Fatal("first submission unexpectedly canceled")
	}

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 1)
	submitted := make(chan bool, 1)
	go func() {
		submitted <- pool.Submit(ctx, func() { started <- struct{}{} })
	}()

	deadline := time.Now().Add(time.Second)
	for pool.Stats().Queued != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if pool.Stats().Queued != 1 {
		t.Fatal("waiter was not queued")
	}

	cancel()
	close(release)
	<-finished
	if <-submitted {
		t.Fatal("canceled waiter should not start when capacity returns")
	}
	select {
	case <-started:
		t.Fatal("canceled task ran")
	default:
	}
}
