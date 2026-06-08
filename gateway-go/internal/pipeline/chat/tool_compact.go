package chat

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

// ansiEscapeRe matches ANSI/VT100 CSI escape sequences (color codes, cursor
// moves) that pollute exec/log tool output with bytes of no value to the model.
var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;?]*[\x20-\x2f]*[\x40-\x7e]`)

const (
	// compactMinInputBytes skips compaction for small outputs — the savings
	// don't justify the work, and tiny outputs are usually already clean.
	compactMinInputBytes = 2048
	// compactDedupMinLines only collapses adjacent duplicates once there are
	// enough lines for repetition to matter (progress bars, repeated log lines).
	compactDedupMinLines = 8
)

// CompactToolOutput is a global post-processor that cheaply cleans verbose tool
// output BEFORE it enters the LLM context: it strips ANSI escapes and collapses
// runs of identical adjacent lines. It is lossless for the model (only terminal
// control bytes and exact-duplicate lines are removed) and deterministic, so it
// never breaks the prompt cache.
//
// Inspired by OpenHuman's TokenJuice: once the marketing (HTML→Markdown,
// LLM-summary, URL shortening — all of which live OUTSIDE TokenJuice in that
// project) is stripped away, its real substance is exactly this kind of
// rule-based terminal cleanup with a "keep only if it shrank" guard.
//
// Registered before OutputTrimmer so the 32K cap sees already-cleaned text (more
// real content fits under the cap). Complements compaction, which prunes OLD
// tool results: this trims a result the moment it returns, lowering the token
// baseline so compaction fires less often.
func CompactToolOutput(_ context.Context, _, output string) string {
	if len(output) < compactMinInputBytes {
		return output
	}
	cleaned := ansiEscapeRe.ReplaceAllString(output, "")
	cleaned = dedupeAdjacentLines(cleaned)
	// Pass-through guard: only adopt the cleaned text if it actually shrank,
	// mirroring TokenJuice's "keep only if smaller" rule.
	if len(cleaned) >= len(output) {
		return output
	}
	return cleaned
}

// dedupeAdjacentLines collapses runs of identical adjacent lines into one. A
// non-blank run gets a " (×N)" marker so the model still knows repetition
// occurred; a blank run collapses silently to a single blank line. Adjacent-
// only (a non-consecutive repeat survives) — cheap and safe, matching
// TokenJuice's dedupe_adjacent. Operates on whole lines, so it is rune-safe.
func dedupeAdjacentLines(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) < compactDedupMinLines {
		return s
	}
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		j := i + 1
		for j < len(lines) && lines[j] == lines[i] {
			j++
		}
		run := j - i
		if run > 1 && strings.TrimSpace(lines[i]) != "" {
			out = append(out, lines[i]+" (×"+strconv.Itoa(run)+")")
		} else {
			// Singletons and blank-line runs: keep one line as-is.
			out = append(out, lines[i])
		}
		i = j
	}
	return strings.Join(out, "\n")
}
