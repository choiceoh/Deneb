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
