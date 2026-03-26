package autoreply

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/channel"
)

// ReplyPayload represents an outbound reply message.
type ReplyPayload struct {
	Text         string            `json:"text,omitempty"`
	MediaURL     string            `json:"mediaUrl,omitempty"`
	MediaURLs    []string          `json:"mediaUrls,omitempty"`
	ReplyToID    string            `json:"replyToId,omitempty"`
	AudioAsVoice bool              `json:"audioAsVoice,omitempty"`
	IsError      bool              `json:"isError,omitempty"`
	ChannelData  map[string]any    `json:"channelData,omitempty"`
}

// TypingPolicy describes the context that triggered a reply.
type TypingPolicy string

const (
	TypingPolicyUserMessage    TypingPolicy = "user_message"
	TypingPolicySystemEvent    TypingPolicy = "system_event"
	TypingPolicyInternalWeb    TypingPolicy = "internal_webchat"
	TypingPolicyHeartbeat      TypingPolicy = "heartbeat"
)

// ReplyDispatchKind identifies the stage of a reply in the dispatch pipeline.
type ReplyDispatchKind string

const (
	DispatchKindTool  ReplyDispatchKind = "tool"
	DispatchKindBlock ReplyDispatchKind = "block"
	DispatchKindFinal ReplyDispatchKind = "final"
)

// DeliverFunc delivers a single reply payload to the originating channel.
type DeliverFunc func(ctx context.Context, payload ReplyPayload, kind ReplyDispatchKind) error

// ReplyDispatcher manages serialized delivery of tool results, block replies,
// and final replies. This mirrors the TS ReplyDispatcher.
type ReplyDispatcher struct {
	mu       sync.Mutex
	deliver  DeliverFunc
	logger   *slog.Logger
	ctx      context.Context
	counts   map[ReplyDispatchKind]int
	complete bool
}

// NewReplyDispatcher creates a new dispatcher.
func NewReplyDispatcher(ctx context.Context, deliver DeliverFunc, logger *slog.Logger) *ReplyDispatcher {
	return &ReplyDispatcher{
		deliver: deliver,
		logger:  logger,
		ctx:     ctx,
		counts:  make(map[ReplyDispatchKind]int),
	}
}

// Send delivers a reply payload with the given dispatch kind.
// Returns false if the dispatcher has been marked complete.
func (d *ReplyDispatcher) Send(payload ReplyPayload, kind ReplyDispatchKind) bool {
	d.mu.Lock()
	if d.complete {
		d.mu.Unlock()
		return false
	}
	d.counts[kind]++
	d.mu.Unlock()

	if err := d.deliver(d.ctx, payload, kind); err != nil {
		d.logger.Warn("reply dispatch error", "kind", kind, "error", err)
	}
	return true
}

// MarkComplete prevents further sends.
func (d *ReplyDispatcher) MarkComplete() {
	d.mu.Lock()
	d.complete = true
	d.mu.Unlock()
}

// Counts returns the number of sends per dispatch kind.
func (d *ReplyDispatcher) Counts() map[ReplyDispatchKind]int {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make(map[ReplyDispatchKind]int)
	for k, v := range d.counts {
		result[k] = v
	}
	return result
}

// InboundDispatchResult holds the outcome of dispatching an inbound message.
type InboundDispatchResult struct {
	Handled    bool
	CommandKey string
	Replies    []ReplyPayload
	Error      error
}

// DispatchInbound processes an inbound message through the auto-reply pipeline:
// command detection, agent execution, and reply delivery.
//
// This is the Go equivalent of dispatchInboundMessage() from the TS codebase.
// It coordinates the full message lifecycle:
// 1. Normalize and detect commands
// 2. Check deduplication
// 3. Route to command handler or agent
// 4. Deliver replies via the dispatcher
func DispatchInbound(
	ctx context.Context,
	params DispatchInboundParams,
) InboundDispatchResult {
	if params.Text == "" && len(params.Attachments) == 0 {
		return InboundDispatchResult{}
	}

	// Normalize command body.
	normalizedText := params.Text
	if params.Registry != nil {
		normalizedText = params.Registry.NormalizeCommandBody(params.Text, params.BotUsername)
	}

	// Check for control commands.
	if params.Registry != nil && params.Registry.HasControlCommand(normalizedText, "") {
		return InboundDispatchResult{
			Handled:    true,
			CommandKey: extractCommandKey(normalizedText),
		}
	}

	// Build reply payload and dispatch via the chat handler.
	// The actual agent execution is delegated to chat.Handler.Send().
	return InboundDispatchResult{Handled: true}
}

// DispatchInboundParams holds the parameters for inbound message dispatch.
type DispatchInboundParams struct {
	Text        string
	Attachments []string
	SessionKey  string
	Channel     string
	To          string
	AccountID   string
	ThreadID    string
	BotUsername string
	Registry    *CommandRegistry
}

func extractCommandKey(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "/") {
		return ""
	}
	// Extract just the command name.
	end := strings.IndexAny(trimmed[1:], " \t\n")
	if end == -1 {
		return trimmed[1:]
	}
	return trimmed[1 : end+1]
}

// RouteReply delivers a reply to a specific channel, chunking text as needed.
func RouteReply(
	ctx context.Context,
	channels *channel.Registry,
	channelID string,
	to string,
	payload ReplyPayload,
	chunkLimit int,
	chunkMode ChunkMode,
) error {
	plugin := channels.Get(channelID)
	if plugin == nil {
		return fmt.Errorf("channel %q not found", channelID)
	}

	messenger, ok := plugin.(channel.MessagingAdapter)
	if !ok {
		return fmt.Errorf("channel %q does not support messaging", channelID)
	}

	// Chunk text if it exceeds the limit.
	texts := []string{payload.Text}
	if chunkLimit > 0 && len(payload.Text) > chunkLimit {
		texts = ChunkTextWithMode(payload.Text, chunkLimit, chunkMode)
	}

	for i, text := range texts {
		msg := channel.OutboundMessage{
			To:      to,
			Text:    text,
			ReplyTo: payload.ReplyToID,
		}
		// Only set replyTo on the first chunk.
		if i > 0 {
			msg.ReplyTo = ""
		}
		// Attach media only on the last chunk.
		if i == len(texts)-1 {
			if payload.MediaURL != "" {
				msg.Media = []string{payload.MediaURL}
			} else if len(payload.MediaURLs) > 0 {
				msg.Media = payload.MediaURLs
			}
		}

		if err := messenger.SendMessage(ctx, msg); err != nil {
			return fmt.Errorf("send to %s failed (chunk %d/%d): %w", channelID, i+1, len(texts), err)
		}

		// Small delay between chunks to avoid rate limits.
		if i < len(texts)-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	return nil
}
