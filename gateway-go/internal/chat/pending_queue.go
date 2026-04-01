// pending_queue.go — Per-session pending message queue for mid-run messages.
//
// When a user sends a message while an agent run is active, the message is
// queued instead of interrupting the run. After the active run completes,
// queued messages are drained and processed sequentially.
package chat

import "sync"

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
	// Keep only the latest pending message. The user's most recent message
	// is the one they care about; older queued messages are stale.
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
