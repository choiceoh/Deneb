// Package plugin implements a simplified plugin runtime for the Go gateway.
//
// This is the Go equivalent of src/plugins/ from the TypeScript codebase,
// radically simplified for the Telegram-only, single-user DGX Spark deployment.
// Instead of the full plugin discovery/loading/manifest system (~36K LOC in TS),
// this provides only what's needed:
//
//   - A minimal plugin registry for wiring channel and provider plugins
//   - A hook runner for pre/post-agent hooks
//   - Provider resolution (model selection, auth)
//
// Multi-channel discovery, marketplace, interactive handlers, and dynamic
// install/update/uninstall are deliberately omitted — the operator configures
// everything directly.
package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// PluginKind identifies the type of plugin.
type PluginKind string

const (
	KindChannel  PluginKind = "channel"
	KindProvider PluginKind = "provider"
	KindHook     PluginKind = "hook"
	KindTool     PluginKind = "tool"
)

// PluginMeta describes a registered plugin.
type PluginMeta struct {
	ID      string     `json:"id"`
	Kind    PluginKind `json:"kind"`
	Label   string     `json:"label,omitempty"`
	Version string     `json:"version,omitempty"`
	Enabled bool       `json:"enabled"`
}

// Registry is a minimal plugin registry for the Telegram-only deployment.
// It holds registered plugins (channels, providers, hooks) and provides
// lookup methods for the gateway runtime.
type Registry struct {
	mu       sync.RWMutex
	plugins  map[string]*PluginMeta
	hooks    []HookEntry
	logger   *slog.Logger
}

// NewRegistry creates a new plugin registry.
func NewRegistry(logger *slog.Logger) *Registry {
	return &Registry{
		plugins: make(map[string]*PluginMeta),
		logger:  logger,
	}
}

// Register adds a plugin to the registry.
func (r *Registry) Register(meta PluginMeta) error {
	if meta.ID == "" {
		return fmt.Errorf("plugin ID is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[meta.ID] = &meta
	r.logger.Info("plugin registered", "id", meta.ID, "kind", meta.Kind)
	return nil
}

// Get returns a plugin by ID.
func (r *Registry) Get(id string) *PluginMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.plugins[id]
}

// List returns all registered plugins.
func (r *Registry) List() []PluginMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]PluginMeta, 0, len(r.plugins))
	for _, p := range r.plugins {
		result = append(result, *p)
	}
	return result
}

// ListByKind returns all plugins of a specific kind.
func (r *Registry) ListByKind(kind PluginKind) []PluginMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []PluginMeta
	for _, p := range r.plugins {
		if p.Kind == kind {
			result = append(result, *p)
		}
	}
	return result
}

// IsEnabled returns true if the plugin exists and is enabled.
func (r *Registry) IsEnabled(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[id]
	return ok && p.Enabled
}

// --- Hook System ---

// HookName identifies a hook point in the gateway lifecycle.
type HookName string

const (
	HookBeforeAgentStart  HookName = "before_agent_start"
	HookAfterAgentEnd     HookName = "after_agent_end"
	HookBeforeSend        HookName = "before_send"
	HookAfterSend         HookName = "after_send"
	HookMessageReceived   HookName = "message_received"
	HookSessionCreated    HookName = "session_created"
	HookSessionReset      HookName = "session_reset"
	HookCronJobStart      HookName = "cron_job_start"
	HookCronJobEnd        HookName = "cron_job_end"
)

// HookFunc is the signature for hook handlers.
type HookFunc func(ctx context.Context, payload map[string]any) error

// HookEntry associates a hook function with a hook name and source plugin.
type HookEntry struct {
	Name     HookName
	PluginID string
	Handler  HookFunc
}

// RegisterHook adds a hook handler to the registry.
func (r *Registry) RegisterHook(name HookName, pluginID string, handler HookFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks = append(r.hooks, HookEntry{
		Name:     name,
		PluginID: pluginID,
		Handler:  handler,
	})
}

// RunHooks executes all registered hooks for the given name.
// Hooks are run sequentially; errors are logged but do not stop execution.
func (r *Registry) RunHooks(ctx context.Context, name HookName, payload map[string]any) []error {
	r.mu.RLock()
	var hooks []HookEntry
	for _, h := range r.hooks {
		if h.Name == name {
			hooks = append(hooks, h)
		}
	}
	r.mu.RUnlock()

	var errs []error
	for _, h := range hooks {
		if err := h.Handler(ctx, payload); err != nil {
			r.logger.Warn("hook error",
				"hook", string(name),
				"plugin", h.PluginID,
				"error", err,
			)
			errs = append(errs, err)
		}
	}
	return errs
}

// --- Provider Resolution ---

// ProviderConfig holds the configuration for a model provider.
type ProviderConfig struct {
	ID       string `json:"id"`
	Label    string `json:"label,omitempty"`
	BaseURL  string `json:"baseUrl,omitempty"`
	APIKey   string `json:"apiKey,omitempty"`
	Default  bool   `json:"default,omitempty"`
}

// ProviderCatalog manages available model providers.
type ProviderCatalog struct {
	mu        sync.RWMutex
	providers map[string]*ProviderConfig
}

// NewProviderCatalog creates a new provider catalog.
func NewProviderCatalog() *ProviderCatalog {
	return &ProviderCatalog{
		providers: make(map[string]*ProviderConfig),
	}
}

// Register adds a provider to the catalog.
func (c *ProviderCatalog) Register(cfg ProviderConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.providers[cfg.ID] = &cfg
}

// Get returns a provider by ID.
func (c *ProviderCatalog) Get(id string) *ProviderConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p := c.providers[id]
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}

// Default returns the default provider, or the first one if none is marked default.
func (c *ProviderCatalog) Default() *ProviderConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, p := range c.providers {
		if p.Default {
			cp := *p
			return &cp
		}
	}
	// Return first available.
	for _, p := range c.providers {
		cp := *p
		return &cp
	}
	return nil
}

// List returns all registered providers.
func (c *ProviderCatalog) List() []ProviderConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]ProviderConfig, 0, len(c.providers))
	for _, p := range c.providers {
		result = append(result, *p)
	}
	return result
}
