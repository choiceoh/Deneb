package queue

import (
	"sync"
	"time"
)

// QueueSettings configures the message queue behavior.
// The queue always operates in auto-debounce mode: incoming messages are
// coalesced during a debounce window and then drained. When the queue is
// full the newest message is dropped (single-user Telegram bot).
type QueueSettings struct {
	DebounceMs int `json:"debounceMs,omitempty"`
	Cap        int `json:"cap,omitempty"`
}

// DefaultQueueSettings returns sensible defaults.
func DefaultQueueSettings() QueueSettings {
	return QueueSettings{
		Cap: 50,
	}
}

// QueueItem represents a queued message.
type QueueItem struct {
	Text       string `json:"text"`
	SessionKey string `json:"sessionKey"`
	QueuedAt   int64  `json:"queuedAt"`
	Priority   int    `json:"priority,omitempty"` // higher = more urgent
}

// MessageQueue manages a bounded queue of pending messages.
type MessageQueue struct {
	mu       sync.Mutex
	items    []QueueItem
	settings QueueSettings
}

// NewMessageQueue creates a new message queue.
func NewMessageQueue(settings QueueSettings) *MessageQueue {
	if settings.Cap <= 0 {
		settings.Cap = 50
	}
	return &MessageQueue{
		settings: settings,
		items:    make([]QueueItem, 0, settings.Cap),
	}
}

// Enqueue adds a message to the queue. Returns false if the message was dropped.
func (q *MessageQueue) Enqueue(item QueueItem) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	item.QueuedAt = time.Now().UnixMilli()

	// Drop newest when at capacity (single-user bot: reject the new message).
	if len(q.items) >= q.settings.Cap {
		return false
	}

	q.items = append(q.items, item)
	return true
}

// Drain removes and returns all queued items.
func (q *MessageQueue) Drain() []QueueItem {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) == 0 {
		return nil
	}
	result := make([]QueueItem, len(q.items))
	copy(result, q.items)
	q.items = q.items[:0]
	return result
}

// Peek returns the next item without removing it.
func (q *MessageQueue) Peek() *QueueItem {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return nil
	}
	cp := q.items[0]
	return &cp
}

// Len returns the current queue length.
func (q *MessageQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Clear removes all queued items.
func (q *MessageQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = q.items[:0]
}

// Cleanup removes items older than maxAgeMs.
func (q *MessageQueue) Cleanup(maxAgeMs int64) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now().UnixMilli()
	pruned := 0
	kept := make([]QueueItem, 0, len(q.items))
	for _, item := range q.items {
		if now-item.QueuedAt > maxAgeMs {
			pruned++
			continue
		}
		kept = append(kept, item)
	}
	q.items = kept
	return pruned
}
