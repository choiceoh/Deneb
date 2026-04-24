package llmerr

// Pattern tables for message-based classification. All strings are
// lowercase; the classifier folds incoming messages with
// strings.ToLower before matching. Keep the tables in sync with
// Hermes's python reference (hermes-agent/agent/error_classifier.py)
// when adding new providers.

// billingPatterns signal confirmed billing exhaustion (not transient
// throttling). A match flips the classifier to ReasonBilling.
var billingPatterns = []string{
	"insufficient credits",
	"insufficient_quota",
	"credit balance",
	"credits have been exhausted",
	"top up your credits",
	"payment required",
	"billing hard limit",
	"exceeded your current quota",
	"account is deactivated",
	"plan does not include",
}

// rateLimitPatterns signal transient throttling that will self-resolve.
var rateLimitPatterns = []string{
	"rate limit",
	"rate_limit",
	"too many requests",
	"throttled",
	"requests per minute",
	"tokens per minute",
	"requests per day",
	"try again in",
	"please retry after",
	"resource_exhausted",
	"rate increased too quickly",
	"throttlingexception",
	"too many concurrent requests",
	"servicequotaexceededexception",
}

// usageLimitPatterns are ambiguous — they might be billing OR rate
// limit. Combine with usageLimitTransientSignals to disambiguate.
var usageLimitPatterns = []string{
	"usage limit",
	"quota",
	"limit exceeded",
	"key limit exceeded",
}

// usageLimitTransientSignals, when paired with a usage-limit pattern,
// indicate a transient window that will reset (not a billing issue).
var usageLimitTransientSignals = []string{
	"try again",
	"retry",
	"resets at",
	"reset in",
	"wait",
	"requests remaining",
	"periodic",
	"window",
}

// payloadTooLargePatterns detect HTTP 413 embedded in error message
// text when the status code is unavailable.
var payloadTooLargePatterns = []string{
	"request entity too large",
	"payload too large",
	"error code: 413",
}

// contextOverflowPatterns cover OpenAI, Anthropic, Gemini, vLLM,
// Ollama, llama.cpp, AWS Bedrock, and common Chinese error strings.
var contextOverflowPatterns = []string{
	"context length",
	"context_length_exceeded",
	"context_too_long",
	"context size",
	"maximum context",
	"token limit",
	"too many tokens",
	"reduce the length",
	"exceeds the limit",
	"context window",
	"prompt is too long",
	"prompt exceeds max length",
	"max_tokens",
	"maximum number of tokens",
	// vLLM / local inference.
	"exceeds the max_model_len",
	"max_model_len",
	"prompt length",
	"input is too long",
	"maximum model length",
	// Ollama.
	"context length exceeded",
	"truncating input",
	// llama.cpp / llama-server.
	"slot context",
	"n_ctx_slot",
	// Chinese.
	"超过最大长度",
	"上下文长度",
	// AWS Bedrock.
	"max input token",
	"input token",
	"exceeds the maximum number of input tokens",
}

// modelNotFoundPatterns match unknown / deprecated / unsupported model
// errors.
var modelNotFoundPatterns = []string{
	"is not a valid model",
	"invalid model",
	"model not found",
	"model_not_found",
	"does not exist",
	"no such model",
	"unknown model",
	"unsupported model",
}

// authPatterns catch authentication failures reported as free text
// (some providers don't surface a 401 status on OAuth-style errors).
var authPatterns = []string{
	"invalid api key",
	"invalid_api_key",
	"authentication",
	"unauthorized",
	"forbidden",
	"invalid token",
	"token expired",
	"token revoked",
	"access denied",
}

// serverDisconnectPatterns signal a transport-level hang-up. When
// paired with a large session, they are reclassified as context
// overflow (some gateways close the socket instead of returning 400).
var serverDisconnectPatterns = []string{
	"server disconnected",
	"peer closed connection",
	"connection reset by peer",
	"connection was closed",
	"network connection lost",
	"unexpected eof",
	"incomplete chunked read",
}

// sslTransientPatterns identify mid-stream SSL/TLS hiccups. These are
// transport issues (not overflow) so they stay at ReasonTimeout even
// for large sessions — compressing the context would do nothing for a
// flaky TLS renegotiation.
var sslTransientPatterns = []string{
	// Space-separated (Python ssl module, most SDKs).
	"bad record mac",
	"ssl alert",
	"tls alert",
	"ssl handshake failure",
	"tlsv1 alert",
	"sslv3 alert",
	// Underscore-separated (OpenSSL error tokens).
	"bad_record_mac",
	"ssl_alert",
	"tls_alert",
	"tls_alert_internal_error",
	// Python ssl module prefix.
	"[ssl:",
}

// transportMessagePatterns detect transport failures by message when
// no typed error is available. Kept small and conservative — over-
// matching here would mask genuine format errors.
var transportMessagePatterns = []string{
	"connection refused",
	"connection reset",
	"broken pipe",
	"no such host",
	"i/o timeout",
	"deadline exceeded",
	"unexpected eof",
	"eof",
	"read timeout",
	"connect timeout",
	"pool timeout",
}
