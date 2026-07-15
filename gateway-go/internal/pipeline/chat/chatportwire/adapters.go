package chatportwire

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/reply"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/typing"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

// NewTypingSignaler builds the native typing indicator used by chat runs.
func NewTypingSignaler(onStart func()) chatport.TypingSignaler {
	ctrl := typing.NewTypingController(typing.TypingControllerConfig{
		OnStart:    onStart,
		IntervalMs: 5000, // 5s keepalive cadence for the native typing indicator
	})
	return typing.NewFullTypingSignaler(ctrl, typing.TypingModeInstant, false)
}

// SanitizeDraft re-exports reply.SanitizeDraftText for the chatport boundary.
func SanitizeDraft(text string) string { return reply.SanitizeDraftText(text) }

// ParseReplyDirectives re-exports reply.ParseReplyDirectives.
var ParseReplyDirectives = reply.ParseReplyDirectives
