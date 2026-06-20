package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

const (
	// translateMaxCharsPerBatch is the PRIMARY batch bound — total source chars per LLM
	// call. Auto-researched on real article prose against the live model: ~1200 chars is
	// the sweet spot on all three axes — 100% reliability, fastest wall-clock, and best
	// quality (a direct read scored ~1200-char batches cleaner than even single-segment
	// translations). Bigger batches (≥~1600) overflow translateMaxTokens → the JSON array
	// truncates → parse fail → slow split-retry storms that leave segments untranslated (a
	// 40-segment page took 75s and half-failed at a fixed count of 10); smaller batches add
	// round-trips and over-concurrency without quality gain.
	translateMaxCharsPerBatch = 1200
	// translateMaxSegmentsPerBatch caps a batch when segments are short (nav/labels) so a
	// run of tiny strings doesn't pack hundreds into one call. The char bound dominates.
	translateMaxSegmentsPerBatch = 20
	// translateMaxTokens is the per-batch output cap — headroom for a ~1200-char batch's
	// translated JSON so the array isn't cut off mid-string.
	translateMaxTokens         = 8192
	defaultTranslateTargetLang = "Korean"
)

// TranslateSegments translates web-page text segments to targetLang for the
// in-app browser's in-place translation. The injected DOM walker sends the
// page's text segments (already de-Korean'd client-side) and this returns a
// SAME-LENGTH, SAME-ORDER slice of translations. Source is usually English or
// Russian; a segment already in the target language is passed through.
//
// Count is sacred: text nodes are replaced by index, so on any batch LLM/parse
// error or count mismatch the originals are kept for that batch — translation
// must never drop, merge, or reorder a page's text.
func TranslateSegments(ctx context.Context, segments []string, targetLang string) ([]string, error) {
	if len(segments) == 0 {
		return nil, nil
	}
	lang := strings.TrimSpace(targetLang)
	if lang == "" {
		lang = defaultTranslateTargetLang
	}
	out := make([]string, len(segments))
	copy(out, segments) // safe default: originals, overwritten only on a clean batch
	for start := 0; start < len(segments); {
		end := nextBatchEnd(segments, start)
		translateRange(ctx, segments, out, start, end, lang)
		start = end
	}
	return out, nil
}

// nextBatchEnd grows a batch from start, accumulating segments until adding the next
// would exceed translateMaxCharsPerBatch or translateMaxSegmentsPerBatch — but always
// takes at least one segment, so a lone over-long segment becomes its own batch (which
// translateRange still handles, splitting only if its output truncates).
func nextBatchEnd(segments []string, start int) int {
	end := start + 1
	chars := len(segments[start])
	for end < len(segments) &&
		end-start < translateMaxSegmentsPerBatch &&
		chars+len(segments[end]) <= translateMaxCharsPerBatch {
		chars += len(segments[end])
		end++
	}
	return end
}

// translateRange translates segments[start:end] into out[start:end]. On a batch
// failure (LLM error, bad JSON, or count mismatch — typically an output too long for
// the token budget) it splits the range in half and retries each half, down to a
// single segment. So one oversized/odd batch self-heals instead of leaving a whole
// span untranslated; only a segment that fails even alone keeps its original.
func translateRange(ctx context.Context, segments, out []string, start, end int, lang string) {
	if start >= end {
		return
	}
	if translated, ok := translateBatch(ctx, segments[start:end], lang); ok {
		copy(out[start:end], translated)
		return
	}
	if end-start <= 1 {
		return // single segment failed → keep its original (already in out)
	}
	mid := start + (end-start)/2
	translateRange(ctx, segments, out, start, mid, lang)
	translateRange(ctx, segments, out, mid, end, lang)
}

func translateBatch(ctx context.Context, batch []string, lang string) ([]string, bool) {
	system, user := buildTranslatePrompt(batch, lang)
	raw, err := pilot.CallTranslationLLM(ctx, system, user, translateMaxTokens)
	if err != nil {
		return nil, false
	}
	return parseTranslations(raw, len(batch))
}

func buildTranslatePrompt(segments []string, lang string) (system, user string) {
	system = fmt.Sprintf(`You translate web-page text to %s for an in-app browser.
Rules:
- Translate each input segment to natural %s. Source text is usually English or Russian.
- If a segment is ALREADY in %s, return it unchanged.
- Preserve meaning, tone, numbers, and inline punctuation; never add notes or explanations.
- Never merge or split segments.
- Output ONLY a JSON array of strings — same length and same order as the input. No prose, no markdown.`, lang, lang, lang)
	payload, _ := json.Marshal(segments)
	user = fmt.Sprintf("Translate these %d segments. Return a JSON array of exactly %d strings in the same order:\n%s",
		len(segments), len(segments), string(payload))
	return system, user
}

// parseTranslations reads the model's JSON array and accepts it ONLY when it has
// exactly want items (and optionally an {"translations":[...]} envelope). Any
// mismatch returns ok=false so the caller keeps the originals — see the count
// invariant in TranslateSegments.
func parseTranslations(raw string, want int) ([]string, bool) {
	if arr, err := jsonutil.UnmarshalLLM[[]string](raw); err == nil && len(arr) == want {
		return arr, true
	}
	type envelope struct {
		Translations []string `json:"translations"`
	}
	if obj, err := jsonutil.UnmarshalLLM[envelope](raw); err == nil && len(obj.Translations) == want {
		return obj.Translations, true
	}
	return nil, false
}
