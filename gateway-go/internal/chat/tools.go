package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolpreset"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

const (
	refWaitInitial = 2 * time.Second
	refWaitMax     = 30 * time.Second
)

// ToolExecutor executes a named tool with JSON input and returns the result.
type ToolExecutor = agent.ToolExecutor

// Type aliases — canonical definitions are in toolctx/.
type ToolFunc = toolctx.ToolFunc
type ToolDef = toolctx.ToolDef

// ToolRegistry maps tool names to tool definitions (executor + schema + description).
type ToolRegistry struct {
	mu                      sync.RWMutex
	tools                   map[string]ToolDef
	order                   []string // preserves registration order
	postProcess             *PostProcessRegistry
	spillStore              *agent.SpilloverStore // optional; spills large tool results to disk
	cachedLLMTools          []llm.Tool            // cached tool list; invalidated on RegisterTool
	cachedLLMToolsAnthropic []llm.Tool            // same list with CacheControl on last tool for Anthropic prompt caching
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
	r.cachedLLMTools = nil          // invalidate full-set cache
	r.cachedLLMToolsAnthropic = nil // invalidate Anthropic variant
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

	// Enforce tool preset: reject tools not in the allowed set.
	// This is a defense-in-depth check — the LLM only sees filtered tools,
	// but if it hallucinates a tool call, this blocks execution.
	if preset := toolctx.ToolPresetFromContext(ctx); preset != "" {
		if allowed := toolpreset.AllowedTools(toolpreset.Preset(preset)); allowed != nil {
			if !allowed[name] {
				return "", fmt.Errorf("tool %q is not allowed for preset %q", name, preset)
			}
		}
	}

	// Check for compress flag before executing (avoids re-parsing in every tool).
	wantCompress := extractCompressFlag(input)

	// Resolve $ref: wait for the referenced tool result and inject it.
	input = resolveRef(ctx, input)

	// Check run-level cache for idempotent read tools (find, tree).
	// Cached results include post-processing but not compression.
	rc := RunCacheFromContext(ctx)
	if rc != nil && IsCacheableTool(name) {
		cacheKey := BuildCacheKey(name, input)
		if cached, ok := rc.Get(cacheKey); ok {
			if wantCompress && len(cached) > 0 {
				return compressToolOutput(ctx, name, cached, slog.Default()), nil
			}
			return cached, nil
		}
	}

	output, err := def.Fn(ctx, input)
	if err != nil {
		return output, err
	}

	// Spill large results to disk before post-processing truncates them.
	// The preview (~2K) replaces the full output in the context window.
	if r.spillStore != nil && len(output) > agent.MaxResultChars {
		sessionKey := toolctx.SessionKeyFromContext(ctx)
		output = r.spillStore.SpillAndPreview(sessionKey, name, output)
	}

	// Invalidate caches when mutation tools modify the file system.
	if IsMutationTool(name) {
		if rc != nil {
			rc.Invalidate()
		}
		// Invalidate the specific file in the file-read dedup cache.
		if fc := toolctx.FileCacheFromContext(ctx); fc != nil {
			if filePath := extractFilePath(input); filePath != "" {
				fc.Invalidate(filePath)
			}
		}
	}

	// Apply post-processors.
	if r.postProcess != nil {
		output = r.postProcess.Apply(ctx, name, output)
	}

	// Store in run cache (after post-processing, before compression).
	if rc != nil && IsCacheableTool(name) {
		cacheKey := BuildCacheKey(name, input)
		rc.Set(cacheKey, output)
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

// SetSpilloverStore attaches a SpilloverStore for spilling large tool results.
func (r *ToolRegistry) SetSpilloverStore(ss *agent.SpilloverStore) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spillStore = ss
}

// SpilloverStore returns the attached SpilloverStore, or nil.
func (r *ToolRegistry) SpilloverStore() *agent.SpilloverStore {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.spillStore
}

// extractFilePath extracts a "file_path" string from tool input JSON.
// Used to invalidate specific file-read cache entries on mutations.
func extractFilePath(input json.RawMessage) string {
	if !bytes.Contains(input, []byte(`"file_path"`)) {
		return ""
	}
	var meta struct {
		FilePath string `json:"file_path"`
	}
	if json.Unmarshal(input, &meta) == nil {
		return meta.FilePath
	}
	return ""
}

// extractCompressFlag checks if input JSON contains "compress": true.
// Fast-path: skip json.Unmarshal when the key is absent (common case).
func extractCompressFlag(input json.RawMessage) bool {
	if !bytes.Contains(input, []byte(`"compress"`)) {
		return false
	}
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
	// Fast-path: skip json.Unmarshal when $ref is absent (vast majority of calls).
	if !bytes.Contains(input, []byte(`"$ref"`)) {
		return input
	}
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

	// Progressive timeout: try a short initial wait first (handles the common
	// case where the referenced tool completes quickly). If that misses, extend
	// to the remaining context deadline (capped at refWaitMax).
	timeout := refWaitInitial
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < timeout {
			timeout = remaining
		}
	}

	result, ok := tc.Wait(ctx, meta.Ref, timeout)
	if !ok && timeout < refWaitMax {
		// First wait expired — try again up to the max.
		extended := refWaitMax - timeout
		if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
			if remaining := time.Until(deadline); remaining < extended {
				extended = remaining
			}
		}
		if extended > 0 {
			result, ok = tc.Wait(ctx, meta.Ref, extended)
		}
	}
	if !ok {
		return injectRefContent(input, fmt.Sprintf("[ref timeout: %s not available within %s]", meta.Ref, refWaitMax))
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
// The returned slice is shared — callers must not mutate it.
func (r *ToolRegistry) LLMTools() []llm.Tool {
	r.mu.RLock()
	if r.cachedLLMTools != nil {
		out := r.cachedLLMTools
		r.mu.RUnlock()
		return out
	}
	r.mu.RUnlock()

	// Cache miss — build and store under write lock.
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cachedLLMTools != nil {
		return r.cachedLLMTools
	}
	r.cachedLLMTools = r.buildLLMToolsLocked()
	return r.cachedLLMTools
}

// LLMToolsAnthropic returns tool definitions with CacheControl set on the last
// tool for Anthropic prompt caching. Results are cached alongside the base list
// and only rebuilt when tools change. The returned slice is shared — callers
// must not mutate it.
func (r *ToolRegistry) LLMToolsAnthropic() []llm.Tool {
	r.mu.RLock()
	if r.cachedLLMToolsAnthropic != nil {
		out := r.cachedLLMToolsAnthropic
		r.mu.RUnlock()
		return out
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cachedLLMToolsAnthropic != nil {
		return r.cachedLLMToolsAnthropic
	}
	// Ensure the base list is built first.
	if r.cachedLLMTools == nil {
		r.cachedLLMTools = r.buildLLMToolsLocked()
	}
	base := r.cachedLLMTools
	if len(base) == 0 {
		r.cachedLLMToolsAnthropic = base
		return base
	}
	// Copy once at build time, then cache. Subsequent calls return the shared slice.
	anthropic := make([]llm.Tool, len(base))
	copy(anthropic, base)
	anthropic[len(anthropic)-1].CacheControl = &llm.CacheControl{Type: "ephemeral"}
	r.cachedLLMToolsAnthropic = anthropic
	return anthropic
}

// buildLLMToolsLocked builds the base tool slice with pre-serialized schemas.
// Pre-serialization avoids re-marshaling deeply nested map[string]any via
// reflection on every LLM API call. Caller must hold r.mu (write).
func (r *ToolRegistry) buildLLMToolsLocked() []llm.Tool {
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
		t := llm.Tool{
			Name:        def.Name,
			Description: def.Description,
			InputSchema: schema,
		}
		t.PreSerialize()
		tools = append(tools, t)
	}
	return tools
}

// FilteredLLMTools returns tool definitions filtered to only include tools in
// the allowed set. If allowed is nil or empty, returns all tools (no filtering).
// Unlike LLMTools(), the result is not cached since the filter varies per call.
func (r *ToolRegistry) FilteredLLMTools(allowed map[string]bool) []llm.Tool {
	if len(allowed) == 0 {
		return r.LLMTools()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]llm.Tool, 0, len(allowed))
	for _, name := range r.order {
		if !allowed[name] {
			continue
		}
		def := r.tools[name]
		if def.Hidden {
			continue
		}
		schema := def.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		t := llm.Tool{
			Name:        def.Name,
			Description: def.Description,
			InputSchema: schema,
		}
		t.PreSerialize()
		tools = append(tools, t)
	}
	return tools
}

// FilteredDefinitions returns tool definitions filtered to only include tools
// in the allowed set. If allowed is nil or empty, returns all definitions.
func (r *ToolRegistry) FilteredDefinitions(allowed map[string]bool) []ToolDef {
	if len(allowed) == 0 {
		return r.Definitions()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]ToolDef, 0, len(allowed))
	for _, name := range r.order {
		if allowed[name] {
			defs = append(defs, r.tools[name])
		}
	}
	return defs
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
