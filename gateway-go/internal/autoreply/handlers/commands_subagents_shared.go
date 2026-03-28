// commands_subagents_shared.go — Backward-compatible facade for subagent command shared types/utilities.
package handlers

import subagentpkg "github.com/choiceoh/deneb/gateway-go/internal/autoreply/subagent"

// SubagentsAction represents the specific subagent command action.
type SubagentsAction = subagentpkg.SubagentsAction

const (
	SubagentsActionList    = subagentpkg.SubagentsActionList
	SubagentsActionKill    = subagentpkg.SubagentsActionKill
	SubagentsActionLog     = subagentpkg.SubagentsActionLog
	SubagentsActionSend    = subagentpkg.SubagentsActionSend
	SubagentsActionSteer   = subagentpkg.SubagentsActionSteer
	SubagentsActionInfo    = subagentpkg.SubagentsActionInfo
	SubagentsActionSpawn   = subagentpkg.SubagentsActionSpawn
	SubagentsActionFocus   = subagentpkg.SubagentsActionFocus
	SubagentsActionUnfocus = subagentpkg.SubagentsActionUnfocus
	SubagentsActionAgents  = subagentpkg.SubagentsActionAgents
	SubagentsActionHelp    = subagentpkg.SubagentsActionHelp
)

// Subagent command prefixes.
const (
	SubagentsCmdPrefix  = subagentpkg.SubagentsCmdPrefix
	SubagentsCmdKill    = subagentpkg.SubagentsCmdKill
	SubagentsCmdSteer   = subagentpkg.SubagentsCmdSteer
	SubagentsCmdTell    = subagentpkg.SubagentsCmdTell
	SubagentsCmdFocus   = subagentpkg.SubagentsCmdFocus
	SubagentsCmdUnfocus = subagentpkg.SubagentsCmdUnfocus
	SubagentsCmdAgents  = subagentpkg.SubagentsCmdAgents
)

const (
	RecentWindowMinutes       = subagentpkg.RecentWindowMinutes
	SteerAbortSettleTimeoutMs = subagentpkg.SteerAbortSettleTimeoutMs
)

type (
	SubagentRunRecord          = subagentpkg.SubagentRunRecord
	SubagentsCommandContext    = subagentpkg.SubagentsCommandContext
	SubagentCommandResult      = subagentpkg.SubagentCommandResult
	SubagentListItem           = subagentpkg.SubagentListItem
	ResolvedSubagentController = subagentpkg.ResolvedSubagentController
)

func subagentStopWithText(text string) *SubagentCommandResult {
	return subagentpkg.StopWithText(text)
}

func ResolveHandledPrefix(normalized string) string {
	return subagentpkg.ResolveHandledPrefix(normalized)
}

func ResolveSubagentsAction(handledPrefix string, restTokens []string) (SubagentsAction, []string) {
	return subagentpkg.ResolveSubagentsAction(handledPrefix, restTokens)
}

func FormatRunLabel(run SubagentRunRecord) string {
	return subagentpkg.FormatRunLabel(run)
}

func FormatRunStatus(run SubagentRunRecord) string {
	return subagentpkg.FormatRunStatus(run)
}

func ResolveDisplayStatus(run SubagentRunRecord, pendingDescendants int) string {
	return subagentpkg.ResolveDisplayStatus(run, pendingDescendants)
}

func FormatDurationCompact(ms int64) string {
	return subagentpkg.FormatDurationCompact(ms)
}

func SortSubagentRuns(runs []SubagentRunRecord) []SubagentRunRecord {
	return subagentpkg.SortSubagentRuns(runs)
}

func TruncateLine(s string, maxLen int) string {
	return subagentpkg.TruncateLine(s, maxLen)
}

func ResolveSubagentTarget(runs []SubagentRunRecord, token string) (*SubagentRunRecord, string) {
	return subagentpkg.ResolveSubagentTarget(runs, token)
}

func BuildSubagentsHelp() string {
	return subagentpkg.BuildSubagentsHelp()
}
