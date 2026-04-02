// registry.go — Full plugin registry with channel/provider/tool/hook registration.
// Mirrors src/plugins/registry.ts (839 LOC).
package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// FullRegistry extends the basic Registry with full registration capabilities
// for providers, tools, commands, services, and HTTP routes.
type FullRegistry struct {
	mu           sync.RWMutex
	plugins      map[string]*PluginMeta
	providers    map[string]ProviderRegistration
	tools        map[string]ToolRegistration
	commands     map[string]CommandRegistration
	services     map[string]ServiceRegistration
	httpRoutes   map[string]HTTPRouteRegistration
	interactives map[string]InteractiveRegistration
	hookRunner   *TypedHookRunner
	logger       *slog.Logger
}

// ProviderRegistration describes a registered LLM provider.
type ProviderRegistration struct {
	PluginID   string
	ProviderID string
	Config     ProviderConfig
}

// ToolRegistration describes a registered tool.
type ToolRegistration struct {
	PluginID   string
	Definition ToolDefinition
	Handler    ToolHandler
}

// CommandRegistration describes a registered CLI command from a plugin.
type CommandRegistration struct {
	PluginID    string
	Name        string
	Description string
	Handler     func(ctx context.Context, args []string) error
}

// ServiceRegistration describes a long-running plugin service.
type ServiceRegistration struct {
	PluginID string
	Name     string
	Start    func(ctx context.Context) error
	Stop     func() error
}

// HTTPRouteRegistration describes a plugin-provided HTTP endpoint.
type HTTPRouteRegistration struct {
	PluginID string
	Method   string // "GET", "POST", etc.
	Path     string
	Handler  interface{} // http.HandlerFunc
}

// InteractiveRegistration describes a plugin-provided interactive handler.
type InteractiveRegistration struct {
	PluginID string
	Name     string
	Handler  func(ctx context.Context, input map[string]any) (map[string]any, error)
}

// NewFullRegistry creates a new full plugin registry.
func NewFullRegistry(logger *slog.Logger) *FullRegistry {
	return &FullRegistry{
		plugins:      make(map[string]*PluginMeta),
		providers:    make(map[string]ProviderRegistration),
		tools:        make(map[string]ToolRegistration),
		commands:     make(map[string]CommandRegistration),
		services:     make(map[string]ServiceRegistration),
		httpRoutes:   make(map[string]HTTPRouteRegistration),
		interactives: make(map[string]InteractiveRegistration),
		hookRunner:   NewTypedHookRunner(logger),
		logger:       logger,
	}
}

// --- Plugin registration ---

func (r *FullRegistry) RegisterPlugin(meta PluginMeta) error {
	if meta.ID == "" {
		return fmt.Errorf("plugin ID is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[meta.ID] = &meta
	r.logger.Info("plugin registered", "id", meta.ID, "kind", meta.Kind)
	return nil
}

func (r *FullRegistry) GetPlugin(id string) *PluginMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.plugins[id]
}

func (r *FullRegistry) ListPlugins() []PluginMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]PluginMeta, 0, len(r.plugins))
	for _, p := range r.plugins {
		result = append(result, *p)
	}
	return result
}

// --- Provider registration ---

func (r *FullRegistry) RegisterProvider(reg ProviderRegistration) error {
	if reg.ProviderID == "" {
		return fmt.Errorf("provider ID is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[reg.ProviderID] = reg
	r.logger.Info("provider registered", "provider", reg.ProviderID, "plugin", reg.PluginID)
	return nil
}

func (r *FullRegistry) GetProvider(id string) *ProviderRegistration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[id]
	if !ok {
		return nil
	}
	return &p
}

func (r *FullRegistry) ListProviders() []ProviderRegistration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ProviderRegistration, 0, len(r.providers))
	for _, p := range r.providers {
		result = append(result, p)
	}
	return result
}

// --- Tool registration ---

func (r *FullRegistry) RegisterTool(reg ToolRegistration) error {
	if reg.Definition.Name == "" {
		return fmt.Errorf("tool name is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[reg.Definition.Name] = reg
	r.logger.Info("tool registered", "tool", reg.Definition.Name, "plugin", reg.PluginID)
	return nil
}

func (r *FullRegistry) GetTool(name string) *ToolRegistration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	if !ok {
		return nil
	}
	return &t
}

func (r *FullRegistry) ListTools() []ToolRegistration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ToolRegistration, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t)
	}
	return result
}

// --- Command registration ---

func (r *FullRegistry) RegisterCommand(reg CommandRegistration) error {
	if reg.Name == "" {
		return fmt.Errorf("command name is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands[reg.Name] = reg
	return nil
}

func (r *FullRegistry) ListCommands() []CommandRegistration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]CommandRegistration, 0, len(r.commands))
	for _, c := range r.commands {
		result = append(result, c)
	}
	return result
}

// --- Service registration ---

func (r *FullRegistry) RegisterService(reg ServiceRegistration) error {
	if reg.Name == "" {
		return fmt.Errorf("service name is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.services[reg.Name] = reg
	return nil
}

func (r *FullRegistry) ListServices() []ServiceRegistration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ServiceRegistration, 0, len(r.services))
	for _, s := range r.services {
		result = append(result, s)
	}
	return result
}

// --- HTTP route registration ---

func (r *FullRegistry) RegisterHTTPRoute(reg HTTPRouteRegistration) error {
	if reg.Path == "" {
		return fmt.Errorf("HTTP route path is required")
	}
	key := reg.Method + " " + reg.Path
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.httpRoutes[key]; exists {
		return fmt.Errorf("HTTP route conflict: %s %s", reg.Method, reg.Path)
	}
	r.httpRoutes[key] = reg
	return nil
}

func (r *FullRegistry) ListHTTPRoutes() []HTTPRouteRegistration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]HTTPRouteRegistration, 0, len(r.httpRoutes))
	for _, route := range r.httpRoutes {
		result = append(result, route)
	}
	return result
}

// --- Interactive registration ---

func (r *FullRegistry) RegisterInteractive(reg InteractiveRegistration) error {
	if reg.Name == "" {
		return fmt.Errorf("interactive handler name is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.interactives[reg.Name] = reg
	return nil
}

// --- Hook registration (delegates to TypedHookRunner) ---

func (r *FullRegistry) RegisterHook(name HookName, pluginID string, handler HookFunc, opts HookOptions) {
	r.hookRunner.Register(TypedHookRegistration{
		HookName: name,
		PluginID: pluginID,
		Handler:  handler,
		Priority: int(opts.Priority),
		Options:  opts,
	})
}

// HookRunner returns the underlying TypedHookRunner so the server can wire
// it to the chat handler as pluginHookRunner.
func (r *FullRegistry) HookRunner() *TypedHookRunner {
	return r.hookRunner
}

// --- Summary ---

// RegistrySummary provides a snapshot of all registrations.
type RegistrySummary struct {
	Plugins      int `json:"plugins"`
	Providers    int `json:"providers"`
	Tools        int `json:"tools"`
	Commands     int `json:"commands"`
	Services     int `json:"services"`
	HTTPRoutes   int `json:"httpRoutes"`
	Interactives int `json:"interactives"`
	Hooks        int `json:"hooks"`
}

func (r *FullRegistry) Summary() RegistrySummary {
	r.mu.RLock()
	defer r.mu.RUnlock()
	hookNames := r.hookRunner.ListRegisteredHooks()
	return RegistrySummary{
		Plugins:      len(r.plugins),
		Providers:    len(r.providers),
		Tools:        len(r.tools),
		Commands:     len(r.commands),
		Services:     len(r.services),
		HTTPRoutes:   len(r.httpRoutes),
		Interactives: len(r.interactives),
		Hooks:        len(hookNames),
	}
}
