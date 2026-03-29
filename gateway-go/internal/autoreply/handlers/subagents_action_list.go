// subagents_action_list.go — Backward-compatible facade for list action types and handlers.
// Implementation lives in internal/autoreply/subagent.
package handlers

import subagentpkg "github.com/choiceoh/deneb/gateway-go/internal/autoreply/subagent"

// BuildSubagentListResult holds the categorized subagent list.
type BuildSubagentListResult = subagentpkg.BuildSubagentListResult

// BuildSubagentList categorizes runs into active and recent lists.
func BuildSubagentList(runs []SubagentRunRecord, recentMinutes int, taskMaxChars int) BuildSubagentListResult {
	return subagentpkg.BuildSubagentList(runs, recentMinutes, taskMaxChars)
}

// HandleSubagentsListAction displays active and recent subagents.
func HandleSubagentsListAction(ctx *SubagentsCommandContext) *SubagentCommandResult {
	return subagentpkg.HandleSubagentsListAction(ctx)
}
