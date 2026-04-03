package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/chat/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolreg"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/tools"
)

// RegisterCoreTools populates the tool registry with all core agent tools.
// It delegates to toolreg.RegisterCoreTools for the bulk of registrations,
// then adds tools that depend on chat-internal state (post-processors).
func RegisterCoreTools(registry *ToolRegistry, deps *CoreToolDeps) {
	sglang := &toolreg.SglangDeps{
		CheckSglangHealth: pilot.CheckSglangHealth,
		BaseURL:           pilot.LightweightBaseURL,
	}
	toolreg.RegisterCoreTools(registry, deps, sglang)

	// Skills discovery: lists non-always skills on demand (lightweight prompt strategy).
	toolreg.RegisterSkillsTools(registry, GetCachedSkillsSnapshot)

	// Deferred tool activation: fetch_tools lets the LLM load schemas on demand.
	registry.RegisterTool(ToolDef{
		Name:        "fetch_tools",
		Description: "Load full schemas for deferred tools so you can call them. Use names (exact) or query (keyword search). The activated tools become available on the next turn",
		InputSchema: toolreg.FetchToolsSchema(),
		Fn:          tools.ToolFetchTools(registry),
	})

	RegisterDefaultPostProcessors(registry)

	// Wire spillover store for large tool result management.
	if deps.SpilloverStore != nil {
		registry.SetSpilloverStore(deps.SpilloverStore)
	}
}
