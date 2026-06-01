package gmailpoll

import (
	"regexp"
	"strings"
)

// The local vLLM reasoning model (step3.7) occasionally leaks chain-of-thought
// delimiters into the answer text even with extended thinking disabled, when
// the server-side reasoning parser fails to split them onto a separate channel.
// The mail-analysis pipeline streams the model output straight to Telegram
// without passing through the chat package's delivery sanitizer, so it needs
// its own guard. This mirrors chat.stripReasoningLeak (the canonical version in
// internal/pipeline/chat/reasoning_leak.go); kept local to avoid an import
// cycle (chat/tools already imports this package).
//
//	(?is): i = case-insensitive, s = dot matches newlines so a multi-line block
//	is removed whole.
var (
	reasoningBlockRe  = regexp.MustCompile(`(?is)<think(?:ing)?>.*?</think(?:ing)?>|\[think(?:ing)?\].*?\[/think(?:ing)?\]`)
	reasoningMarkerRe = regexp.MustCompile(`(?i)</?think(?:ing)?>|\[/?think(?:ing)?\]`)
)

// stripReasoningLeak removes chain-of-thought delimiters (and the content a
// complete pair wraps) that leaked into an analysis answer. Callers should
// TrimSpace the result themselves if needed.
func stripReasoningLeak(s string) string {
	if s == "" {
		return s
	}
	// Fast path: every delimiter starts with '<' or '['. Plain analysis prose
	// (the common case) skips both regexes entirely.
	if !strings.ContainsAny(s, "<[") {
		return s
	}
	s = reasoningBlockRe.ReplaceAllString(s, "")
	s = reasoningMarkerRe.ReplaceAllString(s, "")
	return s
}
