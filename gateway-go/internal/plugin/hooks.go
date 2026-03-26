package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Full set of hook names matching the TS plugin system (24 hooks).
const (
	HookGatewayStart           HookName = "gateway_start"
	HookGatewayStop            HookName = "gateway_stop"
	HookBeforeModelResolve     HookName = "before_model_resolve"
	HookBeforePromptBuild      HookName = "before_prompt_build"
	HookLLMInput               HookName = "llm_input"
	HookLLMOutput              HookName = "llm_output"
	HookAgentEnd               HookName = "agent_end"
	HookBeforeCompaction       HookName = "before_compaction"
	HookAfterCompaction        HookName = "after_compaction"
	HookBeforeReset            HookName = "before_reset"
	HookInboundClaim           HookName = "inbound_claim"
	HookMessageSending         HookName = "message_sending"
	HookMessageSent            HookName = "message_sent"
	HookBeforeToolCall         HookName = "before_tool_call"
	HookAfterToolCall          HookName = "after_tool_call"
	HookToolResultPersist      HookName = "tool_result_persist"
	HookBeforeMessageWrite     HookName = "before_message_write"
	HookSessionStart           HookName = "session_start"
	HookSessionEnd             HookName = "session_end"
	HookSubagentSpawning       HookName = "subagent_spawning"
	HookSubagentDeliveryTarget HookName = "subagent_delivery_target"
	HookSubagentSpawned        HookName = "subagent_spawned"
	HookSubagentEnded          HookName = "subagent_ended"
)

// HookPriority controls execution order (lower runs first).
type HookPriority int

const (
	HookPriorityEarly   HookPriority = -100
	HookPriorityNormal  HookPriority = 0
	HookPriorityLate    HookPriority = 100
)

// HookOptions configures a hook registration.
type HookOptions struct {
	Priority             HookPriority
	AllowPromptInjection bool
}

// HookResult holds the outcome of a single hook execution.
type HookResult struct {
	PluginID  string
	HookName  HookName
	Duration  time.Duration
	Error     error
	Payload   map[string]any // optional mutated payload
}

// HookRunner manages and executes hooks with proper ordering and error handling.
type HookRunner struct {
	hooks  []registeredHook
	logger *slog.Logger
}

type registeredHook struct {
	entry    HookEntry
	options  HookOptions
}

// NewHookRunner creates a new hook runner.
func NewHookRunner(logger *slog.Logger) *HookRunner {
	return &HookRunner{logger: logger}
}

// Register adds a hook with options.
func (r *HookRunner) Register(name HookName, pluginID string, handler HookFunc, opts HookOptions) {
	r.hooks = append(r.hooks, registeredHook{
		entry:   HookEntry{Name: name, PluginID: pluginID, Handler: handler},
		options: opts,
	})
}

// Run executes all hooks for the given name in priority order.
// Returns results for each hook (including errors).
func (r *HookRunner) Run(ctx context.Context, name HookName, payload map[string]any) []HookResult {
	// Collect matching hooks.
	var matching []registeredHook
	for _, h := range r.hooks {
		if h.entry.Name == name {
			matching = append(matching, h)
		}
	}
	if len(matching) == 0 {
		return nil
	}

	// Sort by priority (stable sort).
	sortHooksByPriority(matching)

	// Execute sequentially.
	results := make([]HookResult, 0, len(matching))
	for _, h := range matching {
		start := time.Now()
		err := h.entry.Handler(ctx, payload)
		dur := time.Since(start)

		result := HookResult{
			PluginID: h.entry.PluginID,
			HookName: name,
			Duration: dur,
			Error:    err,
		}
		results = append(results, result)

		if err != nil {
			r.logger.Warn("hook error",
				"hook", string(name),
				"plugin", h.entry.PluginID,
				"error", err,
				"duration", dur,
			)
		}
	}
	return results
}

// RunFireAndForget executes hooks without waiting for results.
func (r *HookRunner) RunFireAndForget(name HookName, payload map[string]any) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		r.Run(ctx, name, payload)
	}()
}

// Count returns the number of registered hooks for a name.
func (r *HookRunner) Count(name HookName) int {
	count := 0
	for _, h := range r.hooks {
		if h.entry.Name == name {
			count++
		}
	}
	return count
}

// ListHookNames returns all unique hook names that have registered handlers.
func (r *HookRunner) ListHookNames() []HookName {
	seen := make(map[HookName]bool)
	var names []HookName
	for _, h := range r.hooks {
		if !seen[h.entry.Name] {
			seen[h.entry.Name] = true
			names = append(names, h.entry.Name)
		}
	}
	return names
}

func sortHooksByPriority(hooks []registeredHook) {
	// Simple insertion sort (hooks list is typically small).
	for i := 1; i < len(hooks); i++ {
		key := hooks[i]
		j := i - 1
		for j >= 0 && hooks[j].options.Priority > key.options.Priority {
			hooks[j+1] = hooks[j]
			j--
		}
		hooks[j+1] = key
	}
}

// ValidateHookName returns an error if the hook name is not recognized.
func ValidateHookName(name HookName) error {
	switch name {
	case HookBeforeAgentStart, HookAfterAgentEnd, HookBeforeSend, HookAfterSend,
		HookMessageReceived, HookSessionCreated, HookSessionReset,
		HookCronJobStart, HookCronJobEnd,
		HookGatewayStart, HookGatewayStop,
		HookBeforeModelResolve, HookBeforePromptBuild,
		HookLLMInput, HookLLMOutput, HookAgentEnd,
		HookBeforeCompaction, HookAfterCompaction, HookBeforeReset,
		HookInboundClaim, HookMessageSending, HookMessageSent,
		HookBeforeToolCall, HookAfterToolCall, HookToolResultPersist,
		HookBeforeMessageWrite, HookSessionStart, HookSessionEnd,
		HookSubagentSpawning, HookSubagentDeliveryTarget,
		HookSubagentSpawned, HookSubagentEnded:
		return nil
	default:
		return fmt.Errorf("unknown hook name: %s", name)
	}
}
