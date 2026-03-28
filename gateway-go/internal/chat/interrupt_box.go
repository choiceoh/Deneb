package chat

import (
	"sync"
	"time"
)

// PendingMessage represents a user message queued for interrupt handling
// during an active agent run.
type PendingMessage struct {
	Message    string
	Delivery   *DeliveryContext
	ReceivedAt time.Time
}

// InterruptBox is a single-capacity message channel between Send() and the
// agent loop. When a new message arrives while the agent is running, it is
// placed here instead of killing the run. The agent loop polls the box at
// turn boundaries, issues a quick LLM response, and continues its work.
//
// Capacity 1: if multiple messages arrive between polls, only the latest
// is kept (previous pending is drained and replaced).
type InterruptBox struct {
	mu     sync.Mutex
	ch     chan PendingMessage
	closed bool
}

// NewInterruptBox creates a ready-to-use InterruptBox.
func NewInterruptBox() *InterruptBox {
	return &InterruptBox{
		ch: make(chan PendingMessage, 1),
	}
}

// Offer queues a message for the agent loop. If a previous message is
// pending, it is drained and replaced with the new one (latest wins).
func (b *InterruptBox) Offer(msg PendingMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	// Drain any existing pending message.
	select {
	case <-b.ch:
	default:
	}
	b.ch <- msg
}

// Poll returns the pending message without blocking. Returns false if
// the box is empty.
func (b *InterruptBox) Poll() (PendingMessage, bool) {
	select {
	case msg := <-b.ch:
		return msg, true
	default:
		return PendingMessage{}, false
	}
}

// Close marks the box as closed. Subsequent Offer calls are no-ops.
// Safe to call multiple times.
func (b *InterruptBox) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
}
