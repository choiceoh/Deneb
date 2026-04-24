// Package llmerr provides a unified classifier for errors returned from LLM
// providers and HTTP clients. It maps provider-specific errors (OpenAI SDK,
// Anthropic SDK, raw HTTP responses, transport errors) to a small enum of
// normalized reasons, each with a recommended recovery action.
//
// Design goals:
//
//   - Zero internal/ dependencies. This package must remain importable from
//     any layer of the codebase (including other pkg/ libraries). It uses only
//     the standard library.
//   - Opportunistic inputs. Callers pass whatever signals they have (error,
//     HTTP status, raw body); unknown fields are tolerated.
//   - Priority-ordered pipeline. Provider-specific patterns first, then HTTP
//     status buckets, then structured error codes, then message heuristics.
//
// The classifier is a Go port of Hermes Agent's
// agent/error_classifier.py, adapted to Go idioms (typed enum, pure func, no
// dataclass mutation).
package llmerr

// Reason is the normalised failure mode for an LLM / provider call.
//
// The zero value is ReasonUnknown — safe to compare against and always
// retryable with backoff.
type Reason int

// The enum mirrors Hermes's FailoverReason. Keep the ordering stable: the
// String method relies on direct int indexing.
const (
	// ReasonUnknown is the fallback when no other category matches.
	// Treated as retryable with exponential backoff.
	ReasonUnknown Reason = iota

	// ReasonAuth is a transient authentication failure (401/403). The
	// caller should refresh the OAuth token or rotate to the next
	// credential before retrying.
	ReasonAuth

	// ReasonAuthPermanent indicates authentication still fails after a
	// refresh/rotation attempt. Abort and surface the error to the user.
	ReasonAuthPermanent

	// ReasonBilling signals credit exhaustion, account deactivation, or
	// similar confirmed billing conditions (402). Rotate credential; do
	// not retry the same key.
	ReasonBilling

	// ReasonRateLimit is transient throttling (429, or 402 with
	// "try again" hint). Rotate credential and/or backoff.
	ReasonRateLimit

	// ReasonOverloaded signals the provider is temporarily overloaded
	// (503/529). Backoff and retry without rotating.
	ReasonOverloaded

	// ReasonServerError is a generic upstream 5xx (500/502). Retry with
	// backoff.
	ReasonServerError

	// ReasonTimeout is a transport-layer timeout or disconnect that is
	// NOT correlated with large-context overflow.
	ReasonTimeout

	// ReasonContextOverflow indicates the request exceeded the model's
	// context window. The caller should compress history before retrying.
	ReasonContextOverflow

	// ReasonPayloadTooLarge corresponds to HTTP 413. Compress the payload
	// (drop attachments, summarize, etc.) and retry.
	ReasonPayloadTooLarge

	// ReasonModelNotFound means the requested model is unknown or no
	// longer served. The caller should pick a different model.
	ReasonModelNotFound

	// ReasonFormatError is a non-retryable 4xx caused by malformed
	// request structure (400 Bad Request without a recognized recovery
	// signal). Retrying the same request is pointless — abort.
	ReasonFormatError

	// ReasonThinkingSignature is an Anthropic-specific 400 where a
	// thinking block's cryptographic signature is invalid or stale. Strip
	// thinking blocks and retry.
	ReasonThinkingSignature

	// ReasonLongContextTier is an Anthropic-specific 429 triggered by the
	// "extra usage" tier gate for long-context requests. Compress and
	// retry.
	ReasonLongContextTier
)

// String returns a stable lowercase identifier for the reason, suitable for
// logging or telemetry. Unknown values render as "unknown".
func (r Reason) String() string {
	switch r {
	case ReasonAuth:
		return "auth"
	case ReasonAuthPermanent:
		return "auth_permanent"
	case ReasonBilling:
		return "billing"
	case ReasonRateLimit:
		return "rate_limit"
	case ReasonOverloaded:
		return "overloaded"
	case ReasonServerError:
		return "server_error"
	case ReasonTimeout:
		return "timeout"
	case ReasonContextOverflow:
		return "context_overflow"
	case ReasonPayloadTooLarge:
		return "payload_too_large"
	case ReasonModelNotFound:
		return "model_not_found"
	case ReasonFormatError:
		return "format_error"
	case ReasonThinkingSignature:
		return "thinking_signature"
	case ReasonLongContextTier:
		return "long_context_tier"
	default:
		return "unknown"
	}
}

// Retryable reports whether the reason may succeed on retry.
//
// Reasons that return false are terminal for the current attempt even if the
// caller rotates credentials — the request as formulated is not going to
// succeed. Reasons that return true may still require preparatory work
// (compression, credential rotation, thinking-block stripping) before the
// retry; see (Reason).DefaultAction.
func (r Reason) Retryable() bool {
	switch r {
	case ReasonAuthPermanent,
		ReasonBilling,
		ReasonFormatError,
		ReasonModelNotFound:
		return false
	default:
		return true
	}
}
