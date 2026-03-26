package session

import "sync"

// EventKind identifies the type of session event.
type EventKind string

const (
	EventCreated       EventKind = "session.created"
	EventStatusChanged EventKind = "session.status_changed"
	EventDeleted       EventKind = "session.deleted"
)

// Event represents a session lifecycle event.
type Event struct {
	Kind      EventKind `json:"kind"`
	Key       string    `json:"key"`
	OldStatus RunStatus `json:"oldStatus,omitempty"`
	NewStatus RunStatus `json:"newStatus,omitempty"`
}

// EventHandler is a callback for session events.
type EventHandler func(event Event)

// subscription wraps a handler with a unique ID for safe unsubscribe.
type subscription struct {
	id      uint64
	handler EventHandler
}

// EventBus provides pub/sub for session lifecycle events.
type EventBus struct {
	mu     sync.RWMutex
	subs   []subscription
	nextID uint64
}

// NewEventBus creates a new event bus.
func NewEventBus() *EventBus {
	return &EventBus{}
}

// Subscribe registers a handler for all session events.
// Returns an unsubscribe function that is safe to call concurrently.
func (b *EventBus) Subscribe(handler EventHandler) func() {
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	b.subs = append(b.subs, subscription{id: id, handler: handler})
	b.mu.Unlock()

	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for i, s := range b.subs {
			if s.id == id {
				// Remove by swapping with last element.
				b.subs[i] = b.subs[len(b.subs)-1]
				b.subs = b.subs[:len(b.subs)-1]
				return
			}
		}
	}
}

// Emit sends an event to all subscribers.
// A panicking handler is recovered so it does not prevent other handlers from executing.
func (b *EventBus) Emit(event Event) {
	b.mu.RLock()
	n := len(b.subs)
	if n == 0 {
		b.mu.RUnlock()
		return
	}
	snapshot := make([]EventHandler, n)
	for i, s := range b.subs {
		snapshot[i] = s.handler
	}
	b.mu.RUnlock()

	// Each handler is called in an isolated closure with panic recovery.
	// Panics are silently swallowed to prevent a buggy subscriber from
	// disrupting event delivery to other subscribers.
	for _, h := range snapshot {
		func() {
			defer func() { recover() }()
			h(event)
		}()
	}
}
