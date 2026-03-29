// subagents_action_info.go — Backward-compatible facade for HandleSubagentsInfoAction.
// Implementation lives in internal/autoreply/subagent.
package handlers

import subagentpkg "github.com/choiceoh/deneb/gateway-go/internal/autoreply/subagent"

// HandleSubagentsInfoAction displays detailed information about a subagent.
func HandleSubagentsInfoAction(ctx *SubagentsCommandContext) *SubagentCommandResult {
	return subagentpkg.HandleSubagentsInfoAction(ctx)
}
