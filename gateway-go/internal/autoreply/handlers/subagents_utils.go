// subagents_utils.go — Subagent run utilities for the legacy command handler layer.
// Display-only helpers (BuildSubagentRunListEntries, FormatSubagentInfo) have moved
// to internal/autoreply/subagent. ResolveSubagentEntryForToken stays here because it
// returns *CommandResult which would create an import cycle if placed in subagent.
package handlers

import (
	"fmt"
	"strconv"
	"strings"

	subagentpkg "github.com/choiceoh/deneb/gateway-go/internal/autoreply/subagent"
)

// Re-export display helpers from subagent package.
type SubagentRunListEntry = subagentpkg.SubagentRunListEntry

func BuildSubagentRunListEntries(runs []*SubagentRunRecord, recentWindowMinutes int, maxLabelLen int) (active, recent []SubagentRunListEntry) {
	return subagentpkg.BuildSubagentRunListEntries(runs, recentWindowMinutes, maxLabelLen)
}

func FormatSubagentInfo(entry *SubagentRunRecord, indent int) string {
	return subagentpkg.FormatSubagentInfo(entry, indent)
}

// ResolveSubagentEntryForToken finds a subagent by token (index, session key, label, or run ID prefix).
// Returns a CommandResult error on failure. Kept here because CommandResult is in the handlers package.
func ResolveSubagentEntryForToken(runs []*SubagentRunRecord, token string) (*SubagentRunRecord, *CommandResult) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return nil, &CommandResult{Reply: "⚠️ Missing subagent target.", SkipAgent: true, IsError: true}
	}

	sorted := sortSubagentRunPtrsLocal(runs)

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

// sortSubagentRunPtrsLocal sorts run pointers: active first, then by creation time descending.
func sortSubagentRunPtrsLocal(runs []*SubagentRunRecord) []*SubagentRunRecord {
	sorted := make([]*SubagentRunRecord, len(runs))
	copy(sorted, runs)
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

