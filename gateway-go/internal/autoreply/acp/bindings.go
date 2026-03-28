// bindings.go — Session-to-conversation binding service for ACP.
// Extracted from commands_subagents_acp.go to live alongside the ACP core types.
package acp

import (
	"fmt"
	"strings"
	"sync"
)

// SessionBindParams holds params for binding a session to a conversation.
type SessionBindParams struct {
	TargetSessionKey string
	TargetKind       string // "subagent" or "session"
	Channel          string
	AccountID        string
	ConversationID   string
	Placement        string // "current" or "child"
	Label            string
	AgentID          string
	BoundBy          string
}

// SessionBindResult holds the result of a session binding.
type SessionBindResult struct {
	BindingID      string
	ConversationID string
	TargetKey      string
}

// SessionBindingEntry represents an active session binding.
type SessionBindingEntry struct {
	BindingID        string
	TargetSessionKey string
	BoundBy          string
}

// AgentBindingEntry represents a session binding for display.
type AgentBindingEntry struct {
	ConversationID string
	Channel        string
	AccountID      string
	Status         string
	TargetKind     string
	TargetKey      string
	Label          string
}

// StoredBinding is the on-disk representation of a session binding.
type StoredBinding struct {
	Channel          string `json:"channel"`
	AccountID        string `json:"accountId"`
	ConversationID   string `json:"conversationId"`
	TargetSessionKey string `json:"targetSessionKey"`
	BoundBy          string `json:"boundBy,omitempty"`
}

// SessionBindingService tracks session-to-conversation bindings.
type SessionBindingService struct {
	mu       sync.RWMutex
	bindings map[string]*SessionBindingEntry // bindingID → entry
	byConvo  map[string]string               // "channel:account:convo" → bindingID
	nextID   int
}

// NewSessionBindingService creates a new binding service.
func NewSessionBindingService() *SessionBindingService {
	return &SessionBindingService{
		bindings: make(map[string]*SessionBindingEntry),
		byConvo:  make(map[string]string),
	}
}

// Bind creates a new session binding.
func (s *SessionBindingService) Bind(params SessionBindParams) *SessionBindResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	bindingID := fmt.Sprintf("bind_%d", s.nextID)
	convoKey := fmt.Sprintf("%s:%s:%s", params.Channel, params.AccountID, params.ConversationID)

	// Remove existing binding for same conversation.
	if oldID, ok := s.byConvo[convoKey]; ok {
		delete(s.bindings, oldID)
	}

	entry := &SessionBindingEntry{
		BindingID:        bindingID,
		TargetSessionKey: params.TargetSessionKey,
		BoundBy:          params.BoundBy,
	}
	s.bindings[bindingID] = entry
	s.byConvo[convoKey] = bindingID

	return &SessionBindResult{
		BindingID:      bindingID,
		ConversationID: params.ConversationID,
		TargetKey:      params.TargetSessionKey,
	}
}

// Resolve finds an active binding for a conversation.
func (s *SessionBindingService) Resolve(channel, accountID, conversationID string) *SessionBindingEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	convoKey := fmt.Sprintf("%s:%s:%s", channel, accountID, conversationID)
	bindingID, ok := s.byConvo[convoKey]
	if !ok {
		return nil
	}
	return s.bindings[bindingID]
}

// Unbind removes a session binding.
func (s *SessionBindingService) Unbind(bindingID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.bindings[bindingID]
	if !ok {
		return fmt.Errorf("binding %q not found", bindingID)
	}
	delete(s.bindings, bindingID)

	// Clean up convo index.
	for key, id := range s.byConvo {
		if id == bindingID {
			delete(s.byConvo, key)
			break
		}
	}
	return nil
}

// ListForSession returns all bindings targeting a session.
func (s *SessionBindingService) ListForSession(sessionKey string) []AgentBindingEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var entries []AgentBindingEntry
	for convoKey, bindingID := range s.byConvo {
		binding := s.bindings[bindingID]
		if binding == nil || binding.TargetSessionKey != sessionKey {
			continue
		}
		parts := strings.SplitN(convoKey, ":", 3)
		entry := AgentBindingEntry{
			ConversationID: "",
			Channel:        "",
			TargetKey:      binding.TargetSessionKey,
		}
		if len(parts) >= 1 {
			entry.Channel = parts[0]
		}
		if len(parts) >= 2 {
			entry.AccountID = parts[1]
		}
		if len(parts) >= 3 {
			entry.ConversationID = parts[2]
		}
		entries = append(entries, entry)
	}
	return entries
}

// Snapshot returns all bindings as StoredBinding entries for persistence.
func (s *SessionBindingService) Snapshot() []StoredBinding {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var entries []StoredBinding
	for convoKey, bindingID := range s.byConvo {
		binding := s.bindings[bindingID]
		if binding == nil {
			continue
		}
		parts := strings.SplitN(convoKey, ":", 3)
		entry := StoredBinding{
			TargetSessionKey: binding.TargetSessionKey,
			BoundBy:          binding.BoundBy,
		}
		if len(parts) >= 1 {
			entry.Channel = parts[0]
		}
		if len(parts) >= 2 {
			entry.AccountID = parts[1]
		}
		if len(parts) >= 3 {
			entry.ConversationID = parts[2]
		}
		entries = append(entries, entry)
	}
	return entries
}

// RestoreAll replaces all bindings from a slice of StoredBinding entries.
func (s *SessionBindingService) RestoreAll(entries []StoredBinding) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bindings = make(map[string]*SessionBindingEntry)
	s.byConvo = make(map[string]string)
	s.nextID = 0

	for _, e := range entries {
		s.nextID++
		bindingID := fmt.Sprintf("bind_%d", s.nextID)
		convoKey := fmt.Sprintf("%s:%s:%s", e.Channel, e.AccountID, e.ConversationID)

		s.bindings[bindingID] = &SessionBindingEntry{
			BindingID:        bindingID,
			TargetSessionKey: e.TargetSessionKey,
			BoundBy:          e.BoundBy,
		}
		s.byConvo[convoKey] = bindingID
	}
}
