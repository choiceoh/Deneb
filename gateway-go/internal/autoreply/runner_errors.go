package autoreply

import "strings"

// Error classification constants (mirrors TS pi-embedded-helpers.ts).
const (
	BillingErrorMessage      = "⚠️ Billing error — please check your API key or plan."
	ContextOverflowMessage   = "⚠️ Context overflow — prompt too large for this model. Try a shorter message or a larger-context model."
	RoleOrderingMessage      = "⚠️ Message ordering conflict. I've reset the conversation - please try again."
	CompactionFailureMessage = "⚠️ Context limit exceeded during compaction. I've reset our conversation to start fresh - please try again."
	TransientRetryDelayMs    = 2500
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
