// Package skill manages skill discovery, installation, and configuration updates.
//
// This ports the TypeScript skill system (src/gateway/server-methods/skills/skills.ts)
// to Go, providing in-memory skill registry and install/update operations.
package skill

import (
	"fmt"
	"sync"
	"time"
)

// Status represents the overall skill status for an agent.
type Status struct {
	Skills       []SkillEntry `json:"skills"`
	RequiredBins []string     `json:"requiredBins,omitempty"`
}

// SkillEntry represents a single skill and its state.
type SkillEntry struct {
	Key         string            `json:"key"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Installed   bool              `json:"installed"`
	Enabled     bool              `json:"enabled"`
	Version     string            `json:"version,omitempty"`
	Config      map[string]string `json:"config,omitempty"`
	UpdatedAtMs int64             `json:"updatedAtMs,omitempty"`
}

// InstallResult holds the result of a skill installation.
type InstallResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// Manager manages skill discovery, installation, and configuration.
type Manager struct {
	mu     sync.RWMutex
	skills map[string]*SkillEntry
	bins   []string
}

// NewManager creates a new skill manager.
func NewManager() *Manager {
	return &Manager{
		skills: make(map[string]*SkillEntry),
	}
}

// GetStatus returns the full skill status report, optionally filtered by agentID.
func (m *Manager) GetStatus(_ string) *Status {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries := make([]SkillEntry, 0, len(m.skills))
	for _, s := range m.skills {
		entries = append(entries, *s)
	}
	return &Status{
		Skills:       entries,
		RequiredBins: m.bins,
	}
}

// ListBins returns the list of required binary dependencies.
func (m *Manager) ListBins() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]string, len(m.bins))
	copy(result, m.bins)
	return result
}

// Install installs a skill by name with the given install ID (for tracking).
func (m *Manager) Install(name, installID string) *InstallResult {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.skills[name]; ok && existing.Installed {
		return &InstallResult{OK: true, Message: fmt.Sprintf("skill %q already installed", name)}
	}

	now := time.Now().UnixMilli()
	m.skills[name] = &SkillEntry{
		Key:         name,
		Name:        name,
		Installed:   true,
		Enabled:     true,
		UpdatedAtMs: now,
	}
	_ = installID

	return &InstallResult{OK: true, Message: fmt.Sprintf("skill %q installed", name)}
}

// Update updates a skill's configuration (enabled state, API key, env vars).
func (m *Manager) Update(skillKey string, patch SkillPatch) (*SkillEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.skills[skillKey]
	if !ok {
		return nil, fmt.Errorf("skill %q not found", skillKey)
	}

	if patch.Enabled != nil {
		entry.Enabled = *patch.Enabled
	}
	if patch.APIKey != "" {
		if entry.Config == nil {
			entry.Config = make(map[string]string)
		}
		entry.Config["apiKey"] = patch.APIKey
	}
	if patch.Env != nil {
		if entry.Config == nil {
			entry.Config = make(map[string]string)
		}
		for k, v := range patch.Env {
			entry.Config[k] = v
		}
	}
	entry.UpdatedAtMs = time.Now().UnixMilli()

	cp := *entry
	return &cp, nil
}

// SkillPatch holds the fields to update on a skill.
type SkillPatch struct {
	Enabled *bool             `json:"enabled,omitempty"`
	APIKey  string            `json:"apiKey,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// RegisterSkill adds a skill to the registry (used during initialization).
func (m *Manager) RegisterSkill(entry SkillEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.skills[entry.Key] = &entry
}

// SetBins sets the required binary dependencies list.
func (m *Manager) SetBins(bins []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bins = bins
}
