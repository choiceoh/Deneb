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

// EventBus provides pub/sub for session lifecycle events.
type EventBus struct {
	mu       sync.RWMutex
	handlers []EventHandler
}

// NewEventBus creates a new event bus.
func NewEventBus() *EventBus {
	return &EventBus{}
}

// Subscribe registers a handler for all session events.
// Returns an unsubscribe function.
func (b *EventBus) Subscribe(handler EventHandler) func() {
	b.mu.Lock()
	idx := len(b.handlers)
	b.handlers = append(b.handlers, handler)
	b.mu.Unlock()

	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if idx < len(b.handlers) {
			b.handlers[idx] = nil
		}
	}
}

// Emit sends an event to all subscribers.
func (b *EventBus) Emit(event Event) {
	b.mu.RLock()
	handlers := make([]EventHandler, len(b.handlers))
	copy(handlers, b.handlers)
	b.mu.RUnlock()

	for _, h := range handlers {
		if h != nil {
			h(event)
		}
	}
}
