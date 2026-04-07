package httpretry

import (
	"fmt"
	"time"
)

// APIError represents a non-2xx HTTP response from an external API
// (LLM provider, Telegram Bot API, etc.). Shared across all API clients
// so retry logic and error inspection use a single type.
type APIError struct {
	StatusCode int
	Message    string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Message)
}

// IsRetryable returns true if the status code suggests the request can be
// retried. Uses the shared httpretry classification policy.
func (e *APIError) IsRetryable() bool {
	return IsRetryable(e.StatusCode)
}

// IsRateLimited returns true if the error is a rate limit (429).
func (e *APIError) IsRateLimited() bool {
	return e.StatusCode == 429
}
