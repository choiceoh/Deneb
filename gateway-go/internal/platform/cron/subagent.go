// subagent.go — Subagent polling for cron jobs.
// When a cron job's agent produces an interim response (e.g., "확인 중"),
// the poller waits for descendant subagents to finish and collects their output.
package cron

import (
	"context"
	"strings"
	"time"
)

// SubagentPoller checks for active descendant subagents in a cron session.
type SubagentPoller interface {
	// HasActiveDescendants returns true if the session has running child subagents.
	HasActiveDescendants(sessionKey string) bool
	// CollectDescendantOutputs gathers completed descendant outputs into a summary.
	CollectDescendantOutputs(sessionKey string) string
}

const (
	subagentPollTimeout  = 60 * time.Second
	subagentPollInterval = 5 * time.Second
)

// pollSubagentOutputs waits for descendant subagents to complete and
// appends their collected output to the original text.
// Returns the original text unchanged if no poller is configured or
// the output doesn't look like an interim message.
func pollSubagentOutputs(ctx context.Context, poller SubagentPoller, sessionKey, output string) string {
	if poller == nil || !isLikelyInterimMessage(output) {
		return output
	}

	deadline := time.Now().Add(subagentPollTimeout)
	for time.Now().Before(deadline) && poller.HasActiveDescendants(sessionKey) {
		select {
		case <-ctx.Done():
			return output
		case <-time.After(subagentPollInterval):
		}
	}

	if extra := poller.CollectDescendantOutputs(sessionKey); extra != "" {
		return output + "\n\n" + extra
	}
	return output
}

// isLikelyInterimMessage checks if agent output looks like an interim ack
// (suggesting subagents are still running). Only triggers on short responses
// (<100 chars) containing specific patterns.
func isLikelyInterimMessage(text string) bool {
	if text == "" {
		return false
	}
	trimmed := strings.TrimSpace(text)
	if len(trimmed) >= 100 {
		return false
	}
	lower := strings.ToLower(trimmed)

	interimPatterns := []string{
		"working on", "let me", "i'll", "one moment", "processing",
		"looking into", "checking", "running", "executing",
		"작업 중", "확인 중", "수집 중", "처리 중", "잠시만", "진행 중",
	}
	for _, pattern := range interimPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}
