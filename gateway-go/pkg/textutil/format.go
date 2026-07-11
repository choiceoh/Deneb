// Package textutil provides shared text formatting helpers
// used across multiple internal packages.
package textutil

import "fmt"

// FormatDuration formats a millisecond duration into a human-readable string.
// Examples: 500 → "500ms", 1500 → "1.5s", 90000 → "1m30s".
func FormatDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	secs := float64(ms) / 1000.0
	if secs < 60 {
		return fmt.Sprintf("%.1fs", secs)
	}
	mins := int(secs / 60)
	remainSecs := int(secs) % 60
	return fmt.Sprintf("%dm%ds", mins, remainSecs)
}

// FormatBytes renders a byte count with the compact B, KB, or MB units used in
// user-facing storage and attachment summaries.
func FormatBytes(bytes int64) string {
	switch {
	case bytes >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	case bytes >= 1024:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// GroupThousands inserts comma separators into a non-negative integer string.
func GroupThousands(integer string) string {
	var out []byte
	for i, digit := range []byte(integer) {
		if i > 0 && (len(integer)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, digit)
	}
	return string(out)
}
