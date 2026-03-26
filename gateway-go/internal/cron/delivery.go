package cron

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/chunk"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/tokens"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/channel"
)

// DeliveryTarget specifies where to deliver cron job output.
type DeliveryTarget struct {
	Channel   string `json:"channel"`
	To        string `json:"to"`
	AccountID string `json:"accountId,omitempty"`
	ThreadID  string `json:"threadId,omitempty"`
	Mode      string `json:"mode,omitempty"` // "explicit" or "implicit"
}

// DeliveryResult reports the outcome of delivering cron output to a channel.
type DeliveryResult struct {
	Delivered bool   `json:"delivered"`
	Channel   string `json:"channel"`
	To        string `json:"to"`
	Error     string `json:"error,omitempty"`
	LatencyMs int64  `json:"latencyMs,omitempty"`
}

// DeliveryPlan holds the resolved delivery parameters for a cron job run.
type DeliveryPlan struct {
	Target     DeliveryTarget `json:"target"`
	BestEffort bool           `json:"bestEffort,omitempty"`
	ChunkLimit int            `json:"chunkLimit,omitempty"`
	ChunkMode  string         `json:"chunkMode,omitempty"`
}

// JobDeliveryConfig is the delivery section of a cron job definition.
type JobDeliveryConfig struct {
	Channel    string `json:"channel,omitempty"`    // channel ID or "last"
	To         string `json:"to,omitempty"`         // recipient
	AccountID  string `json:"accountId,omitempty"`  // explicit account override
	BestEffort bool   `json:"bestEffort,omitempty"` // don't fail job on delivery error
}

// ResolveDeliveryTarget resolves the delivery target for a cron job.
// In the Telegram-only DGX Spark deployment, this is straightforward:
// default to the Telegram channel with the configured chat ID.
func ResolveDeliveryTarget(
	jobDelivery *JobDeliveryConfig,
	defaultChannel string,
	defaultTo string,
) (*DeliveryTarget, error) {
	ch := defaultChannel
	to := defaultTo

	if jobDelivery != nil {
		if jobDelivery.Channel != "" && jobDelivery.Channel != "last" {
			ch = jobDelivery.Channel
		}
		if jobDelivery.To != "" {
			to = jobDelivery.To
		}
	}

	if ch == "" {
		return nil, fmt.Errorf("no delivery channel configured")
	}
	if to == "" {
		return nil, fmt.Errorf("no delivery recipient configured")
	}

	accountID := ""
	if jobDelivery != nil {
		accountID = jobDelivery.AccountID
	}

	return &DeliveryTarget{
		Channel:   ch,
		To:        NormalizeDeliveryTarget(ch, to),
		AccountID: accountID,
		Mode:      "explicit",
	}, nil
}

// NormalizeDeliveryTarget normalizes the "to" field for channel-specific formats.
func NormalizeDeliveryTarget(ch, to string) string {
	channelLower := strings.ToLower(strings.TrimSpace(ch))
	toTrimmed := strings.TrimSpace(to)

	// Feishu/Lark prefix stripping.
	if channelLower == "feishu" || channelLower == "lark" {
		lowered := strings.ToLower(toTrimmed)
		if strings.HasPrefix(lowered, "user:") {
			return strings.TrimSpace(toTrimmed[5:])
		}
		if strings.HasPrefix(lowered, "chat:") {
			return strings.TrimSpace(toTrimmed[5:])
		}
	}
	return toTrimmed
}

// MatchesDeliveryTarget checks if a messaging tool target matches a delivery target.
// Used for deduplication of cron delivery when the agent already sent via a tool.
func MatchesDeliveryTarget(
	targetProvider, targetTo, targetAccountID string,
	deliveryCh, deliveryTo, deliveryAccountID string,
) bool {
	if deliveryCh == "" || deliveryTo == "" || targetTo == "" {
		return false
	}
	chLower := strings.ToLower(strings.TrimSpace(deliveryCh))
	provLower := strings.ToLower(strings.TrimSpace(targetProvider))
	if provLower != "" && provLower != "message" && provLower != chLower {
		return false
	}
	if targetAccountID != "" && deliveryAccountID != "" && targetAccountID != deliveryAccountID {
		return false
	}
	normalizedTargetTo := NormalizeDeliveryTarget(chLower, stripTopicSuffix(targetTo))
	normalizedDeliveryTo := NormalizeDeliveryTarget(chLower, deliveryTo)
	return normalizedTargetTo == normalizedDeliveryTo
}

var topicSuffixCutset = ":topic:"

func stripTopicSuffix(to string) string {
	if idx := strings.LastIndex(to, topicSuffixCutset); idx >= 0 {
		return to[:idx]
	}
	return to
}

// DeliverCronOutput delivers the output of a cron job run to the resolved target.
// This is the Go equivalent of dispatchCronDelivery() from the TS codebase.
func DeliverCronOutput(
	ctx context.Context,
	channels *channel.Registry,
	target DeliveryTarget,
	payloads []types.ReplyPayload,
	opts DeliverOutputOptions,
) DeliveryResult {
	start := time.Now()
	result := DeliveryResult{
		Channel: target.Channel,
		To:      target.To,
	}

	if len(payloads) == 0 {
		result.Delivered = true
		return result
	}

	plugin := channels.Get(target.Channel)
	if plugin == nil {
		result.Error = fmt.Sprintf("channel %q not found", target.Channel)
		result.LatencyMs = time.Since(start).Milliseconds()
		return result
	}

	messenger, ok := plugin.(channel.MessagingAdapter)
	if !ok {
		result.Error = fmt.Sprintf("channel %q does not support messaging", target.Channel)
		result.LatencyMs = time.Since(start).Milliseconds()
		return result
	}

	chunkLimit := opts.ChunkLimit
	if chunkLimit <= 0 {
		chunkLimit = chunk.DefaultLimit
	}

	for _, payload := range payloads {
		// Skip silent replies.
		if tokens.IsSilentReplyText(payload.Text, "") {
			continue
		}

		// Chunk text if needed.
		texts := []string{payload.Text}
		if len(payload.Text) > chunkLimit {
			texts = chunk.TextWithMode(payload.Text, chunkLimit, chunk.Mode(opts.ChunkMode))
		}

		for i, text := range texts {
			msg := channel.OutboundMessage{
				To:   target.To,
				Text: text,
			}
			if i == len(texts)-1 {
				if payload.MediaURL != "" {
					msg.Media = []string{payload.MediaURL}
				} else if len(payload.MediaURLs) > 0 {
					msg.Media = payload.MediaURLs
				}
			}

			if err := messenger.SendMessage(ctx, msg); err != nil {
				result.Error = err.Error()
				result.LatencyMs = time.Since(start).Milliseconds()
				if opts.Logger != nil {
					opts.Logger.Warn("cron delivery error",
						"channel", target.Channel,
						"to", target.To,
						"error", err,
					)
				}
				if !opts.BestEffort {
					return result
				}
			}

			// Small delay between chunks.
			if i < len(texts)-1 {
				select {
				case <-ctx.Done():
					result.Error = ctx.Err().Error()
					result.LatencyMs = time.Since(start).Milliseconds()
					return result
				case <-time.After(200 * time.Millisecond):
				}
			}
		}
	}

	result.Delivered = true
	result.LatencyMs = time.Since(start).Milliseconds()
	return result
}

// DeliverOutputOptions configures cron output delivery.
type DeliverOutputOptions struct {
	ChunkLimit int
	ChunkMode  string // "length" or "newline"
	BestEffort bool
	Logger     *slog.Logger
}
