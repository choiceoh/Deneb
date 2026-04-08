package autoreply

import "strings"

// AgentErrorKind classifies an agent/LLM error for recovery decisions.
type AgentErrorKind int

const (
	AgentErrorUnknown         AgentErrorKind = iota
	AgentErrorTransient                      // 502, 503, 521, 429, 529 — retryable
	AgentErrorContextOverflow                // context too large — needs session reset
	AgentErrorBilling                        // billing/payment — terminal
	AgentErrorRoleOrdering                   // role alternation — needs session reset
	AgentErrorCompaction                     // compaction failure — needs session reset
	AgentErrorAuth                           // 401/invalid key — terminal
	AgentErrorRateLimit                      // 429 specifically — terminal with backoff hint
	AgentErrorServerDown                     // 502/503/521/529 (non-429) — terminal
)

// ClassifyAgentError determines the error kind from a raw error message.
// The message typically comes from an LLM API HTTP response.
func ClassifyAgentError(msg string) AgentErrorKind {
	lower := strings.ToLower(msg)

	// Context overflow (check first — most actionable).
	if isContextOverflow(lower) {
		return AgentErrorContextOverflow
	}
	// Role ordering conflict.
	if strings.Contains(lower, "roles must alternate") || strings.Contains(lower, "incorrect role") {
		return AgentErrorRoleOrdering
	}
	// Compaction failure.
	if strings.Contains(lower, "compaction") && (strings.Contains(lower, "fail") || strings.Contains(lower, "error")) {
		return AgentErrorCompaction
	}
	// Auth (before transient — 401 is not retryable).
	if strings.Contains(msg, "401") ||
		strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "invalid_api_key") ||
		strings.Contains(lower, "authentication_error") ||
		strings.Contains(lower, "invalid api key") {
		return AgentErrorAuth
	}
	// Billing.
	if strings.Contains(lower, "billing") || strings.Contains(lower, "payment") || strings.Contains(lower, "insufficient_quota") {
		return AgentErrorBilling
	}
	// Rate limit (429 specifically).
	if strings.Contains(msg, "429") {
		return AgentErrorRateLimit
	}
	// Server unavailable (502/503/521/529 — without 429).
	for _, code := range []string{"502", "503", "521", "529"} {
		if strings.Contains(msg, code) {
			return AgentErrorServerDown
		}
	}
	return AgentErrorUnknown
}

// IsTransient returns true for errors that may succeed on retry.
func (k AgentErrorKind) IsTransient() bool {
	return k == AgentErrorTransient || k == AgentErrorRateLimit || k == AgentErrorServerDown
}

// IsTransientHTTPError checks if an error is a retryable transient HTTP error.
func IsTransientHTTPError(msg string) bool {
	k := ClassifyAgentError(msg)
	return k.IsTransient()
}

func isContextOverflow(lower string) bool {
	if strings.Contains(lower, "context") && (strings.Contains(lower, "overflow") || strings.Contains(lower, "too large") || strings.Contains(lower, "exceeded") || strings.Contains(lower, "too long")) {
		return true
	}
	return strings.Contains(lower, "max_tokens") || strings.Contains(lower, "token limit")
}
