// commands_subagents.go — Backward-compatible facade for HandleSubagentsCommand.
// Implementation lives in internal/autoreply/subagent.
package handlers

import subagentpkg "github.com/choiceoh/deneb/gateway-go/internal/autoreply/subagent"

// SubagentCommandDeps re-exported from subagent package.
type SubagentCommandDeps = subagentpkg.SubagentCommandDeps

// HandleSubagentsCommand is the main entry point for subagent commands.
func HandleSubagentsCommand(
	normalized string,
	sessionKey string,
	channel string,
	accountID string,
	threadID string,
	senderID string,
	isGroup bool,
	isAuthorized bool,
	deps *SubagentCommandDeps,
) *SubagentCommandResult {
	return subagentpkg.HandleSubagentsCommand(
		normalized, sessionKey, channel, accountID, threadID,
		senderID, isGroup, isAuthorized, deps,
	)
}
