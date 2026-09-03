// Package opstranslate renders operator-facing prose into Korean on the way out
// of an RPC, for the self-improvement surfaces the agent writes about itself.
//
// Why this exists: the self-correction queue and the skill lifecycle log are
// written by review models that answer in English. Measured over the live
// ledgers on 2026-09-03 — 1,063 self-correction rows and 735 genesis log rows —
// 78% of candidate titles and 71% of reasons carried no Hangul at all. The
// operator reviewing that queue reads Korean.
//
// Translation happens on the SERVE path, not at write time, for three reasons:
// the ledgers are append-only and must keep exactly what the model said; one
// mechanism then covers the thousands of rows already on disk without a
// backfill; and a translation that turns out wrong can be corrected by changing
// this package instead of rewriting history.
//
// The cost of serving is paid once ever per distinct string, because the cache
// is on disk. That is the whole reason this package owns a cache at all rather
// than leaning on translateops' in-memory LRU: this gateway redeploys several
// times a day, and an in-memory cache means every deploy re-translates the same
// backlog. At DeepL Pro rates a single 60-row list view is ~60k characters, so
// "re-translate after every restart" is a real recurring bill for text that has
// not changed since it was written.
package opstranslate

import (
	"context"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/translateops"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

const (
	// Shared with TranslateThinking's bar — see textutil.HangulRatio. A block at
	// or above this Hangul share is already Korean; translating it spends quota
	// and can only lose fidelity.
	hangulBar = 0.30
	// Fields reach here already truncated to 2000 runes by the caller; this is
	// the backstop so a pathological row cannot become an unbounded bill.
	maxFieldBytes = 8000
	// A display nicety must never hold an RPC open. Originals are served when
	// this expires, and the next request finds the cache warm.
	budget = 6 * time.Second
	// Per-request ceiling on FRESH translations. The rest of a cold backlog
	// serves as originals and warms over the next few requests instead of making
	// one unlucky caller wait for all of it.
	maxFreshPerCall = 120

	// Handed to DeepL as untranslated context. These rows describe an AI agent
	// reviewing and repairing its own skills and code, and the jargon is its own.
	domainContext = "Deneb is an AI agent that reviews and repairs its own skills and source code. " +
		"These are entries from its self-correction queue: a proposed code or skill change, " +
		"the evidence behind it, and a reviewer's verdict. Terms like evolve, held-out validation, " +
		"guardrail, patch-first gate, dispatch and skill lifecycle are names of stages in that pipeline."
	domainRole = "self-improvement review queue entry"
)

// Fields returns Korean renderings of texts, index-aligned with the input.
// A string that is already Korean, is not prose, or could not be translated in
// time comes back unchanged — never empty, never reordered.
//
// Callers decide WHICH fields to pass. Machine evidence (telemetry lines,
// session keys, JSON blobs) is deliberately not filtered out here by guesswork:
// the call site knows what a field means, and a heuristic that silently mangles
// proof text is worse than English proof text.
func Fields(ctx context.Context, texts []string) []string {
	out := make([]string, len(texts))
	copy(out, texts)
	if len(texts) == 0 {
		return out
	}

	// Pass 1 — cache hits and the eligibility decision, no network.
	type pending struct {
		idx  int
		text string
	}
	var misses []pending
	seen := make(map[string]int, len(texts))
	for i, text := range texts {
		if !translatable(text) {
			continue
		}
		if hit, ok := cacheGet(text); ok {
			out[i] = hit
			continue
		}
		// Same string twice in one payload asks once.
		if _, dup := seen[text]; dup {
			continue
		}
		seen[text] = i
		if len(misses) >= maxFreshPerCall {
			continue
		}
		misses = append(misses, pending{idx: i, text: text})
	}
	// The cache is the durable asset, and it is consulted above unconditionally:
	// a gateway with no DeepL key still serves every translation it has already
	// paid for. Only the network step below needs a configured translator.
	if len(misses) == 0 || !translateops.DeepLConfigured() {
		return out
	}

	segments := make([]string, len(misses))
	for i, m := range misses {
		// The domain hint reaches DeepL's `context` parameter and is not itself
		// translated. Without it the review vocabulary lands wrong: "evolve" and
		// "held-out validation" are artifacts of this agent's own skill pipeline,
		// not general English, and a bare sentence gives DeepL nothing to
		// disambiguate them against.
		segments[i] = translateops.Segment(m.text, domainContext, domainRole)
	}
	callCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	translated, err := translateops.TranslateSegments(callCtx, segments, "Korean")
	if err != nil || len(translated) != len(segments) {
		return out // originals stand; the error is the translator's to log
	}

	// Pass 2 — apply, and fan the result out to every index holding that string.
	for i, m := range misses {
		got := strings.TrimSpace(translated[i])
		if got == "" || got == m.text {
			continue // unchanged means the batch failed for it, or it needed nothing
		}
		cachePut(m.text, translated[i])
	}
	for i, text := range texts {
		if out[i] != text {
			continue // already filled from cache
		}
		if hit, ok := cacheGet(text); ok {
			out[i] = hit
		}
	}
	return out
}

// translatable answers "would sending this to DeepL produce something better
// than what we have". It is deliberately conservative: the cost of a wrong yes
// is mangled text on a review screen, the cost of a wrong no is English.
func translatable(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || len(trimmed) > maxFieldBytes {
		return false
	}
	// Already Korean.
	if textutil.HangulRatio(trimmed) >= hangulBar {
		return false
	}
	// A serialized payload, not prose. These appear in the genesis log's
	// `result` field, which stores whole JSON objects.
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return false
	}
	// A single token — an id, a session key, a skill name, a path. Nothing to
	// translate, and DeepL would happily "translate" an identifier.
	if !strings.ContainsAny(trimmed, " \t\n") {
		return false
	}
	// No letters at all (numbers, punctuation, arrows).
	letters := 0
	for _, r := range trimmed {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
			letters++
		}
	}
	return letters >= 8
}
