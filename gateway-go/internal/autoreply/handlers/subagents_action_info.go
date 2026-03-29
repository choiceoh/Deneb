package handlers

import (
	"fmt"
	"strings"
)

// HandleSubagentsInfoAction displays detailed information about a subagent.
func HandleSubagentsInfoAction(ctx *SubagentsCommandContext) *SubagentCommandResult {
	target := resolveFirstRestToken(ctx.RestTokens)
	if target == "" {
		return subagentStopWithText("ℹ️ Usage: /subagents info <id|#>")
	}

	entry, errMsg := ResolveSubagentTarget(ctx.Runs, target)
	if entry == nil {
		return subagentStopWithText(fmt.Sprintf("⚠️ %s", errMsg))
	}

	runtime, _ := computeRunRuntime(*entry, currentTimeMs())
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
		fmt.Sprintf("Created: %s", FormatTimestampWithAge(entry.CreatedAt)),
		fmt.Sprintf("Started: %s", FormatTimestampWithAge(entry.StartedAt)),
		fmt.Sprintf("Ended: %s", FormatTimestampWithAge(entry.EndedAt)),
		fmt.Sprintf("Cleanup: %s", entry.Cleanup),
	)
	if entry.AccumulatedRuntimeMs > 0 {
		lines = append(lines, fmt.Sprintf("Accumulated runtime: %s", FormatDurationCompact(entry.AccumulatedRuntimeMs)))
	}
	if entry.ArchiveAtMs > 0 {
		lines = append(lines, fmt.Sprintf("Archive: %s", FormatTimestampWithAge(entry.ArchiveAtMs)))
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

	return subagentStopWithText(strings.Join(lines, "\n"))
}
