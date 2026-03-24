// Package events provides a pub/sub event broadcasting system for WebSocket clients.
//
// This mirrors the event dispatch logic in src/gateway/server-broadcast.ts where
// events are broadcast to connected clients with RBAC scope guards, slow consumer
// detection, targeted delivery, and session subscription tracking.
package events

import (
	"encoding/json"
	"log/slog"
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
	// Role returns the client's RBAC role (e.g., "operator", "viewer", "agent").
	Role() string
	// Scopes returns the client's permission scopes.
	Scopes() []string
	// BufferedAmount returns bytes queued but not yet sent. Used for slow consumer detection.
	BufferedAmount() int64
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

// BroadcastOpts controls per-broadcast behavior.
type BroadcastOpts struct {
	// DropIfSlow skips subscribers whose send buffer exceeds MaxBufferedBytes.
	DropIfSlow bool
	// StateVersion includes snapshot versioning for client-side dedup.
	StateVersion *protocol.StateVersion
	// TargetConnIDs restricts delivery to specific connection IDs (nil = all).
	TargetConnIDs map[string]bool
}

// Scope constants for event RBAC guards.
const (
	ScopeRead      = "read"
	ScopeWrite     = "write"
	ScopeAdmin     = "admin"
	ScopeExecute   = "execute"
	ScopeApprovals = "approvals"
)

// eventScopeGuards maps event names to required scopes.
// Events not listed here are delivered to all authenticated subscribers.
var eventScopeGuards = map[string][]string{
	"exec.approval.requested": {ScopeApprovals, ScopeAdmin},
	"exec.approval.resolved":  {ScopeApprovals, ScopeAdmin},
	"sessions.changed":        {ScopeRead},
	"session.message":         {ScopeRead},
	"session.tool":            {ScopeRead},
	"agent":                   {ScopeRead},
	"agent.event":             {ScopeRead},
	"channels.changed":        {ScopeRead},
	"config.changed":          {ScopeAdmin},
}

// maxBufferedBytes is the threshold for slow consumer detection.
// Subscribers exceeding this are dropped when DropIfSlow is set.
const maxBufferedBytes int64 = 50 * 1024 * 1024 // 50 MB

// Broadcaster distributes events to subscribed WebSocket clients.
type Broadcaster struct {
	mu          sync.RWMutex
	subscribers map[string]subscriberEntry
	seq         uint64
	logger      *slog.Logger

	// Session event subscriptions: connID -> subscribed.
	sessionSubMu   sync.RWMutex
	sessionSubs    map[string]bool             // connIDs subscribed to session events
	sessionMsgSubs map[string]map[string]bool  // sessionKey -> set of connIDs

	// Tool event recipients: runId -> connID.
	toolRecipientMu sync.RWMutex
	toolRecipients  map[string]string
}

type subscriberEntry struct {
	sub    Subscriber
	filter Filter
}

// NewBroadcaster creates a new event broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subscribers:    make(map[string]subscriberEntry),
		sessionSubs:    make(map[string]bool),
		sessionMsgSubs: make(map[string]map[string]bool),
		toolRecipients: make(map[string]string),
		logger:         slog.Default(),
	}
}

// SetLogger sets the broadcaster logger.
func (b *Broadcaster) SetLogger(l *slog.Logger) {
	b.logger = l
}

// Subscribe adds a subscriber. If the subscriber ID already exists, it is replaced.
func (b *Broadcaster) Subscribe(sub Subscriber, filter Filter) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[sub.ID()] = subscriberEntry{sub: sub, filter: filter}
}

// Unsubscribe removes a subscriber by ID and cleans up all subscriptions.
func (b *Broadcaster) Unsubscribe(id string) {
	b.mu.Lock()
	delete(b.subscribers, id)
	b.mu.Unlock()

	b.sessionSubMu.Lock()
	delete(b.sessionSubs, id)
	for key, subs := range b.sessionMsgSubs {
		delete(subs, id)
		if len(subs) == 0 {
			delete(b.sessionMsgSubs, key)
		}
	}
	b.sessionSubMu.Unlock()
}

// Count returns the number of active subscribers.
func (b *Broadcaster) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}

// Broadcast sends an event to all matching subscribers.
// Returns the number of subscribers that received the event and any send errors.
func (b *Broadcaster) Broadcast(event string, payload any) (sent int, errs []error) {
	return b.BroadcastWithOpts(event, payload, BroadcastOpts{})
}

// BroadcastWithOpts sends an event with advanced options (targeting, slow consumer, state version).
func (b *Broadcaster) BroadcastWithOpts(event string, payload any, opts BroadcastOpts) (sent int, errs []error) {
	b.mu.Lock()
	b.seq++
	seq := b.seq
	b.mu.Unlock()

	frame, err := protocol.NewEventFrame(event, payload)
	if err != nil {
		return 0, []error{err}
	}
	// Only include seq for broadcast (non-targeted) events.
	if opts.TargetConnIDs == nil {
		frame.Seq = &seq
	}
	if opts.StateVersion != nil {
		frame.StateVersion = opts.StateVersion
	}

	data, err := json.Marshal(frame)
	if err != nil {
		return 0, []error{err}
	}

	entries := b.snapshotSubscribers()

	for _, entry := range entries {
		if !entry.sub.IsAuthenticated() {
			continue
		}
		// Target filtering.
		if opts.TargetConnIDs != nil && !opts.TargetConnIDs[entry.sub.ID()] {
			continue
		}
		if !entry.filter.Accepts(event) {
			continue
		}
		// Scope guard check.
		if !hasEventScope(entry.sub, event) {
			continue
		}
		// Slow consumer detection.
		if opts.DropIfSlow && entry.sub.BufferedAmount() > maxBufferedBytes {
			if b.logger != nil {
				b.logger.Warn("dropping slow consumer",
					"connId", entry.sub.ID(),
					"buffered", entry.sub.BufferedAmount(),
					"event", event,
				)
			}
			continue
		}
		if sendErr := entry.sub.SendEvent(data); sendErr != nil {
			errs = append(errs, sendErr)
		} else {
			sent++
		}
	}
	return sent, errs
}

// BroadcastToConnIDs sends an event to specific connection IDs.
func (b *Broadcaster) BroadcastToConnIDs(event string, payload any, connIDs map[string]bool) (int, []error) {
	return b.BroadcastWithOpts(event, payload, BroadcastOpts{TargetConnIDs: connIDs})
}

// BroadcastRaw sends pre-serialized event data to all authenticated subscribers
// that accept the given event name.
func (b *Broadcaster) BroadcastRaw(event string, data []byte) (sent int) {
	entries := b.snapshotSubscribers()

	for _, entry := range entries {
		if !entry.sub.IsAuthenticated() {
			continue
		}
		if !entry.filter.Accepts(event) {
			continue
		}
		if !hasEventScope(entry.sub, event) {
			continue
		}
		if err := entry.sub.SendEvent(data); err == nil {
			sent++
		}
	}
	return sent
}

// --- Session event subscription tracking ---

// SubscribeSessionEvents registers a connID to receive session events.
func (b *Broadcaster) SubscribeSessionEvents(connID string) {
	b.sessionSubMu.Lock()
	defer b.sessionSubMu.Unlock()
	b.sessionSubs[connID] = true
}

// UnsubscribeSessionEvents removes a connID from session events.
func (b *Broadcaster) UnsubscribeSessionEvents(connID string) {
	b.sessionSubMu.Lock()
	defer b.sessionSubMu.Unlock()
	delete(b.sessionSubs, connID)
}

// SubscribeSessionMessageEvents registers a connID to receive messages for a specific session.
func (b *Broadcaster) SubscribeSessionMessageEvents(connID, sessionKey string) {
	b.sessionSubMu.Lock()
	defer b.sessionSubMu.Unlock()
	if b.sessionMsgSubs[sessionKey] == nil {
		b.sessionMsgSubs[sessionKey] = make(map[string]bool)
	}
	b.sessionMsgSubs[sessionKey][connID] = true
}

// UnsubscribeSessionMessageEvents removes a connID from a session's message events.
func (b *Broadcaster) UnsubscribeSessionMessageEvents(connID, sessionKey string) {
	b.sessionSubMu.Lock()
	defer b.sessionSubMu.Unlock()
	if subs, ok := b.sessionMsgSubs[sessionKey]; ok {
		delete(subs, connID)
		if len(subs) == 0 {
			delete(b.sessionMsgSubs, sessionKey)
		}
	}
}

// GetSessionEventSubscriberConnIDs returns the set of connIDs subscribed to session events.
func (b *Broadcaster) GetSessionEventSubscriberConnIDs() map[string]bool {
	b.sessionSubMu.RLock()
	defer b.sessionSubMu.RUnlock()
	result := make(map[string]bool, len(b.sessionSubs))
	for id := range b.sessionSubs {
		result[id] = true
	}
	return result
}

// GetSessionMessageSubscriberConnIDs returns connIDs subscribed to a specific session's messages.
func (b *Broadcaster) GetSessionMessageSubscriberConnIDs(sessionKey string) map[string]bool {
	b.sessionSubMu.RLock()
	defer b.sessionSubMu.RUnlock()
	subs, ok := b.sessionMsgSubs[sessionKey]
	if !ok {
		return nil
	}
	result := make(map[string]bool, len(subs))
	for id := range subs {
		result[id] = true
	}
	return result
}

// --- Tool event recipients ---

// RegisterToolEventRecipient maps a run ID to a specific connID for tool events.
func (b *Broadcaster) RegisterToolEventRecipient(runID, connID string) {
	b.toolRecipientMu.Lock()
	defer b.toolRecipientMu.Unlock()
	b.toolRecipients[runID] = connID
}

// UnregisterToolEventRecipient removes a tool event recipient mapping.
func (b *Broadcaster) UnregisterToolEventRecipient(runID string) {
	b.toolRecipientMu.Lock()
	defer b.toolRecipientMu.Unlock()
	delete(b.toolRecipients, runID)
}

// GetToolEventRecipient returns the connID for a given tool run.
func (b *Broadcaster) GetToolEventRecipient(runID string) string {
	b.toolRecipientMu.RLock()
	defer b.toolRecipientMu.RUnlock()
	return b.toolRecipients[runID]
}

// --- Helpers ---

// hasEventScope checks if a subscriber has the required scope for an event.
func hasEventScope(sub Subscriber, event string) bool {
	required, ok := eventScopeGuards[event]
	if !ok {
		return true // No scope guard — allow all authenticated.
	}
	scopes := sub.Scopes()
	for _, req := range required {
		for _, s := range scopes {
			if s == req || s == ScopeAdmin {
				return true
			}
		}
	}
	return false
}

// snapshotSubscribers returns a snapshot of all subscribers under read lock.
func (b *Broadcaster) snapshotSubscribers() []subscriberEntry {
	b.mu.RLock()
	n := len(b.subscribers)
	entries := make([]subscriberEntry, 0, n)
	for _, entry := range b.subscribers {
		entries = append(entries, entry)
	}
	b.mu.RUnlock()
	return entries
}
