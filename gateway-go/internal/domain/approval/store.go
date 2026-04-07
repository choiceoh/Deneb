// Package approval manages execution approval requests and their lifecycle.
//
// This ports the TypeScript exec-approval system (src/gateway/server-methods/exec/exec-approval.ts)
// to Go, providing in-memory storage for approval requests with decision resolution.
package approval

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Decision represents an exec approval decision.
type Decision string

const (
	DecisionAllowOnce   Decision = "allow-once"
	DecisionAllowAlways Decision = "allow-always"
	DecisionDeny        Decision = "deny"
)

// Request represents a pending or resolved execution approval request.
type Request struct {
	ID             string            `json:"id"`
	Command        string            `json:"command"`
	CommandArgv    []string          `json:"commandArgv,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Cwd            string            `json:"cwd,omitempty"`
	SystemRunPlan  any               `json:"systemRunPlan,omitempty"`
	Host           string            `json:"host,omitempty"`
	Security       string            `json:"security,omitempty"`
	Ask            string            `json:"ask,omitempty"`
	AgentID        string            `json:"agentId,omitempty"`
	ResolvedPath   string            `json:"resolvedPath,omitempty"`
	SessionKey     string            `json:"sessionKey,omitempty"`
	Decision       *Decision         `json:"decision"`
	CreatedAtMs    int64             `json:"createdAtMs"`
	ExpiresAtMs    int64             `json:"expiresAtMs"`
	ResolvedAtMs   *int64            `json:"resolvedAtMs,omitempty"`
	TwoPhase       bool              `json:"twoPhase,omitempty"`
	TurnSourceInfo *TurnSourceInfo   `json:"turnSourceInfo,omitempty"`
}

// TurnSourceInfo identifies the originating channel turn for an approval.
type TurnSourceInfo struct {
	Channel   string `json:"channel,omitempty"`
	To        string `json:"to,omitempty"`
	AccountID string `json:"accountId,omitempty"`
	ThreadID  string `json:"threadId,omitempty"`
}

// ApprovalsFile represents the persisted exec approvals configuration.
type ApprovalsFile struct {
	Version    int               `json:"version"`
	Rules      []ApprovalRule    `json:"rules,omitempty"`
	GlobalDeny []string          `json:"globalDeny,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// ApprovalRule is a single exec approval rule entry.
type ApprovalRule struct {
	Pattern  string `json:"pattern"`
	Decision string `json:"decision"`
	AddedAt  string `json:"addedAt,omitempty"`
}

// Snapshot is the read-only view of exec approvals state.
type Snapshot struct {
	File     ApprovalsFile `json:"file"`
	Hash     string        `json:"hash"`
	LoadedAt int64         `json:"loadedAt"`
}

// Store manages exec approval requests in memory with decision waiters.
type Store struct {
	mu       sync.RWMutex
	requests map[string]*Request
	waiters  map[string][]chan struct{}

	globalSnapshot *Snapshot

	defaultTTL time.Duration
}

// NewStore creates a new approval store with default settings.
func NewStore() *Store {
	return &Store{
		requests:       make(map[string]*Request),
		waiters:        make(map[string][]chan struct{}),
		globalSnapshot: &Snapshot{File: ApprovalsFile{Version: 1}, LoadedAt: time.Now().UnixMilli()},
		defaultTTL:     5 * time.Minute,
	}
}

// CreateRequest creates a new approval request and returns it.
func (s *Store) CreateRequest(params CreateRequestParams) *Request {
	id := params.ID
	if id == "" {
		id = generateID()
	}

	now := time.Now().UnixMilli()
	ttl := s.defaultTTL
	if params.TimeoutMs > 0 {
		ttl = time.Duration(params.TimeoutMs) * time.Millisecond
	}

	req := &Request{
		ID:            id,
		Command:       params.Command,
		CommandArgv:   params.CommandArgv,
		Env:           params.Env,
		Cwd:           params.Cwd,
		SystemRunPlan: params.SystemRunPlan,
		Host:          params.Host,
		Security:      params.Security,
		Ask:           params.Ask,
		AgentID:       params.AgentID,
		ResolvedPath:  params.ResolvedPath,
		SessionKey:    params.SessionKey,
		TwoPhase:      params.TwoPhase,
		CreatedAtMs:   now,
		ExpiresAtMs:   now + ttl.Milliseconds(),
	}
	if params.TurnSource != nil {
		req.TurnSourceInfo = params.TurnSource
	}

	s.mu.Lock()
	s.requests[id] = req
	s.mu.Unlock()

	return req
}

// CreateRequestParams holds input for creating an approval request.
type CreateRequestParams struct {
	ID            string
	Command       string
	CommandArgv   []string
	Env           map[string]string
	Cwd           string
	SystemRunPlan any
	Host          string
	Security      string
	Ask           string
	AgentID       string
	ResolvedPath  string
	SessionKey    string
	TimeoutMs     int64
	TwoPhase      bool
	TurnSource    *TurnSourceInfo
}

// Get returns an approval request by ID, or nil if not found.
func (s *Store) Get(id string) *Request {
	s.mu.RLock()
	defer s.mu.RUnlock()
	req := s.requests[id]
	if req == nil {
		return nil
	}
	cp := *req
	return &cp
}

// Resolve sets the decision on an approval request and notifies waiters.
func (s *Store) Resolve(id string, decision Decision) error {
	s.mu.Lock()
	req, ok := s.requests[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("approval request %q not found", id)
	}
	if req.Decision != nil {
		s.mu.Unlock()
		return fmt.Errorf("approval request %q already resolved", id)
	}
	now := time.Now().UnixMilli()
	req.Decision = &decision
	req.ResolvedAtMs = &now

	// Wake all waiters.
	waiters := s.waiters[id]
	delete(s.waiters, id)
	s.mu.Unlock()

	for _, ch := range waiters {
		close(ch)
	}
	return nil
}

// WaitForDecision blocks until the request is resolved or context is cancelled.
// Returns a snapshot of the request.
func (s *Store) WaitForDecision(id string) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	req := s.requests[id]
	if req == nil || req.Decision != nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}

	ch := make(chan struct{})
	s.waiters[id] = append(s.waiters[id], ch)
	return ch
}

// GetGlobalSnapshot returns the global exec approvals snapshot.
func (s *Store) GetGlobalSnapshot() *Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.globalSnapshot == nil {
		return nil
	}
	cp := *s.globalSnapshot
	return &cp
}

// SetGlobalSnapshot sets the global exec approvals configuration.
func (s *Store) SetGlobalSnapshot(file ApprovalsFile, hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.globalSnapshot = &Snapshot{
		File:     file,
		Hash:     hash,
		LoadedAt: time.Now().UnixMilli(),
	}
}

// Cleanup removes expired, unresolved requests.
func (s *Store) Cleanup() int {
	now := time.Now().UnixMilli()
	s.mu.Lock()
	defer s.mu.Unlock()

	removed := 0
	for id, req := range s.requests {
		if req.Decision == nil && req.ExpiresAtMs < now {
			delete(s.requests, id)
			// Wake waiters with nil decision.
			for _, ch := range s.waiters[id] {
				close(ch)
			}
			delete(s.waiters, id)
			removed++
		}
	}
	return removed
}

func generateID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
