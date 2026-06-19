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
	registry.SetToolProvenanceRoot(deps.WorkspaceDir)
	toolreg.RegisterCoreTools(registry, deps)

	// Skills discovery + management: list, create, patch, delete skills at runtime.
	toolreg.RegisterSkillsTools(registry, CachedSkillsSnapshot,
		resolveWorkspaceDirForPrompt(), deps.BundledSkillsDir, InvalidateSkillsCache)

	// Wiki knowledge base tools (always active when wiki is configured).
	toolreg.RegisterWikiTools(registry, &deps.Wiki, deps.WorkspaceDir)

	// Notebook: NotebookLM-style scoped source collections for grounded, cited
	// synthesis (딜/프로젝트 브리핑). Active when the notebook store is wired.
	toolreg.RegisterNotebookTool(registry, &deps.Notebook)

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
	// Eager (2026-06-17): batching N read/grep/calendar/wiki ops into one Python
	// turn collapses N tool-loop steps into 1, and multi-step tool turns are
	// decode-bound (each step pays full thinking decode), so fewer steps is the
	// main latency lever — worth the ~300-450 prompt tokens/turn for its schema.
	// Main-only is preserved independently: absent from toolpreset, so restricted
	// sub-agents cannot reach this primitive (the preset allowlist gates them
	// regardless of eager/deferred).
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "code_action",
		Description: tools.CodeActionDescription,
		InputSchema: tools.CodeActionSchema(),
		Fn: tools.ToolCodeAction(tools.CodeActionDeps{
			Invoker:  registry,
			Contacts: deps.Contacts.Store, // structured deneb.contacts(as_json=True); nil-safe
			Calendar: &deps.Calendar,      // structured deneb.calendar(as_json=True)
			Wiki:     deps.Wiki.Store,     // structured deneb.wiki(as_json=True); nil-safe
		}),
	})

	RegisterDefaultPostProcessors(registry)

	// Wire spillover store for large tool result management.
	if deps.SpilloverStore != nil {
		registry.SetSpilloverStore(deps.SpilloverStore)
	}

	// Apply per-tool output budgets from tool_schemas.json.
	registry.ApplyMaxOutputs(toolreg.ToolMaxOutputs())
}
