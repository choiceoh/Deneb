// format.go — human-readable rendering of Gmail results (search lists,
// full messages, labels) for the agent transcript. Split from operations.go
// (pure move).
package gmail

import (
	"fmt"
	"strings"
)

// FormatSearchResults formats message summaries into a readable string.
// Unread items are flagged with a single hollow circle (○) before the sender
// and keep the bold sender; read items render plain. One quiet dot, nothing
// else.
func FormatSearchResults(msgs []MessageSummary) string {
	if len(msgs) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, m := range msgs {
		if i > 0 {
			sb.WriteString("\n")
		}
		if hasUnreadLabel(m.Labels) {
			fmt.Fprintf(&sb, "○ **%s** — %s\n", m.From, m.Date)
		} else {
			fmt.Fprintf(&sb, "%s — %s\n", m.From, m.Date)
		}
		fmt.Fprintf(&sb, "  %s\n", m.Subject)
		if m.Snippet != "" {
			fmt.Fprintf(&sb, "  %s\n", m.Snippet)
		}
		fmt.Fprintf(&sb, "  ID: %s", m.ID)
	}
	return sb.String()
}

// hasUnreadLabel reports whether labels contain the Gmail UNREAD system label.
func hasUnreadLabel(labels []string) bool {
	for _, l := range labels {
		if l == "UNREAD" {
			return true
		}
	}
	return false
}

// FormatMessage formats a full message detail into a readable string.
func FormatMessage(m *MessageDetail) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "**From:** %s\n", m.From)
	fmt.Fprintf(&sb, "**To:** %s\n", m.To)
	if m.CC != "" {
		fmt.Fprintf(&sb, "**CC:** %s\n", m.CC)
	}
	fmt.Fprintf(&sb, "**Subject:** %s\n", m.Subject)
	fmt.Fprintf(&sb, "**Date:** %s\n", m.Date)
	fmt.Fprintf(&sb, "**ID:** %s\n", m.ID)
	if len(m.Attachments) > 0 {
		names := make([]string, len(m.Attachments))
		for i, a := range m.Attachments {
			names[i] = a.Filename
		}
		fmt.Fprintf(&sb, "**첨부:** %s  (gmail attachment 액션으로 내용 확인)\n", strings.Join(names, ", "))
	}
	sb.WriteString("\n")
	sb.WriteString(m.Body)
	return sb.String()
}

// FormatLabels formats label info into a readable list.
func FormatLabels(labels []LabelInfo) string {
	if len(labels) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, l := range labels {
		ltype := ""
		if l.Type == "system" {
			ltype = " (시스템)"
		}
		fmt.Fprintf(&sb, "- %s%s\n", l.Name, ltype)
	}
	return sb.String()
}
