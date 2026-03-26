// queue_normalize.go — Queue mode and drop policy normalization.
// Mirrors src/auto-reply/reply/queue/normalize.ts (47 LOC).
package autoreply

import "strings"

// NormalizeFollowupQueueMode parses a raw string into a FollowupQueueMode.
// Returns empty string if the input is not recognized.
func NormalizeFollowupQueueMode(raw string) FollowupQueueMode {
	if raw == "" {
		return ""
	}
	cleaned := strings.TrimSpace(strings.ToLower(raw))
	switch cleaned {
	case "queue", "queued":
		return FollowupModeSteer
	case "interrupt", "interrupts", "abort":
		return FollowupModeInterrupt
	case "steer", "steering":
		return FollowupModeSteer
	case "followup", "follow-ups", "followups":
		return FollowupModeFollowup
	case "collect", "coalesce":
		return FollowupModeCollect
	case "steer+backlog", "steer-backlog", "steer_backlog":
		return FollowupModeSteerBacklog
	default:
		return ""
	}
}

// NormalizeFollowupDropPolicy parses a raw string into a FollowupDropPolicy.
// Returns empty string if the input is not recognized.
func NormalizeFollowupDropPolicy(raw string) FollowupDropPolicy {
	if raw == "" {
		return ""
	}
	cleaned := strings.TrimSpace(strings.ToLower(raw))
	switch cleaned {
	case "old", "oldest":
		return FollowupDropOld
	case "new", "newest":
		return FollowupDropNew
	case "summarize", "summary":
		return FollowupDropSummarize
	default:
		return ""
	}
}
