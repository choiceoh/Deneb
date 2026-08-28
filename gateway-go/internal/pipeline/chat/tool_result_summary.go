// tool_result_summary.go — the one-line digest a finished tool chip shows.
//
// The `started` frame already carries the human hint (the command, query, or
// path). What a finished chip was missing is the OUTCOME: whether the call
// produced two lines or two hundred. The full result is not wire material for a
// chip — it can be thousands of characters, may hold secrets, and already
// reaches the model through the tool message — so the gateway owns the digest
// and clients render it verbatim (same wording everywhere, no client-side
// truncation policy to drift).
package chat

import (
	"fmt"
	"strings"
	"unicode"
)

// toolSummaryMaxRunes bounds the digest to chip width. Runes, not bytes: Korean
// results would otherwise be cut to a third of the intended length.
const toolSummaryMaxRunes = 60

// Preview bounds. The chip's expandable body shows what came back without
// carrying the whole result to the client: enough to read a short file, a diff
// hunk, or a command's output, and no more.
const (
	toolPreviewMaxRunes = 800
	toolPreviewMaxLines = 24
)

// summarizeToolResult renders a raw tool result as one short line: the first
// meaningful line, plus a count of the lines it stands for. Returns "" when the
// result carries nothing worth showing — the chip then reads as it does today.
func summarizeToolResult(result string) string {
	// Carriage returns separate lines in terminal output — treat them as breaks
	// rather than dropping them, or two logical lines would fuse into one.
	normalized := strings.ReplaceAll(strings.ReplaceAll(result, "\r\n", "\n"), "\r", "\n")
	lines := make([]string, 0, 8)
	for _, raw := range strings.Split(normalized, "\n") {
		// Control characters (a stray CR, ANSI leftovers) would corrupt the SSE
		// line and the chip; drop them rather than escaping.
		cleaned := strings.TrimSpace(strings.Map(func(r rune) rune {
			if r != '\t' && unicode.IsControl(r) {
				return -1
			}
			return r
		}, raw))
		if cleaned != "" {
			lines = append(lines, cleaned)
		}
	}
	if len(lines) == 0 {
		return ""
	}

	head := strings.Join(strings.Fields(lines[0]), " ")
	if runes := []rune(head); len(runes) > toolSummaryMaxRunes {
		head = strings.TrimRight(string(runes[:toolSummaryMaxRunes]), " ") + "…"
	}
	if len(lines) > 1 {
		return fmt.Sprintf("%s · %d줄", head, len(lines))
	}
	return head
}

// toolResultPreview renders the readable head of a tool result for the chip's
// expandable body: line structure kept (a diff or listing must stay readable),
// control characters dropped, bounded in both lines and runes. Returns "" when
// there is nothing to show.
func toolResultPreview(result string) string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(result, "\r\n", "\n"), "\r", "\n")
	normalized = strings.Map(func(r rune) rune {
		if r != '\t' && r != '\n' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, normalized)
	if strings.TrimSpace(normalized) == "" {
		return ""
	}

	lines := strings.Split(normalized, "\n")
	truncated := false
	if len(lines) > toolPreviewMaxLines {
		lines = lines[:toolPreviewMaxLines]
		truncated = true
	}
	body := strings.TrimRight(strings.Join(lines, "\n"), " \t\n")
	if runes := []rune(body); len(runes) > toolPreviewMaxRunes {
		body = string(runes[:toolPreviewMaxRunes])
		truncated = true
	}
	if truncated {
		body += "\n…"
	}
	return body
}
