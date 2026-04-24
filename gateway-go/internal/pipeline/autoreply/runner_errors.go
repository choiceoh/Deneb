package autoreply

import (
	"errors"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/llmerr"
)

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
//
// Classification first delegates to pkg/llmerr so the autoreply runner
// shares one taxonomy with chat.isContextOverflow and
// chat.isTransientLLMError. The local AgentErrorKind enum is preserved
// because it has two categories — AgentErrorRoleOrdering and
// AgentErrorCompaction — that are peculiar to the autoreply session-reset
// flow and are not modelled in llmerr. Those two are detected inline
// before we hand the message to llmerr.
//
// A handful of legacy substring checks (bare HTTP status codes like "502"
// embedded in free-form messages, and the loose "billing"/"payment"
// keywords) are retained as a fallback because llmerr requires either a
// structured status/code or a specific provider phrase. Without the
// fallback, error strings like "openai: 502 bad gateway" would be
// reclassified from AgentErrorServerDown to AgentErrorUnknown, which
// changes retry behavior.
func ClassifyAgentError(msg string) AgentErrorKind {
	lower := strings.ToLower(msg)

	// Role-ordering and compaction failures are autoreply-specific signals
	// that trigger a session reset. They are not HTTP errors, so llmerr
	// has no category for them — handle them up front.
	if strings.Contains(lower, "roles must alternate") || strings.Contains(lower, "incorrect role") {
		return AgentErrorRoleOrdering
	}
	if strings.Contains(lower, "compaction") && (strings.Contains(lower, "fail") || strings.Contains(lower, "error")) {
		return AgentErrorCompaction
	}

	// Delegate the rest to llmerr. We synthesise an error from msg since
	// the legacy API takes only a string; llmerr.Classify tolerates this.
	reason := llmerr.Classify(errors.New(msg), 0, nil).Reason
	switch reason {
	case llmerr.ReasonContextOverflow, llmerr.ReasonPayloadTooLarge, llmerr.ReasonLongContextTier:
		return AgentErrorContextOverflow
	case llmerr.ReasonAuth, llmerr.ReasonAuthPermanent:
		return AgentErrorAuth
	case llmerr.ReasonBilling:
		return AgentErrorBilling
	case llmerr.ReasonRateLimit:
		return AgentErrorRateLimit
	case llmerr.ReasonServerError, llmerr.ReasonOverloaded:
		return AgentErrorServerDown
	case llmerr.ReasonUnknown,
		llmerr.ReasonTimeout,
		llmerr.ReasonModelNotFound,
		llmerr.ReasonFormatError,
		llmerr.ReasonThinkingSignature:
		// Fall through to the legacy substring fallback below. These
		// llmerr categories have no direct AgentErrorKind equivalent
		// (autoreply never needed a distinct Timeout/ModelNotFound/
		// FormatError/ThinkingSignature bucket), so the caller gets
		// AgentErrorUnknown by default unless the legacy patterns match.
	}

	// Legacy fallback — preserved so we do not silently reclassify error
	// messages that contain a bare HTTP status or the words "billing" /
	// "payment" without a structured body. llmerr deliberately avoids
	// matching these because bare digits produce false positives on
	// structured inputs; in the autoreply path we only see free-form
	// strings so the risk is bounded.
	if strings.Contains(lower, "billing") || strings.Contains(lower, "payment") {
		return AgentErrorBilling
	}
	if strings.Contains(msg, "429") {
		return AgentErrorRateLimit
	}
	for _, code := range []string{"502", "503", "521", "529"} {
		if strings.Contains(msg, code) {
			return AgentErrorServerDown
		}
	}
	if strings.Contains(msg, "401") {
		return AgentErrorAuth
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
