package session

import "time"

// PatchFields represents the fields that can be patched on a session via
// the sessions.patch RPC method. Nil pointer fields are left unchanged.
type PatchFields struct {
	Label                *string `json:"label,omitempty"`
	Model                *string `json:"model,omitempty"`
	ThinkingLevel        *string `json:"thinkingLevel,omitempty"`
	FastMode             *bool   `json:"fastMode,omitempty"`
	VerboseLevel         *string `json:"verboseLevel,omitempty"`
	ReasoningLevel       *string `json:"reasoningLevel,omitempty"`
	ElevatedLevel        *string `json:"elevatedLevel,omitempty"`
	ExecHost             *string `json:"execHost,omitempty"`
	ExecSecurity         *string `json:"execSecurity,omitempty"`
	ExecAsk              *string `json:"execAsk,omitempty"`
	ExecNode             *string `json:"execNode,omitempty"`
	ResponseUsage        *string `json:"responseUsage,omitempty"`
	SpawnedBy            *string `json:"spawnedBy,omitempty"`
	SpawnedWorkspaceDir  *string `json:"spawnedWorkspaceDir,omitempty"`
	SpawnDepth           *int    `json:"spawnDepth,omitempty"`
	SubagentRole         *string `json:"subagentRole,omitempty"`
	SubagentControlScope *string `json:"subagentControlScope,omitempty"`
	SendPolicy           *string `json:"sendPolicy,omitempty"`
	GroupActivation      *string `json:"groupActivation,omitempty"`
}

// ApplyPatch applies non-nil fields from the patch to the session in place.
// Returns true if any field was changed.
func (s *Session) ApplyPatch(p PatchFields) bool {
	changed := false

	if p.Label != nil && *p.Label != s.Label {
		s.Label = *p.Label
		changed = true
	}
	if p.Model != nil && *p.Model != s.Model {
		s.Model = *p.Model
		changed = true
	}
	if p.ThinkingLevel != nil && *p.ThinkingLevel != s.ThinkingLevel {
		s.ThinkingLevel = *p.ThinkingLevel
		changed = true
	}
	if p.FastMode != nil {
		if s.FastMode == nil || *p.FastMode != *s.FastMode {
			v := *p.FastMode
			s.FastMode = &v
			changed = true
		}
	}
	if p.VerboseLevel != nil && *p.VerboseLevel != s.VerboseLevel {
		s.VerboseLevel = *p.VerboseLevel
		changed = true
	}
	if p.ReasoningLevel != nil && *p.ReasoningLevel != s.ReasoningLevel {
		s.ReasoningLevel = *p.ReasoningLevel
		changed = true
	}
	if p.ElevatedLevel != nil && *p.ElevatedLevel != s.ElevatedLevel {
		s.ElevatedLevel = *p.ElevatedLevel
		changed = true
	}
	if p.ExecHost != nil && *p.ExecHost != s.ExecHost {
		s.ExecHost = *p.ExecHost
		changed = true
	}
	if p.ExecSecurity != nil && *p.ExecSecurity != s.ExecSecurity {
		s.ExecSecurity = *p.ExecSecurity
		changed = true
	}
	if p.ExecAsk != nil && *p.ExecAsk != s.ExecAsk {
		s.ExecAsk = *p.ExecAsk
		changed = true
	}
	if p.ExecNode != nil && *p.ExecNode != s.ExecNode {
		s.ExecNode = *p.ExecNode
		changed = true
	}
	if p.ResponseUsage != nil && *p.ResponseUsage != s.ResponseUsage {
		s.ResponseUsage = *p.ResponseUsage
		changed = true
	}
	if p.SpawnedBy != nil && *p.SpawnedBy != s.SpawnedBy {
		s.SpawnedBy = *p.SpawnedBy
		changed = true
	}
	if p.SpawnedWorkspaceDir != nil && *p.SpawnedWorkspaceDir != s.SpawnedWorkspaceDir {
		s.SpawnedWorkspaceDir = *p.SpawnedWorkspaceDir
		changed = true
	}
	if p.SpawnDepth != nil {
		if s.SpawnDepth == nil || *p.SpawnDepth != *s.SpawnDepth {
			v := *p.SpawnDepth
			s.SpawnDepth = &v
			changed = true
		}
	}
	if p.SubagentRole != nil && *p.SubagentRole != s.SubagentRole {
		s.SubagentRole = *p.SubagentRole
		changed = true
	}
	if p.SubagentControlScope != nil && *p.SubagentControlScope != s.SubagentControlScope {
		s.SubagentControlScope = *p.SubagentControlScope
		changed = true
	}
	if p.SendPolicy != nil && *p.SendPolicy != s.SendPolicy {
		s.SendPolicy = *p.SendPolicy
		changed = true
	}
	if p.GroupActivation != nil && *p.GroupActivation != s.GroupActivation {
		s.GroupActivation = *p.GroupActivation
		changed = true
	}

	if changed {
		s.UpdatedAt = time.Now().UnixMilli()
	}
	return changed
}

// Patch applies a PatchFields to the session identified by key.
// Creates the session if it doesn't exist. Returns a snapshot copy.
func (m *Manager) Patch(key string, patch PatchFields) *Session {
	m.mu.Lock()
	s := m.sessions[key]
	if s == nil {
		s = &Session{Key: key, Kind: KindUnknown, CreatedAt: time.Now()}
		m.sessions[key] = s
	}
	changed := s.ApplyPatch(patch)
	cp := *s
	m.mu.Unlock()

	if changed {
		m.eventBus.Emit(Event{Kind: EventStatusChanged, Key: key})
	}
	return &cp
}

// ResetSession resets a session's runtime state to initial values.
// Returns a snapshot copy of the reset session, or nil if not found.
func (m *Manager) ResetSession(key string) *Session {
	m.mu.Lock()
	s := m.sessions[key]
	if s == nil {
		m.mu.Unlock()
		return nil
	}
	oldStatus := s.Status
	s.Status = ""
	s.StartedAt = nil
	s.EndedAt = nil
	s.RuntimeMs = nil
	s.AbortedLastRun = false
	s.InputTokens = nil
	s.OutputTokens = nil
	s.TotalTokens = nil
	s.UpdatedAt = time.Now().UnixMilli()
	cp := *s
	m.mu.Unlock()

	if oldStatus != "" {
		m.eventBus.Emit(Event{Kind: EventStatusChanged, Key: key, OldStatus: oldStatus, NewStatus: ""})
	}
	return &cp
}

// FindBySessionID scans all sessions for one matching the given sessionId.
// Returns nil if not found.
func (m *Manager) FindBySessionID(sessionID string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.sessions {
		if s.SessionID == sessionID {
			cp := *s
			return &cp
		}
	}
	return nil
}

// FindByLabel scans all sessions for one matching the given label.
// Returns nil if not found, or the first match if multiple exist.
func (m *Manager) FindByLabel(label string) []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var matches []*Session
	for _, s := range m.sessions {
		if s.Label == label {
			cp := *s
			matches = append(matches, &cp)
		}
	}
	return matches
}

// ClearTokens clears token accounting fields for a session.
// Used after compaction to reset stale token counts.
func (m *Manager) ClearTokens(key string) {
	m.mu.Lock()
	s := m.sessions[key]
	if s != nil {
		s.InputTokens = nil
		s.OutputTokens = nil
		s.TotalTokens = nil
		s.UpdatedAt = time.Now().UnixMilli()
	}
	m.mu.Unlock()
}
