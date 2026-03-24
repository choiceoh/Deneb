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
	Key       string    `json:"key"`
	Kind      Kind      `json:"kind"`
	Status    RunStatus `json:"status,omitempty"`
	Channel   string    `json:"channel,omitempty"`
	Model     string    `json:"model,omitempty"`
	UpdatedAt int64     `json:"updatedAt,omitempty"`
	StartedAt *int64    `json:"startedAt,omitempty"`
	EndedAt   *int64    `json:"endedAt,omitempty"`
	RuntimeMs *int64    `json:"runtimeMs,omitempty"`
	CreatedAt time.Time `json:"-"`
}

// Manager tracks active sessions in memory.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewManager creates an empty session manager.
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
	}
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

// Set stores or updates a session.
func (m *Manager) Set(s *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.Key] = s
}

// Delete removes a session by key. Returns true if the session existed.
func (m *Manager) Delete(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[key]
	delete(m.sessions, key)
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
func (m *Manager) Create(key string, kind Kind) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	s := &Session{
		Key:       key,
		Kind:      kind,
		UpdatedAt: now.UnixMilli(),
		CreatedAt: now,
	}
	m.sessions[key] = s
	return s
}

// ApplyLifecycleEvent applies a lifecycle event to a session, creating it if needed.
func (m *Manager) ApplyLifecycleEvent(key string, event LifecycleEvent) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing := m.sessions[key]
	snap := DeriveLifecycleSnapshot(existing, event)

	if existing == nil {
		existing = &Session{Key: key, Kind: KindUnknown, CreatedAt: time.Now()}
		m.sessions[key] = existing
	}

	existing.Status = snap.Status
	existing.UpdatedAt = time.Now().UnixMilli()
	// Only overwrite fields that the snapshot explicitly sets (non-nil).
	// PhaseStart sets StartedAt and clears EndedAt/RuntimeMs.
	// PhaseEnd/Error sets EndedAt and RuntimeMs, preserving StartedAt.
	if snap.StartedAt != nil {
		existing.StartedAt = snap.StartedAt
		// New start clears terminal fields.
		existing.EndedAt = nil
		existing.RuntimeMs = nil
	}
	if snap.EndedAt != nil {
		existing.EndedAt = snap.EndedAt
	}
	if snap.RuntimeMs != nil {
		existing.RuntimeMs = snap.RuntimeMs
	}

	return existing
}
