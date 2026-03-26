package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// ToolExecutor executes a named tool with JSON input and returns the result.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, input json.RawMessage) (string, error)
}

// ToolFunc is an adapter to use ordinary functions as tool executors.
type ToolFunc func(ctx context.Context, input json.RawMessage) (string, error)

// ToolDef describes a tool with its schema, description, and executor function.
type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]any
	Fn          ToolFunc
}

// ToolRegistry maps tool names to tool definitions (executor + schema + description).
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]ToolDef
	order []string // preserves registration order
}

// NewToolRegistry creates an empty tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]ToolDef),
	}
}

// Register adds a tool function by name with a placeholder description and empty schema.
// Prefer RegisterTool for full definitions.
func (r *ToolRegistry) Register(name string, fn ToolFunc) {
	r.RegisterTool(ToolDef{
		Name:        name,
		Description: "Tool: " + name,
		InputSchema: map[string]any{"type": "object"},
		Fn:          fn,
	})
}

// RegisterTool adds a fully defined tool (name, description, schema, executor).
func (r *ToolRegistry) RegisterTool(def ToolDef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[def.Name]; !exists {
		r.order = append(r.order, def.Name)
	}
	r.tools[def.Name] = def
}

// Execute runs the named tool. Returns an error if the tool is not found.
func (r *ToolRegistry) Execute(ctx context.Context, name string, input json.RawMessage) (string, error) {
	r.mu.RLock()
	def, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("unknown tool: %q", name)
	}
	return def.Fn(ctx, input)
}

// Names returns all registered tool names in registration order.
func (r *ToolRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// LLMTools returns tool definitions formatted for LLM API requests,
// in registration order.
func (r *ToolRegistry) LLMTools() []llm.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]llm.Tool, 0, len(r.order))
	for _, name := range r.order {
		def := r.tools[name]
		schema := def.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		tools = append(tools, llm.Tool{
			Name:        def.Name,
			Description: def.Description,
			InputSchema: schema,
		})
	}
	return tools
}

// Summaries returns a map of tool name → description for system prompt assembly.
func (r *ToolRegistry) Summaries() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m := make(map[string]string, len(r.tools))
	for name, def := range r.tools {
		m[name] = def.Description
	}
	return m
}

// SortedNames returns registered tool names sorted alphabetically.
func (r *ToolRegistry) SortedNames() []string {
	names := r.Names()
	sort.Strings(names)
	return names
}
