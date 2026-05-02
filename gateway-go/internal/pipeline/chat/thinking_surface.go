package chat

import (
	"html"
	"strings"
)

// showThinkingInChat reports whether the agent should surface extended-thinking
// text alongside the reply for this session. ON by default; flipped per
// session via `/think show off`.
func showThinkingInChat(deps runDeps, sessionKey string) bool {
	if deps.sessions == nil {
		return true
	}
	sess := deps.sessions.Get(sessionKey)
	if sess == nil || sess.ShowThinkingInChat == nil {
		return true
	}
	return *sess.ShowThinkingInChat
}

// formatThinkingForChannel renders the raw thinking text into a channel-
// appropriate prefix block. Returns "" when the input is blank.
//
// Telegram (HTML mode): wraps the text in an expandable blockquote so it
// collapses by default and does not blow past the 4096-char message limit
// on long reasoning traces. HTML-escapes the body so model markup
// (`<thinking>`, `<example>`, etc.) cannot break parse_mode=HTML.
//
// Other channels: falls back to a markdown blockquote prefix.
func formatThinkingForChannel(channel, thinking string) string {
	thinking = strings.TrimSpace(thinking)
	if thinking == "" {
		return ""
	}
	switch strings.ToLower(channel) {
	case "telegram":
		// Telegram <blockquote expandable> renders as a collapsed block with
		// "Show more" affordance; long traces stay scannable.
		return "<blockquote expandable>🧠 " + html.EscapeString(thinking) + "</blockquote>"
	default:
		var b strings.Builder
		b.Grow(len(thinking) + 8)
		b.WriteString("> 🧠 ")
		b.WriteString(strings.ReplaceAll(thinking, "\n", "\n> "))
		return b.String()
	}
}
