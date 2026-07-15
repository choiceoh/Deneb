// Package secret manages credential/secret resolution and reloading.
//
// Originally ported from the retired TypeScript gateway secrets system (was src/gateway/server-methods/admin/secrets.ts)
// to Go, providing in-memory secret resolution with reload capability.
package secret

import (
	"encoding/json"
	"sync"
	"time"
)

// Assignment represents a resolved secret path-value binding.
type Assignment struct {
	Path         string   `json:"path"`
	PathSegments []string `json:"pathSegments"`
	Value        rawJSON  `json:"value"`
}

// ResolveResult holds the result of a secret resolution call.
type ResolveResult struct {
	OK               bool         `json:"ok"`
	Assignments      []Assignment `json:"assignments"`
	Diagnostics      []string     `json:"diagnostics,omitempty"`
	InactiveRefPaths []string     `json:"inactiveRefPaths,omitempty"`
}

// ReloadResult holds the result of a secrets reload.
type ReloadResult struct {
	OK           bool `json:"ok"`
	WarningCount int  `json:"warningCount"`
}

// Resolver manages secret resolution and caching.
type Resolver struct {
	mu         sync.RWMutex
	secrets    map[string]rawJSON // flat path -> value
	loadedAtMs int64
	warnings   []string
}

// NewResolver creates a new secret resolver.
func NewResolver() *Resolver {
	return &Resolver{
		secrets:    make(map[string]rawJSON),
		loadedAtMs: time.Now().UnixMilli(),
	}
}

// Reload reloads secrets from the backing store (config/env).
// In the Go gateway, this refreshes the in-memory cache.
func (r *Resolver) Reload() *ReloadResult {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Re-read from environment or config. The actual secret loading
	// is delegated to the config system; here we clear warnings and re-timestamp.
	r.loadedAtMs = time.Now().UnixMilli()
	warningCount := len(r.warnings)
	r.warnings = nil

	return &ReloadResult{
		OK:           true,
		WarningCount: warningCount,
	}
}

// Resolve resolves secrets for a given command and target IDs.
func (r *Resolver) Resolve(commandName string, targetIDs []string) *ResolveResult {
	r.mu.RLock()
	defer r.mu.RUnlock()

	assignments := make([]Assignment, 0, len(targetIDs))
	var diagnostics []string
	var inactive []string

	for _, id := range targetIDs {
		path := commandName + "." + id
		val, ok := r.secrets[path]
		if ok {
			assignments = append(assignments, Assignment{
				Path:         path,
				PathSegments: []string{commandName, id},
				Value:        val,
			})
		} else {
			inactive = append(inactive, path)
		}
	}

	return &ResolveResult{
		OK:               true,
		Assignments:      assignments,
		Diagnostics:      diagnostics,
		InactiveRefPaths: inactive,
	}
}

// Set stores a secret value at the given path.
func (r *Resolver) Set(path string, value rawJSON) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.secrets[path] = value
}

// SetValue marshals v and stores it at path.
func (r *Resolver) SetValue(path string, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	r.Set(path, raw)
}
