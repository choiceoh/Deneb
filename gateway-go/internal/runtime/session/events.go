package session

import (
	"log/slog"
	"sync"
)

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

// subscriberQueueSize bounds each subscriber's per-handler mailbox. A slow
// handler that falls behind by more than this many events starts dropping
// (with a warning log) instead of blocking Emit and stalling other subscribers.
const subscriberQueueSize = 64

// subscription wraps a handler with a unique ID and its own async mailbox.
// Each subscriber runs in its own goroutine so a slow handler cannot delay
// event delivery to other subscribers. The goroutine exits when done is closed.
type subscription struct {
	id      uint64
	handler EventHandler
	queue   chan Event
	done    chan struct{}
}

// EventBus provides pub/sub for session lifecycle events.
// Dispatch is asynchronous: each subscriber has a buffered mailbox and a
// dedicated goroutine. This isolates slow or panicking handlers so they
// cannot stall other subscribers or the caller of Emit.
type EventBus struct {
	mu     sync.RWMutex
	subs   []*subscription
	nextID uint64
}

// NewEventBus creates a new event bus.
func NewEventBus() *EventBus {
	return &EventBus{}
}

// Subscribe registers a handler and spawns a worker goroutine that drains
// its mailbox sequentially. Returns an unsubscribe function that is safe
// to call concurrently; it stops the worker and removes the subscription.
func (b *EventBus) Subscribe(handler EventHandler) func() {
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	sub := &subscription{
		id:      id,
		handler: handler,
		queue:   make(chan Event, subscriberQueueSize),
		done:    make(chan struct{}),
	}
	b.subs = append(b.subs, sub)
	b.mu.Unlock()

	go runSubscriber(sub)

	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			for i, s := range b.subs {
				if s.id == id {
					b.subs[i] = b.subs[len(b.subs)-1]
					b.subs = b.subs[:len(b.subs)-1]
					break
				}
			}
			b.mu.Unlock()
			close(sub.done)
		})
	}
}

// runSubscriber drains the subscription mailbox until done is closed.
// Each handler invocation is protected by panic recovery.
func runSubscriber(sub *subscription) {
	for {
		select {
		case <-sub.done:
			return
		case event := <-sub.queue:
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("event bus: handler panicked",
							"event", event.Kind, "key", event.Key, "panic", r)
					}
				}()
				sub.handler(event)
			}()
		}
	}
}

// Emit enqueues an event to every subscriber's mailbox. Emission is
// non-blocking: if a subscriber's mailbox is full (slow handler), the
// event is dropped with a warning log rather than stalling the producer.
func (b *EventBus) Emit(event Event) {
	b.mu.RLock()
	if len(b.subs) == 0 {
		b.mu.RUnlock()
		return
	}
	// Copy pointers out so we can release the read lock before any channel
	// sends — the lock protects the subs slice, not the mailboxes.
	snapshot := make([]*subscription, len(b.subs))
	copy(snapshot, b.subs)
	b.mu.RUnlock()

	for _, sub := range snapshot {
		select {
		case sub.queue <- event:
		default:
			// Slow subscriber: mailbox full. Drop to keep producer non-blocking.
			// The underlying state change has already been applied; losing the
			// notification is a lesser harm than stalling Emit.
			slog.Warn("event bus: subscriber mailbox full, dropping event",
				"event", event.Kind, "key", event.Key, "subscriberID", sub.id)
		}
	}
}
