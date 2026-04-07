package chat

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolreg"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/tools"
	"github.com/choiceoh/deneb/gateway-go/internal/rlm"
)

// RegisterCoreTools populates the tool registry with all core agent tools.
// It delegates to toolreg.RegisterCoreTools for the bulk of registrations,
// then adds tools that depend on chat-internal state (post-processors).
// agentTraces is optional; when non-nil, worker LLM sub-agent calls record traces.
func RegisterCoreTools(registry *ToolRegistry, deps *CoreToolDeps, agentTraces ...*rlm.AgentTraceStore) {
	var traceStore *rlm.AgentTraceStore
	if len(agentTraces) > 0 {
		traceStore = agentTraces[0]
	}
	localAI := &toolreg.LocalAIDeps{
		CheckLocalAIHealth: pilot.CheckLocalAIHealth,
		BaseURL:            pilot.LightweightBaseURL,
	}
	toolreg.RegisterCoreTools(registry, deps, localAI)

	// Skills discovery + management: list, create, patch, delete skills at runtime.
	toolreg.RegisterSkillsTools(registry, GetCachedSkillsSnapshot,
		resolveWorkspaceDirForPrompt(), InvalidateSkillsCache)

	// Deferred tool activation: fetch_tools lets the LLM load schemas on demand.
	registry.RegisterTool(ToolDef{
		Name:        "fetch_tools",
		Description: "Load full schemas for deferred tools so you can call them. Use names (exact) or query (keyword search). The activated tools become available on the next turn",
		InputSchema: toolreg.FetchToolsSchema(),
		Fn:          tools.ToolFetchTools(registry),
	})

	// RLM: context externalization + wiki knowledge base (always active).
	{
		cfg := rlm.ConfigFromEnv()
		toolreg.RegisterRLMTools(registry, &deps.Wiki, deps.WorkspaceDir)

		if deps.LLMClient != nil {
			spawnFn, batchFn := buildRLMSpawnFuncs(deps, registry, cfg, traceStore)
			toolreg.RegisterRLMSpawnTools(registry, spawnFn, batchFn, cfg.MaxSubSpawns)
		}

		// REPL tool: Starlark-based context exploration.
		toolreg.RegisterREPLTools(registry)
	}

	RegisterDefaultPostProcessors(registry)

	// Wire spillover store for large tool result management.
	if deps.SpilloverStore != nil {
		registry.SetSpilloverStore(deps.SpilloverStore)
	}

	// Apply per-tool output budgets from tool_schemas.yaml.
	registry.ApplyMaxOutputs(toolreg.ToolMaxOutputs())
}

// rlmDataToolNames lists the Phase 1 tool names available to sub-LLMs.
// llm_spawn/llm_spawn_batch are excluded to prevent recursion.
var rlmDataToolNames = []string{
	"projects_list",
	"projects_get_field",
	"projects_search",
	"projects_get_document",
	"wiki",
}

// rlmSpawnToolNames are tools that must never be given to sub-LLMs
// (unbounded recursion).
var rlmSpawnToolNames = map[string]bool{
	"llm_spawn":       true,
	"llm_spawn_batch": true,
}

// buildRLMSpawnFuncs creates the spawn/batch closures that capture the LLM
// client, tool registry, and config needed by Phase 2 sub-LLM tools.
// Each invocation creates a fresh TokenBudget so budgets are per-call,
// not cumulative across the server's lifetime.
func buildRLMSpawnFuncs(deps *CoreToolDeps, registry *ToolRegistry, cfg rlm.Config, agentTraces ...*rlm.AgentTraceStore) (tools.SpawnFunc, tools.SpawnBatchFunc) {
	var traceStore *rlm.AgentTraceStore
	if len(agentTraces) > 0 {
		traceStore = agentTraces[0]
	}
	budgetLimit := cfg.TotalTokenBudget

	// Build the LLM tool list available to sub-LLMs (data tools only).
	subTools := registry.FilteredLLMTools(filterMap(rlmDataToolNames))

	spawnFn := func(ctx context.Context, prompt string, toolNames []string, maxTokens, maxTurns int) (*rlm.SubAgentResult, error) {
		selectedTools := subTools
		if len(toolNames) > 0 {
			selectedTools = registry.FilteredLLMTools(filterMap(stripSpawnTools(toolNames)))
		}

		// Fresh budget per call — prevents cumulative exhaustion across requests.
		budget := rlm.NewTokenBudget(budgetLimit)

		// Inherit session memory from parent context for sub-agent continuity.
		system := buildSubAgentSystem(ctx, deps)

		return rlm.RunSubAgent(ctx, rlm.SubAgentConfig{
			Prompt:       prompt,
			System:       system,
			Tools:        selectedTools,
			ToolExecutor: registry,
			Client:       deps.LLMClient,
			Model:        deps.DefaultModel,
			MaxTokens:    maxTokens,
			MaxTurns:     maxTurns,
			Budget:       budget,
			Logger:       slog.Default().With("component", "rlm_sub"),
			AgentTraces:  traceStore,
		})
	}

	batchFn := func(ctx context.Context, prompts []string, toolNames []string, maxTokens int) ([]rlm.SubAgentResult, error) {
		selectedTools := subTools
		if len(toolNames) > 0 {
			selectedTools = registry.FilteredLLMTools(filterMap(stripSpawnTools(toolNames)))
		}

		tasks := make([]rlm.SubAgentTask, len(prompts))
		for i, p := range prompts {
			tasks[i] = rlm.SubAgentTask{Index: i, Prompt: p}
		}

		// Fresh budget per batch — shared across tasks within this one call.
		budget := rlm.NewTokenBudget(budgetLimit)

		system := buildSubAgentSystem(ctx, deps)

		return rlm.RunSubAgentBatch(ctx, rlm.BatchConfig{
			Tasks:        tasks,
			System:       system,
			Tools:        selectedTools,
			ToolExecutor: registry,
			Client:       deps.LLMClient,
			Model:        deps.DefaultModel,
			MaxTokens:    maxTokens,
			MaxTurns:     cfg.SubMaxToolCalls,
			Budget:       budget,
			Logger:       slog.Default().With("component", "rlm_batch"),
			AgentTraces:  traceStore,
		})
	}

	return spawnFn, batchFn
}

// stripSpawnTools removes spawn tool names from the list to prevent
// sub-LLM recursion, even if the LLM explicitly requests them.
func stripSpawnTools(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if !rlmSpawnToolNames[n] {
			out = append(out, n)
		}
	}
	return out
}

// buildSubAgentSystem builds a lightweight system prompt for RLM sub-agents,
// inheriting session memory from the parent context.
func buildSubAgentSystem(ctx context.Context, deps *CoreToolDeps) json.RawMessage {
	var sessionMemory string
	if deps.SessionMemoryFn != nil {
		sessionKey := SessionKeyFromContext(ctx)
		if sessionKey != "" {
			sessionMemory = deps.SessionMemoryFn(sessionKey)
		}
	}
	return rlm.BuildSubAgentSystem(sessionMemory)
}

// filterMap converts a name slice to an allowed-set map for FilteredLLMTools.
func filterMap(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}
