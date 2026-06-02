package chat

import (
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

// formatThinkingForChannel renders the raw thinking text as a markdown
// blockquote prefix suitable for the native client. Returns "" when blank.
// The Telegram expandable-blockquote variant was retired with the bot.
func formatThinkingForChannel(_, thinking string) string {
	thinking = strings.TrimSpace(thinking)
	if thinking == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(thinking) + 8)
	b.WriteString("> 🧠 ")
	b.WriteString(strings.ReplaceAll(thinking, "\n", "\n> "))
	return b.String()
}
