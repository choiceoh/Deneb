// hook_runner.go — Full typed hook runner with merge strategies.
// Mirrors src/plugins/hooks.ts (955 LOC) — typed hook execution with
// priority ordering, merge strategies, void/modifying hook patterns,
// error handling, and targeted inbound claim.
package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// TypedHookRegistration is a strongly-typed hook registration.
type TypedHookRegistration struct {
	HookName HookName
	PluginID string
	Handler  HookFunc
	Priority int // higher runs first
	Options  HookOptions
}

// TypedHookRunner provides typed hook execution with merge strategies.
type TypedHookRunner struct {
	mu         sync.RWMutex
	hooks      []TypedHookRegistration
	logger     *slog.Logger
	catchErrors bool
}

// NewTypedHookRunner creates a new typed hook runner.
func NewTypedHookRunner(logger *slog.Logger, catchErrors bool) *TypedHookRunner {
	return &TypedHookRunner{
		logger:      logger,
		catchErrors: catchErrors,
	}
}

// Register adds a typed hook.
func (r *TypedHookRunner) Register(reg TypedHookRegistration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks = append(r.hooks, reg)
}

// getHooksForName returns hooks sorted by priority (higher first).
func (r *TypedHookRunner) getHooksForName(name HookName) []TypedHookRegistration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var matched []TypedHookRegistration
	for _, h := range r.hooks {
		if h.HookName == name {
			matched = append(matched, h)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].Priority > matched[j].Priority
	})
	return matched
}

// getHooksForNameAndPlugin returns hooks for a name filtered by plugin.
func (r *TypedHookRunner) getHooksForNameAndPlugin(name HookName, pluginID string) []TypedHookRegistration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var matched []TypedHookRegistration
	for _, h := range r.hooks {
		if h.HookName == name && h.PluginID == pluginID {
			matched = append(matched, h)
		}
	}
	return matched
}

// --- Void hooks (fire-and-forget, parallel execution) ---

// RunVoidHook runs all handlers for a hook in parallel.
func (r *TypedHookRunner) RunVoidHook(ctx context.Context, name HookName, payload map[string]any) {
	hooks := r.getHooksForName(name)
	if len(hooks) == 0 {
		return
	}

	r.logger.Debug("running void hook", "hook", string(name), "handlers", len(hooks))

	// Execute all handlers concurrently.
	done := make(chan struct{}, len(hooks))
	for _, h := range hooks {
		go func(hook TypedHookRegistration) {
			defer func() { done <- struct{}{} }()
			if err := hook.Handler(ctx, payload); err != nil {
				r.handleError(name, hook.PluginID, err)
			}
		}(h)
	}

	// Wait for all with timeout.
	timeout := time.After(30 * time.Second)
	for i := 0; i < len(hooks); i++ {
		select {
		case <-done:
		case <-timeout:
			r.logger.Warn("void hook timeout", "hook", string(name))
			return
		case <-ctx.Done():
			return
		}
	}
}

// --- Modifying hooks (sequential, results merged) ---

// BeforeModelResolveResult holds the merged result of before_model_resolve hooks.
type BeforeModelResolveResult struct {
	ModelOverride    string `json:"modelOverride,omitempty"`
	ProviderOverride string `json:"providerOverride,omitempty"`
}

// RunBeforeModelResolve runs before_model_resolve hooks sequentially, merging results.
func (r *TypedHookRunner) RunBeforeModelResolve(ctx context.Context, payload map[string]any) *BeforeModelResolveResult {
	hooks := r.getHooksForName(HookBeforeModelResolve)
	if len(hooks) == 0 {
		return nil
	}

	var result BeforeModelResolveResult
	for _, h := range hooks {
		if err := h.Handler(ctx, payload); err != nil {
			r.handleError(HookBeforeModelResolve, h.PluginID, err)
			continue
		}
		// Extract overrides from payload (hooks mutate the payload).
		if v, ok := payload["modelOverride"].(string); ok && v != "" && result.ModelOverride == "" {
			result.ModelOverride = v
		}
		if v, ok := payload["providerOverride"].(string); ok && v != "" && result.ProviderOverride == "" {
			result.ProviderOverride = v
		}
	}

	if result.ModelOverride == "" && result.ProviderOverride == "" {
		return nil
	}
	return &result
}

// BeforePromptBuildResult holds the merged result of before_prompt_build hooks.
type BeforePromptBuildResult struct {
	SystemPrompt         string `json:"systemPrompt,omitempty"`
	PrependContext       string `json:"prependContext,omitempty"`
	PrependSystemContext string `json:"prependSystemContext,omitempty"`
	AppendSystemContext  string `json:"appendSystemContext,omitempty"`
}

// RunBeforePromptBuild runs before_prompt_build hooks, merging prompt modifications.
func (r *TypedHookRunner) RunBeforePromptBuild(ctx context.Context, payload map[string]any) *BeforePromptBuildResult {
	hooks := r.getHooksForName(HookBeforePromptBuild)
	if len(hooks) == 0 {
		return nil
	}

	var result BeforePromptBuildResult
	for _, h := range hooks {
		if err := h.Handler(ctx, payload); err != nil {
			r.handleError(HookBeforePromptBuild, h.PluginID, err)
			continue
		}
		if v, ok := payload["systemPrompt"].(string); ok && v != "" {
			result.SystemPrompt = v
		}
		if v, ok := payload["prependContext"].(string); ok && v != "" {
			result.PrependContext = concatTextSegments(result.PrependContext, v)
		}
		if v, ok := payload["prependSystemContext"].(string); ok && v != "" {
			result.PrependSystemContext = concatTextSegments(result.PrependSystemContext, v)
		}
		if v, ok := payload["appendSystemContext"].(string); ok && v != "" {
			result.AppendSystemContext = concatTextSegments(result.AppendSystemContext, v)
		}
	}
	return &result
}

// BeforeAgentStartResult holds the result of before_agent_start hooks.
type BeforeAgentStartResult struct {
	SystemPromptAddition string `json:"systemPromptAddition,omitempty"`
	Skip                 bool   `json:"skip,omitempty"`
}

// RunBeforeAgentStart runs before_agent_start hooks.
func (r *TypedHookRunner) RunBeforeAgentStart(ctx context.Context, payload map[string]any) *BeforeAgentStartResult {
	hooks := r.getHooksForName(HookBeforeAgentStart)
	if len(hooks) == 0 {
		return nil
	}

	var result BeforeAgentStartResult
	for _, h := range hooks {
		if err := h.Handler(ctx, payload); err != nil {
			r.handleError(HookBeforeAgentStart, h.PluginID, err)
			continue
		}
		if v, ok := payload["systemPromptAddition"].(string); ok && v != "" {
			result.SystemPromptAddition = concatTextSegments(result.SystemPromptAddition, v)
		}
		if v, ok := payload["skip"].(bool); ok && v {
			result.Skip = true
		}
	}
	return &result
}

// MessageSendingResult holds the result of message_sending hooks.
type MessageSendingResult struct {
	ModifiedText string `json:"modifiedText,omitempty"`
	Cancel       bool   `json:"cancel,omitempty"`
}

// RunMessageSending runs message_sending hooks (can modify or cancel messages).
func (r *TypedHookRunner) RunMessageSending(ctx context.Context, payload map[string]any) *MessageSendingResult {
	hooks := r.getHooksForName(HookMessageSending)
	if len(hooks) == 0 {
		return nil
	}

	var result MessageSendingResult
	for _, h := range hooks {
		if err := h.Handler(ctx, payload); err != nil {
			r.handleError(HookMessageSending, h.PluginID, err)
			continue
		}
		if v, ok := payload["modifiedText"].(string); ok {
			result.ModifiedText = v
		}
		if v, ok := payload["cancel"].(bool); ok && v {
			result.Cancel = true
			break // short-circuit on cancel
		}
	}
	return &result
}

// BeforeToolCallResult holds the result of before_tool_call hooks.
type BeforeToolCallResult struct {
	Cancel      bool   `json:"cancel,omitempty"`
	CancelReason string `json:"cancelReason,omitempty"`
}

// RunBeforeToolCall runs before_tool_call hooks.
func (r *TypedHookRunner) RunBeforeToolCall(ctx context.Context, payload map[string]any) *BeforeToolCallResult {
	hooks := r.getHooksForName(HookBeforeToolCall)
	if len(hooks) == 0 {
		return nil
	}

	for _, h := range hooks {
		if err := h.Handler(ctx, payload); err != nil {
			r.handleError(HookBeforeToolCall, h.PluginID, err)
			continue
		}
		if v, ok := payload["cancel"].(bool); ok && v {
			reason := ""
			if r, ok := payload["cancelReason"].(string); ok {
				reason = r
			}
			return &BeforeToolCallResult{Cancel: true, CancelReason: reason}
		}
	}
	return nil
}

// SubagentSpawningResult holds the result of subagent_spawning hooks.
type SubagentSpawningResult struct {
	Status             string `json:"status"` // "ok" or "error"
	Error              string `json:"error,omitempty"`
	ThreadBindingReady bool   `json:"threadBindingReady,omitempty"`
}

// RunSubagentSpawning runs subagent_spawning hooks.
func (r *TypedHookRunner) RunSubagentSpawning(ctx context.Context, payload map[string]any) *SubagentSpawningResult {
	hooks := r.getHooksForName(HookSubagentSpawning)
	if len(hooks) == 0 {
		return &SubagentSpawningResult{Status: "ok"}
	}

	result := &SubagentSpawningResult{Status: "ok"}
	for _, h := range hooks {
		if err := h.Handler(ctx, payload); err != nil {
			result.Status = "error"
			result.Error = err.Error()
			r.handleError(HookSubagentSpawning, h.PluginID, err)
			break
		}
		if v, ok := payload["threadBindingReady"].(bool); ok && v {
			result.ThreadBindingReady = true
		}
	}
	return result
}

// InboundClaimOutcome represents the result of targeted inbound claim.
type InboundClaimOutcome struct {
	Status string // "handled", "missing_plugin", "no_handler", "declined", "error"
	Error  string
}

// RunTargetedInboundClaim runs inbound_claim for a specific plugin.
func (r *TypedHookRunner) RunTargetedInboundClaim(ctx context.Context, pluginID string, payload map[string]any) InboundClaimOutcome {
	hooks := r.getHooksForNameAndPlugin(HookInboundClaim, pluginID)
	if len(hooks) == 0 {
		return InboundClaimOutcome{Status: "no_handler"}
	}

	for _, h := range hooks {
		if err := h.Handler(ctx, payload); err != nil {
			return InboundClaimOutcome{Status: "error", Error: err.Error()}
		}
		if v, ok := payload["handled"].(bool); ok && v {
			return InboundClaimOutcome{Status: "handled"}
		}
	}
	return InboundClaimOutcome{Status: "declined"}
}

// --- Error handling ---

func (r *TypedHookRunner) handleError(name HookName, pluginID string, err error) {
	msg := fmt.Sprintf("[hooks] %s handler from %s failed: %s", name, pluginID, sanitizeHookError(err))
	if r.catchErrors {
		r.logger.Error(msg)
		return
	}
	panic(msg)
}

func sanitizeHookError(err error) string {
	if err == nil {
		return "unknown error"
	}
	msg := err.Error()
	firstLine := strings.SplitN(msg, "\n", 2)[0]
	return strings.TrimSpace(firstLine)
}

func concatTextSegments(left, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	return left + "\n" + right
}

// --- Convenience methods ---

// Count returns the total number of typed hook registrations.
func (r *TypedHookRunner) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.hooks)
}

// CountForHook returns registrations for a specific hook.
func (r *TypedHookRunner) CountForHook(name HookName) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, h := range r.hooks {
		if h.HookName == name {
			count++
		}
	}
	return count
}

// ListRegisteredHooks returns unique hook names that have handlers.
func (r *TypedHookRunner) ListRegisteredHooks() []HookName {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := make(map[HookName]bool)
	var names []HookName
	for _, h := range r.hooks {
		if !seen[h.HookName] {
			seen[h.HookName] = true
			names = append(names, h.HookName)
		}
	}
	return names
}
