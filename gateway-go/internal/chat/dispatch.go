package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/channel"
	"golang.org/x/sync/errgroup"
)

// DispatchResult reports the outcome of delivering a message to one channel.
type DispatchResult struct {
	Channel   string        `json:"channel"`
	Delivered bool          `json:"delivered"`
	Error     error         `json:"error,omitempty"`
	Latency   time.Duration `json:"latency_ms"`
}

// DeliveryTarget specifies where to deliver a response message.
type DeliveryTarget struct {
	Channel   string `json:"channel"`
	To        string `json:"to"`
	AccountID string `json:"accountId,omitempty"`
	ThreadID  string `json:"threadId,omitempty"`
	ReplyTo   string `json:"replyTo,omitempty"`
}

// Dispatch delivers a message to multiple channels concurrently using errgroup.
// Each delivery is independent: a failure in one channel does not cancel others.
// Returns a result for each target.
func Dispatch(
	ctx context.Context,
	channels *channel.Registry,
	targets []DeliveryTarget,
	text string,
	media []string,
) []DispatchResult {
	if len(targets) == 0 {
		return nil
	}

	results := make([]DispatchResult, len(targets))
	g, gctx := errgroup.WithContext(ctx)

	for i, t := range targets {
		g.Go(func() error {
			results[i] = deliverToChannel(gctx, channels, t, text, media)
			return nil // Never return error — collect per-target, don't cancel siblings.
		})
	}

	g.Wait()
	return results
}

func deliverToChannel(
	ctx context.Context,
	channels *channel.Registry,
	target DeliveryTarget,
	text string,
	media []string,
) DispatchResult {
	start := time.Now()
	result := DispatchResult{Channel: target.Channel}

	plugin := channels.Get(target.Channel)
	if plugin == nil {
		result.Error = fmt.Errorf("channel %q not found", target.Channel)
		result.Latency = time.Since(start)
		return result
	}

	messenger, ok := plugin.(channel.MessagingAdapter)
	if !ok {
		result.Error = fmt.Errorf("channel %q does not support messaging", target.Channel)
		result.Latency = time.Since(start)
		return result
	}

	err := messenger.SendMessage(ctx, channel.OutboundMessage{
		To:      target.To,
		Text:    text,
		ReplyTo: target.ReplyTo,
		Media:   media,
	})
	result.Delivered = err == nil
	result.Error = err
	result.Latency = time.Since(start)
	return result
}
