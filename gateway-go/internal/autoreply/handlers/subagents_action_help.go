// subagents_action_help.go — Backward-compatible facade for HandleSubagentsHelpAction.
// Implementation lives in internal/autoreply/subagent.
package handlers

import subagentpkg "github.com/choiceoh/deneb/gateway-go/internal/autoreply/subagent"

// HandleSubagentsHelpAction returns the subagent help text.
func HandleSubagentsHelpAction() *SubagentCommandResult {
	return subagentpkg.HandleSubagentsHelpAction()
}
