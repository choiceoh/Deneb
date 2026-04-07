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

// NeedsSessionReset returns true for errors requiring conversation reset.
func (k AgentErrorKind) NeedsSessionReset() bool {
	return k == AgentErrorContextOverflow || k == AgentErrorRoleOrdering || k == AgentErrorCompaction
}

// UserMessage returns a Korean user-facing error message.
// Returns "" for AgentErrorUnknown (caller should use the raw error).
func (k AgentErrorKind) UserMessage() string {
	switch k {
	case AgentErrorContextOverflow:
		return ContextOverflowMessage
	case AgentErrorBilling:
		return BillingErrorMessage
	case AgentErrorRoleOrdering:
		return RoleOrderingMessage
	case AgentErrorCompaction:
		return CompactionFailureMessage
	case AgentErrorAuth:
		return AuthFailedMessage
	case AgentErrorRateLimit:
		return RateLimitMessage
	case AgentErrorServerDown:
		return ServerUnavailableMessage
	default:
		return ""
	}
}

// Error message constants.
const (
	BillingErrorMessage      = "⚠️ Billing error — please check your API key or plan."
	ContextOverflowMessage   = "⚠️ Context overflow — prompt too large for this model. Try a shorter message or a larger-context model."
	RoleOrderingMessage      = "⚠️ Message ordering conflict. I've reset the conversation - please try again."
	CompactionFailureMessage = "⚠️ Context limit exceeded during compaction. I've reset our conversation to start fresh - please try again."
	TransientRetryDelayMs    = 2500

	RateLimitMessage         = "⚠️ API 요청 한도 초과 (429) — 잠시 후 다시 시도해 주세요."
	ServerUnavailableMessage = "⚠️ 서버 일시 장애 — 잠시 후 다시 시도해 주세요."
	AuthFailedMessage        = "⚠️ API 인증 실패 — API 키를 확인해 주세요."
)

// Legacy classification functions — kept for external callers (e.g. chat/run_exec.go).

// IsTransientHTTPError checks if an error is a retryable transient HTTP error.
func IsTransientHTTPError(msg string) bool {
	k := ClassifyAgentError(msg)
	return k.IsTransient()
}

// IsContextOverflowError checks if an error message indicates context overflow.
func IsContextOverflowError(msg string) bool {
	return ClassifyAgentError(msg) == AgentErrorContextOverflow
}

// IsBillingError checks if an error is billing-related.
func IsBillingError(msg string) bool {
	return ClassifyAgentError(msg) == AgentErrorBilling
}

// IsRoleOrderingError checks if an error is a role ordering conflict.
func IsRoleOrderingError(msg string) bool {
	return ClassifyAgentError(msg) == AgentErrorRoleOrdering
}

// IsCompactionFailure checks if an error occurred during compaction.
func IsCompactionFailure(msg string) bool {
	return ClassifyAgentError(msg) == AgentErrorCompaction
}

// ClassifyErrorMessage returns a user-facing Korean error message.
// Returns "" if no specific classification matches.
func ClassifyErrorMessage(msg string) string {
	return ClassifyAgentError(msg).UserMessage()
}

// resetReason returns the session reset reason string for errors that need it.
func (k AgentErrorKind) resetReason() string {
	switch k {
	case AgentErrorContextOverflow:
		return "context_overflow"
	case AgentErrorRoleOrdering:
		return "role_ordering"
	case AgentErrorCompaction:
		return "compaction_failure"
	default:
		return "unknown"
	}
}

func isContextOverflow(lower string) bool {
	if strings.Contains(lower, "context") && (strings.Contains(lower, "overflow") || strings.Contains(lower, "too large") || strings.Contains(lower, "exceeded") || strings.Contains(lower, "too long")) {
		return true
	}
	return strings.Contains(lower, "max_tokens") || strings.Contains(lower, "token limit")
}
