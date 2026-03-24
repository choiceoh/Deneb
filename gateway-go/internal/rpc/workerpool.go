package rpc

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// WorkerPool is a bounded goroutine pool for RPC handler execution.
// It prevents unbounded goroutine creation under burst load while
// allowing full utilization of available CPU cores.
type WorkerPool struct {
	sem     chan struct{}
	maxSize int
	active  atomic.Int64
	queued  atomic.Int64
	done    atomic.Int64
	mu      sync.Mutex // protects resize
}

// NewWorkerPool creates a worker pool sized to the current hardware.
// Default size: 2× logical CPU cores, clamped to [4, 64].
func NewWorkerPool(maxWorkers int) *WorkerPool {
	if maxWorkers <= 0 {
		maxWorkers = defaultPoolSize()
	}
	return &WorkerPool{
		sem:     make(chan struct{}, maxWorkers),
		maxSize: maxWorkers,
	}
}

func defaultPoolSize() int {
	n := runtime.NumCPU() * 2
	if n < 4 {
		n = 4
	}
	if n > 64 {
		n = 64
	}
	return n
}

// Submit queues a task for execution. It blocks if all workers are busy,
// providing natural back-pressure to callers.
func (wp *WorkerPool) Submit(task func()) {
	wp.queued.Add(1)
	wp.sem <- struct{}{} // blocks when pool is full
	wp.queued.Add(-1)
	wp.active.Add(1)

	go func() {
		defer func() {
			wp.active.Add(-1)
			wp.done.Add(1)
			<-wp.sem
		}()
		task()
	}()
}

// Stats returns a snapshot of pool utilization.
func (wp *WorkerPool) Stats() WorkerPoolStats {
	return WorkerPoolStats{
		MaxSize: wp.maxSize,
		Active:  int(wp.active.Load()),
		Queued:  int(wp.queued.Load()),
		Done:    int(wp.done.Load()),
	}
}

// WorkerPoolStats is a snapshot of worker pool utilization.
type WorkerPoolStats struct {
	MaxSize int `json:"maxSize"`
	Active  int `json:"active"`
	Queued  int `json:"queued"`
	Done    int `json:"done"`
}
