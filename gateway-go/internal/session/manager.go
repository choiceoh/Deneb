// Package session manages gateway session lifecycle.
//
// This implements the session state machine from
// src/gateway/session/ in Go.
package session

import (
	"sync"
	"time"
)

// RunStatus mirrors SessionRunStatus from TypeScript.
type RunStatus string

const (
	StatusRunning RunStatus = "running"
	StatusDone    RunStatus = "done"
	StatusFailed  RunStatus = "failed"
	StatusKilled  RunStatus = "killed"
	StatusTimeout RunStatus = "timeout"
)

// Kind mirrors the TypeScript session kind union.
type Kind string

const (
	KindDirect  Kind = "direct"
	KindGroup   Kind = "group"
	KindGlobal  Kind = "global"
	KindUnknown Kind = "unknown"
)

// Session represents a gateway session row.
type Session struct {
	Key            string    `json:"key"`
	Kind           Kind      `json:"kind"`
	Status         RunStatus `json:"status,omitempty"`
	Channel        string    `json:"channel,omitempty"`
	Model          string    `json:"model,omitempty"`
	UpdatedAt      int64     `json:"updatedAt,omitempty"`
	StartedAt      *int64    `json:"startedAt,omitempty"`
	EndedAt        *int64    `json:"endedAt,omitempty"`
	RuntimeMs      *int64    `json:"runtimeMs,omitempty"`
	AbortedLastRun bool      `json:"abortedLastRun"`
	CreatedAt      time.Time `json:"-"`
}

// Manager tracks active sessions in memory.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	eventBus *EventBus
}

// NewManager creates an empty session manager with an integrated event bus.
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		eventBus: NewEventBus(),
	}
}

// EventBusRef returns the session event bus for subscribing to lifecycle events.
func (m *Manager) EventBusRef() *EventBus {
	return m.eventBus
}

// Get returns a snapshot copy of a session by key, or nil if not found.
// The returned Session is safe to read without holding the manager lock.
func (m *Manager) Get(key string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := m.sessions[key]
	if s == nil {
		return nil
	}
	cp := *s
	return &cp
}

// Set stores or updates a session. Panics if s is nil.
func (m *Manager) Set(s *Session) {
	if s == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.Key] = s
}

// Delete removes a session by key. Returns true if the session existed.
func (m *Manager) Delete(key string) bool {
	m.mu.Lock()
	s := m.sessions[key]
	ok := s != nil
	var oldStatus RunStatus
	if s != nil {
		oldStatus = s.Status
	}
	delete(m.sessions, key)
	m.mu.Unlock()

	if ok {
		m.eventBus.Emit(Event{Kind: EventDeleted, Key: key, OldStatus: oldStatus})
	}
	return ok
}

// List returns snapshot copies of all sessions.
func (m *Manager) List() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		cp := *s
		result = append(result, &cp)
	}
	return result
}

// Count returns the number of active sessions.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// Create creates a new session with the given key and kind.
// Returns a snapshot copy safe for concurrent use.
func (m *Manager) Create(key string, kind Kind) *Session {
	m.mu.Lock()
	now := time.Now()
	s := &Session{
		Key:       key,
		Kind:      kind,
		UpdatedAt: now.UnixMilli(),
		CreatedAt: now,
	}
	m.sessions[key] = s
	cp := *s
	m.mu.Unlock()

	m.eventBus.Emit(Event{Kind: EventCreated, Key: key})
	return &cp
}

// ApplyLifecycleEvent applies a lifecycle event to a session, creating it if needed.
// Returns a snapshot copy safe for concurrent use.
func (m *Manager) ApplyLifecycleEvent(key string, event LifecycleEvent) *Session {
	m.mu.Lock()

	existing := m.sessions[key]
	snap := DeriveLifecycleSnapshot(existing, event)

	// Empty snapshot means unknown phase — no-op.
	if snap.Status == "" {
		if existing != nil {
			cp := *existing
			m.mu.Unlock()
			return &cp
		}
		m.mu.Unlock()
		return &Session{Key: key, Kind: KindUnknown}
	}

	// Capture old status before mutation.
	oldStatus := RunStatus("")
	if existing != nil {
		oldStatus = existing.Status
	}

	if existing == nil {
		existing = &Session{Key: key, Kind: KindUnknown, CreatedAt: time.Now()}
		m.sessions[key] = existing
	}

	existing.Status = snap.Status
	existing.AbortedLastRun = snap.AbortedLastRun

	// Prefer snapshot-derived UpdatedAt; fall back to now.
	if snap.UpdatedAt != nil {
		existing.UpdatedAt = *snap.UpdatedAt
	} else {
		existing.UpdatedAt = time.Now().UnixMilli()
	}

	if snap.Status == StatusRunning {
		// Start phase: set StartedAt, clear terminal fields.
		existing.StartedAt = snap.StartedAt
		existing.EndedAt = nil
		existing.RuntimeMs = nil
	} else {
		// End/Error phase: preserve existing StartedAt if snapshot doesn't set one.
		if snap.StartedAt != nil {
			existing.StartedAt = snap.StartedAt
		}
		existing.EndedAt = snap.EndedAt
		existing.RuntimeMs = snap.RuntimeMs
	}

	newStatus := existing.Status
	cp := *existing
	m.mu.Unlock()

	m.eventBus.Emit(Event{Kind: EventStatusChanged, Key: key, OldStatus: oldStatus, NewStatus: newStatus})
	return &cp
}
