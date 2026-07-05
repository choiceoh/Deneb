package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

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
	translateMaxTokens             = 8192
	translateMaxConcurrentBatches  = 3
	defaultTranslateTargetLang     = "Korean"
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

type translatePromptSegment struct {
	Text    string   `json:"text"`
	Parts   []string `json:"parts,omitempty"`
	Context string   `json:"context,omitempty"`
	Role    string   `json:"role,omitempty"`
}

type translateBatchRange struct {
	Start int
	End   int
}

var translateBatchFn = translateBatch

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
	inputs := normalizeTranslateInputs(segments)
	lang := strings.TrimSpace(targetLang)
	if lang == "" {
		lang = defaultTranslateTargetLang
	}
	out := make([]string, len(segments))
	for i, in := range inputs {
		out[i] = in.Text // safe default: originals, overwritten only on a clean batch
	}
	ranges := translateBatchRanges(inputs)
	if len(ranges) <= 1 {
		for _, r := range ranges {
			translateRange(ctx, inputs, out, r.Start, r.End, lang)
		}
		return out, nil
	}
	translateRangesConcurrently(ctx, inputs, out, ranges, lang)
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

func translateRangesConcurrently(ctx context.Context, inputs []translateInput, out []string, ranges []translateBatchRange, lang string) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, translateMaxConcurrentBatches)
	for _, r := range ranges {
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
			translateRange(ctx, inputs, out, r.Start, r.End, lang)
		}(r)
	}
	wg.Wait()
}

func nextInputBatchEnd(inputs []translateInput, start int) int {
	end := start + 1
	chars := translateInputCost(inputs[start])
	for end < len(inputs) &&
		end-start < translateMaxSegmentsPerBatch &&
		chars+translateInputCost(inputs[end]) <= translateMaxCharsPerBatch {
		chars += translateInputCost(inputs[end])
		end++
	}
	return end
}

func translateInputCost(in translateInput) int {
	cost := len(in.Text)
	if in.Context != "" {
		cost += len(in.Context) / 4
	}
	return cost
}

// translateRange translates segments[start:end] into out[start:end]. On a batch
// failure (LLM error, bad JSON, or count mismatch — typically an output too long for
// the token budget) it splits the range in half and retries each half, down to a
// single segment. So one oversized/odd batch self-heals instead of leaving a whole
// span untranslated; only a segment that fails even alone keeps its original.
func translateRange(ctx context.Context, inputs []translateInput, out []string, start, end int, lang string) {
	if start >= end {
		return
	}
	if translated, ok := translateBatchFn(ctx, inputs[start:end], lang); ok {
		copy(out[start:end], translated)
		return
	}
	if end-start <= 1 {
		return // single segment failed → keep its original (already in out)
	}
	mid := start + (end-start)/2
	translateRange(ctx, inputs, out, start, mid, lang)
	translateRange(ctx, inputs, out, mid, end, lang)
}

func translateBatch(ctx context.Context, batch []translateInput, lang string) ([]string, bool) {
	if translated, ok := translateBatchDeepL(ctx, batch, lang); ok {
		return translated, true
	}
	return translateBatchLLM(ctx, batch, lang)
}

func translateBatchLLM(ctx context.Context, batch []translateInput, lang string) ([]string, bool) {
	system, user := buildTranslatePrompt(batch, lang)
	raw, err := pilot.CallTranslationLLM(ctx, system, user, translateMaxTokens)
	if err != nil {
		return nil, false
	}
	return parseTranslationItems(raw, batch)
}

func buildTranslatePrompt(segments []translateInput, lang string) (system, user string) {
	system = fmt.Sprintf(`You translate web-page text to %s for an in-app browser.
Rules:
- Each input item has either "text" or "parts". Source text is usually English or Russian.
- For a "text" item, return one natural %s string.
- For a "parts" item, return a JSON array of translated strings, same length and same order as "parts"; never merge or split parts.
- Use "context" only to choose the right meaning, pronouns, terminology, and sentence flow for adjacent DOM text in the same block.
- If source text is ALREADY in %s, return it unchanged.
- Preserve meaning, tone, numbers, inline punctuation, and leading/trailing whitespace from each source string; never add notes or explanations.
- Never merge or split segments.
- Output ONLY a top-level JSON array — same length and same order as the input. Each top-level item is a string for "text" or an array of strings for "parts". No prose, no markdown.`, lang, lang, lang)
	payload := make([]translatePromptSegment, len(segments))
	for i, s := range segments {
		payload[i] = translatePromptSegment{
			Text:    s.Text,
			Parts:   s.Parts,
			Context: s.Context,
			Role:    s.Role,
		}
		if len(s.Parts) > 0 {
			payload[i].Text = ""
		}
	}
	payloadJSON, _ := json.Marshal(payload)
	user = fmt.Sprintf("Translate these %d segments. Return a JSON array of exactly %d strings in the same order:\n%s",
		len(segments), len(segments), string(payloadJSON))
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

func parseTranslationItems(raw string, inputs []translateInput) ([]string, bool) {
	if allTextInputs(inputs) {
		return parseTranslations(raw, len(inputs))
	}
	items, ok := parseRawTranslationItems(raw, len(inputs))
	if !ok {
		return nil, false
	}
	out := make([]string, len(items))
	for i, item := range items {
		if len(inputs[i].Parts) == 0 {
			var translated string
			if err := json.Unmarshal(item, &translated); err != nil {
				return nil, false
			}
			out[i] = translated
			continue
		}
		var parts []string
		if err := json.Unmarshal(item, &parts); err != nil || len(parts) != len(inputs[i].Parts) {
			return nil, false
		}
		encoded, err := json.Marshal(parts)
		if err != nil {
			return nil, false
		}
		out[i] = translatePartsEnvelopePrefix + string(encoded)
	}
	return out, true
}

func allTextInputs(inputs []translateInput) bool {
	for _, in := range inputs {
		if len(in.Parts) > 0 {
			return false
		}
	}
	return true
}

func parseRawTranslationItems(raw string, want int) ([]json.RawMessage, bool) {
	if arr, err := jsonutil.UnmarshalLLM[[]json.RawMessage](raw); err == nil && len(arr) == want {
		return arr, true
	}
	type envelope struct {
		Translations []json.RawMessage `json:"translations"`
	}
	if obj, err := jsonutil.UnmarshalLLM[envelope](raw); err == nil && len(obj.Translations) == want {
		return obj.Translations, true
	}
	return nil, false
}
