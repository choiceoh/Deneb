// queue_normalize.go — Queue mode and drop policy normalization.
// Simplified: the queue always operates in collect (auto-debounce) mode
// with summarize drop policy for the single-user Telegram bot.
package queue

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"strings"
)

// NormalizeFollowupQueueMode always returns FollowupModeCollect.
// Other modes (steer, interrupt, followup, steer-backlog) have been removed
// since the single-user Telegram bot only needs auto-debounce (collect).
func NormalizeFollowupQueueMode(raw string) types.FollowupQueueMode {
	if raw == "" {
		return ""
	}
	cleaned := strings.TrimSpace(strings.ToLower(raw))
	if cleaned == "" {
		return ""
	}
	// All recognized inputs map to collect mode.
	switch cleaned {
	case "queue", "queued", "steer", "steering", "interrupt", "interrupts",
		"abort", "followup", "follow-ups", "followups", "collect", "coalesce",
		"steer+backlog", "steer-backlog", "steer_backlog":
		return types.FollowupModeCollect
	default:
		return ""
	}
}

// NormalizeFollowupDropPolicy always returns FollowupDropSummarize for any
// recognized input. Drop policy choice has been removed (single-user bot
// always uses summarize).
func NormalizeFollowupDropPolicy(raw string) types.FollowupDropPolicy {
	if raw == "" {
		return ""
	}
	cleaned := strings.TrimSpace(strings.ToLower(raw))
	switch cleaned {
	case "old", "oldest", "new", "newest", "summarize", "summary":
		return types.FollowupDropSummarize
	default:
		return ""
	}
}
