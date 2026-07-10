package rpc

import (
	"context"
	"runtime"
	"sync/atomic"
)

// WorkerPool is a bounded goroutine pool for RPC handler execution.
// It prevents unbounded goroutine creation under burst load while
// allowing full utilization of available CPU cores.
type WorkerPool struct {
	sem                 chan struct{}
	maxSize             int
	active              atomic.Int64
	queued              atomic.Int64
	peakQueued          atomic.Int64
	saturationEvents    atomic.Int64
	done                atomic.Int64
	canceledBeforeStart atomic.Int64
}

// NewWorkerPool creates a worker pool sized to the current hardware.
// Default size: 2× logical CPU cores, clamped to [4, 128].
func NewWorkerPool(maxWorkers int) *WorkerPool {
	if maxWorkers <= 0 {
		maxWorkers = defaultPoolSize()
	}
	return &WorkerPool{
		sem:     make(chan struct{}, maxWorkers),
		maxSize: maxWorkers,
	}
}

// defaultPoolSize computes the pool size: 2× logical CPU cores, clamped to [4, 128].
// The 2× multiplier accounts for I/O-bound handlers (DB, LLM calls) that spend
// most time waiting rather than using CPU. Upper bound raised for DGX Spark
// workloads where GPU inference waits dominate.
func defaultPoolSize() int {
	n := runtime.NumCPU() * 2
	if n < 4 {
		n = 4
	}
	if n > 128 {
		n = 128
	}
	return n
}

// Submit queues a task for execution. It waits for capacity while ctx is live,
// providing back-pressure without trapping a caller after its deadline expires.
// It returns false when ctx is canceled before a worker slot is acquired.
func (wp *WorkerPool) Submit(ctx context.Context, task func()) bool {
	if ctx.Err() != nil {
		wp.canceledBeforeStart.Add(1)
		return false
	}

	select {
	case wp.sem <- struct{}{}:
		// Fast path: capacity was immediately available.
	default:
		wp.saturationEvents.Add(1)
		waiting := wp.queued.Add(1)
		wp.observePeakQueued(waiting)
		select {
		case wp.sem <- struct{}{}:
		case <-ctx.Done():
			wp.queued.Add(-1)
			wp.canceledBeforeStart.Add(1)
			return false
		}
		wp.queued.Add(-1)
	}
	// Cancellation and slot availability can become ready at the same time.
	// Re-check after admission so a canceled waiter never starts merely because
	// select chose the semaphore case.
	if ctx.Err() != nil {
		<-wp.sem
		wp.canceledBeforeStart.Add(1)
		return false
	}
	wp.active.Add(1)

	go func() {
		defer func() {
			wp.active.Add(-1)
			wp.done.Add(1)
			<-wp.sem
		}()
		task()
	}()
	return true
}

func (wp *WorkerPool) observePeakQueued(current int64) {
	for {
		peak := wp.peakQueued.Load()
		if current <= peak || wp.peakQueued.CompareAndSwap(peak, current) {
			return
		}
	}
}

// Stats returns a snapshot of pool utilization.
func (wp *WorkerPool) Stats() WorkerPoolStats {
	active := int(wp.active.Load())
	queued := int(wp.queued.Load())
	return WorkerPoolStats{
		MaxSize:             wp.maxSize,
		Active:              active,
		Queued:              queued,
		PeakQueued:          int(wp.peakQueued.Load()),
		SaturationEvents:    int(wp.saturationEvents.Load()),
		Done:                int(wp.done.Load()),
		CanceledBeforeStart: int(wp.canceledBeforeStart.Load()),
		Saturated:           active >= wp.maxSize && queued > 0,
	}
}

// WorkerPoolStats is a snapshot of worker pool utilization.
type WorkerPoolStats struct {
	MaxSize             int  `json:"maxSize"`
	Active              int  `json:"active"`
	Queued              int  `json:"queued"`
	PeakQueued          int  `json:"peakQueued"`
	SaturationEvents    int  `json:"saturationEvents"`
	Done                int  `json:"done"`
	CanceledBeforeStart int  `json:"canceledBeforeStart"`
	Saturated           bool `json:"saturated"`
}
