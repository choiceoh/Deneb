package handlers

import (
	"fmt"
	"strings"
)

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
	now := currentTimeMs()
	recentCutoff := now - int64(recentMinutes)*60*1000

	var active, recent []SubagentListItem
	idx := 1
	for _, run := range sorted {
		isActive := run.EndedAt == 0
		runtime, runtimeMs := computeRunRuntime(run, now)
		status := FormatRunStatus(run)
		label := FormatRunLabel(run)
		line := buildRunListLine(idx, run, runtime, taskMaxChars)
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

	return BuildSubagentListResult{Total: len(sorted), Active: active, Recent: recent}
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
	return subagentStopWithText(strings.Join(lines, "\n"))
}
