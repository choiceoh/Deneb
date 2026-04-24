// compact_guard.go — Anti-thrashing guard for mid-loop compaction retries.
//
// The mid-loop compaction retry path in runAgentWithFallback used to run
// compact.Compact up to maxCompactionRetries times in a row on the same
// messages slice without any per-attempt guard. Two failure modes fell out
// of this:
//
//  1. **Idempotent compaction** — the first compaction was a no-op because
//     tokens were already ≤ threshold, yet the same error was returned again
//     (e.g., a provider-specific "too long" quirk not matched by our budget
//     math). Each subsequent attempt produced the exact same input, so we
//     were just burning the retry budget.
//
//  2. **Budget-impossible sessions** — the recent-turns protected zone alone
//     (keepRecentTurns assistant turns plus the pre-stripped head) already
//     exceeded the configured budget. Compaction physically can't help:
//     there's nothing in the middle to summarize. The old code happily tried
//     anyway, then surfaced a cryptic "context overflow" error to the user.
//
// This guard runs before each compaction pass:
//
//   - hashes the messages slice; if identical to the prior attempt's input,
//     skip compaction (logged at Warn) — the compaction pipeline proved it
//     can't reduce this state on its own.
//   - if the head + tail protected zones together already exceed budget,
//     bail out with stopReasonCompressionStuck so the caller can surface
//     a user-visible Korean error instead of hitting the LLM again.
//
// Naming mirrors the polaris CircuitBreaker (failure tracking), but the
// scope is different: polaris tracks *summary persistence* failures across
// turns, while compactGuard tracks *in-turn retry thrashing* within a
// single user-facing run.
package chat

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// stopReasonCompressionStuck marks runs aborted because compaction cannot
// reduce the context any further. Surfaced to the user via
// fallbackForStopReason (run_lifecycle.go).
const stopReasonCompressionStuck = "compression_stuck"

// compactionKoreanError is the user-visible Korean message delivered when
// compaction thrashes or is physically impossible. Distinct from the
// generic max_turns / timeout fallbacks because the *recommendation* is
// different: the session cannot recover without /reset.
const compactionKoreanError = "컨텍스트를 더 이상 압축할 수 없습니다. /reset 으로 새 세션을 시작하는 것을 권장합니다."

// hashMessages returns a short stable digest of the messages slice. Used
// to detect compaction idempotency — if the next retry's input hash
// matches the previous attempt's, the cheap-first shrink pipeline
// (TruncateToolCallArgs + StripImageBlocks) plus the LLM summarizer
// could not change anything, so another call will do the same.
func hashMessages(messages []llm.Message) string {
	h := sha256.New()
	for _, m := range messages {
		// Include role so two messages with identical content but different
		// roles don't collide. A single byte separator is enough.
		_, _ = h.Write([]byte(m.Role))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(m.Content)
		_, _ = h.Write([]byte{1})
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// protectedZoneExceedsBudget reports whether the head + tail protected
// zones already exceed the context budget, making compaction useless.
//
// Layout that Polaris/emergency compaction preserves on every call:
//
//	[head (2)]  [middle — candidate for summary]  [tail (keepRecentTurns ~= 6)]
//
// If head+tail is already over budget, summarizing the middle to zero
// bytes still leaves us over. The caller should abort instead of burning
// the retry budget and hitting the LLM again.
//
// budget is the effective token budget (MemoryTokenBudget - SystemPromptBudget).
// A non-positive budget disables the check (returns false) — used by
// boot sessions / subagents where no budget is configured.
func protectedZoneExceedsBudget(messages []llm.Message, budget int) bool {
	if budget <= 0 {
		return false
	}
	// These mirror compact.EmergencyCompact recentKeep=4 and
	// compact.keepRecentTurns=6. We use the larger of the two (6) as the
	// tail bound because LLMCompact preserves at least keepRecentTurns
	// assistant turns — a larger protected zone than emergency's recentKeep.
	const headCount = 2
	const tailCount = 6

	if len(messages) <= headCount+tailCount {
		// Everything is protected — compaction has no room to work. Treat
		// as "stuck" iff the whole slice exceeds budget.
		return messagesExceedBudget(messages, budget)
	}

	head := messages[:headCount]
	tail := messages[len(messages)-tailCount:]

	total := estimateMessagesRoughTokens(head) + estimateMessagesRoughTokens(tail)
	return total > budget
}

// estimateMessagesRoughTokens is a lightweight byte-length-based token
// estimator for the anti-thrashing guard. We intentionally don't call
// tokenest.Estimate (the chat-wide helper) because this path needs to be
// independent of per-script calibration — we're deciding whether to hit
// the LLM again, not sizing a prompt. The 2-byte-per-token heuristic is
// conservative (overshoots for ASCII, undershoots for CJK), which on the
// overshoot side means we abort earlier — the safer failure mode.
func estimateMessagesRoughTokens(messages []llm.Message) int {
	total := 0
	for _, m := range messages {
		total += byteLenToTokens(len(m.Content)) + 4 // +4 role overhead
	}
	return total
}

// byteLenToTokens approximates compaction.EstimateTokens's 2-runes-per-
// token rule using raw byte length.
func byteLenToTokens(nBytes int) int {
	if nBytes <= 0 {
		return 0
	}
	est := nBytes / 2
	if est < 1 {
		return 1
	}
	return est
}

// messagesExceedBudget reports whether the full messages slice exceeds budget.
func messagesExceedBudget(messages []llm.Message, budget int) bool {
	return estimateMessagesRoughTokens(messages) > budget
}
