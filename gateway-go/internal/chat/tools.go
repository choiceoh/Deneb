package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// ToolExecutor executes a named tool with JSON input and returns the result.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, input json.RawMessage) (string, error)
}

// ToolFunc is an adapter to use ordinary functions as tool executors.
type ToolFunc func(ctx context.Context, input json.RawMessage) (string, error)

// ToolRegistry maps tool names to executor functions.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]ToolFunc
}

// NewToolRegistry creates an empty tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]ToolFunc),
	}
}

// Register adds a tool function by name.
func (r *ToolRegistry) Register(name string, fn ToolFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[name] = fn
}

// Execute runs the named tool. Returns an error if the tool is not found.
func (r *ToolRegistry) Execute(ctx context.Context, name string, input json.RawMessage) (string, error) {
	r.mu.RLock()
	fn, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("unknown tool: %q", name)
	}
	return fn(ctx, input)
}

// Names returns all registered tool names.
func (r *ToolRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}
