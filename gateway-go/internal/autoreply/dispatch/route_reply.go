package dispatch

import (
	"context"
	"fmt"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/chunk"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/channel"
)

// RouteReply delivers a reply to a specific channel, chunking text as needed.
func RouteReply(
	ctx context.Context,
	channels *channel.Registry,
	channelID string,
	to string,
	payload types.ReplyPayload,
	chunkLimit int,
	chunkMode chunk.Mode,
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
		texts = chunk.TextWithMode(payload.Text, chunkLimit, chunkMode)
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
