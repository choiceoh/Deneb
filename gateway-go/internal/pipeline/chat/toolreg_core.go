package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire"
)

// RegisterCoreTools populates the tool registry with all core agent tools.
// It delegates to toolwire.RegisterCoreTools for the bulk of registrations,
// then adds tools that depend on chat-internal state (post-processors).
func RegisterCoreTools(registry *ToolRegistry, deps *CoreToolDeps) {
	registry.SetToolProvenanceRoot(deps.WorkspaceDir)
	toolwire.RegisterCoreTools(registry, deps)

	// Skills discovery + management: list, create, patch, delete skills at runtime.
	toolwire.RegisterSkillsTools(registry, CachedSkillsSnapshot,
		resolveWorkspaceDirForPrompt(), deps.BundledSkillsDir, InvalidateSkillsCache)

	// Wiki knowledge base tools (always active when wiki is configured).
	// flushSessionPromptCaches lets wiki_forget drop this session's prompt
	// snapshots so a forgotten page can't re-surface from a frozen snapshot.
	toolwire.RegisterWikiTools(registry, &deps.Wiki, deps.WorkspaceDir, flushSessionPromptCaches)

	// preference: append-only standing behavior rules → workspace SOUL.md.
	// Bind to the SAME workspace file/wiki tools use (deps.WorkspaceDir), falling
	// back to the prompt default only when unset — this mirrors how the prompt
	// resolves context files (prewarmPromptWorkspace: params.WorkspaceDir else
	// default), so the rule is written where the active prompt reads SOUL.md and
	// an isolated (eval/subagent) run writes to its own workspace, not the real
	// persona file.
	personaWorkspace := deps.WorkspaceDir
	if personaWorkspace == "" {
		personaWorkspace = resolveWorkspaceDirForPrompt()
	}
	toolwire.RegisterPersonaTools(registry, personaWorkspace)

	// Notebook: NotebookLM-style scoped source collections for grounded, cited
	// synthesis (딜/프로젝트 브리핑). Active when the notebook store is wired.
	toolwire.RegisterNotebookTool(registry, &deps.Notebook)

	// people: one entry point over the address book, the org chart, and the live
	// groupware HR area. Active when the contacts store is wired (native-client
	// contacts sync); the wiki store lets the groupware leg enrich 인물 pages.
	toolwire.RegisterPeopleTool(registry, &deps.Contacts, deps.Wiki.Store)

	// Calendar (read merged Google + local; write local). Active when either a
	// Google client factory or a local store is wired. Chat-side twin of the
	// miniapp.calendar.* RPC surface — the agent's 일정 hand.
	toolwire.RegisterCalendarTool(registry, &deps.Calendar)

	// fetch_tools + code_action dial back into this ToolRegistry; registration
	// lives in toolreg so the chat parent does not import those tool packages.
	toolwire.RegisterRegistryBridgeTools(registry, deps)

	RegisterDefaultPostProcessors(registry)

	// Wire spillover store for large tool result management.
	if deps.SpilloverStore != nil {
		registry.SetSpilloverStore(deps.SpilloverStore)
	}

	// Apply per-tool output budgets from tool_schemas.json.
	registry.ApplyMaxOutputs(toolwire.ToolMaxOutputs())
}

// Ensure *ToolRegistry satisfies the bridge surface used by toolwire.
var (
	_ toolwire.RegistryBridge = (*ToolRegistry)(nil)
	_ toolport.ToolRegistrar  = (*ToolRegistry)(nil)
)
