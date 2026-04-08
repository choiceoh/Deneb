package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolreg"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
)

// RegisterCoreTools populates the tool registry with all core agent tools.
// It delegates to toolreg.RegisterCoreTools for the bulk of registrations,
// then adds tools that depend on chat-internal state (post-processors).
func RegisterCoreTools(registry *ToolRegistry, deps *CoreToolDeps) {
	localAI := &toolreg.LocalAIDeps{
		CheckLocalAIHealth: pilot.CheckLocalAIHealth,
		BaseURL:            pilot.LightweightBaseURL,
	}
	toolreg.RegisterCoreTools(registry, deps, localAI)

	// Skills discovery + management: list, create, patch, delete skills at runtime.
	toolreg.RegisterSkillsTools(registry, CachedSkillsSnapshot,
		resolveWorkspaceDirForPrompt(), InvalidateSkillsCache)

	// Deferred tool activation: fetch_tools lets the LLM load schemas on demand.
	registry.RegisterTool(ToolDef{
		Name:        "fetch_tools",
		Description: "Load full schemas for deferred tools so you can call them. Use names (exact) or query (keyword search). The activated tools become available on the next turn",
		InputSchema: toolreg.FetchToolsSchema(),
		Fn:          tools.ToolFetchTools(registry),
	})

	// Wiki knowledge base tools (always active when wiki is configured).
	toolreg.RegisterWikiTools(registry, &deps.Wiki, deps.WorkspaceDir)

	RegisterDefaultPostProcessors(registry)

	// Wire spillover store for large tool result management.
	if deps.SpilloverStore != nil {
		registry.SetSpilloverStore(deps.SpilloverStore)
	}

	// Apply per-tool output budgets from tool_schemas.json.
	registry.ApplyMaxOutputs(toolreg.ToolMaxOutputs())
}
