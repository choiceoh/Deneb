package handlers

import "fmt"

// SubagentKillDeps provides dependencies for the kill action.
type SubagentKillDeps struct {
	KillRun func(runID string) (killed bool, err error)
	KillAll func(controllerKey string, runs []SubagentRunRecord) (killed int, err error)
}

// HandleSubagentsKillAction kills a specific subagent or all subagents.
func HandleSubagentsKillAction(ctx *SubagentsCommandContext, deps *SubagentKillDeps) *SubagentCommandResult {
	target := resolveFirstRestToken(ctx.RestTokens)
	if target == "" {
		if ctx.HandledPrefix == SubagentsCmdPrefix {
			return subagentStopWithText("Usage: /subagents kill <id|#|all>")
		}
		return subagentStopWithText("Usage: /kill <id|#|all>")
	}

	if target == "all" || target == "*" {
		if deps == nil || deps.KillAll == nil {
			return subagentStopWithText("⚠️ Kill all not available.")
		}
		killed, err := deps.KillAll(ctx.RequesterKey, ctx.Runs)
		if err != nil {
			return subagentStopWithText(fmt.Sprintf("⚠️ %s", err))
		}
		if killed == 0 {
			return subagentStopWithText("No active subagents to kill.")
		}
		return &SubagentCommandResult{Reply: fmt.Sprintf("Killed %d subagent(s).", killed), ShouldStop: true}
	}

	entry, errMsg := ResolveSubagentTarget(ctx.Runs, target)
	if entry == nil {
		return subagentStopWithText(fmt.Sprintf("⚠️ %s", errMsg))
	}
	if entry.EndedAt > 0 {
		return subagentStopWithText(fmt.Sprintf("%s is already finished.", FormatRunLabel(*entry)))
	}

	if deps == nil || deps.KillRun == nil {
		return subagentStopWithText("⚠️ Kill not available.")
	}
	killed, err := deps.KillRun(entry.RunID)
	if err != nil {
		return subagentStopWithText(fmt.Sprintf("⚠️ %s", err))
	}
	if !killed {
		return subagentStopWithText(fmt.Sprintf("⚠️ Failed to kill %s.", FormatRunLabel(*entry)))
	}
	return &SubagentCommandResult{ShouldStop: true}
}
