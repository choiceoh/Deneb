package chat

import (
	"regexp"
	"strings"
)

// Reasoning models served through vLLM occasionally leak their chain-of-thought
// delimiters into the *answer* text when the server-side reasoning parser fails
// to split them onto a separate reasoning channel (observed with the step3.7
// build emitting a literal "[thinking]" into chat). The legitimate reasoning
// path is surfaced separately by thinking_surface.go's 🧠 blockquote, so any of
// these delimiters appearing in the answer body are leaks and must be stripped
// before delivery.
//
// We remove both complete blocks (<think>…</think>, [thinking]…[/thinking]) and
// any leftover standalone delimiters. (?is): i = case-insensitive, s = dot
// matches newlines so a multi-line reasoning block is removed whole.
var (
	reasoningBlockRe  = regexp.MustCompile(`(?is)<think(?:ing)?>.*?</think(?:ing)?>|\[think(?:ing)?\].*?\[/think(?:ing)?\]`)
	reasoningMarkerRe = regexp.MustCompile(`(?i)</?think(?:ing)?>|\[/?think(?:ing)?\]`)
)

// stripReasoningLeak removes chain-of-thought delimiters (and the content a
// complete pair wraps) that leaked into an answer. It does NOT trim surrounding
// whitespace, so it is safe to call on individual stream deltas where inter-
// chunk spacing must be preserved; callers handling a finalized answer should
// strings.TrimSpace the result themselves.
//
// For the streaming path a reasoning block is usually split across several
// deltas, so the block regex cannot match mid-stream — but the standalone-marker
// regex still strips the ugly "[thinking]" / "<think>" tokens from each delta, so
// the literal delimiter never reaches the user. The final answer text is cleaned
// in full by buildSyncResult, which runs this over the assembled string where the
// block regex does match and collapses the whole leak.
func stripReasoningLeak(s string) string {
	if s == "" {
		return s
	}
	// Fast path: every delimiter starts with '<' or '['. Plain prose (the
	// overwhelming common case) skips both regexes entirely.
	if !strings.ContainsAny(s, "<[") {
		return s
	}
	s = reasoningBlockRe.ReplaceAllString(s, "")
	s = reasoningMarkerRe.ReplaceAllString(s, "")
	return s
}
