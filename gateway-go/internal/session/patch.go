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
	ResponseUsage        *string `json:"responseUsage,omitempty"`
	SpawnedBy            *string `json:"spawnedBy,omitempty"`
	SpawnedWorkspaceDir  *string `json:"spawnedWorkspaceDir,omitempty"`
	SpawnDepth           *int    `json:"spawnDepth,omitempty"`
	SubagentRole         *string `json:"subagentRole,omitempty"`
	SubagentControlScope *string `json:"subagentControlScope,omitempty"`
	SendPolicy           *string `json:"sendPolicy,omitempty"`
	GroupActivation      *string `json:"groupActivation,omitempty"`
}

// patchStr sets dst to *src if src is non-nil and differs.
func patchStr(dst *string, src *string) bool {
	if src != nil && *src != *dst {
		*dst = *src
		return true
	}
	return false
}

// patchBool sets dst to a copy of *src if src is non-nil and differs.
func patchBool(dst **bool, src *bool) bool {
	if src == nil {
		return false
	}
	if *dst == nil || **dst != *src {
		v := *src
		*dst = &v
		return true
	}
	return false
}

// patchInt sets dst to a copy of *src if src is non-nil and differs.
func patchInt(dst **int, src *int) bool {
	if src == nil {
		return false
	}
	if *dst == nil || **dst != *src {
		v := *src
		*dst = &v
		return true
	}
	return false
}

// ApplyPatch applies non-nil fields from the patch to the session in place.
// Returns true if any field was changed.
func (s *Session) ApplyPatch(p PatchFields) bool {
	changed := false
	changed = patchStr(&s.Label, p.Label) || changed
	changed = patchStr(&s.Model, p.Model) || changed
	changed = patchStr(&s.ThinkingLevel, p.ThinkingLevel) || changed
	changed = patchBool(&s.FastMode, p.FastMode) || changed
	changed = patchStr(&s.VerboseLevel, p.VerboseLevel) || changed
	changed = patchStr(&s.ReasoningLevel, p.ReasoningLevel) || changed
	changed = patchStr(&s.ElevatedLevel, p.ElevatedLevel) || changed
	changed = patchStr(&s.ExecHost, p.ExecHost) || changed
	changed = patchStr(&s.ExecSecurity, p.ExecSecurity) || changed
	changed = patchStr(&s.ExecAsk, p.ExecAsk) || changed
	changed = patchStr(&s.ResponseUsage, p.ResponseUsage) || changed
	changed = patchStr(&s.SpawnedBy, p.SpawnedBy) || changed
	changed = patchStr(&s.SpawnedWorkspaceDir, p.SpawnedWorkspaceDir) || changed
	changed = patchInt(&s.SpawnDepth, p.SpawnDepth) || changed
	changed = patchStr(&s.SubagentRole, p.SubagentRole) || changed
	changed = patchStr(&s.SubagentControlScope, p.SubagentControlScope) || changed
	changed = patchStr(&s.SendPolicy, p.SendPolicy) || changed
	changed = patchStr(&s.GroupActivation, p.GroupActivation) || changed

	if changed {
		s.UpdatedAt = time.Now().UnixMilli()
	}
	return changed
}

// Patch applies a PatchFields to the session identified by key.
// Creates the session if it doesn't exist. Returns a snapshot copy.
func (m *Manager) Patch(key string, patch PatchFields) *Session {
	m.emitMu.Lock()
	defer m.emitMu.Unlock()

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
	m.emitMu.Lock()
	defer m.emitMu.Unlock()

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

// FindByLabel returns all sessions matching the given label.
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
