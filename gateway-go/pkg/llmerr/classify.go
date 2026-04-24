package llmerr

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
)

// Classified is the structured result of a classification attempt.
//
// Zero value is a valid ReasonUnknown classification with no metadata.
type Classified struct {
	// Reason is the normalised failure mode.
	Reason Reason

	// HTTPStatus is the HTTP status code from the response, or 0 if the
	// call never reached a response stage (transport error).
	HTTPStatus int

	// ProviderCode is the structured error.code field from the response
	// body, when present (for example "insufficient_quota" on OpenAI).
	ProviderCode string

	// Message is the sanitised error message used for pattern matching,
	// trimmed to at most 500 characters.
	Message string

	// Err is the original error passed to Classify, preserved so callers
	// can use errors.Is / errors.As against the classified value.
	Err error
}

// Hint passes optional session-level signals that can refine ambiguous
// classifications. In particular, a transport-level disconnect on a large
// session is much more likely to indicate context overflow than a transient
// network hiccup.
//
// All fields are optional; a zero-valued Hint behaves as if no hint was
// supplied.
type Hint struct {
	// TokenUsagePercent is the current context utilisation (0.0 - 1.0+).
	// Values above LargeSessionPercent count as a "large session" for the
	// ambiguous-disconnect heuristic.
	TokenUsagePercent float64

	// ApproxTokens is the approximate context token count. Values above
	// LargeSessionAbsoluteTokens count as "large session".
	ApproxTokens int

	// Provider is the lowercase provider ID (for example "anthropic",
	// "openai"). Used only for provider-specific tie-breaks today.
	Provider string
}

// Thresholds for the ambiguous-disconnect heuristic. Mirrors Hermes's
// classifier defaults (60% or 120K tokens).
const (
	// LargeSessionPercent is the utilisation above which a disconnect
	// with no status code is reclassified as context overflow.
	LargeSessionPercent = 0.60

	// LargeSessionAbsoluteTokens is the absolute token count above which
	// a disconnect is treated as context overflow regardless of percent.
	LargeSessionAbsoluteTokens = 120_000
)

// Classify inspects an error and returns the normalised reason along with
// supporting metadata. The pipeline (priority ordered):
//
//  1. Provider-specific patterns (thinking signature, long-context tier).
//  2. HTTP status code with message-aware refinement.
//  3. Structured provider error code.
//  4. Free-form message pattern match.
//  5. Transport-layer heuristics (timeout, disconnect-while-large).
//  6. Fallback: ReasonUnknown (retryable with backoff).
//
// Any parameter may be the zero value. Callers pass what they have; the
// classifier never panics on nil / empty inputs.
func Classify(err error, httpStatus int, body []byte) Classified {
	return ClassifyWithHint(err, httpStatus, body, Hint{})
}

// ClassifyWithHint is like Classify but also accepts session-level context
// for large-session heuristics.
func ClassifyWithHint(err error, httpStatus int, body []byte, hint Hint) Classified {
	// Best-effort body parse. Keep parsing cheap and silent — the body
	// might be text/plain, invalid JSON, or empty.
	bodyMsg, providerCode := extractBodySignals(body)
	msg := combineMessage(err, bodyMsg)

	result := Classified{
		HTTPStatus:   httpStatus,
		ProviderCode: providerCode,
		Message:      truncate(msg, 500),
		Err:          err,
	}

	// 1. Provider-specific patterns (highest priority).
	if r, ok := classifyProviderSpecific(httpStatus, msg); ok {
		result.Reason = r
		return result
	}

	// 2. HTTP status code classification.
	if httpStatus != 0 {
		if r, ok := classifyByStatus(httpStatus, msg, hint); ok {
			result.Reason = r
			return result
		}
	}

	// 3. Structured error code from body.
	if providerCode != "" {
		if r, ok := classifyByErrorCode(providerCode); ok {
			result.Reason = r
			return result
		}
	}

	// 4. Free-form message pattern matching.
	if r, ok := classifyByMessage(msg); ok {
		result.Reason = r
		return result
	}

	// 5. Transport heuristics (timeout, disconnect-while-large, SSL
	//    transient). Only meaningful when we have an actual error value.
	if err != nil {
		if r, ok := classifyTransport(err, msg, httpStatus, hint); ok {
			result.Reason = r
			return result
		}
	}

	// 6. Fallback: unknown.
	result.Reason = ReasonUnknown
	return result
}

// ─── Pipeline helpers ───────────────────────────────────────────────────

func classifyProviderSpecific(httpStatus int, msg string) (Reason, bool) {
	// Anthropic thinking block signature invalid (400). Gated on the
	// unique "signature" + "thinking" conjunction so OpenRouter-proxied
	// Anthropic errors (provider != "anthropic") still match.
	if httpStatus == 400 &&
		strings.Contains(msg, "signature") &&
		strings.Contains(msg, "thinking") {
		return ReasonThinkingSignature, true
	}

	// Anthropic long-context tier gate (429 with "extra usage" +
	// "long context").
	if httpStatus == 429 &&
		strings.Contains(msg, "extra usage") &&
		strings.Contains(msg, "long context") {
		return ReasonLongContextTier, true
	}
	return 0, false
}

func classifyByStatus(status int, msg string, hint Hint) (Reason, bool) {
	switch status {
	case 401:
		return ReasonAuth, true
	case 403:
		// OpenRouter 403 "key limit exceeded" is actually billing.
		if strings.Contains(msg, "key limit exceeded") ||
			strings.Contains(msg, "spending limit") {
			return ReasonBilling, true
		}
		return ReasonAuth, true
	case 402:
		return classify402(msg), true
	case 404:
		if matchAny(msg, modelNotFoundPatterns) {
			return ReasonModelNotFound, true
		}
		// Generic 404 without a model-not-found signal is more likely a
		// routing misconfiguration than a missing model. Map to unknown
		// (retryable) so the caller surfaces the real error instead of
		// silently switching models.
		return ReasonUnknown, true
	case 413:
		return ReasonPayloadTooLarge, true
	case 429:
		return ReasonRateLimit, true
	case 400:
		return classify400(msg, hint), true
	case 500, 502:
		return ReasonServerError, true
	case 503, 529:
		return ReasonOverloaded, true
	}

	if status >= 400 && status < 500 {
		return ReasonFormatError, true
	}
	if status >= 500 && status < 600 {
		return ReasonServerError, true
	}
	return 0, false
}

// classify402 disambiguates payment-required: billing exhaustion vs
// transient usage-limit window that will reset.
func classify402(msg string) Reason {
	hasUsage := matchAny(msg, usageLimitPatterns)
	hasTransient := matchAny(msg, usageLimitTransientSignals)
	if hasUsage && hasTransient {
		return ReasonRateLimit
	}
	return ReasonBilling
}

// classify400 narrows 400 Bad Request into context overflow, model-not-found,
// rate-limit, billing, or generic format error. A large session with an
// otherwise-generic 400 body is treated as overflow (Anthropic sometimes
// returns a bare "Error" when context is too large).
func classify400(msg string, hint Hint) Reason {
	if matchAny(msg, contextOverflowPatterns) {
		return ReasonContextOverflow
	}
	if matchAny(msg, modelNotFoundPatterns) {
		return ReasonModelNotFound
	}
	if matchAny(msg, rateLimitPatterns) {
		return ReasonRateLimit
	}
	if matchAny(msg, billingPatterns) {
		return ReasonBilling
	}
	// Generic 400 + large session → probable overflow.
	if isLargeSession(hint) && isGenericMessage(msg) {
		return ReasonContextOverflow
	}
	return ReasonFormatError
}

func classifyByErrorCode(code string) (Reason, bool) {
	lower := strings.ToLower(strings.TrimSpace(code))
	switch lower {
	case "resource_exhausted", "throttled", "rate_limit_exceeded":
		return ReasonRateLimit, true
	case "insufficient_quota", "billing_not_active", "payment_required":
		return ReasonBilling, true
	case "model_not_found", "model_not_available", "invalid_model":
		return ReasonModelNotFound, true
	case "context_length_exceeded", "max_tokens_exceeded":
		return ReasonContextOverflow, true
	case "invalid_api_key":
		return ReasonAuth, true
	}
	return 0, false
}

func classifyByMessage(msg string) (Reason, bool) {
	if msg == "" {
		return 0, false
	}

	// Payload-too-large patterns (embedded in message text when the
	// status code is missing, for example when a proxy strips the
	// response but leaves "Error code: 413" in the body).
	if matchAny(msg, payloadTooLargePatterns) {
		return ReasonPayloadTooLarge, true
	}

	// Explicit billing patterns win outright — they name the billing
	// condition unambiguously.
	if matchAny(msg, billingPatterns) {
		return ReasonBilling, true
	}

	// Explicit rate-limit patterns win before the ambiguous "usage
	// limit" / "limit exceeded" bucket. A bare "rate limit exceeded"
	// technically matches both "rate limit" and "limit exceeded"; the
	// rate-limit intent is clearer and more actionable.
	if matchAny(msg, rateLimitPatterns) {
		return ReasonRateLimit, true
	}

	// Usage-limit needs disambiguation with a transient signal, same
	// as HTTP 402. Only reached when neither explicit billing nor
	// explicit rate-limit patterns matched.
	hasUsage := matchAny(msg, usageLimitPatterns)
	if hasUsage {
		if matchAny(msg, usageLimitTransientSignals) {
			return ReasonRateLimit, true
		}
		return ReasonBilling, true
	}

	if matchAny(msg, contextOverflowPatterns) {
		return ReasonContextOverflow, true
	}
	if matchAny(msg, authPatterns) {
		return ReasonAuth, true
	}
	if matchAny(msg, modelNotFoundPatterns) {
		return ReasonModelNotFound, true
	}
	return 0, false
}

// classifyTransport handles transport-layer errors (timeout, disconnect,
// SSL transient). Called only after HTTP / code / message classification
// has failed to find a match.
func classifyTransport(err error, msg string, httpStatus int, hint Hint) (Reason, bool) {
	// Explicit ctx errors. DeadlineExceeded is a timeout; Canceled is also
	// surfaced as timeout since it is typically the client-side deadline
	// firing on a request-scoped ctx.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ReasonTimeout, true
	}

	// net.Error timeouts (net.DNSError, *net.OpError with .Timeout() = true).
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return ReasonTimeout, true
	}

	// SSL/TLS transient alerts: classify as timeout (retryable transport)
	// even when the wrapping error leaves no structural hint.
	if matchAny(msg, sslTransientPatterns) {
		return ReasonTimeout, true
	}

	// Server disconnect patterns. When there's no HTTP status AND the
	// session is large, the disconnect is most likely the server
	// rejecting an oversized request — route to ReasonContextOverflow so
	// the caller compresses before retry.
	isDisconnect := matchAny(msg, serverDisconnectPatterns)
	if isDisconnect && httpStatus == 0 {
		if isLargeSession(hint) {
			return ReasonContextOverflow, true
		}
		return ReasonTimeout, true
	}

	// Generic timeout type names (including connection-refused, broken
	// pipe, EOF mid-stream). Keep after disconnect so the large-session
	// case is caught first.
	if isTransportError(err, msg) {
		return ReasonTimeout, true
	}
	return 0, false
}

// ─── Extraction helpers ─────────────────────────────────────────────────

// extractBodySignals parses the response body opportunistically. Returns the
// nested error message (if any) and the provider error code.
//
// Handles common shapes:
//   - {"error": {"message": "...", "code": "..." | "type": "..."}}
//   - {"message": "..."}
//   - {"error": {"metadata": {"raw": "{...}"}}} (OpenRouter-wrapped)
func extractBodySignals(body []byte) (msg, code string) {
	if len(body) == 0 {
		return "", ""
	}
	var top map[string]any
	if err := json.Unmarshal(body, &top); err != nil {
		return "", ""
	}

	// Nested error object.
	if obj, ok := top["error"].(map[string]any); ok {
		if m, ok := obj["message"].(string); ok && m != "" {
			msg = m
		}
		if c, ok := obj["code"].(string); ok && c != "" {
			code = c
		} else if t, ok := obj["type"].(string); ok && t != "" {
			code = t
		}
		// OpenRouter wraps upstream errors inside metadata.raw.
		if msg != "" {
			if meta, ok := obj["metadata"].(map[string]any); ok {
				if rawStr, ok := meta["raw"].(string); ok && rawStr != "" {
					if inner := extractInnerMessage(rawStr); inner != "" {
						msg = msg + " " + inner
					}
				}
			}
		}
	}
	// Flat body: {"message": "..."}.
	if msg == "" {
		if m, ok := top["message"].(string); ok && m != "" {
			msg = m
		}
	}
	if code == "" {
		if c, ok := top["code"].(string); ok && c != "" {
			code = c
		} else if c, ok := top["error_code"].(string); ok && c != "" {
			code = c
		}
	}
	return msg, code
}

// extractInnerMessage parses OpenRouter-wrapped raw provider errors.
func extractInnerMessage(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw[0] != '{' {
		return ""
	}
	var inner map[string]any
	if err := json.Unmarshal([]byte(raw), &inner); err != nil {
		return ""
	}
	if obj, ok := inner["error"].(map[string]any); ok {
		if m, ok := obj["message"].(string); ok {
			return m
		}
	}
	if m, ok := inner["message"].(string); ok {
		return m
	}
	return ""
}

// combineMessage builds a single lowercased string combining err.Error() and
// the body message. Used only for pattern matching; the preserved Message on
// Classified comes from the same source but without case folding.
func combineMessage(err error, bodyMsg string) string {
	var raw string
	if err != nil {
		raw = err.Error()
	}
	if bodyMsg != "" && !strings.Contains(raw, bodyMsg) {
		if raw != "" {
			raw = raw + " " + bodyMsg
		} else {
			raw = bodyMsg
		}
	}
	return strings.ToLower(raw)
}

// ─── Small helpers ──────────────────────────────────────────────────────

func matchAny(haystack string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

func isLargeSession(hint Hint) bool {
	if hint.TokenUsagePercent >= LargeSessionPercent {
		return true
	}
	if hint.ApproxTokens >= LargeSessionAbsoluteTokens {
		return true
	}
	return false
}

// isGenericMessage returns true for empty / bare "Error" bodies, which
// providers sometimes emit in place of a structured overflow message.
func isGenericMessage(msg string) bool {
	trimmed := strings.TrimSpace(msg)
	if len(trimmed) < 30 {
		return true
	}
	return trimmed == "error"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// isTransportError reports whether the error is a transport-layer failure
// (connection reset, EOF mid-stream, etc.). Uses message content matching as
// a last resort since SDKs wrap their own error types without preserving the
// chain.
func isTransportError(err error, msg string) bool {
	if err == nil {
		return false
	}
	// net.Error covers DNS, dial, op errors.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	// OSError-style transport messages.
	return matchAny(msg, transportMessagePatterns)
}
