// subagents_utils.go — Subagent run utilities for commands_handlers.go.
// Provides BuildSubagentRunListEntries, ResolveSubagentEntryForToken,
// and FormatSubagentInfo which are used by the legacy command handler layer.
package handlers

import (
	"fmt"
	"strconv"
	"strings"
)

// SubagentRunListEntry holds a formatted run entry for display.
type SubagentRunListEntry struct {
	Entry *SubagentRunRecord
	Line  string
}

// BuildSubagentRunListEntries builds formatted entries for the /agents list.
func BuildSubagentRunListEntries(runs []*SubagentRunRecord, recentWindowMinutes int, maxLabelLen int) (active, recent []SubagentRunListEntry) {
	sorted := sortSubagentRunPtrs(runs)
	recentCutoff := currentTimeMs() - int64(recentWindowMinutes)*60_000
	if maxLabelLen <= 0 {
		maxLabelLen = 110
	}

	idx := 1
	for _, e := range sorted {
		if e.EndedAt != 0 {
			continue
		}
		label := FormatRunLabel(*e)
		line := fmt.Sprintf("#%d %s — %s", idx, label, FormatRunStatus(*e))
		active = append(active, SubagentRunListEntry{Entry: e, Line: line})
		idx++
	}
	for _, e := range sorted {
		if e.EndedAt == 0 {
			continue
		}
		if e.EndedAt < recentCutoff {
			continue
		}
		label := FormatRunLabel(*e)
		line := fmt.Sprintf("#%d %s — %s", idx, label, FormatRunStatus(*e))
		recent = append(recent, SubagentRunListEntry{Entry: e, Line: line})
		idx++
	}
	return active, recent
}

func sortSubagentRunPtrs(runs []*SubagentRunRecord) []*SubagentRunRecord {
	sorted := make([]*SubagentRunRecord, len(runs))
	copy(sorted, runs)
	// Active first, then by creation time descending.
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			iActive := sorted[i].EndedAt == 0
			jActive := sorted[j].EndedAt == 0
			if (!iActive && jActive) || (iActive == jActive && sorted[i].CreatedAt < sorted[j].CreatedAt) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	return sorted
}

// ResolveSubagentEntryForToken finds a subagent by token (index, session key, label, or run ID prefix).
func ResolveSubagentEntryForToken(runs []*SubagentRunRecord, token string) (*SubagentRunRecord, *CommandResult) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return nil, &CommandResult{Reply: "⚠️ Missing subagent target.", SkipAgent: true, IsError: true}
	}

	sorted := sortSubagentRunPtrs(runs)

	if trimmed == "last" {
		if len(sorted) > 0 {
			return sorted[0], nil
		}
		return nil, &CommandResult{Reply: "⚠️ No subagent runs found.", SkipAgent: true, IsError: true}
	}

	// Build numeric order: active first, then recently ended.
	var numOrder []*SubagentRunRecord
	for _, e := range sorted {
		if e.EndedAt == 0 {
			numOrder = append(numOrder, e)
		}
	}
	for _, e := range sorted {
		if e.EndedAt != 0 {
			numOrder = append(numOrder, e)
		}
	}

	// Numeric index (1-based).
	if n, err := strconv.Atoi(trimmed); err == nil && n > 0 && n <= len(numOrder) {
		return numOrder[n-1], nil
	}

	// Session key.
	if strings.Contains(trimmed, ":") {
		for _, e := range sorted {
			if e.ChildSessionKey == trimmed {
				return e, nil
			}
		}
		return nil, &CommandResult{Reply: fmt.Sprintf("⚠️ No subagent with session key %q.", trimmed), SkipAgent: true, IsError: true}
	}

	lowered := strings.ToLower(trimmed)

	// Label match.
	for _, e := range sorted {
		if strings.ToLower(e.Label) == lowered {
			return e, nil
		}
	}
	for _, e := range sorted {
		if strings.HasPrefix(strings.ToLower(e.Label), lowered) {
			return e, nil
		}
	}

	// Run ID prefix.
	for _, e := range sorted {
		if strings.HasPrefix(e.RunID, trimmed) {
			return e, nil
		}
	}

	return nil, &CommandResult{Reply: fmt.Sprintf("⚠️ No subagent matching %q.", trimmed), SkipAgent: true, IsError: true}
}

// FormatSubagentInfo returns a detailed info string for a subagent run.
func FormatSubagentInfo(entry *SubagentRunRecord, indent int) string {
	var lines []string
	prefix := strings.Repeat(" ", indent)
	lines = append(lines, prefix+"Label: "+FormatRunLabel(*entry))
	lines = append(lines, prefix+"Status: "+FormatRunStatus(*entry))
	lines = append(lines, prefix+"RunID: "+entry.RunID)
	lines = append(lines, prefix+"Session: "+entry.ChildSessionKey)
	if entry.Task != "" {
		lines = append(lines, prefix+"Task: "+entry.Task)
	}
	if entry.Model != "" {
		lines = append(lines, prefix+"Model: "+entry.Model)
	}
	lines = append(lines, prefix+"Cleanup: "+entry.Cleanup)
	lines = append(lines, prefix+"Created: "+FormatTimestampWithAge(entry.CreatedAt))
	return strings.Join(lines, "\n")
}
