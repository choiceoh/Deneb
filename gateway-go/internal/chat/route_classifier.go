package chat

import (
	"strings"
	"unicode/utf8"
)

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

// interruptSlashPrefixes are slash commands that imply task interruption.
var interruptSlashPrefixes = []string{
	"/kill", "/stop", "/reset", "/new",
}

// classifyRoute decides whether a user message during an active task should
// run concurrently (chat alongside the task) or interrupt the task.
//
// Design philosophy: default to RouteConcurrent (preserve the running task).
// Interruption requires unambiguous intent — explicit keywords in short
// messages or interrupt-specific slash commands. A long, detailed message
// is treated as concurrent even if it contains interrupt words (e.g.,
// "중단 없이 계속해" = "continue without stopping").
//
// This avoids the latency of an LLM classification call. If the user really
// wants to stop the task, they can use /kill or send a short "그만".
func classifyRoute(message string) RouteDecision {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return RouteConcurrent
	}

	// Slash commands that imply interruption — always honored regardless of length.
	for _, prefix := range interruptSlashPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return RouteInterrupt
		}
	}

	// Short messages (≤ 30 runes) containing interrupt keywords.
	// Short = likely a standalone command, not an embedded mention.
	if utf8.RuneCountInString(lower) <= 30 {
		for _, kw := range interruptKeywords {
			if strings.Contains(lower, kw) {
				return RouteInterrupt
			}
		}
	}

	return RouteConcurrent
}
