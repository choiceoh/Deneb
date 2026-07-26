package toolbind

import (
	"context"
	"os"
	"strings"
	"time"
	"unicode"
)

const (
	// Thinking at or above this Hangul share is left alone. Measured over the
	// 766 stored reasoning blocks on 2026-07-26: the median block is 3.8%
	// Hangul and 631 of them (82%) sit under 20%, because the system prompt is
	// 44KB of ~87% English and the model follows the mass of its context, not
	// the language of the user's message. Translating a block that is already
	// Korean spends quota and can only lose fidelity.
	//
	// The bar is deliberately low. Identifiers count as English letters, so a
	// Korean sentence naming one symbol ("이 함수는 `GetAttachment` 를 호출한다")
	// measures only ~0.41 Hangul; a bar near 0.5 would keep re-translating
	// ordinary Korean reasoning about code.
	thinkingHangulBar = 0.30
	// A pathological run must not turn into an unbounded translation bill.
	thinkingMaxBytes = 20000
	// Generous next to the 1.3s median measured on real blocks; this is a
	// deadline for giving up, not a target.
	thinkingTimeout = 15 * time.Second
)

// ThinkingTranslatorEnabled reports whether a thinking translator can work at
// all. Wiring the translator without a key would make every turn log a failure
// for a feature that was never configured, so the composition root asks first
// and leaves the hook nil when the answer is no.
func ThinkingTranslatorEnabled() bool {
	return strings.TrimSpace(os.Getenv("DEEPL_API_KEY")) != ""
}

// hangulRatio reports the share of letters that are Hangul, or 0 when there are
// no letters at all. Code, punctuation, and digits are ignored so a block that
// is Korean prose around English identifiers still reads as Korean.
func hangulRatio(s string) float64 {
	var letters, hangul int
	for _, r := range s {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if unicode.Is(unicode.Hangul, r) {
			hangul++
		}
	}
	if letters == 0 {
		return 0
	}
	return float64(hangul) / float64(letters)
}

// TranslateThinking renders a block of extended-thinking text into Korean.
//
// This is the single owner of the policy — which blocks are worth translating,
// how long to wait, how big is too big — because the same reasoning reaches the
// operator through two unrelated surfaces (the native client's expandable block
// via the SSE done frame, and the 🧠 markdown blockquote on channel delivery).
// Two copies of the rules would drift.
//
// Translation is line-by-line rather than one blob because the blockquote
// renderer turns every newline into a quote marker: preserving the line
// structure exactly is what keeps the rendered block from collapsing. Blank
// lines are never sent — they carry no text, and a batch of empty strings only
// spends quota.
//
// TranslateSegments already defaults each segment to its own original text and
// overwrites only on a clean batch, so a partial failure degrades to "that line
// stays English" instead of losing it.
func TranslateThinking(ctx context.Context, text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || len(trimmed) > thinkingMaxBytes {
		return "", false
	}
	if hangulRatio(trimmed) >= thinkingHangulBar {
		return "", false
	}

	lines := strings.Split(text, "\n")
	idx := make([]int, 0, len(lines))
	segments := make([]string, 0, len(lines))
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		idx = append(idx, i)
		segments = append(segments, line)
	}
	if len(segments) == 0 {
		return "", false
	}

	// The deadline lives here so no caller can forget it: a hung translator
	// must never hold a turn open for a display nicety.
	ctx, cancel := context.WithTimeout(ctx, thinkingTimeout)
	defer cancel()

	translated, err := TranslateSegments(ctx, segments, "Korean")
	if err != nil || len(translated) != len(segments) {
		return "", false
	}
	changed := false
	for n, i := range idx {
		if out := translated[n]; strings.TrimSpace(out) != "" {
			if out != lines[i] {
				changed = true
			}
			lines[i] = out
		}
	}
	// Nothing came back different: report failure so the caller keeps the
	// original rather than claiming a translation that did not happen.
	if !changed {
		return "", false
	}
	return strings.Join(lines, "\n"), true
}
