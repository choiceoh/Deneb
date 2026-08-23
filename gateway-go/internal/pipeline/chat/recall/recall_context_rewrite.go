// recall_context_rewrite.go — multiturn context rewrite for elliptical queries.
//
// Grounding (docs/research/wiki-retrieval-baseline-2026-08-23.md): elliptical
// follow-ups ("그 현장 계약 조건은 어떻게 됐지?") score r@8 18.8% while the same
// questions rewritten self-contained score 100% — retrieval is healthy, the
// missing piece is referent resolution. This file adds the deterministic half:
// when the message carries a deictic marker and too few signal terms to stand
// alone, the previous user turn (transcript tail) is prepended, so the existing
// query assembly, project/counterparty anchors, and ranking all see a
// self-contained text. No LLM call — preflight stays deterministic and inside
// its 1.5s fan-out budget.
//
// Known limitation: the turn snapshot cache keys on the raw message
// (CueFingerprint), so the same elliptical text in two different contexts
// shares a cache slot. Bounded by the cache TTL; revisit if elliptical repeats
// ever show stale evidence in practice.
package recall

import (
	"regexp"
	"strings"
)

// recallDeicticMarkers are message substrings that point at a PRIOR turn's
// subject instead of naming it. Discourse connectives (그리고, 그래서, 그런데)
// are deliberately absent — they continue a topic without anaphora, and the
// current message usually carries its own vocabulary there.
var recallDeicticMarkers = []string{
	"그거", "그것", "그건", "그게", "그때", "그쪽", "거기", "그분",
	"그 사람", "그 현장", "그 사업", "그 프로젝트", "그 회사", "그 담당", "그 날",
	"아까", "저번", "지난번", "예전에", "전에",
}

// transcriptTimestampPrefix matches the "[RFC3339] " prefix formatTurnUserMessage
// bakes into persisted user turns; polaris-bridge turns may lack it.
var transcriptTimestampPrefix = regexp.MustCompile(`^\[[0-9]{4}-[0-9]{2}-[0-9]{2}T[^\]]*\]\s*`)

// recallRewriteMaxPriorRunes bounds the prepended prior turn. The TAIL is kept:
// an elliptical follow-up refers to the end of the prior exchange, not its opening.
const recallRewriteMaxPriorRunes = 120

// needsContextRewrite reports whether the message is too thin to search on its
// own: a deictic marker present AND at most 4 signal terms (the elliptical
// cohort measures ≤4 — sentence-final verb stems like "됐지" count as signal).
// A self-contained message that merely mentions "아까" mid-sentence keeps its
// own vocabulary — its signal terms exceed the gate and no rewrite happens.
func needsContextRewrite(message string) bool {
	if len(recallSignalTerms(message)) > 4 {
		return false
	}
	lower := strings.ToLower(message)
	for _, marker := range recallDeicticMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// lastPriorUserTurn returns the most recent earlier user message of the
// session, or "". The current inbound message (already persisted ahead of
// preflight) is skipped by exact text match; the persisted form carries a
// timestamp prefix, so both raw and prefixed forms are compared.
func lastPriorUserTurn(deps Deps, sessionKey, currentMessage string) string {
	if deps.Transcript == nil || strings.TrimSpace(sessionKey) == "" {
		return ""
	}
	messages, _, err := deps.Transcript.Load(sessionKey, 12)
	if err != nil {
		return ""
	}
	current := strings.TrimSpace(currentMessage)
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		text := strings.TrimSpace(messages[i].TextContent())
		text = stripTranscriptTimestamp(text)
		if text == "" || text == current {
			continue
		}
		return tailRunes(text, recallRewriteMaxPriorRunes)
	}
	return ""
}

func stripTranscriptTimestamp(s string) string {
	return strings.TrimSpace(transcriptTimestampPrefix.ReplaceAllString(s, ""))
}

func tailRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[len(runes)-max:])
}
