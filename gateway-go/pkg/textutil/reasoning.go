package textutil

import (
	"regexp"
	"strings"
)

var (
	reasoningBlockPattern  = regexp.MustCompile(`(?is)<think(?:ing)?>.*?</think(?:ing)?>|\[think(?:ing)?\].*?\[/think(?:ing)?\]`)
	reasoningMarkerPattern = regexp.MustCompile(`(?i)</?think(?:ing)?>|\[/?think(?:ing)?\]`)
)

// StripReasoningLeak removes chain-of-thought delimiters and content wrapped
// by a complete delimiter pair. It preserves surrounding whitespace so it is
// safe for both stream deltas and finalized responses.
func StripReasoningLeak(value string) string {
	if value == "" || !strings.ContainsAny(value, "<[") {
		return value
	}
	value = reasoningBlockPattern.ReplaceAllString(value, "")
	return reasoningMarkerPattern.ReplaceAllString(value, "")
}
