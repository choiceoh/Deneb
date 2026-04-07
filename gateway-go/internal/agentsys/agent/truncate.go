package agent

import (
	"fmt"
	"strings"
)

// DefaultMaxOutput is the head/tail truncation budget for tool results.
// When output exceeds this limit the middle is discarded and replaced with
// a truncation marker — Claude Code style.  Both ends are preserved so the
// LLM sees context (paths, invocations) at the top and errors/results at
// the bottom.
const DefaultMaxOutput = 32 * 1024 // 32K chars

// TruncateHeadTail preserves the first and last half of content when it
// exceeds maxChars, replacing the middle with a truncation marker.
//
// If spillID is non-empty the marker includes a read_spillover reference
// so the LLM can retrieve the full content on demand.
func TruncateHeadTail(content string, maxChars int, spillID string) string {
	if len(content) <= maxChars {
		return content
	}

	half := maxChars / 2
	head := content[:half]
	tail := content[len(content)-half:]

	// Count lines in the discarded middle for the marker.
	middle := content[half : len(content)-half]
	truncatedLines := strings.Count(middle, "\n")

	var marker string
	if spillID != "" {
		marker = fmt.Sprintf(
			"\n\n... [%d lines truncated — use read_spillover(%q) for full content] ...\n\n",
			truncatedLines, spillID)
	} else {
		marker = fmt.Sprintf("\n\n... [%d lines truncated] ...\n\n", truncatedLines)
	}

	return head + marker + tail
}
