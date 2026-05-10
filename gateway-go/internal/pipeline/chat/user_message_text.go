// user_message_text.go provides helpers for working with the timestamp-
// prefixed user message text that executeAgentRun stores in the transcript.
// The prefix shape is "[<RFC3339 ts>] " — see the transcript persist site
// in run_exec.go for the rationale (cache-stable wall-clock signal: the
// system prompt's date field is day-only precision so the dynamic block
// stays byte-stable for trailing message cache markers).
package chat

import "strings"

// StripUserMessageTimestamp removes a leading "[<ISO 8601 ts>] " prefix
// from a user message text. Returns the input unchanged when no prefix is
// present (e.g. messages persisted before the timestamp policy, non-user
// messages that never gain one, or content where the closing "] " marker
// is absent so the heuristic refuses to chop bracketed content that was
// never a timestamp).
//
// The helper is conservative on purpose: callers that need raw user input
// (UI display, transcript review, recall search snippets) can opt in;
// recall preflight's substring search keeps the prefix in the haystack
// since the digits don't collide with cue words anyway.
func StripUserMessageTimestamp(text string) string {
	if !strings.HasPrefix(text, "[") {
		return text
	}
	end := strings.Index(text, "] ")
	if end < 0 {
		return text
	}
	return text[end+len("] "):]
}
