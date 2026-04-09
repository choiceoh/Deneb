// registry.go manages installed skill state: install/enable/disable/configure.
//
// This is the RPC-facing skill CRUD store (previously the skill/ package).
// It tracks which skills are installed, enabled, and their configuration.
// Distinct from Catalog which tracks filesystem-discovered skill metadata.
package skills

import (
	"fmt"
	"sync"
	"time"
)

// RegistryStatus represents the overall installed skill status.
type RegistryStatus struct {
	Skills       []RegisteredSkill `json:"skills"`
	RequiredBins []string          `json:"requiredBins,omitempty"`
}

// RegisteredSkill represents a single installed skill and its state.
type RegisteredSkill struct {
	Key         string            `json:"key"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Installed   bool              `json:"installed"`
	Enabled     bool              `json:"enabled"`
	Version     string            `json:"version,omitempty"`
	Config      map[string]string `json:"config,omitempty"`
	UpdatedAtMs int64             `json:"updatedAtMs,omitempty"`
}

// InstallAck holds the result of a skill installation request.
type InstallAck struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// ConfigPatch holds the fields to update on an installed skill.
type ConfigPatch struct {
	Enabled *bool             `json:"enabled,omitempty"`
	APIKey  string            `json:"apiKey,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// Registry manages installed skill state (install/enable/disable/configure).
type Registry struct {
	mu     sync.RWMutex
	skills map[string]*RegisteredSkill
	bins   []string
}

// NewRegistry creates a new skill registry.
func NewRegistry() *Registry {
	return &Registry{
		skills: make(map[string]*RegisteredSkill),
	}
}

// Status returns the full installed skill status report.
func (r *Registry) Status(_ string) *RegistryStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries := make([]RegisteredSkill, 0, len(r.skills))
	for _, s := range r.skills {
		entries = append(entries, *s)
	}
	return &RegistryStatus{
		Skills:       entries,
		RequiredBins: r.bins,
	}
}

// ListBins returns the list of required binary dependencies.
func (r *Registry) ListBins() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]string, len(r.bins))
	copy(result, r.bins)
	return result
}

// Install marks a skill as installed in the registry.
func (r *Registry) Install(name, _ string) *InstallAck {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.skills[name]; ok && existing.Installed {
		return &InstallAck{OK: true, Message: fmt.Sprintf("skill %q already installed", name)}
	}

	now := time.Now().UnixMilli()
	r.skills[name] = &RegisteredSkill{
		Key:         name,
		Name:        name,
		Installed:   true,
		Enabled:     true,
		UpdatedAtMs: now,
	}

	return &InstallAck{OK: true, Message: fmt.Sprintf("skill %q installed", name)}
}

// Update updates an installed skill's configuration.
func (r *Registry) Update(skillKey string, patch ConfigPatch) (*RegisteredSkill, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.skills[skillKey]
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

// RegisterSkill adds a skill to the registry (used during initialization).
func (r *Registry) RegisterSkill(entry RegisteredSkill) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skills[entry.Key] = &entry
}

// SetBins sets the required binary dependencies list.
func (r *Registry) SetBins(bins []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bins = bins
}
