package chat

import (
	"context"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

// translateThinkingForDisplay renders foreign-language reasoning into Korean for
// the 🧠 blockquote on channel delivery. Display only: the streamed reasoning
// payload is untouched, so this changes what the finished turn shows, not what
// the model produced.
//
// The policy — which blocks are worth translating, the deadline, the size cap —
// belongs to the injected translator, which the native client's SSE path shares,
// so the two surfaces cannot drift apart.
//
// Fail-open: an unreachable translator, a timeout, or a refusal all fall back to
// the original text. Losing the reply because a display nicety failed would be a
// far worse trade than reading English.
func translateThinkingForDisplay(deps runDeps, thinking string) string {
	if deps.translateThinking == nil || strings.TrimSpace(thinking) == "" {
		return thinking
	}
	translated, ok := deps.translateThinking(context.Background(), thinking)
	if !ok || strings.TrimSpace(translated) == "" {
		return thinking
	}
	return translated
}

// showThinkingInChat reports whether the agent should surface extended-thinking
// text alongside the reply for this session. ON by default; flipped through the
// session's ShowThinkingInChat field (sessions.patch). The `/think` slash
// command that used to set it was retired with the other conversational
// commands — nothing on the native surface sets it today.
func showThinkingInChat(deps runDeps, sessionKey string) bool {
	return sessionShowsThinking(deps.sessions, sessionKey)
}

// sessionShowsThinking answers the same question for callers that hold the
// manager rather than a run's deps. It exists because the setting has to reach
// every surface that renders reasoning — for a long time it gated only the 🧠
// blockquote, whose delivery path has no production wiring at all
// (ChannelCallbacks.SetReplyFunc has no caller outside tests), so turning
// thinking off changed nothing the operator could see.
func sessionShowsThinking(sessions *session.Manager, sessionKey string) bool {
	if sessions == nil {
		return true
	}
	sess := sessions.Get(sessionKey)
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
