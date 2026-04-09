package cron

import (
	"strings"
	"unicode/utf8"
)

const summaryMaxChars = 2000

// PickSummaryFromOutput extracts a summary from agent output text.
// Truncates to summaryMaxChars if needed.
func PickSummaryFromOutput(text string) string {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return ""
	}
	if utf8.RuneCountInString(clean) > summaryMaxChars {
		return truncateUTF8(clean, summaryMaxChars) + "…"
	}
	return clean
}

// truncateUTF8 truncates a string to at most maxRunes runes.
func truncateUTF8(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count >= maxRunes {
			return s[:i]
		}
		count++
	}
	return s
}
