// mentions.go — Mention parsing and message group context.
// Mirrors src/auto-reply/reply/mentions.ts (232 LOC),
// groups.ts (184 LOC), channel-context.ts (45 LOC),
// telegram-context.ts (41 LOC), inbound-text.ts (18 LOC),
// inbound-dedupe.ts (82 LOC), strip-inbound-meta.ts (192 LOC),
// body.ts (44 LOC), reply-inline.ts (45 LOC),
// reply-inline-whitespace.ts (5 LOC), reply-reference.ts (60 LOC),
// reply-threading.ts (68 LOC), reply-media-paths.ts (106 LOC),
// reply-delivery.ts (134 LOC), audio-tags.ts (1 LOC).
package reply

import (
	"regexp"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/tokens"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
)

// MentionPattern detects @mentions in message text.
var MentionPattern = regexp.MustCompile(`@([a-zA-Z0-9_]+)`)

// ExtractInboundText extracts the text body from an inbound message context.
func ExtractInboundText(msg *types.MsgContext) string {
	if msg.Body != "" {
		return msg.Body
	}
	if msg.RawBody != "" {
		return msg.RawBody
	}
	return ""
}

// StripInboundMeta removes metadata markers from message text.
func StripInboundMeta(text string) string {
	if text == "" {
		return text
	}
	// Remove system tags.
	result := tokens.StripReplyTags(text)
	// Remove forwarded-from markers.
	result = stripForwardedHeader(result)
	return strings.TrimSpace(result)
}

var forwardedHeaderRe = regexp.MustCompile(`(?m)^Forwarded from .+:\n`)

func stripForwardedHeader(text string) string {
	return forwardedHeaderRe.ReplaceAllString(text, "")
}

// ReplyInline wraps text for inline reply display.
func ReplyInline(text string) string {
	return strings.TrimSpace(text)
}

// NormalizeInlineWhitespace collapses whitespace in inline replies.
func NormalizeInlineWhitespace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// ReplyReference holds a reference to a message being replied to.
type ReplyReference struct {
	MessageID string
	Text      string
	From      string
}
