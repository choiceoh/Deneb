// run_utils.go — Subagent run list utilities.
// Used by both the new subagent command layer and the legacy command handler layer.
package subagent

import (
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/session"
)

// SubagentRunListEntry holds a formatted run entry for display.
type SubagentRunListEntry struct {
	Entry *SubagentRunRecord
	Line  string
}

// BuildSubagentRunListEntries builds formatted entries for the /agents list.
func BuildSubagentRunListEntries(runs []*SubagentRunRecord, recentWindowMinutes int, maxLabelLen int) (active, recent []SubagentRunListEntry) {
	sorted := sortSubagentRunPtrs(runs)
	recentCutoff := time.Now().UnixMilli() - int64(recentWindowMinutes)*60_000
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
	lines = append(lines, prefix+"Created: "+session.FormatTimestampWithAge(entry.CreatedAt))
	return strings.Join(lines, "\n")
}
