// commands_subagents_actions.go — Backward-compatible facade for subagent action types/handlers.
// Implementation lives in internal/autoreply/subagent.
package handlers

import subagentpkg "github.com/choiceoh/deneb/gateway-go/internal/autoreply/subagent"

// Re-export action dep types.
type (
	SubagentLogDeps     = subagentpkg.SubagentLogDeps
	ChatLogMessage      = subagentpkg.ChatLogMessage
	SubagentSendDeps    = subagentpkg.SubagentSendDeps
	SubagentSendResult  = subagentpkg.SubagentSendResult
	SubagentSteerResult = subagentpkg.SubagentSteerResult
	SubagentSpawnDeps   = subagentpkg.SubagentSpawnDeps
	SubagentSpawnParams = subagentpkg.SubagentSpawnParams
	SubagentSpawnContext = subagentpkg.SubagentSpawnContext
	SubagentSpawnResult = subagentpkg.SubagentSpawnResult
	SubagentFocusDeps   = subagentpkg.SubagentFocusDeps
	SubagentUnfocusDeps = subagentpkg.SubagentUnfocusDeps
	SubagentAgentsDeps  = subagentpkg.SubagentAgentsDeps
)

// Re-export action handler functions.

func HandleSubagentsLogAction(ctx *SubagentsCommandContext, deps *SubagentLogDeps) *SubagentCommandResult {
	return subagentpkg.HandleSubagentsLogAction(ctx, deps)
}

func HandleSubagentsSendAction(ctx *SubagentsCommandContext, steerRequested bool, deps *SubagentSendDeps) *SubagentCommandResult {
	return subagentpkg.HandleSubagentsSendAction(ctx, steerRequested, deps)
}

func HandleSubagentsSpawnAction(ctx *SubagentsCommandContext, deps *SubagentSpawnDeps) *SubagentCommandResult {
	return subagentpkg.HandleSubagentsSpawnAction(ctx, deps)
}

func HandleSubagentsFocusAction(ctx *SubagentsCommandContext, deps *SubagentFocusDeps) *SubagentCommandResult {
	return subagentpkg.HandleSubagentsFocusAction(ctx, deps)
}

func HandleSubagentsUnfocusAction(ctx *SubagentsCommandContext, deps *SubagentUnfocusDeps) *SubagentCommandResult {
	return subagentpkg.HandleSubagentsUnfocusAction(ctx, deps)
}

func HandleSubagentsAgentsAction(ctx *SubagentsCommandContext, deps *SubagentAgentsDeps) *SubagentCommandResult {
	return subagentpkg.HandleSubagentsAgentsAction(ctx, deps)
}
