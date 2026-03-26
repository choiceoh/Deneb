// queue_normalize.go — Queue mode and drop policy normalization.
// Mirrors src/auto-reply/reply/queue/normalize.ts (47 LOC).
package queue

import (
	"strings"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

// NormalizeFollowupQueueMode parses a raw string into a types.FollowupQueueMode.
// Returns empty string if the input is not recognized.
func NormalizeFollowupQueueMode(raw string) types.FollowupQueueMode {
	if raw == "" {
		return ""
	}
	cleaned := strings.TrimSpace(strings.ToLower(raw))
	switch cleaned {
	case "queue", "queued":
		return types.FollowupModeSteer
	case "interrupt", "interrupts", "abort":
		return types.FollowupModeInterrupt
	case "steer", "steering":
		return types.FollowupModeSteer
	case "followup", "follow-ups", "followups":
		return types.FollowupModeFollowup
	case "collect", "coalesce":
		return types.FollowupModeCollect
	case "steer+backlog", "steer-backlog", "steer_backlog":
		return types.FollowupModeSteerBacklog
	default:
		return ""
	}
}

// NormalizeFollowupDropPolicy parses a raw string into a types.FollowupDropPolicy.
// Returns empty string if the input is not recognized.
func NormalizeFollowupDropPolicy(raw string) types.FollowupDropPolicy {
	if raw == "" {
		return ""
	}
	cleaned := strings.TrimSpace(strings.ToLower(raw))
	switch cleaned {
	case "old", "oldest":
		return types.FollowupDropOld
	case "new", "newest":
		return types.FollowupDropNew
	case "summarize", "summary":
		return types.FollowupDropSummarize
	default:
		return ""
	}
}
