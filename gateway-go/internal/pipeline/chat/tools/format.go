package tools

import (
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// Generic formatting helpers shared by the remaining root tools, including wiki
// storage reports and recall snippets. Document-specific presentation lives in
// the document subpackage.

// truncate caps s to maxRunes on a rune boundary — so multibyte Korean text is
// never split mid-character — and appends an ellipsis when it trims.
func truncate(s string, maxRunes int) string {
	return textutil.TruncateRunes(s, maxRunes, "...")
}

// truncateRunes applies the longer omission marker used by stored and ingested
// document excerpts.
func truncateRunes(s string, maxRunes int) string {
	return textutil.TruncateRunes(s, maxRunes, "\n... (이하 생략)")
}

// formatBytes renders a byte count as a human-readable size (B / KB / MB).
func formatBytes(b int64) string {
	return textutil.FormatBytes(b)
}

// firstLine returns the first line of s for compact metadata and summaries.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
