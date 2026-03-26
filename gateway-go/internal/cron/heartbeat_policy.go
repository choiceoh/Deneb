// heartbeat_policy.go — Determines whether to skip heartbeat-only deliveries.
// Mirrors src/cron/heartbeat-policy.ts (49 LOC).
package cron

import (
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/tokens"
)

// HeartbeatDeliveryPayload is a minimal payload for heartbeat detection.
type HeartbeatDeliveryPayload struct {
	Text      string
	MediaURL  string
	MediaURLs []string
}

// ShouldSkipHeartbeatOnlyDelivery returns true if all payloads are heartbeat-only
// (no media, no non-heartbeat text).
func ShouldSkipHeartbeatOnlyDelivery(payloads []HeartbeatDeliveryPayload, ackMaxChars int) bool {
	if len(payloads) == 0 {
		return true
	}

	// Check if any payload has media.
	for _, p := range payloads {
		if p.MediaURL != "" || len(p.MediaURLs) > 0 {
			return false
		}
	}

	// Check if any payload is heartbeat-only.
	for _, p := range payloads {
		result := tokens.StripHeartbeatToken(p.Text, tokens.StripModeHeartbeat, ackMaxChars)
		if result.ShouldSkip {
			return true
		}
	}

	return false
}

// ShouldEnqueueCronMainSummary determines if a cron summary should be enqueued
// as a system event after failed delivery.
func ShouldEnqueueCronMainSummary(
	summaryText string,
	deliveryRequested bool,
	delivered bool,
	deliveryAttempted bool,
	suppressMainSummary bool,
	isCronSystemEvent func(string) bool,
) bool {
	trimmed := strings.TrimSpace(summaryText)
	if trimmed == "" {
		return false
	}
	return isCronSystemEvent(trimmed) &&
		deliveryRequested &&
		!delivered &&
		!deliveryAttempted &&
		!suppressMainSummary
}
