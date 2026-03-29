package autoreply

import "strings"

// Error classification constants (mirrors TS pi-embedded-helpers.ts).
const (
	BillingErrorMessage      = "⚠️ Billing error — please check your API key or plan."
	ContextOverflowMessage   = "⚠️ Context overflow — prompt too large for this model. Try a shorter message or a larger-context model."
	RoleOrderingMessage      = "⚠️ Message ordering conflict. I've reset the conversation - please try again."
	CompactionFailureMessage = "⚠️ Context limit exceeded during compaction. I've reset our conversation to start fresh - please try again."
	TransientRetryDelayMs    = 2500

	// Korean-language error messages for specific HTTP error categories.
	RateLimitMessage        = "⚠️ API 요청 한도 초과 (429) — 잠시 후 다시 시도해 주세요."
	ServerUnavailableMessage = "⚠️ 서버 일시 장애 — 잠시 후 다시 시도해 주세요."
	AuthFailedMessage       = "⚠️ API 인증 실패 — API 키를 확인해 주세요."
)

// IsContextOverflowError checks if an error message indicates context overflow.
func IsContextOverflowError(msg string) bool {
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "context") && (strings.Contains(lower, "overflow") || strings.Contains(lower, "too large") || strings.Contains(lower, "exceeded") || strings.Contains(lower, "too long")) {
		return true
	}
	if strings.Contains(lower, "max_tokens") || strings.Contains(lower, "token limit") {
		return true
	}
	return false
}

// IsBillingError checks if an error is billing-related.
func IsBillingError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "billing") || strings.Contains(lower, "payment") || strings.Contains(lower, "insufficient_quota")
}

// IsTransientHTTPError checks if an error is a retryable transient HTTP error.
func IsTransientHTTPError(msg string) bool {
	for _, code := range []string{"502", "503", "521", "429", "529"} {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}

// IsRateLimitError checks if an error is specifically a 429 rate limit error.
func IsRateLimitError(msg string) bool {
	return strings.Contains(msg, "429")
}

// IsServerUnavailableError checks if an error is a non-429 server unavailability error (502/503/521/529).
func IsServerUnavailableError(msg string) bool {
	for _, code := range []string{"502", "503", "521", "529"} {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}

// IsAuthError checks if an error is an authentication/authorization failure.
func IsAuthError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(msg, "401") ||
		strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "invalid_api_key") ||
		strings.Contains(lower, "authentication_error") ||
		strings.Contains(lower, "invalid api key")
}

// ClassifyErrorMessage returns a user-facing Korean error message for the given error string,
// preferring specific messages over the generic fallback.
// Returns "" if no specific classification matches.
func ClassifyErrorMessage(msg string) string {
	switch {
	case IsRateLimitError(msg):
		return RateLimitMessage
	case IsAuthError(msg):
		return AuthFailedMessage
	case IsBillingError(msg):
		return BillingErrorMessage
	case IsServerUnavailableError(msg):
		return ServerUnavailableMessage
	default:
		return ""
	}
}

// IsRoleOrderingError checks if an error is a role ordering conflict.
func IsRoleOrderingError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "roles must alternate") || strings.Contains(lower, "incorrect role")
}

// IsCompactionFailure checks if an error occurred during compaction.
func IsCompactionFailure(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "compaction") && (strings.Contains(lower, "fail") || strings.Contains(lower, "error"))
}
