package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolreg"
)

// RegisterCoreTools populates the tool registry with all core agent tools.
// It delegates to toolreg.RegisterCoreTools for the bulk of registrations,
// then adds tools that depend on chat-internal state (pilot, post-processors).
func RegisterCoreTools(registry *ToolRegistry, deps *CoreToolDeps) {
	sglang := &toolreg.SglangDeps{
		CallLocalLLM:      callLocalLLM,
		CheckSglangHealth: checkSglangHealth,
		BaseURL:           lightweightBaseURL,
	}
	toolreg.RegisterCoreTools(registry, deps, sglang)

	// Pilot registered here: it depends on sglang hooks from chat/sglang_hooks.go.
	registry.RegisterTool(ToolDef{
		Name:        "pilot",
		Description: "Local AI analysis for noisy outputs or multi-source orchestration. Prefer direct tools for simple read/grep/find/tree/git/web/http/memory-style lookups. Shortcuts: file, exec, grep, find, url, diff, test, tree, git_log, health, memory, vega, image + more",
		InputSchema: toolreg.PilotToolSchema(),
		Fn:          toolPilot(registry, deps.WorkspaceDir),
	})

	RegisterDefaultPostProcessors(registry)
}
