// reply_context.go — extract Telegram reply context from inbound messages.
//
// When a user replies to a specific message in Telegram, this extracts the
// replied-to message body and sender so the LLM agent has context about what
// the user is referencing.
package server

import (
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
)

// ReplyContext holds the extracted reply metadata from a Telegram message.
type ReplyContext struct {
	ReplyToID     string // message ID of the replied-to message (tg-chatID-msgID)
	ReplyToBody   string // text content of the replied-to message
	ReplyToSender string // display name of the replied-to message sender
	IsBot         bool   // true if the replied-to message was sent by our bot
}

// maxReplyQuoteLen caps the quoted text to avoid inflating the agent message.
const maxReplyQuoteLen = 500

// ExtractReplyContext extracts reply metadata from a Telegram message.
// Returns nil if the message is not a reply.
func ExtractReplyContext(msg *telegram.Message, botUserID int64) *ReplyContext {
	if msg == nil || msg.ReplyToMessage == nil {
		return nil
	}
	replied := msg.ReplyToMessage

	// Extract body: prefer Text, fall back to Caption for media messages.
	body := replied.Text
	if body == "" {
		body = replied.Caption
	}

	// Truncate long quotes.
	if len(body) > maxReplyQuoteLen {
		body = body[:maxReplyQuoteLen] + "…"
	}

	// Resolve sender name.
	sender := buildReplyContextSenderName(replied.From)

	// Determine if the replied-to message was from our bot.
	isBot := replied.From != nil && replied.From.ID == botUserID

	return &ReplyContext{
		ReplyToID:     fmt.Sprintf("tg-%d-%d", msg.Chat.ID, replied.MessageID),
		ReplyToBody:   body,
		ReplyToSender: sender,
		IsBot:         isBot,
	}
}

// FormatReplyPrefix builds a context prefix that gets prepended to the user
// message so the LLM knows what the user is replying to.
func FormatReplyPrefix(rc *ReplyContext) string {
	if rc == nil || rc.ReplyToBody == "" {
		return ""
	}

	var sb strings.Builder
	if rc.ReplyToSender != "" {
		fmt.Fprintf(&sb, "[%s에 대한 답장]\n", rc.ReplyToSender)
	} else {
		sb.WriteString("[답장]\n")
	}

	// Quote the replied-to text with > prefix.
	for _, line := range strings.Split(rc.ReplyToBody, "\n") {
		sb.WriteString("> ")
		sb.WriteString(line)
		sb.WriteString("\n")
	}

	return sb.String()
}

// buildReplyContextSenderName constructs a display name from a Telegram user.
func buildReplyContextSenderName(from *telegram.User) string {
	if from == nil {
		return ""
	}
	name := from.FirstName
	if from.LastName != "" {
		name += " " + from.LastName
	}
	return name
}
