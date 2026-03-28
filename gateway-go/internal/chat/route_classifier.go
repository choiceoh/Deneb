package chat

import "strings"

// RouteDecision indicates how to handle a message when a task is already running.
type RouteDecision int

const (
	// RouteConcurrent means the message should be handled by a concurrent
	// response run while the task continues in the background.
	RouteConcurrent RouteDecision = iota

	// RouteInterrupt means the message is an explicit interrupt/new-task
	// request: the current task should be cancelled first.
	RouteInterrupt
)

// interruptKeywords are explicit interrupt signals that should cancel the
// running task. Kept intentionally narrow — only unambiguous stop signals.
var interruptKeywords = []string{
	// Korean
	"중단", "그만", "멈춰", "취소", "중지", "스톱",
	// English
	"stop", "cancel", "abort", "kill",
}

// classifyRoute decides whether a user message during an active task should
// run concurrently (chat alongside the task) or interrupt the task.
//
// Uses keyword matching only — no LLM call — for zero latency.
// Default is RouteConcurrent (keep the task alive).
func classifyRoute(message string) RouteDecision {
	lower := strings.ToLower(strings.TrimSpace(message))

	// Slash commands that imply interruption.
	if strings.HasPrefix(lower, "/kill") ||
		strings.HasPrefix(lower, "/stop") ||
		strings.HasPrefix(lower, "/reset") ||
		strings.HasPrefix(lower, "/new") {
		return RouteInterrupt
	}

	// Check for explicit interrupt keywords.
	// Only match if the keyword is the entire message or clearly dominant
	// (short messages ≤ 20 chars containing an interrupt keyword).
	if len(lower) <= 20 {
		for _, kw := range interruptKeywords {
			if strings.Contains(lower, kw) {
				return RouteInterrupt
			}
		}
	}

	return RouteConcurrent
}
