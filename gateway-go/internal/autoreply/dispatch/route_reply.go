package dispatch

import (
	"context"
	"fmt"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/chunk"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
)

// RouteReply delivers a reply to Telegram, chunking text as needed.
func RouteReply(
	ctx context.Context,
	tgPlugin *telegram.Plugin,
	channelID string,
	to string,
	payload types.ReplyPayload,
	chunkLimit int,
	chunkMode chunk.Mode,
) error {
	if channelID != "telegram" || tgPlugin == nil {
		return fmt.Errorf("channel %q not available", channelID)
	}

	// Chunk text if it exceeds the limit.
	texts := []string{payload.Text}
	if chunkLimit > 0 && len(payload.Text) > chunkLimit {
		texts = chunk.TextWithMode(payload.Text, chunkLimit, chunkMode)
	}

	for i, text := range texts {
		msg := telegram.OutboundMessage{
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

		if err := tgPlugin.SendMessage(ctx, msg); err != nil {
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
