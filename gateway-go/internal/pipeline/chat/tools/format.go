package tools

import "fmt"

// Generic formatting helpers shared across tools (wiki storage reports, recall
// snippets, attachment/document formatting). They live here — not in any single
// tool file — so a tool never has to reach into an unrelated tool (e.g. wiki)
// just to format a byte count or trim a string.

// truncate caps s to maxRunes on a rune boundary — so multibyte Korean text is
// never split mid-character — and appends an ellipsis when it trims.
func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// formatBytes renders a byte count as a human-readable size (B / KB / MB).
func formatBytes(b int64) string {
	switch {
	case b >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
