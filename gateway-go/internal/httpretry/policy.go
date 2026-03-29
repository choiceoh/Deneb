// Package httpretry provides a shared HTTP status code retry policy used
// across LLM, Discord, and Telegram API clients.
//
// # Retry Categories
//
// HTTP status codes fall into three retry categories:
//
//   - CategoryNone — permanent client or protocol errors; never retry.
//     Examples: 400 Bad Request, 401 Unauthorized, 403 Forbidden,
//     404 Not Found, 405 Method Not Allowed, 410 Gone (resource permanently
//     removed), 422 Unprocessable Entity, 501 Not Implemented.
//
//   - CategoryTransient — transient server-side failures; retry with
//     standard exponential backoff.
//     Examples: 500 Internal Server Error, 502 Bad Gateway,
//     503 Service Unavailable, 529 Site Overloaded (Anthropic-specific).
//
//   - CategoryTimeout — gateway or request timeouts; retry with standard
//     backoff (same schedule as CategoryTransient).
//     Examples: 408 Request Timeout, 504 Gateway Timeout.
//
//   - CategoryRateLimit — the client is being throttled; retry after
//     honoring the Retry-After header and applying a higher minimum delay.
//     Examples: 429 Too Many Requests.
package httpretry

// Category classifies an HTTP status code for retry policy decisions.
type Category int

const (
	// CategoryNone means the error is permanent and must not be retried.
	// Covers all 4xx codes except 408 and 429, and 501 Not Implemented.
	CategoryNone Category = iota

	// CategoryTransient means a transient server-side failure.
	// Retry with standard exponential backoff.
	CategoryTransient

	// CategoryTimeout means a gateway or request timeout.
	// Retry with standard exponential backoff (same schedule as CategoryTransient).
	CategoryTimeout

	// CategoryRateLimit means the server is rate-limiting the client.
	// Retry after the Retry-After header duration, with a higher minimum delay.
	CategoryRateLimit
)

// Classify returns the retry Category for the given HTTP status code.
//
// Explicit allowlist: only codes listed here are considered retryable.
// Everything else — including unknown 5xx codes such as 501 Not Implemented —
// falls into CategoryNone to avoid retrying permanent errors.
func Classify(status int) Category {
	switch status {
	case 429:
		return CategoryRateLimit
	case 408, 504:
		return CategoryTimeout
	case 500, 502, 503, 529:
		return CategoryTransient
	default:
		// Explicit non-retryable examples for documentation clarity:
		//   400 Bad Request, 401 Unauthorized, 403 Forbidden,
		//   404 Not Found, 405 Method Not Allowed, 410 Gone,
		//   422 Unprocessable Entity, 501 Not Implemented.
		return CategoryNone
	}
}

// IsRetryable reports whether the HTTP status code warrants a retry attempt.
func IsRetryable(status int) bool {
	return Classify(status) != CategoryNone
}
