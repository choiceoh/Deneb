package gmailpoll

import (
	"regexp"
	"strings"
)

// The local vLLM reasoning model (step3.7) occasionally leaks chain-of-thought
// delimiters into the answer text even with extended thinking disabled, when
// the server-side reasoning parser fails to split them onto a separate channel.
// The mail-analysis pipeline streams the model output straight to the user
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

// Tool-call choreography the synthesis model emits as TEXT. The mail-analysis
// prompt describes an agent flow ("(1) mail_archive list … (2) wiki search …"),
// but the final synthesis is a single tool-less completion — so the model
// role-plays the steps, emitting <tool_call>{…}</tool_call> blocks and "먼저 …
// 하겠습니다" narration before the actual report. None of it is a real tool call
// (there are no tools on this call); it is a leak the feed must not show.
//
//	(?is): i = case-insensitive, s = dot matches newlines so a multi-line block
//	(the JSON arguments span lines) is removed whole.
var (
	toolCallBlockRe  = regexp.MustCompile(`(?is)<tool_(?:call|calls|response|result)\b[^>]*>.*?</tool_(?:call|calls|response|result)>`)
	toolCallMarkerRe = regexp.MustCompile(`(?i)</?tool_(?:call|calls|response|result)\b[^>]*>`)
	// A leading line that is tool-use process narration: a first-person "I'll do
	// X" announcement (…하겠습니다) or one naming a tool. Used only to drop the
	// agent-roleplay preamble from the HEAD, and only once tool-call markup has
	// confirmed this is a leak — so a clean report is never touched.
	toolNarrationLineRe = regexp.MustCompile(`하겠습니다|하겠음|mail_archive|fetch_tools`)
)

// stripToolCallLeak removes tool-call markup the synthesis model emitted as text,
// plus the leading "먼저 메일 목록을 확인하겠습니다 …" narration that accompanies it,
// so only the analysis report reaches the feed. Gated on tool-call markup being
// present: a clean analysis (the common case, and any run without the leak) is
// returned untouched, so the head-narration strip can never eat a real report.
func stripToolCallLeak(s string) string {
	if !strings.Contains(s, "<tool_") && !strings.Contains(s, "</tool_") {
		return s
	}
	s = toolCallBlockRe.ReplaceAllString(s, "")
	s = toolCallMarkerRe.ReplaceAllString(s, "")
	// Markup was present → drop the agent-roleplay preamble it left behind: blank
	// lines and tool-use announcements at the head, stopping at the first real
	// report line (which matches neither pattern).
	lines := strings.Split(s, "\n")
	i := 0
	for i < len(lines) {
		t := strings.TrimSpace(lines[i])
		if t == "" || toolNarrationLineRe.MatchString(t) {
			i++
			continue
		}
		break
	}
	return strings.Join(lines[i:], "\n")
}

// sanitizeAnalysisLeak scrubs both leak classes from a finalized analysis answer:
// chain-of-thought delimiters (stripReasoningLeak) and agent tool-call roleplay
// (stripToolCallLeak). Callers TrimSpace the result themselves.
func sanitizeAnalysisLeak(s string) string {
	return stripToolCallLeak(stripReasoningLeak(s))
}
