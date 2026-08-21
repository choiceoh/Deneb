package translateops

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
)

const (
	// translateMaxCharsPerBatch is the PRIMARY batch bound — total source chars per
	// DeepL call. ~1200 chars is the researched sweet spot on real article prose;
	// bigger batches add latency and smaller ones add round-trips without quality gain.
	translateMaxCharsPerBatch = 1200
	// translateMaxSegmentsPerBatch caps a batch when segments are short (nav/labels) so a
	// run of tiny strings doesn't pack hundreds into one call. The char bound dominates.
	translateMaxSegmentsPerBatch   = 20
	translateMaxConcurrentBatches  = 3
	defaultTranslateTargetLang     = "Korean"
	translateSegmentEnvelopePrefix = "deneb_translate_segment:v1:"
	translatePartsEnvelopePrefix   = "deneb_translate_parts:v1:"
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
// failure (DeepL error or count mismatch) it splits the range in half and retries
// each half, down to a single segment. So one odd batch self-heals instead of
// leaving a whole span untranslated; only a segment that fails even alone keeps
// its original.
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

// translateBatch translates one batch via DeepL. Browser translation is
// DeepL-only: when DeepL is unconfigured, errors, or returns an unusable
// (count-mismatched) response it reports ok=false and the caller keeps the
// batch's originals — never dropping, merging, or reordering page text.
func translateBatch(ctx context.Context, batch []translateInput, lang string) ([]string, bool) {
	return translateBatchDeepL(ctx, batch, lang)
}
