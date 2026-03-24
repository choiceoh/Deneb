// Package session manages gateway session lifecycle.
//
// This will implement the session state machine from
// src/gateway/session/machine.ts in Go.
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

// Get returns a session by key, or nil if not found.
func (m *Manager) Get(key string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[key]
}

// Set stores or updates a session.
func (m *Manager) Set(s *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.Key] = s
}

// Delete removes a session by key.
func (m *Manager) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, key)
}

// List returns all sessions.
func (m *Manager) List() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result
}

// Count returns the number of active sessions.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}
