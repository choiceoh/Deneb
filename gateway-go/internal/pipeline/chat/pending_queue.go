// pending_queue.go — Per-session pending message queue for mid-run messages.
//
// When a user sends a message while an agent run is active, the message is
// queued instead of interrupting the run. After the active run completes,
// queued messages are drained and processed sequentially.
package chat

import "sync"

// PendingQueue manages per-session message queues. Thread-safe.
// When a run is active, incoming messages are queued; on run completion
// they are drained and processed.
type PendingQueue struct {
	mu    sync.Mutex
	items map[string]*pendingRunQueue // sessionKey -> queued messages
}

// NewPendingQueue creates a ready-to-use PendingQueue.
func NewPendingQueue() *PendingQueue {
	return &PendingQueue{items: make(map[string]*pendingRunQueue)}
}

// Enqueue queues a message for processing after the active run completes.
func (pq *PendingQueue) Enqueue(sessionKey string, params RunParams) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	q, ok := pq.items[sessionKey]
	if !ok {
		q = &pendingRunQueue{}
		pq.items[sessionKey] = q
	}
	q.enqueue(params)
}

// Drain removes and returns the next pending message for a session.
func (pq *PendingQueue) Drain(sessionKey string) *RunParams {
	pq.mu.Lock()
	q, ok := pq.items[sessionKey]
	pq.mu.Unlock()
	if !ok {
		return nil
	}
	return q.drain()
}

// Clear removes all pending messages for a session (used on /reset).
func (pq *PendingQueue) Clear(sessionKey string) {
	pq.mu.Lock()
	delete(pq.items, sessionKey)
	pq.mu.Unlock()
}

// Len returns the number of queued items for a session.
func (pq *PendingQueue) Len(sessionKey string) int {
	pq.mu.Lock()
	q, ok := pq.items[sessionKey]
	pq.mu.Unlock()
	if !ok {
		return 0
	}
	return q.len()
}

// Reset clears all sessions' pending queues.
func (pq *PendingQueue) Reset() {
	pq.mu.Lock()
	pq.items = make(map[string]*pendingRunQueue)
	pq.mu.Unlock()
}

// pendingRunQueue holds messages waiting for the current run to finish.
// Thread-safe; accessed by both the RPC goroutine (enqueue) and the
// agent run goroutine (drain).
type pendingRunQueue struct {
	mu    sync.Mutex
	items []RunParams
}

// enqueue adds a message to the pending queue. At most 1 pending message
// is kept: newer messages replace older ones (the user's latest intent
// supersedes previous queued messages).
func (q *pendingRunQueue) enqueue(params RunParams) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = []RunParams{params}
}

// drain removes and returns the pending message, if any.
func (q *pendingRunQueue) drain() *RunParams {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return nil
	}
	p := q.items[0]
	q.items = q.items[:0]
	return &p
}

// len returns the number of queued items.
func (q *pendingRunQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}
