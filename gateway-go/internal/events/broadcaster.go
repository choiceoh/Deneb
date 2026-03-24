// Package events provides a pub/sub event broadcasting system for WebSocket clients.
//
// This mirrors the event dispatch logic in src/gateway/server.impl.ts where events
// are broadcast to connected clients (e.g., session updates, channel status changes,
// presence updates, agent events).
package events

import (
	"encoding/json"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Subscriber represents a connected client that can receive events.
type Subscriber interface {
	// ID returns the unique identifier for this subscriber (e.g., connID).
	ID() string
	// SendEvent writes an event frame to the subscriber. Returns an error if the
	// write fails (e.g., connection closed).
	SendEvent(data []byte) error
	// IsAuthenticated returns true if the subscriber has completed the handshake.
	IsAuthenticated() bool
}

// Filter controls which events a subscriber receives.
type Filter struct {
	// Events is a set of event names to receive. If empty, all events are received.
	Events map[string]bool
}

// Accepts returns true if the filter allows the given event name.
func (f *Filter) Accepts(event string) bool {
	if len(f.Events) == 0 {
		return true
	}
	return f.Events[event]
}

// Broadcaster distributes events to subscribed WebSocket clients.
type Broadcaster struct {
	mu          sync.RWMutex
	subscribers map[string]subscriberEntry
	seq         uint64
}

type subscriberEntry struct {
	sub    Subscriber
	filter Filter
}

// NewBroadcaster creates a new event broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subscribers: make(map[string]subscriberEntry),
	}
}

// Subscribe adds a subscriber. If the subscriber ID already exists, it is replaced.
func (b *Broadcaster) Subscribe(sub Subscriber, filter Filter) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[sub.ID()] = subscriberEntry{sub: sub, filter: filter}
}

// Unsubscribe removes a subscriber by ID.
func (b *Broadcaster) Unsubscribe(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subscribers, id)
}

// Count returns the number of active subscribers.
func (b *Broadcaster) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}

// Broadcast sends an event to all matching subscribers.
// Returns the number of subscribers that received the event and any send errors.
func (b *Broadcaster) Broadcast(event string, payload any) (sent int, errors []error) {
	b.mu.Lock()
	b.seq++
	seq := b.seq
	b.mu.Unlock()

	frame, err := protocol.NewEventFrame(event, payload)
	if err != nil {
		return 0, []error{err}
	}
	frame.Seq = &seq

	data, err := json.Marshal(frame)
	if err != nil {
		return 0, []error{err}
	}

	b.mu.RLock()
	entries := make([]subscriberEntry, 0, len(b.subscribers))
	for _, entry := range b.subscribers {
		entries = append(entries, entry)
	}
	b.mu.RUnlock()

	for _, entry := range entries {
		if !entry.sub.IsAuthenticated() {
			continue
		}
		if !entry.filter.Accepts(event) {
			continue
		}
		if err := entry.sub.SendEvent(data); err != nil {
			errors = append(errors, err)
		} else {
			sent++
		}
	}
	return sent, errors
}

// BroadcastRaw sends pre-serialized event data to all authenticated subscribers
// that accept the given event name.
func (b *Broadcaster) BroadcastRaw(event string, data []byte) (sent int) {
	b.mu.RLock()
	entries := make([]subscriberEntry, 0, len(b.subscribers))
	for _, entry := range b.subscribers {
		entries = append(entries, entry)
	}
	b.mu.RUnlock()

	for _, entry := range entries {
		if !entry.sub.IsAuthenticated() {
			continue
		}
		if !entry.filter.Accepts(event) {
			continue
		}
		if err := entry.sub.SendEvent(data); err == nil {
			sent++
		}
	}
	return sent
}
