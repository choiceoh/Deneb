package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

const refWaitTimeout = 30 * time.Second

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
	Hidden      bool   // if true, excluded from LLMTools() but still callable via Execute (e.g. pilot-only tools)
	Profile     string // optional: "coding" = coding-only, "" = available in all profiles
}

// ToolRegistry maps tool names to tool definitions (executor + schema + description).
type ToolRegistry struct {
	mu             sync.RWMutex
	tools          map[string]ToolDef
	order          []string // preserves registration order
	postProcess    *PostProcessRegistry
	cachedLLMTools []llm.Tool // cached tool list; invalidated on RegisterTool
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
	r.cachedLLMTools = nil // invalidate cache
}

// Execute runs the named tool. Returns an error if the tool is not found.
//
// If the input contains "$ref", the referenced tool's output (from TurnContext)
// is injected into the input as "_ref_content" before execution.
//
// If the input contains "compress": true, the tool output is automatically
// compressed via the local sglang model before returning. This lets the AI
// agent opt-in to compression on a per-call basis to save context tokens.
func (r *ToolRegistry) Execute(ctx context.Context, name string, input json.RawMessage) (string, error) {
	r.mu.RLock()
	def, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("unknown tool: %q", name)
	}

	// Check for compress flag before executing (avoids re-parsing in every tool).
	wantCompress := extractCompressFlag(input)

	// Resolve $ref: wait for the referenced tool result and inject it.
	input = resolveRef(ctx, input)

	output, err := def.Fn(ctx, input)
	if err != nil {
		return output, err
	}

	// Apply post-processors.
	if r.postProcess != nil {
		output = r.postProcess.Apply(ctx, name, output)
	}

	// Apply compression if requested by the agent.
	if wantCompress && len(output) > 0 {
		output = compressToolOutput(ctx, name, output, slog.Default())
	}

	return output, nil
}

// SetPostProcess attaches a PostProcessRegistry to the tool registry.
func (r *ToolRegistry) SetPostProcess(pp *PostProcessRegistry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.postProcess = pp
}

// extractCompressFlag checks if input JSON contains "compress": true.
func extractCompressFlag(input json.RawMessage) bool {
	var meta struct {
		Compress bool `json:"compress"`
	}
	if json.Unmarshal(input, &meta) == nil {
		return meta.Compress
	}
	return false
}

// resolveRef checks for a "$ref" field in the input. If present, it waits for
// the referenced tool result from TurnContext and injects the output as
// "_ref_content" into the input JSON. This enables tool chaining: one tool can
// consume the output of a previously (or concurrently) executed tool.
func resolveRef(ctx context.Context, input json.RawMessage) json.RawMessage {
	var meta struct {
		Ref string `json:"$ref"`
	}
	if json.Unmarshal(input, &meta) != nil || meta.Ref == "" {
		return input
	}

	tc := TurnContextFromContext(ctx)
	if tc == nil {
		return input
	}

	result, ok := tc.Wait(ctx, meta.Ref, refWaitTimeout)
	if !ok {
		// Timeout — inject error message as ref content.
		return injectRefContent(input, fmt.Sprintf("[ref timeout: %s not available within %s]", meta.Ref, refWaitTimeout))
	}

	return injectRefContent(input, result.Output)
}

// injectRefContent adds "_ref_content" to the input JSON object.
func injectRefContent(input json.RawMessage, content string) json.RawMessage {
	var obj map[string]json.RawMessage
	if json.Unmarshal(input, &obj) != nil {
		return input
	}
	contentBytes, _ := json.Marshal(content)
	obj["_ref_content"] = contentBytes
	result, err := json.Marshal(obj)
	if err != nil {
		return input
	}
	return result
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
// in registration order. Results are cached and only rebuilt when tools change.
func (r *ToolRegistry) LLMTools() []llm.Tool {
	r.mu.RLock()
	if r.cachedLLMTools != nil {
		// Return a copy so callers (e.g., Anthropic cache_control injection)
		// can mutate their slice without corrupting the cache.
		out := make([]llm.Tool, len(r.cachedLLMTools))
		copy(out, r.cachedLLMTools)
		r.mu.RUnlock()
		return out
	}
	r.mu.RUnlock()

	// Cache miss — build and store under write lock.
	r.mu.Lock()
	defer r.mu.Unlock()
	// Double-check after acquiring write lock.
	if r.cachedLLMTools != nil {
		out := make([]llm.Tool, len(r.cachedLLMTools))
		copy(out, r.cachedLLMTools)
		return out
	}
	tools := make([]llm.Tool, 0, len(r.order))
	for _, name := range r.order {
		def := r.tools[name]
		if def.Hidden {
			continue
		}
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
	r.cachedLLMTools = tools
	out := make([]llm.Tool, len(tools))
	copy(out, tools)
	return out
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
