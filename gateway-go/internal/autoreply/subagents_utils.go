// subagents_utils.go — Subagent run record utilities.
// Mirrors src/auto-reply/reply/subagents-utils.ts (109 LOC).
package autoreply

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// SubagentRunRecord represents a single subagent run entry.
type SubagentRunRecord struct {
	RunID               string             `json:"runId"`
	ChildSessionKey     string             `json:"childSessionKey"`
	RequesterSessionKey string             `json:"requesterSessionKey"`
	RequesterDisplayKey string             `json:"requesterDisplayKey"`
	Task                string             `json:"task"`
	Label               string             `json:"label,omitempty"`
	Model               string             `json:"model,omitempty"`
	CreatedAt           int64              `json:"createdAt"`
	StartedAt           *int64             `json:"startedAt,omitempty"`
	EndedAt             *int64             `json:"endedAt,omitempty"`
	Outcome             *SubagentRunOutcome `json:"outcome,omitempty"`
}

// SubagentRunOutcome records the outcome of a subagent run.
type SubagentRunOutcome struct {
	Status string `json:"status"` // "ok", "error", "timeout"
}

// ResolveSubagentLabel returns a display label for the run record.
func ResolveSubagentLabel(entry *SubagentRunRecord, fallback string) string {
	if fallback == "" {
		fallback = "subagent"
	}
	if label := strings.TrimSpace(entry.Label); label != "" {
		return label
	}
	if task := strings.TrimSpace(entry.Task); task != "" {
		return task
	}
	return fallback
}

// FormatRunLabel returns a truncated display label.
func FormatRunLabel(entry *SubagentRunRecord, maxLength int) string {
	raw := ResolveSubagentLabel(entry, "")
	if maxLength <= 0 {
		maxLength = 72
	}
	if len(raw) > maxLength {
		return strings.TrimRight(raw[:maxLength], " \t") + "…"
	}
	return raw
}

// FormatRunStatus returns a human-readable status string.
func FormatRunStatus(entry *SubagentRunRecord) string {
	if entry.EndedAt == nil {
		return "running"
	}
	status := "done"
	if entry.Outcome != nil && entry.Outcome.Status != "" {
		status = entry.Outcome.Status
	}
	if status == "ok" {
		return "done"
	}
	return status
}

// SortSubagentRuns returns a copy sorted by start/create time descending (newest first).
func SortSubagentRuns(runs []*SubagentRunRecord) []*SubagentRunRecord {
	sorted := make([]*SubagentRunRecord, len(runs))
	copy(sorted, runs)
	sort.Slice(sorted, func(i, j int) bool {
		return runTime(sorted[i]) > runTime(sorted[j])
	})
	return sorted
}

func runTime(r *SubagentRunRecord) int64 {
	if r.StartedAt != nil {
		return *r.StartedAt
	}
	return r.CreatedAt
}

// SubagentTargetResolution holds the result of resolving a subagent target.
type SubagentTargetResolution struct {
	Entry *SubagentRunRecord
	Error string
}

// SubagentTargetErrors provides error message templates for target resolution.
type SubagentTargetErrors struct {
	MissingTarget      string
	InvalidIndex       func(value string) string
	UnknownSession     func(value string) string
	AmbiguousLabel     func(value string) string
	AmbiguousLabelPfx  func(value string) string
	AmbiguousRunIDPfx  func(value string) string
	UnknownTarget      func(value string) string
}

// ResolveSubagentTargetFromRuns finds a subagent run by token (index, session key, label, or run ID prefix).
func ResolveSubagentTargetFromRuns(
	runs []*SubagentRunRecord,
	token string,
	recentWindowMinutes int,
	labelFn func(*SubagentRunRecord) string,
	isActiveFn func(*SubagentRunRecord) bool,
	errors SubagentTargetErrors,
) SubagentTargetResolution {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return SubagentTargetResolution{Error: errors.MissingTarget}
	}

	sorted := SortSubagentRuns(runs)

	// "last" shortcut.
	if trimmed == "last" {
		if len(sorted) > 0 {
			return SubagentTargetResolution{Entry: sorted[0]}
		}
		return SubagentTargetResolution{Error: errors.MissingTarget}
	}

	if isActiveFn == nil {
		isActiveFn = func(e *SubagentRunRecord) bool { return e.EndedAt == nil }
	}

	recentCutoff := time.Now().UnixMilli() - int64(recentWindowMinutes)*60_000

	// Build numeric order: active first, then recently ended.
	var numericOrder []*SubagentRunRecord
	for _, e := range sorted {
		if isActiveFn(e) {
			numericOrder = append(numericOrder, e)
		}
	}
	for _, e := range sorted {
		if !isActiveFn(e) && e.EndedAt != nil && *e.EndedAt >= recentCutoff {
			numericOrder = append(numericOrder, e)
		}
	}

	// Numeric index (1-based).
	if isDigits(trimmed) {
		idx, err := strconv.Atoi(trimmed)
		if err != nil || idx <= 0 || idx > len(numericOrder) {
			return SubagentTargetResolution{Error: errors.InvalidIndex(trimmed)}
		}
		return SubagentTargetResolution{Entry: numericOrder[idx-1]}
	}

	// Session key (contains ":").
	if strings.Contains(trimmed, ":") {
		for _, e := range sorted {
			if e.ChildSessionKey == trimmed {
				return SubagentTargetResolution{Entry: e}
			}
		}
		return SubagentTargetResolution{Error: errors.UnknownSession(trimmed)}
	}

	lowered := strings.ToLower(trimmed)

	// Exact label match.
	var exactMatches []*SubagentRunRecord
	for _, e := range sorted {
		if strings.ToLower(labelFn(e)) == lowered {
			exactMatches = append(exactMatches, e)
		}
	}
	if len(exactMatches) == 1 {
		return SubagentTargetResolution{Entry: exactMatches[0]}
	}
	if len(exactMatches) > 1 {
		return SubagentTargetResolution{Error: errors.AmbiguousLabel(trimmed)}
	}

	// Label prefix match.
	var prefixMatches []*SubagentRunRecord
	for _, e := range sorted {
		if strings.HasPrefix(strings.ToLower(labelFn(e)), lowered) {
			prefixMatches = append(prefixMatches, e)
		}
	}
	if len(prefixMatches) == 1 {
		return SubagentTargetResolution{Entry: prefixMatches[0]}
	}
	if len(prefixMatches) > 1 {
		return SubagentTargetResolution{Error: errors.AmbiguousLabelPfx(trimmed)}
	}

	// Run ID prefix match.
	var idMatches []*SubagentRunRecord
	for _, e := range sorted {
		if strings.HasPrefix(e.RunID, trimmed) {
			idMatches = append(idMatches, e)
		}
	}
	if len(idMatches) == 1 {
		return SubagentTargetResolution{Entry: idMatches[0]}
	}
	if len(idMatches) > 1 {
		return SubagentTargetResolution{Error: errors.AmbiguousRunIDPfx(trimmed)}
	}

	return SubagentTargetResolution{Error: errors.UnknownTarget(trimmed)}
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
