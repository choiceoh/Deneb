package sglang

import (
	"container/heap"
	"sync"
	"time"
)

// queueEntry wraps a request with dispatch metadata.
type queueEntry struct {
	req        *Request
	resultCh   chan submitResult
	enqueuedAt time.Time
	index      int // heap index, managed by container/heap
}

type submitResult struct {
	resp Response
	err  error
}

// requestQueue is a thread-safe, heap-backed priority queue.
// Lower Priority value = higher dispatch priority. FIFO within same priority.
type requestQueue struct {
	mu   sync.Mutex
	cond *sync.Cond
	h    queueHeap
}

func newRequestQueue() *requestQueue {
	q := &requestQueue{}
	q.cond = sync.NewCond(&q.mu)
	heap.Init(&q.h)
	return q
}

// Push adds an entry and signals the dispatcher.
func (q *requestQueue) Push(e *queueEntry) {
	q.mu.Lock()
	heap.Push(&q.h, e)
	q.mu.Unlock()
	q.cond.Signal()
}

// PopWait blocks until an entry is available or done is closed.
// Returns nil if done fires first.
func (q *requestQueue) PopWait(done <-chan struct{}) *queueEntry {
	q.mu.Lock()
	defer q.mu.Unlock()
	for q.h.Len() == 0 {
		// Release lock and wait; re-check on wake.
		waitCh := make(chan struct{})
		go func() {
			q.mu.Lock()
			q.cond.Wait()
			q.mu.Unlock()
			close(waitCh)
		}()
		q.mu.Unlock()
		select {
		case <-done:
			q.mu.Lock()
			return nil
		case <-waitCh:
			q.mu.Lock()
		}
	}
	return heap.Pop(&q.h).(*queueEntry)
}

// Len returns the current queue depth (unlocked snapshot).
func (q *requestQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.h.Len()
}

// DropOldestBackground removes and fails the oldest Background entry if the
// queue exceeds maxDepth. Returns true if an entry was dropped.
func (q *requestQueue) DropOldestBackground(maxDepth int) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.h.Len() <= maxDepth {
		return false
	}
	// Linear scan for oldest background entry.
	oldest := -1
	var oldestTime time.Time
	for i, e := range q.h {
		if e.req.Priority == PriorityBackground {
			if oldest == -1 || e.enqueuedAt.Before(oldestTime) {
				oldest = i
				oldestTime = e.enqueuedAt
			}
		}
	}
	if oldest == -1 {
		return false
	}
	entry := heap.Remove(&q.h, oldest).(*queueEntry)
	entry.resultCh <- submitResult{err: ErrQueueFull}
	return true
}

// DrainAll fails all queued entries with the given error.
func (q *requestQueue) DrainAll(err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for q.h.Len() > 0 {
		entry := heap.Pop(&q.h).(*queueEntry)
		entry.resultCh <- submitResult{err: err}
	}
}

// --- heap implementation ---

type queueHeap []*queueEntry

func (h queueHeap) Len() int { return len(h) }

func (h queueHeap) Less(i, j int) bool {
	if h[i].req.Priority != h[j].req.Priority {
		return h[i].req.Priority < h[j].req.Priority
	}
	return h[i].enqueuedAt.Before(h[j].enqueuedAt)
}

func (h queueHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *queueHeap) Push(x any) {
	e := x.(*queueEntry)
	e.index = len(*h)
	*h = append(*h, e)
}

func (h *queueHeap) Pop() any {
	old := *h
	n := len(old)
	e := old[n-1]
	old[n-1] = nil // help GC
	e.index = -1
	*h = old[:n-1]
	return e
}
