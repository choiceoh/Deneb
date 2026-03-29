// Subagent read-only query actions: help, list, info.
package subagent

import (
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/session"
)

// ---------------------------------------------------------------------------
// action-help
// ---------------------------------------------------------------------------

// HandleSubagentsHelpAction returns the subagent help text.
func HandleSubagentsHelpAction() *SubagentCommandResult {
	return StopWithText(BuildSubagentsHelp())
}

// ---------------------------------------------------------------------------
// action-list
// ---------------------------------------------------------------------------

// BuildSubagentListResult holds the categorized subagent list.
type BuildSubagentListResult struct {
	Total  int
	Active []SubagentListItem
	Recent []SubagentListItem
}

// BuildSubagentList categorizes runs into active and recent lists.
func BuildSubagentList(runs []SubagentRunRecord, recentMinutes int, taskMaxChars int) BuildSubagentListResult {
	if taskMaxChars <= 0 {
		taskMaxChars = 110
	}
	sorted := SortSubagentRuns(runs)
	now := time.Now().UnixMilli()
	recentCutoff := now - int64(recentMinutes)*60*1000

	var active, recent []SubagentListItem
	idx := 1
	for _, run := range sorted {
		isActive := run.EndedAt == 0
		status := FormatRunStatus(run)
		runtimeMs := int64(0)
		if run.StartedAt > 0 {
			end := run.EndedAt
			if end == 0 {
				end = now
			}
			runtimeMs = end - run.StartedAt
		}
		runtime := FormatDurationCompact(runtimeMs)
		task := TruncateLine(run.Task, taskMaxChars)
		label := FormatRunLabel(run)

		// Build display line with optional depth and model annotations.
		depthTag := ""
		if run.SpawnDepth > 1 {
			depthTag = fmt.Sprintf(" d%d", run.SpawnDepth)
		}
		modelTag := ""
		if run.Model != "" {
			modelTag = fmt.Sprintf(" [%s]", run.Model)
		}
		descendantTag := ""
		if run.PendingDescendants > 0 {
			descendantTag = fmt.Sprintf(" +%d pending", run.PendingDescendants)
		}
		line := fmt.Sprintf("#%d %s [%s] (%s%s%s%s) %s", idx, label, status, runtime, depthTag, modelTag, descendantTag, task)
		item := SubagentListItem{
			Index:      idx,
			Line:       line,
			RunID:      run.RunID,
			SessionKey: run.ChildSessionKey,
			Label:      label,
			Task:       run.Task,
			Status:     status,
			Runtime:    runtime,
			RuntimeMs:  runtimeMs,
			Model:      run.Model,
			StartedAt:  run.StartedAt,
			EndedAt:    run.EndedAt,
		}

		if isActive {
			active = append(active, item)
		} else if run.EndedAt >= recentCutoff {
			recent = append(recent, item)
		}
		idx++
	}

	return BuildSubagentListResult{
		Total:  len(sorted),
		Active: active,
		Recent: recent,
	}
}

// HandleSubagentsListAction displays active and recent subagents.
func HandleSubagentsListAction(ctx *SubagentsCommandContext) *SubagentCommandResult {
	list := BuildSubagentList(ctx.Runs, RecentWindowMinutes, 110)
	lines := []string{"active subagents:", "-----"}
	if len(list.Active) == 0 {
		lines = append(lines, "(none)")
	} else {
		for _, entry := range list.Active {
			lines = append(lines, entry.Line)
		}
	}
	lines = append(lines, "", fmt.Sprintf("recent subagents (last %dm):", RecentWindowMinutes), "-----")
	if len(list.Recent) == 0 {
		lines = append(lines, "(none)")
	} else {
		for _, entry := range list.Recent {
			lines = append(lines, entry.Line)
		}
	}
	return StopWithText(strings.Join(lines, "\n"))
}

// ---------------------------------------------------------------------------
// action-kill
// ---------------------------------------------------------------------------

// SubagentKillDeps provides dependencies for the kill action.
type SubagentKillDeps struct {
	KillRun func(runID string) (killed bool, err error)
	KillAll func(controllerKey string, runs []SubagentRunRecord) (killed int, err error)
}

// HandleSubagentsKillAction kills a specific subagent or all subagents.
func HandleSubagentsKillAction(ctx *SubagentsCommandContext, deps *SubagentKillDeps) *SubagentCommandResult {
	target := ""
	if len(ctx.RestTokens) > 0 {
		target = ctx.RestTokens[0]
	}
	if target == "" {
		if ctx.HandledPrefix == SubagentsCmdPrefix {
			return StopWithText("Usage: /subagents kill <id|#|all>")
		}
		return StopWithText("Usage: /kill <id|#|all>")
	}

	if target == "all" || target == "*" {
		if deps == nil || deps.KillAll == nil {
			return StopWithText("⚠️ Kill all not available.")
		}
		killed, err := deps.KillAll(ctx.RequesterKey, ctx.Runs)
		if err != nil {
			return StopWithText(fmt.Sprintf("⚠️ %s", err))
		}
		if killed == 0 {
			return StopWithText("No active subagents to kill.")
		}
		return &SubagentCommandResult{
			Reply:      fmt.Sprintf("Killed %d subagent(s).", killed),
			ShouldStop: true,
		}
	}

	entry, errMsg := ResolveSubagentTarget(ctx.Runs, target)
	if entry == nil {
		return StopWithText(fmt.Sprintf("⚠️ %s", errMsg))
	}
	if entry.EndedAt > 0 {
		return StopWithText(fmt.Sprintf("%s is already finished.", FormatRunLabel(*entry)))
	}

	if deps == nil || deps.KillRun == nil {
		return StopWithText("⚠️ Kill not available.")
	}
	killed, err := deps.KillRun(entry.RunID)
	if err != nil {
		return StopWithText(fmt.Sprintf("⚠️ %s", err))
	}
	if !killed {
		return StopWithText(fmt.Sprintf("⚠️ Failed to kill %s.", FormatRunLabel(*entry)))
	}
	return &SubagentCommandResult{ShouldStop: true}
}

// ---------------------------------------------------------------------------
// action-info
// ---------------------------------------------------------------------------

// HandleSubagentsInfoAction displays detailed information about a subagent.
func HandleSubagentsInfoAction(ctx *SubagentsCommandContext) *SubagentCommandResult {
	target := ""
	if len(ctx.RestTokens) > 0 {
		target = ctx.RestTokens[0]
	}
	if target == "" {
		return StopWithText("ℹ️ Usage: /subagents info <id|#>")
	}

	entry, errMsg := ResolveSubagentTarget(ctx.Runs, target)
	if entry == nil {
		return StopWithText(fmt.Sprintf("⚠️ %s", errMsg))
	}

	now := time.Now().UnixMilli()
	runtime := "n/a"
	if entry.StartedAt > 0 {
		end := entry.EndedAt
		if end == 0 {
			end = now
		}
		runtime = FormatDurationCompact(end - entry.StartedAt)
	}

	outcome := "n/a"
	if entry.OutcomeStatus != "" {
		outcome = entry.OutcomeStatus
		if entry.OutcomeError != "" {
			outcome += fmt.Sprintf(" (%s)", entry.OutcomeError)
		}
	}

	lines := []string{
		"ℹ️ Subagent info",
		fmt.Sprintf("Status: %s", ResolveDisplayStatus(*entry, entry.PendingDescendants)),
		fmt.Sprintf("Label: %s", FormatRunLabel(*entry)),
		fmt.Sprintf("Task: %s", entry.Task),
		fmt.Sprintf("Run: %s", entry.RunID),
		fmt.Sprintf("Session: %s", entry.ChildSessionKey),
	}
	if entry.Model != "" {
		lines = append(lines, fmt.Sprintf("Model: %s", entry.Model))
	}
	if entry.SpawnDepth > 0 {
		lines = append(lines, fmt.Sprintf("Depth: %d", entry.SpawnDepth))
	}
	if entry.SpawnMode != "" && entry.SpawnMode != "run" {
		lines = append(lines, fmt.Sprintf("Mode: %s", entry.SpawnMode))
	}
	lines = append(lines,
		fmt.Sprintf("Runtime: %s", runtime),
		fmt.Sprintf("Created: %s", session.FormatTimestampWithAge(entry.CreatedAt)),
		fmt.Sprintf("Started: %s", session.FormatTimestampWithAge(entry.StartedAt)),
		fmt.Sprintf("Ended: %s", session.FormatTimestampWithAge(entry.EndedAt)),
		fmt.Sprintf("Cleanup: %s", entry.Cleanup),
	)
	if entry.AccumulatedRuntimeMs > 0 {
		lines = append(lines, fmt.Sprintf("Accumulated runtime: %s", FormatDurationCompact(entry.AccumulatedRuntimeMs)))
	}
	if entry.ArchiveAtMs > 0 {
		lines = append(lines, fmt.Sprintf("Archive: %s", session.FormatTimestampWithAge(entry.ArchiveAtMs)))
	}
	if entry.CleanupHandled {
		lines = append(lines, "Cleanup handled: yes")
	}
	if entry.PendingDescendants > 0 {
		lines = append(lines, fmt.Sprintf("Pending descendants: %d", entry.PendingDescendants))
	}
	if entry.EndedReason != "" {
		lines = append(lines, fmt.Sprintf("End reason: %s", entry.EndedReason))
	}
	lines = append(lines, fmt.Sprintf("Outcome: %s", outcome))

	return StopWithText(strings.Join(lines, "\n"))
}
