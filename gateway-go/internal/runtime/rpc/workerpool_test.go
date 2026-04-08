package rpc

import (
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

	stats := pool.Stats()
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

func TestWorkerPoolDefaultSize(t *testing.T) {
	pool := NewWorkerPool(0) // should use default
	stats := pool.Stats()
	if stats.MaxSize < 4 || stats.MaxSize > 128 {
		t.Errorf("default pool size out of range [4, 128]: %d", stats.MaxSize)
	}
}
