package translateops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	// translateMaxCharsPerBatch is the PRIMARY batch bound — total source chars per
	// DeepL call.
	//
	// It used to be 1200 on the belief that "bigger batches add latency". Measured
	// 2026-09-03 against api.deepl.com with 120 real segments (10,392 chars) from a
	// topwar.ru article: per-call latency is FLAT in payload size — median 1,004ms
	// at the 1200-char shape vs 1,054ms at 3000 — so the wall time is round-trip
	// COUNT, not bytes. Same segments, same concurrency 3:
	//     20 texts / 1200 chars → 12 calls → 4,395ms
	//     50 texts / 3000 chars →  5 calls → 2,593ms   (−41%)
	// DeepL translates each text independently (the `context` hint is separate and
	// capped), so batch size does not touch translation quality.
	translateMaxCharsPerBatch = 3000
	// translateMaxSegmentsPerBatch caps a batch when segments are short (nav/labels) so a
	// run of tiny strings doesn't pack hundreds into one call. On a front page the
	// segments are ~24 chars, so THIS bound decides there — 20 was leaving the
	// DeepL request two-thirds empty. 50 is DeepL's own per-request text limit.
	translateMaxSegmentsPerBatch = 50
	// One RPC's batches run together, so this decides how many WAVES a page costs.
	// Measured 2026-09-03 by replaying a real topwar.ru article through the client
	// walker with the server's batching simulated: the "rest" tier ships one RPC of
	// 27 segments / 12,107 chars → 5 batches. At 3 that is two waves and the page
	// finishes at 3,244ms; at 6 it is one wave and 2,083ms (−36%).
	// Safe to raise only because callDeepL now retries a 429 with Retry-After —
	// before that, extra concurrency turned a rate limit into untranslated text.
	translateMaxConcurrentBatches = 6
	defaultTranslateTargetLang    = "Korean"
	// The U+E000 sentinel is written as an ESCAPE on purpose. It is a private-use
	// rune, so a literal one renders as nothing in terminals, grep, sed and git
	// diffs — a 2026-08-30 investigation read its absence from `sed` output and
	// from a diff whose minus side was escaped, concluded the protocol had been
	// broken for seven weeks, and nearly "fixed" it by prepending a second
	// sentinel (which would have broken it for real). Keep the escape so the
	// rune is visible in every tool. The client spells it the same way
	// (deneb-translate.js SEGMENT_PAYLOAD_PREFIX / PARTS_RESULT_PREFIX); the
	// cross-language contract test pins the two to the same bytes.
	translateSegmentEnvelopePrefix = "\ue000deneb_translate_segment:v1:"
	translatePartsEnvelopePrefix   = "\ue000deneb_translate_parts:v1:"
)

type translateInput struct {
	Text    string
	Parts   []string
	Context string
	Role    string
}

type translateSegmentEnvelope struct {
	Text    string   `json:"text"`
	Parts   []string `json:"parts,omitempty"`
	Context string   `json:"context,omitempty"`
	Role    string   `json:"role,omitempty"`
}

type translateBatchRange struct {
	Start int
	End   int
}

// translateBatchOutcome distinguishes "this BATCH failed" from "the TRANSLATOR
// is refusing". translateRange answers a failure by splitting the range and
// retrying each half, which is right for a batch DeepL choked on — and exactly
// wrong when the answer will be identical every time. A 40-segment range that
// hits an auth error, an exhausted quota (456) or a missing key used to cost
// 2n-1 = 79 calls to learn the same thing 79 times, and with the 429 retry that
// landed in #5010 each of those leaves could sleep 3s first.
type translateBatchOutcome int

const (
	batchOK translateBatchOutcome = iota
	// batchRetryable: this batch is bad (too large, a segment DeepL rejects, a
	// transient error that outlived its retries). A smaller range may succeed.
	batchRetryable
	// batchHopeless: the translator itself is unusable — no key, no endpoint, an
	// unsupported target language, or a status DeepL will repeat verbatim.
	// Splitting cannot help; stop.
	batchHopeless
)

var translateBatchFn = translateBatch

// TranslateSegments translates web-page text segments to targetLang for the
// in-app browser's in-place translation. The injected DOM walker sends the
// page's text segments (already de-Korean'd client-side) and this returns a
// SAME-LENGTH, SAME-ORDER slice of translations. Source is usually English or
// Russian; a segment already in the target language is passed through.
//
// Count is sacred: text nodes are replaced by index, so on any batch error or
// count mismatch the originals are kept for that batch — translation must never
// drop, merge, or reorder a page's text.
func TranslateSegments(ctx context.Context, segments []string, targetLang string) ([]string, error) {
	if len(segments) == 0 {
		return nil, nil
	}
	inputs := normalizeTranslateInputs(segments)
	lang := strings.TrimSpace(targetLang)
	if lang == "" {
		lang = defaultTranslateTargetLang
	}
	out := make([]string, len(segments))
	for i, in := range inputs {
		out[i] = in.Text // safe default: originals, overwritten only on a clean batch
	}
	var st translateRangeState
	ranges := translateBatchRanges(inputs)
	if len(ranges) <= 1 {
		for _, r := range ranges {
			translateRange(ctx, inputs, out, &st, r.Start, r.End, lang)
		}
	} else {
		translateRangesConcurrently(ctx, inputs, out, ranges, lang, &st)
	}
	// Nothing came back at all — no API key, quota exhausted, DeepL down, or the
	// context died. Saying "here are your segments" with a nil error is a LIE the
	// callers cannot see through, and it is an expensive one: the in-app browser
	// caches what it is handed, so a total failure writes `original → original`
	// into that device's site and global stores and those segments then never ask
	// again. One outage silently and permanently stops translating them.
	//
	// A partial failure still returns originals for the batches that failed —
	// that is the deliberate never-drop-page-text contract, and translateRange's
	// bisection makes it rare and narrow. This guard is for the systemic case.
	if st.done.Load() == 0 {
		return out, fmt.Errorf("translate: all %d segments failed (translator unavailable or rejecting)", len(inputs))
	}
	return out, nil
}

func normalizeTranslateInputs(segments []string) []translateInput {
	inputs := make([]translateInput, len(segments))
	for i, raw := range segments {
		inputs[i] = parseTranslateInput(raw)
	}
	return inputs
}

func parseTranslateInput(raw string) translateInput {
	if !strings.HasPrefix(raw, translateSegmentEnvelopePrefix) {
		return translateInput{Text: raw}
	}
	var env translateSegmentEnvelope
	if err := json.Unmarshal([]byte(strings.TrimPrefix(raw, translateSegmentEnvelopePrefix)), &env); err != nil {
		return translateInput{Text: raw}
	}
	parts := nonEmptyParts(env.Parts)
	if len(parts) > 0 {
		return translateInput{
			Text:    strings.Join(parts, ""),
			Parts:   parts,
			Context: strings.TrimSpace(env.Context),
			Role:    strings.TrimSpace(env.Role),
		}
	}
	if env.Text == "" {
		return translateInput{Text: raw}
	}
	return translateInput{
		Text:    env.Text,
		Context: strings.TrimSpace(env.Context),
		Role:    strings.TrimSpace(env.Role),
	}
}

func nonEmptyParts(parts []string) []string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func translateBatchRanges(inputs []translateInput) []translateBatchRange {
	ranges := make([]translateBatchRange, 0, (len(inputs)/translateMaxSegmentsPerBatch)+1)
	for start := 0; start < len(inputs); {
		end := nextInputBatchEnd(inputs, start)
		ranges = append(ranges, translateBatchRange{Start: start, End: end})
		start = end
	}
	return ranges
}

func translateRangesConcurrently(ctx context.Context, inputs []translateInput, out []string, ranges []translateBatchRange, lang string, st *translateRangeState) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, translateMaxConcurrentBatches)
	for _, r := range ranges {
		if st.hopeless.Load() {
			break // the translator is refusing; do not queue the rest
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		wg.Add(1)
		go func(r translateBatchRange) {
			defer wg.Done()
			defer func() { <-sem }()
			translateRange(ctx, inputs, out, st, r.Start, r.End, lang)
		}(r)
	}
	wg.Wait()
}

func nextInputBatchEnd(inputs []translateInput, start int) int {
	end := start + 1
	chars := translateInputCost(inputs[start])
	texts := translateInputTexts(inputs[start])
	for end < len(inputs) &&
		end-start < translateMaxSegmentsPerBatch &&
		chars+translateInputCost(inputs[end]) <= translateMaxCharsPerBatch &&
		texts+translateInputTexts(inputs[end]) <= maxDeepLTextsPerRequest {
		chars += translateInputCost(inputs[end])
		texts += translateInputTexts(inputs[end])
		end++
	}
	return end
}

// translateInputTexts counts the DeepL `text` fields one input will flatten into
// (see flattenDeepLInputs): a segment with parts contributes one per non-blank
// part, not one per segment. The batcher bounds this because translateBatchDeepL
// REJECTS a batch that flattens past maxDeepLTextsPerRequest instead of chunking
// it — translateRange then splits and retries, so every such batch costs a wasted
// round trip. Now the rejection is unreachable by construction.
func translateInputTexts(in translateInput) int {
	if len(in.Parts) == 0 {
		if strings.TrimSpace(in.Text) == "" {
			return 0
		}
		return 1
	}
	n := 0
	for _, part := range in.Parts {
		if strings.TrimSpace(part) != "" {
			n++
		}
	}
	return n
}

func translateInputCost(in translateInput) int {
	cost := len(in.Text)
	if in.Context != "" {
		cost += len(in.Context) / 4
	}
	return cost
}

// translateRange translates segments[start:end] into out[start:end]. On a batch
// failure (DeepL error or count mismatch) it splits the range in half and retries
// each half, down to a single segment. So one odd batch self-heals instead of
// leaving a whole span untranslated; only a segment that fails even alone keeps
// its original.
func translateRange(ctx context.Context, inputs []translateInput, out []string, st *translateRangeState, start, end int, lang string) {
	if start >= end || st.hopeless.Load() {
		return
	}
	translated, outcome := translateBatchFn(ctx, inputs[start:end], lang)
	switch outcome {
	case batchOK:
		copy(out[start:end], translated)
		st.done.Add(int64(end - start))
		return
	case batchHopeless:
		// The translator, not this batch. Splitting asks the same question again.
		// Latch it so the sibling ranges already in flight stop too.
		st.hopeless.Store(true)
		return
	case batchRetryable:
		// This batch is what DeepL choked on — a smaller one may go through.
		// Handled by the split below.
	}
	if end-start <= 1 {
		return // single segment failed → keep its original (already in out)
	}
	mid := start + (end-start)/2
	translateRange(ctx, inputs, out, st, start, mid, lang)
	translateRange(ctx, inputs, out, st, mid, end, lang)
}

// translateRangeState is shared by every range of one TranslateSegments call:
// how many segments actually came back, and whether the translator itself has
// given up.
type translateRangeState struct {
	done     atomic.Int64
	hopeless atomic.Bool
}

// translateBatch translates one batch via DeepL. Browser translation is
// DeepL-only: when DeepL is unconfigured, errors, or returns an unusable
// (count-mismatched) response it reports ok=false and the caller keeps the
// batch's originals — never dropping, merging, or reordering page text.
func translateBatch(ctx context.Context, batch []translateInput, lang string) ([]string, translateBatchOutcome) {
	return translateBatchDeepL(ctx, batch, lang)
}

// Segment wraps one text in the envelope TranslateSegments understands, so a
// caller can hand DeepL the surrounding meaning without it being translated.
//
// role and context reach DeepL's `context` parameter, which disambiguates
// vocabulary the sentence alone cannot: the same word is "평가" in one domain
// and "심사" in another. Both are optional — an empty envelope is equivalent to
// passing the bare text.
func Segment(text, context, role string) string {
	if strings.TrimSpace(context) == "" && strings.TrimSpace(role) == "" {
		return text
	}
	blob, err := json.Marshal(translateSegmentEnvelope{Text: text, Context: context, Role: role})
	if err != nil {
		return text
	}
	return translateSegmentEnvelopePrefix + string(blob)
}
