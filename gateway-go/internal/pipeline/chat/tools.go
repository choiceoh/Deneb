package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/runtimeops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/toolpreset"
)

const (
	refWaitInitial = 2 * time.Second
	refWaitMax     = 30 * time.Second
)

// ToolExecutor executes a named tool with JSON input and returns the result.
type ToolExecutor = agent.ToolExecutor

// Type aliases — canonical definitions are in toolctx/.
type (
	ToolFunc = toolctx.ToolFunc
	ToolDef  = toolctx.ToolDef
)

// Compile-time interface compliance.
var (
	_ agent.ToolExecutor    = (*ToolRegistry)(nil)
	_ toolctx.ToolRegistrar = (*ToolRegistry)(nil)
)

// ToolRegistry maps tool names to tool definitions (executor + schema + description).
type ToolRegistry struct {
	mu             sync.RWMutex
	tools          map[string]ToolDef
	order          []string // preserves registration order
	postProcess    *PostProcessRegistry
	spillStore     *agent.SpilloverStore // optional; spills large tool results to disk
	provenanceRoot string                // optional workspace root for content-free file effect metadata
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
//
// Re-registering an existing name silently replaces the prior definition.
// This is intentional (tests, hot-reload, and schema updates all rely on it)
// but future plugin systems may accidentally claim core tool names. We log a
// Warn so that collisions are at least visible in the operator log — see
// docs/research/tool-interception-gap.md §7 for the rationale.
func (r *ToolRegistry) RegisterTool(def ToolDef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[def.Name]; !exists {
		r.order = append(r.order, def.Name)
	} else {
		slog.Warn("tool registration replaced existing entry",
			"name", def.Name,
			"hint", "if this is unexpected, a plugin may be shadowing a core tool")
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
	if ctx == nil {
		return "", fmt.Errorf("tool %q requires a context", name)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	r.mu.RLock()
	def, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return "", r.unknownToolError(name)
	}
	presetName := toolctx.ToolPresetFromContext(ctx)
	briefcasePreset := presetName == string(toolpreset.PresetBriefcase)

	// Enforce tool preset: reject tools not in the allowed set.
	// This is a defense-in-depth check — the LLM only sees filtered tools,
	// but if it hallucinates a tool call, this blocks execution.
	if presetName != "" {
		if allowed := toolpreset.AllowedTools(toolpreset.Preset(presetName)); allowed != nil {
			if _, ok := allowed[name]; !ok {
				return "", fmt.Errorf("tool %q is not allowed for preset %q", name, presetName)
			}
		}
	}

	// Dry-run: suppress side-effect tools (everything not on the read-only
	// allowlist) before any execution machinery runs. See tool_dry_run.go.
	if toolctx.ToolDryRunFromContext(ctx) {
		if _, safe := dryRunSafeTools[name]; !safe {
			stub := dryRunStub(name)
			// Keep the verify gate faithful in replays (review catch on
			// #3171): the stub tells the model the call succeeded, so a
			// stubbed write/edit must arm the gate and a stubbed
			// verification exec must disarm it — otherwise a replayed edit
			// flow finishes without the finalize nudge a real run gets.
			verifyGateFromContext(ctx).recordTool(name, input, stub, nil)
			return stub, nil
		}
	}

	// Repair common malformed-JSON argument patterns from open-weight models
	// (markdown fences, Python literals, trailing commas) before any input
	// parsing. Only invalid JSON is touched, and only when the repair makes it
	// valid; otherwise the original bytes fall through to the tool's own
	// fail-fast parse error (the loop detector remains the backstop). The Warn
	// is the measurement signal for how often the main model emits malformed
	// calls — no argument content is logged (it may be sensitive).
	if !briefcasePreset {
		if repaired, didRepair := repairToolArguments(input); didRepair {
			slog.Warn("repaired malformed tool-call arguments", "tool", name, "bytes", len(input))
			toolctx.ToolExecStatsFromContext(ctx).RecordRepaired(name)
			input = repaired
		}
	}
	if briefcasePreset && (hasTopLevelJSONKey(input, "compress") || hasTopLevelJSONKey(input, "$ref")) {
		return "", fmt.Errorf("tool %q uses metadata forbidden by the briefcase preset", name)
	}
	// Check for compress flag before executing (avoids re-parsing in every tool).
	wantCompress := !briefcasePreset && extractCompressFlag(input)

	// Resolve $ref: wait for the referenced tool result and inject it.
	if !briefcasePreset {
		input = resolveRef(ctx, input)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Check run-level cache for idempotent tools (grep, fetch_tools).
	// Cached results include post-processing but not compression.
	rc := RunCacheFromContext(ctx)
	cacheable := rc != nil && IsCacheableTool(name)
	var cacheKey string
	if cacheable {
		cacheKey = BuildCacheKey(name, input)
		if cached, ok := rc.Get(cacheKey); ok {
			// Registry-internal outcome — counted here because the tool fn
			// never runs (see ToolExecStats).
			toolctx.ToolExecStatsFromContext(ctx).RecordCacheHit(name)
			if wantCompress && cached != "" {
				return compressToolOutput(ctx, name, cached, slog.Default()), nil
			}
			return cached, nil
		}
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}
	output, err := def.Fn(ctx, input)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return output, ctxErr
	}
	// Verification-gate bookkeeping (verify_gate.go): a successful write/edit
	// arms the gate; a successful verification exec disarms it. Nil-safe.
	verifyGateFromContext(ctx).recordTool(name, input, output, err)
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
		toolctx.ToolExecStatsFromContext(ctx).RecordTruncated(name)
		var spillID string
		// Spill full content to disk so the LLM can retrieve it via read_spillover.
		if r.spillStore != nil {
			sessionKey := toolctx.SessionKeyFromContext(ctx)
			spillID, _ = r.spillStore.Store(sessionKey, name, output)
		}
		output = agent.TruncateHeadTail(output, maxOutput, spillID)
	}

	// Invalidate caches when this tool may have modified the file system.
	// Must run after execution and before the cache Set below, or a call
	// could re-cache the result it just invalidated.
	invalidateCachesAfterTool(ctx, name, input, rc)

	// Apply post-processors.
	if r.postProcess != nil {
		output = r.postProcess.Apply(ctx, name, output)
	}

	// Store in run cache (after post-processing, before compression).
	if cacheable {
		rc.SetWithScope(cacheKey, output, extractPathScope(input))
	}

	// Apply compression if requested by the agent.
	if wantCompress && output != "" {
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

// SetToolProvenanceRoot sets the workspace root used by the agent executor
// when logging content-free file effect metadata for mutating tools.
func (r *ToolRegistry) SetToolProvenanceRoot(root string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.provenanceRoot = root
}

// ToolProvenanceRoot returns the workspace root used by the agent executor
// for provenance snapshots. It satisfies agent's optional provider interface.
func (r *ToolRegistry) ToolProvenanceRoot() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.provenanceRoot
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

// invalidateCachesAfterTool is the single home for post-execution cache
// invalidation policy. Two regimes:
//
//   - Synchronous mutations (write/edit; foreground exec whose command is not
//     provably read-only per runtimeops.ExecCommandPreservesRunCache) have fully
//     landed by the time this runs, so a point-in-time invalidation brackets
//     them: by mutated path where known, whole cache otherwise.
//   - Async writers (background exec with a non-read-only command; a spawned
//     sub-agent whose preset carries write/edit/exec; any process-tool
//     interaction — the tracked process may be mid-write regardless of which
//     run launched it) mutate at unpredictable future points, so the run
//     cache is DISABLED for the rest of the run (RunCache.Disable) — this
//     replaced per-poll invalidation, which both left a stale window between
//     a background job's writes and the next poll AND wiped re-cached entries
//     on every poll of a still-running job.
//
// Residual (documented, accepted): an async writer from a PREVIOUS run that
// this run never observes via exec/process/sessions_spawn — e.g. monitoring
// an old child only through the subagents tool — does not trip the latch.
// FileCache needs no exec/process handling: its entries are mtime+hash
// validated on read (agent.FileChanged).
func invalidateCachesAfterTool(ctx context.Context, name string, input json.RawMessage, rc *RunCache) {
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
		return
	}
	if rc == nil {
		return
	}
	switch name {
	case "exec":
		cmd, background := extractExecMeta(input)
		if runtimeops.ExecCommandPreservesRunCache(cmd) {
			return
		}
		if background {
			rc.Disable() // writes land later — latch even when nothing is cached yet
		} else if rc.Len() > 0 {
			rc.Invalidate()
		}
	case "process":
		rc.Disable()
	case "sessions_spawn":
		if spawnedChildCanWrite(input) {
			rc.Disable()
		}
	}
}

// spawnedChildCanWrite reports whether a sessions_spawn call creates a child
// that could mutate the shared workspace: its tool preset (or the absence of
// one) grants write, edit, or exec. Read-focused presets (researcher,
// wiki-research) keep the parent's cache alive. Unknown presets fail closed —
// sessions_spawn itself rejects them, but the cache must not depend on that.
func spawnedChildCanWrite(input json.RawMessage) bool {
	var meta struct {
		ToolPreset string `json:"tool_preset"`
	}
	if json.Unmarshal(input, &meta) != nil {
		return true
	}
	allowed := toolpreset.AllowedTools(toolpreset.Preset(strings.TrimSpace(meta.ToolPreset)))
	if allowed == nil {
		return true // no/unknown preset — unrestricted child
	}
	for _, mutating := range []string{"write", "edit", "exec"} {
		if _, ok := allowed[mutating]; ok {
			return true
		}
	}
	return false
}

// extractExecMeta extracts the command string and background flag from exec
// tool input JSON.
func extractExecMeta(input json.RawMessage) (command string, background bool) {
	var meta struct {
		Command    string `json:"command"`
		Background bool   `json:"background"`
	}
	if json.Unmarshal(input, &meta) == nil {
		return meta.Command, meta.Background
	}
	return "", false
}

// extractExecCommand extracts the "command" string from exec tool input JSON
// (verify gate's view of extractExecMeta).
func extractExecCommand(input json.RawMessage) string {
	cmd, _ := extractExecMeta(input)
	return cmd
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

func hasTopLevelJSONKey(input json.RawMessage, key string) bool {
	if key == "" || !bytes.Contains(input, []byte(`"`+key+`"`)) {
		return false
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(input, &object) != nil {
		return false
	}
	_, ok := object[key]
	return ok
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
		tools = append(tools, toLLMTool(def))
	}
	return tools
}

// FilteredLLMTools returns tool definitions filtered to only include tools in
// the allowed set. If allowed is nil or empty, returns all tools (no filtering).
// Unlike LLMTools(), the result is not cached since the filter varies per call.
func (r *ToolRegistry) FilteredLLMTools(allowed map[string]struct{}) []llm.Tool {
	if len(allowed) == 0 {
		return r.LLMTools()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]llm.Tool, 0, len(allowed))
	for _, name := range r.order {
		if _, ok := allowed[name]; !ok {
			continue
		}
		def := r.tools[name]
		if def.Hidden || def.Deferred {
			continue
		}
		tools = append(tools, toLLMTool(def))
	}
	return tools
}

// FilteredDefinitions returns tool definitions filtered to only include tools
// in the allowed set. If allowed is nil or empty, returns all definitions.
func (r *ToolRegistry) FilteredDefinitions(allowed map[string]struct{}) []ToolDef {
	if len(allowed) == 0 {
		return r.Definitions()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]ToolDef, 0, len(allowed))
	for _, name := range r.order {
		if _, ok := allowed[name]; ok {
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
		tools = append(tools, toLLMTool(def))
	}
	return tools
}

// toLLMTool is the single schema-normalization and pre-serialization path for
// every registry view exposed to an LLM.
func toLLMTool(def ToolDef) llm.Tool {
	schema := def.InputSchema
	if schema == nil {
		schema = map[string]any{"type": "object"}
	}
	tool := llm.Tool{
		Name:        def.Name,
		Description: def.Description,
		InputSchema: schema,
	}
	tool.PreSerialize()
	return tool
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
