package cron

import (
	"fmt"
	"strings"
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

// JobDeliveryConfig is the delivery section of a cron job definition.
type JobDeliveryConfig struct {
	Channel    string `json:"channel,omitempty"`    // channel ID or "last"
	To         string `json:"to,omitempty"`         // recipient
	AccountID  string `json:"accountId,omitempty"`  // explicit account override
	ThreadID   string `json:"threadId,omitempty"`   // native client topic ID for per-topic knowledge routing; empty for the 업무 home
	BestEffort bool   `json:"bestEffort,omitempty"` // don't fail job on delivery error
}

// ResolveDeliveryTarget resolves the delivery target for a cron job. Output is
// delivered to the native client via the main-session handoff (see
// service_execution.go); this only resolves the channel/recipient metadata
// recorded with the run.
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
	threadID := ""
	if jobDelivery != nil {
		accountID = jobDelivery.AccountID
		threadID = jobDelivery.ThreadID
	}

	return &DeliveryTarget{
		Channel:   ch,
		To:        NormalizeDeliveryTarget(ch, to),
		AccountID: accountID,
		ThreadID:  threadID,
		Mode:      "explicit",
	}, nil
}

// NormalizeDeliveryTarget normalizes the "to" field for channel-specific formats.
func NormalizeDeliveryTarget(ch, to string) string {
	return strings.TrimSpace(to)
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
