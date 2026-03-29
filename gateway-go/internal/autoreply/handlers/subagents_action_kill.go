// subagents_action_kill.go — Backward-compatible facade for SubagentKillDeps and HandleSubagentsKillAction.
// Implementation lives in internal/autoreply/subagent.
package handlers

import subagentpkg "github.com/choiceoh/deneb/gateway-go/internal/autoreply/subagent"

// SubagentKillDeps provides dependencies for the kill action.
type SubagentKillDeps = subagentpkg.SubagentKillDeps

// HandleSubagentsKillAction kills a specific subagent or all subagents.
func HandleSubagentsKillAction(ctx *SubagentsCommandContext, deps *SubagentKillDeps) *SubagentCommandResult {
	return subagentpkg.HandleSubagentsKillAction(ctx, deps)
}
