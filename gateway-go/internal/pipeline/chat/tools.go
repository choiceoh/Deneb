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

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolpreset"
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
	mu             sync.RWMutex
	tools          map[string]ToolDef
	order          []string // preserves registration order
	postProcess    *PostProcessRegistry
	spillStore     *agent.SpilloverStore // optional; spills large tool results to disk
	cachedLLMTools []llm.Tool            // cached tool list; invalidated on RegisterTool
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
// compressed via the local AI model before returning. This lets the AI
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

	// Head/tail truncation — preserve both ends for LLM comprehension.
	// Build errors and test failures are typically at the end of output,
	// while context (paths, invocations) is at the start.  Keep both visible.
	maxOutput := agent.DefaultMaxOutput
	if def.MaxOutput > 0 {
		maxOutput = def.MaxOutput
	}
	if len(output) > maxOutput {
		var spillID string
		// Spill full content to disk so the LLM can retrieve it via read_spillover.
		if r.spillStore != nil {
			sessionKey := toolctx.SessionKeyFromContext(ctx)
			spillID, _ = r.spillStore.Store(sessionKey, name, output)
		}
		output = agent.TruncateHeadTail(output, maxOutput, spillID)
	}

	// Invalidate caches when mutation tools modify the file system.
	if IsMutationTool(name) {
		mutPath := extractFilePath(input)
		if rc != nil {
			if mutPath != "" {
				rc.InvalidateByPath(mutPath)
			} else {
				rc.Invalidate()
			}
		}
		if fc := toolctx.FileCacheFromContext(ctx); fc != nil {
			if mutPath != "" {
				fc.Invalidate(mutPath)
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
		scope := extractPathScope(input)
		rc.SetWithScope(cacheKey, output, scope)
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

// ApplyMaxOutputs sets per-tool max output budgets from a name→chars map.
// Tools not in the map keep their current MaxOutput (zero = default).
func (r *ToolRegistry) ApplyMaxOutputs(budgets map[string]int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, max := range budgets {
		if def, ok := r.tools[name]; ok {
			def.MaxOutput = max
			r.tools[name] = def
		}
	}
}

// IsConcurrencySafe returns true if the named tool declared ConcurrencySafe
// during registration, meaning it performs no shared-state mutations and is
// safe for parallel execution.
func (r *ToolRegistry) IsConcurrencySafe(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if def, ok := r.tools[name]; ok {
		return def.ConcurrencySafe
	}
	return false
}

// IsConcurrencySafeWithInput extends IsConcurrencySafe with input-aware
// classification. For most tools, this delegates to the static ConcurrencySafe
// flag. For "exec", it parses the command and checks whether it is read-only
// (e.g., "go test", "git status", "ls") to allow concurrent execution.
func (r *ToolRegistry) IsConcurrencySafeWithInput(name string, input json.RawMessage) bool {
	r.mu.RLock()
	def, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return false
	}
	if def.ConcurrencySafe {
		return true
	}
	// Input-aware override: exec commands that are read-only can run concurrently.
	if name == "exec" {
		return agent.IsReadOnlyExecCommand(input)
	}
	return false
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

// extractPathScope extracts a path scope from cacheable tool input JSON.
// Cacheable tools use "path" (find/tree/grep) or "file" (analyze) to indicate
// the search scope. Returns "" when no scope is present (workspace-wide).
func extractPathScope(input json.RawMessage) string {
	if !bytes.Contains(input, []byte(`"path"`)) && !bytes.Contains(input, []byte(`"file"`)) {
		return ""
	}
	var meta struct {
		Path string `json:"path"`
		File string `json:"file"`
	}
	if json.Unmarshal(input, &meta) == nil {
		if meta.Path != "" {
			return meta.Path
		}
		return meta.File
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

// buildLLMToolsLocked builds the base tool slice with pre-serialized schemas.
// Pre-serialization avoids re-marshaling deeply nested map[string]any via
// reflection on every LLM API call. Caller must hold r.mu (write).
func (r *ToolRegistry) buildLLMToolsLocked() []llm.Tool {
	tools := make([]llm.Tool, 0, len(r.order))
	for _, name := range r.order {
		def := r.tools[name]
		if def.Hidden || def.Deferred {
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
		if def.Hidden || def.Deferred {
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

// DeferredLLMTools returns pre-serialized LLM tool definitions for the named
// deferred tools. Unknown or non-deferred names are silently skipped.
func (r *ToolRegistry) DeferredLLMTools(names []string) []llm.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]llm.Tool, 0, len(names))
	for _, name := range names {
		def, ok := r.tools[name]
		if !ok || !def.Deferred {
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

// DeferredSummaries returns name+description for all deferred (non-hidden) tools.
func (r *ToolRegistry) DeferredSummaries() []toolctx.DeferredToolSummary {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []toolctx.DeferredToolSummary
	for _, name := range r.order {
		def := r.tools[name]
		if def.Deferred && !def.Hidden {
			out = append(out, toolctx.DeferredToolSummary{
				Name:        def.Name,
				Description: def.Description,
			})
		}
	}
	return out
}

// DeferredToolDef returns the ToolDef for a deferred tool, or false if not found/not deferred.
func (r *ToolRegistry) DeferredToolDef(name string) (ToolDef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.tools[name]
	if !ok || !def.Deferred {
		return ToolDef{}, false
	}
	return def, true
}
