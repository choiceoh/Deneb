package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolreg"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
)

// RegisterCoreTools populates the tool registry with all core agent tools.
// It delegates to toolreg.RegisterCoreTools for the bulk of registrations,
// then adds tools that depend on chat-internal state (post-processors).
func RegisterCoreTools(registry *ToolRegistry, deps *CoreToolDeps) {
	toolreg.RegisterCoreTools(registry, deps)

	// Skills discovery + management: list, create, patch, delete skills at runtime.
	toolreg.RegisterSkillsTools(registry, CachedSkillsSnapshot,
		resolveWorkspaceDirForPrompt(), InvalidateSkillsCache)

	// Wiki knowledge base tools (always active when wiki is configured).
	toolreg.RegisterWikiTools(registry, &deps.Wiki, deps.WorkspaceDir)

	// Contacts address-book lookup (phone lookup + name/company search).
	// Active when the contacts store is wired (native-client contacts sync).
	toolreg.RegisterContactsTool(registry, &deps.Contacts)

	// Calendar (read merged Google + local; write local). Active when either a
	// Google client factory or a local store is wired. Chat-side twin of the
	// miniapp.calendar.* RPC surface — the agent's 일정 hand.
	toolreg.RegisterCalendarTool(registry, &deps.Calendar)

	// Deferred tool activation: fetch_tools lets the LLM load schemas on demand.
	// Registered here (not in toolreg/) because it needs the chat-side
	// ToolRegistry to satisfy FetchToolsRegistry (DeferredToolDef / DeferredSummaries).
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "fetch_tools",
		Description: "Load full schemas for deferred tools so you can call them. Use names (exact) or query (keyword search). The activated tools become available on the next turn",
		InputSchema: toolreg.FetchToolsSchema(),
		Fn:          tools.ToolFetchTools(registry),
	})

	// code_action (CodeAct): the model writes Python to orchestrate several
	// read-only tools / batch-process data in one turn. Registered here (like
	// fetch_tools) because it dials back into this ToolRegistry as its bridge.
	// Deferred — niche but powerful, so it stays out of the eager prompt and is
	// loaded via fetch_tools. Main-only by construction: it is absent from
	// toolpreset, so restricted sub-agents cannot reach this primitive.
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "code_action",
		Description: tools.CodeActionDescription,
		InputSchema: tools.CodeActionSchema(),
		Deferred:    true,
		Fn:          tools.ToolCodeAction(registry),
	})

	RegisterDefaultPostProcessors(registry)

	// Wire spillover store for large tool result management.
	if deps.SpilloverStore != nil {
		registry.SetSpilloverStore(deps.SpilloverStore)
	}

	// Apply per-tool output budgets from tool_schemas.json.
	registry.ApplyMaxOutputs(toolreg.ToolMaxOutputs())
}
