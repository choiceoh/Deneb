// mentions.go — Mention parsing and message group context.
// Mirrors src/auto-reply/reply/mentions.ts (232 LOC),
// groups.ts (184 LOC), channel-context.ts (45 LOC),
// telegram-context.ts (41 LOC), inbound-text.ts (18 LOC),
// inbound-dedupe.ts (82 LOC), strip-inbound-meta.ts (192 LOC),
// body.ts (44 LOC), reply-inline.ts (45 LOC),
// reply-inline-whitespace.ts (5 LOC), reply-reference.ts (60 LOC),
// reply-threading.ts (68 LOC), reply-media-paths.ts (106 LOC),
// reply-delivery.ts (134 LOC), audio-tags.ts (1 LOC).
package autoreply

import (
	"regexp"
	"strings"
)

// MentionPattern detects @mentions in message text.
var MentionPattern = regexp.MustCompile(`@([a-zA-Z0-9_]+)`)

// ExtractMentions returns all @mentioned usernames from text.
func ExtractMentions(text string) []string {
	matches := MentionPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	var mentions []string
	seen := make(map[string]bool)
	for _, m := range matches {
		username := m[1]
		if !seen[username] {
			seen[username] = true
			mentions = append(mentions, username)
		}
	}
	return mentions
}

// ContainsMention checks if text mentions a specific username.
func ContainsMention(text, username string) bool {
	if username == "" {
		return false
	}
	re := regexp.MustCompile(`(?i)@` + regexp.QuoteMeta(username) + `\b`)
	return re.MatchString(text)
}

// GroupContext holds context for group chat messages.
type GroupContext struct {
	GroupID     string
	GroupTitle  string
	MemberCount int
	IsThread    bool
	ThreadID    string
}

// ChannelContext holds channel-specific context.
type ChannelContext struct {
	Channel     string
	AccountID   string
	BotUsername string
	ChatType    string // "direct", "group", "supergroup", "channel"
}

// TelegramContext holds Telegram-specific message context.
type TelegramContext struct {
	ChatID           int64
	MessageID        int64
	ThreadID         int64
	IsForward        bool
	IsReply          bool
	ReplyToMessageID int64
}

// ExtractInboundText extracts the text body from an inbound message context.
func ExtractInboundText(msg *MsgContext) string {
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
	result := StripReplyTags(text)
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

// ReplyThreading resolves threading for a reply.
type ReplyThreading struct {
	ReplyToID      string
	ReplyToCurrent bool
	ThreadID       string
}

// ResolveReplyThreading determines the threading for a reply payload.
func ResolveReplyThreading(payload ReplyPayload, msg *MsgContext) ReplyThreading {
	threading := ReplyThreading{}

	// Check for explicit reply-to tag.
	replyTo, current := ApplyReplyThreading(payload.Text, "")
	if current {
		threading.ReplyToCurrent = true
		threading.ReplyToID = msg.MessageSid
	} else if replyTo != "" {
		threading.ReplyToID = replyTo
	} else if payload.ReplyToID != "" {
		threading.ReplyToID = payload.ReplyToID
	}

	// Thread ID from message context.
	threading.ThreadID = msg.ThreadID

	return threading
}

// MediaPathResolver resolves media file paths for delivery.
type MediaPathResolver struct {
	BaseDir string
}

// ResolvePath resolves a media path to an absolute path.
func (r *MediaPathResolver) ResolvePath(path string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "http") {
		return path
	}
	if r.BaseDir != "" {
		return r.BaseDir + "/" + path
	}
	return path
}

// ReplyDelivery handles the final delivery of a reply to a channel.
type ReplyDeliveryConfig struct {
	Channel    string
	To         string
	AccountID  string
	ThreadID   string
	ReplyToID  string
	ChunkLimit int
	ChunkMode  ChunkMode
}

// AudioTag represents audio metadata.
type AudioTag struct {
	IsVoice  bool
	Duration int // seconds
}
