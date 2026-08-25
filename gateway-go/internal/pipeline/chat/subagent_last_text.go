package chat

import "strings"

// lastAssistantTextFromTranscript returns a child session's final assistant
// text, the same fallback the subagents tool uses when session.LastOutput is
// not populated. The completion notifier needs it for the same reason: the
// parent is told to "synthesize the result below", so an empty payload leaves
// it either inventing a summary or silently dropping the child's work
// (observed 2026-08-26 in puppet mode).
func (h *Handler) lastAssistantTextFromTranscript(sessionKey string) string {
	if h == nil || h.transcript == nil || sessionKey == "" {
		return ""
	}
	msgs, _, err := h.transcript.Load(sessionKey, 30)
	if err != nil {
		return ""
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "assistant" {
			continue
		}
		if text := strings.TrimSpace(msgs[i].TextContent()); text != "" {
			return text
		}
	}
	return ""
}
