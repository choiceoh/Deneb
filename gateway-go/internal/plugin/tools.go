package plugin

import (
	"context"
	"sync"
)

// ToolDefinition describes a tool available to the agent.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
	PluginID    string         `json:"pluginId,omitempty"`
}

// ToolHandler executes a tool invocation.
type ToolHandler func(ctx context.Context, input map[string]any) (output string, err error)

// ToolRegistry manages available tools and their handlers.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]registeredTool
}

type registeredTool struct {
	definition ToolDefinition
	handler    ToolHandler
}

// NewToolRegistry creates a new tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]registeredTool),
	}
}

// Register adds a tool to the registry.
func (r *ToolRegistry) Register(def ToolDefinition, handler ToolHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[def.Name] = registeredTool{definition: def, handler: handler}
}

// Unregister removes a tool from the registry.
func (r *ToolRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
}

// Get returns a tool definition by name.
func (r *ToolRegistry) Get(name string) *ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	if !ok {
		return nil
	}
	cp := t.definition
	return &cp
}

// Execute invokes a tool by name with the given input.
func (r *ToolRegistry) Execute(ctx context.Context, name string, input map[string]any) (string, error) {
	r.mu.RLock()
	t, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return "", &ToolNotFoundError{Name: name}
	}
	return t.handler(ctx, input)
}

// List returns all registered tool definitions.
func (r *ToolRegistry) List() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t.definition)
	}
	return result
}

// ListByPlugin returns tools registered by a specific plugin.
func (r *ToolRegistry) ListByPlugin(pluginID string) []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []ToolDefinition
	for _, t := range r.tools {
		if t.definition.PluginID == pluginID {
			result = append(result, t.definition)
		}
	}
	return result
}

// Count returns the total number of registered tools.
func (r *ToolRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// ToolNotFoundError is returned when a tool is not in the registry.
type ToolNotFoundError struct {
	Name string
}

func (e *ToolNotFoundError) Error() string {
	return "tool not found: " + e.Name
}
