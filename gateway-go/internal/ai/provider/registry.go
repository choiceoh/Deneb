package provider

import (
	"context"
	"fmt"
	"sync"
)

// Plugin is the base interface that all provider plugins must implement.
type Plugin interface {
	ID() string
	Label() string
	AuthMethods() []AuthMethod
}

// --- Optional adapter interfaces (checked via type assertion) ---

// CatalogProvider discovers models from a provider.
type CatalogProvider interface {
	Catalog(ctx context.Context, cctx CatalogContext) (*CatalogResult, error)
}

// RuntimeAuthProvider prepares runtime authentication credentials.
type RuntimeAuthProvider interface {
	PrepareRuntimeAuth(ctx context.Context, cctx RuntimeAuthContext) (*PreparedAuth, error)
}

// CapabilitiesProvider reports provider-level feature flags.
type CapabilitiesProvider interface {
	Capabilities() Capabilities
}

// Registry manages registered provider plugins.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Plugin
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Plugin),
	}
}

// Register adds a provider plugin to the registry.
func (r *Registry) Register(p Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	id := p.ID()
	if _, exists := r.providers[id]; exists {
		return fmt.Errorf("provider %q already registered", id)
	}
	r.providers[id] = p
	return nil
}

// Get returns a provider plugin by ID, or nil if not found.
func (r *Registry) Get(id string) Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.providers[id]
}

// List returns all registered provider IDs.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	return ids
}

// Snapshot returns a copy of all registered plugins for iteration
// without holding the registry lock.
func (r *Registry) Snapshot() map[string]Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snap := make(map[string]Plugin, len(r.providers))
	for id, p := range r.providers {
		snap[id] = p
	}
	return snap
}
