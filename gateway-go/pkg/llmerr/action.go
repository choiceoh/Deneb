package llmerr

import "time"

// Action is the recommended recovery step for a given Reason. The zero
// value means "no action" — caller should surface the error to the user.
//
// Multiple flags may be set simultaneously. For example a ReasonRateLimit
// action has both Rotate = true and Backoff > 0 so the caller first tries
// the next credential after waiting.
type Action struct {
	// Rotate signals the caller should attempt the next credential in
	// the pool before retrying with the current one.
	Rotate bool

	// Backoff is the duration to wait before retrying. Zero means no
	// wait is required (caller can retry immediately).
	Backoff time.Duration

	// Compress asks the caller to shrink the conversation (drop images,
	// summarise older turns, strip tool-call arguments) before retrying.
	Compress bool

	// Refresh asks the caller to refresh the current credential's
	// short-lived token (for example, re-run OAuth). Distinct from
	// Rotate, which moves to a different credential entirely.
	Refresh bool

	// StripThink tells the caller to drop thinking blocks from the
	// message history before retrying (Anthropic signature errors).
	StripThink bool

	// Abort signals the caller should NOT retry. Surface the error.
	Abort bool

	// RetryOnce signals a single immediate retry is warranted, after
	// which the caller should treat the failure as terminal.
	RetryOnce bool
}

// Backoff schedule for retryable reasons. Exponential with a 30-second cap;
// the first attempt waits BaseBackoff, subsequent attempts double. Attempts
// are 1-indexed; attempt <= 1 returns BaseBackoff.
const (
	// BaseBackoff is the minimum wait before the first retry.
	BaseBackoff = 500 * time.Millisecond

	// MaxBackoff caps the exponential schedule so long-running
	// retry loops don't stall for minutes.
	MaxBackoff = 30 * time.Second
)

// backoffFor returns an exponential backoff for the given 1-indexed attempt.
// Callers should add jitter where appropriate — this function is
// deterministic so tests can assert exact durations.
func backoffFor(attempt int) time.Duration {
	if attempt <= 1 {
		return BaseBackoff
	}
	// Shift BaseBackoff left by (attempt-1). Cap at MaxBackoff. Guard
	// against overflow for absurdly large attempt counts.
	shift := attempt - 1
	if shift >= 16 {
		return MaxBackoff
	}
	d := BaseBackoff << shift
	if d > MaxBackoff || d < 0 {
		return MaxBackoff
	}
	return d
}

// DefaultAction returns the recommended recovery for this reason at the
// given 1-indexed attempt number. The attempt parameter controls the
// backoff schedule; zero or negative values are treated as attempt 1.
//
// The mapping mirrors Hermes's implied action matrix:
//
//	auth                -> refresh / rotate
//	auth_permanent      -> abort
//	billing             -> rotate
//	rate_limit          -> rotate + backoff
//	overloaded          -> backoff + retry
//	server_error        -> backoff + retry
//	timeout             -> backoff + retry (single immediate retry for small N)
//	context_overflow    -> compress
//	payload_too_large   -> compress
//	model_not_found     -> abort (caller must pick a new model)
//	format_error        -> abort
//	thinking_signature  -> strip + retry
//	long_context_tier   -> compress + backoff
//	unknown             -> backoff + retry
func (r Reason) DefaultAction(attempt int) Action {
	if attempt < 1 {
		attempt = 1
	}
	switch r {
	case ReasonAuth:
		return Action{Rotate: true, Refresh: true, RetryOnce: true}
	case ReasonAuthPermanent:
		return Action{Abort: true}
	case ReasonBilling:
		return Action{Rotate: true, Abort: true}
	case ReasonRateLimit:
		return Action{Rotate: true, Backoff: backoffFor(attempt)}
	case ReasonOverloaded:
		return Action{Backoff: backoffFor(attempt)}
	case ReasonServerError:
		return Action{Backoff: backoffFor(attempt)}
	case ReasonTimeout:
		return Action{Backoff: backoffFor(attempt)}
	case ReasonContextOverflow:
		return Action{Compress: true}
	case ReasonPayloadTooLarge:
		return Action{Compress: true}
	case ReasonModelNotFound:
		return Action{Abort: true}
	case ReasonFormatError:
		return Action{Abort: true}
	case ReasonThinkingSignature:
		return Action{StripThink: true, RetryOnce: true}
	case ReasonLongContextTier:
		return Action{Compress: true, Backoff: backoffFor(attempt)}
	default:
		return Action{Backoff: backoffFor(attempt)}
	}
}
