package streaming

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
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

// Dispatch delivers a message to multiple targets concurrently using errgroup.
// Each delivery is independent: a failure in one does not cancel others.
// Returns a result for each target.
func Dispatch(
	ctx context.Context,
	tgPlugin *telegram.Plugin,
	targets []DeliveryTarget,
	text string,
	media []string,
) []DispatchResult {
	if len(targets) == 0 {
		return nil
	}

	results := make([]DispatchResult, len(targets))
	var wg sync.WaitGroup

	for i, t := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = deliverToTelegram(ctx, tgPlugin, t, text, media)
		}()
	}

	wg.Wait()
	return results
}

func deliverToTelegram(
	ctx context.Context,
	tgPlugin *telegram.Plugin,
	target DeliveryTarget,
	text string,
	media []string,
) DispatchResult {
	start := time.Now()
	result := DispatchResult{Channel: target.Channel}

	if target.Channel != "telegram" || tgPlugin == nil {
		result.Error = fmt.Errorf("channel %q not available", target.Channel)
		result.Latency = time.Since(start)
		return result
	}

	err := tgPlugin.SendMessage(ctx, telegram.OutboundMessage{
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
