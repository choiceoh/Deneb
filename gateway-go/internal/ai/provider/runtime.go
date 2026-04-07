// runtime.go — Provider runtime resolution for the Go gateway.
//
// Resolves provider plugins and dispatches provider hook calls
// (capabilities, runtime auth).
package provider

import (
	"context"
	"log/slog"
	"sync"
)

// ProviderRuntimeResolver resolves provider plugins and dispatches hook calls.
type ProviderRuntimeResolver struct {
	mu       sync.RWMutex
	registry *Registry
	cache    map[string]Plugin // cached lookups by normalized provider ID
	logger   *slog.Logger
}

// NewProviderRuntimeResolver creates a new provider runtime resolver.
func NewProviderRuntimeResolver(registry *Registry, logger *slog.Logger) *ProviderRuntimeResolver {
	return &ProviderRuntimeResolver{
		registry: registry,
		cache:    make(map[string]Plugin),
		logger:   logger,
	}
}

// ResetCache clears the cached plugin lookups.
func (r *ProviderRuntimeResolver) ResetCache() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = make(map[string]Plugin)
}

// ResolvePlugin finds the provider plugin for the given provider ID.
// Uses normalized ID matching with alias support.
func (r *ProviderRuntimeResolver) ResolvePlugin(providerID string) Plugin {
	normalized := NormalizeProviderID(providerID)
	if normalized == "" {
		return nil
	}

	// Check cache first.
	r.mu.RLock()
	cached, ok := r.cache[normalized]
	r.mu.RUnlock()
	if ok {
		return cached
	}

	// Look up in registry using the normalized ID.
	plugin := r.registry.ByNormalizedID(normalized)

	// Cache the result (including nil for negative caching).
	r.mu.Lock()
	r.cache[normalized] = plugin
	r.mu.Unlock()

	return plugin
}

// ResolveCapabilities returns the plugin's static capabilities.
func (r *ProviderRuntimeResolver) ResolveCapabilities(providerID string) *Capabilities {
	plugin := r.ResolvePlugin(providerID)
	if plugin == nil {
		return nil
	}
	if cp, ok := plugin.(CapabilitiesProvider); ok {
		caps := cp.Capabilities()
		return &caps
	}
	return nil
}

// PrepareRuntimeAuth calls the plugin's runtime auth exchange hook.
func (r *ProviderRuntimeResolver) PrepareRuntimeAuth(ctx context.Context, providerID string, actx RuntimeAuthContext) (*PreparedAuth, error) {
	plugin := r.ResolvePlugin(providerID)
	if plugin == nil {
		return nil, nil
	}
	if rap, ok := plugin.(RuntimeAuthProvider); ok {
		return rap.PrepareRuntimeAuth(ctx, actx)
	}
	return nil, nil
}
