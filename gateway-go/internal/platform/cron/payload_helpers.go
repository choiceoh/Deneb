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

// PickSummaryFromPayloads returns the last non-error, non-empty text from payloads.
func PickSummaryFromPayloads(payloads []PayloadSummary) string {
	// Try non-error payloads first (reverse order).
	for i := len(payloads) - 1; i >= 0; i-- {
		if payloads[i].IsError {
			continue
		}
		s := PickSummaryFromOutput(payloads[i].Text)
		if s != "" {
			return s
		}
	}
	// Fall back to error payloads.
	for i := len(payloads) - 1; i >= 0; i-- {
		s := PickSummaryFromOutput(payloads[i].Text)
		if s != "" {
			return s
		}
	}
	return ""
}

// PayloadSummary is a minimal payload for summary extraction.
type PayloadSummary struct {
	Text    string
	IsError bool
}

// PickLastNonEmptyText returns the last non-empty text from payloads.
func PickLastNonEmptyText(payloads []PayloadSummary) string {
	for i := len(payloads) - 1; i >= 0; i-- {
		if payloads[i].IsError {
			continue
		}
		clean := strings.TrimSpace(payloads[i].Text)
		if clean != "" {
			return clean
		}
	}
	for i := len(payloads) - 1; i >= 0; i-- {
		clean := strings.TrimSpace(payloads[i].Text)
		if clean != "" {
			return clean
		}
	}
	return ""
}
