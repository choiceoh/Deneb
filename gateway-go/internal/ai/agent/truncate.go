package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// DefaultMaxOutput is the head/tail truncation budget for tool results.
// When output exceeds this limit the middle is discarded and replaced with
// a truncation marker — Claude Code style.  Both ends are preserved so the
// LLM sees context (paths, invocations) at the top and errors/results at
// the bottom. Spillover stores the full content so the agent can recover
// via read_spillover.
const DefaultMaxOutput = 24 * 1024 // 24K chars

// CompactedMaxOutput is the reduced budget applied to tool results from
// previous turns. The LLM already processed the full result on the turn it
// was produced; subsequent turns only need enough context to remember what
// the tool returned. This dramatically reduces token cost in multi-turn
// agent loops where the full message history is resent every turn.
const CompactedMaxOutput = 4 * 1024 // 4K chars

// TruncateHeadTail preserves the first and last half of content when it
// exceeds maxChars, replacing the middle with a truncation marker.
//
// If spillID is non-empty the marker includes a read_spillover reference
// so the LLM can retrieve the full content on demand.
func TruncateHeadTail(content string, maxChars int, spillID string) string {
	if len(content) <= maxChars {
		return content
	}

	half := maxChars / 2
	head := textutil.TruncateBytes(content, half)
	tail := textutil.TailBytes(content, half)

	// Count lines in the discarded middle for the marker. Bounds follow the
	// rune-safe head/tail so the slice never splits a multi-byte character.
	middle := content[len(head) : len(content)-len(tail)]
	truncatedLines := strings.Count(middle, "\n")

	var marker string
	if spillID != "" {
		// The discarded middle is unaddressable from head+tail alone: an
		// outline of the section markers inside it, with their line numbers,
		// is what lets the model aim read_spillover(offset=…) or a grep
		// instead of paging blind. Only the dropped region is outlined —
		// what survives in head/tail needs no pointer.
		//
		// The read_spillover("sp_…") substring below is parsed by a regex in
		// pipeline/compaction/protected.go; keep its shape intact.
		marker = fmt.Sprintf(
			"\n\n... [%d lines truncated — use read_spillover(%q) for full content] ...%s\n\n",
			truncatedLines, spillID, spilledOutline(content, maxChars),
		)
	} else {
		marker = fmt.Sprintf("\n\n... [%d lines truncated] ...\n\n", truncatedLines)
	}

	return head + marker + tail
}

// Outline bounds. The outline sits inside a truncation marker the model reads
// every turn, so it must stay negligible next to the head/tail halves it
// annotates.
const (
	outlineMinEntries    = 2 // one heading is noise, not a map
	outlineMaxEntries    = 10
	outlineEntryMaxChars = 70
)

// spilledOutline maps the PERSISTED spill file's structure, which is what a
// read_spillover(offset=N) actually opens.
//
// Store writes persistedForm(content), which differs from the raw output twice
// over. Redaction is not line-count preserving — a PEM block spanning thirty
// lines collapses to a single token, so every heading after it sits that many
// lines earlier in the file — and an oversized result is clamped, so a heading
// past the ceiling is not in the file at all. Numbers taken from the raw
// content would point past their heading, or at nothing, silently defeating the
// absolute-offset navigation the outline exists to provide.
//
// It scans the WHOLE file rather than just the region the raw preview drops.
// The preview's head/tail are raw and the offsets are persisted, so the two
// live in different coordinate systems: any attempt to outline "only what the
// preview omits" loses headings that redaction shifted across the boundary —
// absent from the outline and from the preview both. Ranking (below) keeps the
// dropped region's headings first, so scanning wide costs priority, not slots.
//
// The cost is one more pass over the output, paid only when the result spilled.
func spilledOutline(content string, maxChars int) string {
	persisted := persistedForm(content)

	// The region the model cannot see, in PERSISTED coordinates. Approximate by
	// construction — the preview it stands in for is raw — which is exactly why
	// it only ranks entries instead of filtering them.
	half := maxChars / 2
	hidden := [2]int{0, 0}
	if len(persisted) > maxChars {
		head := textutil.TruncateBytes(persisted, half)
		tail := textutil.TailBytes(persisted, half)
		hidden[0] = strings.Count(head, "\n") + 1
		hidden[1] = strings.Count(persisted[:len(persisted)-len(tail)], "\n") + 1
	}
	return outlineOf(persisted, hidden)
}

// outlineOf renders the section markers in text, each with its 1-based line
// number, preferring those inside the hidden line range when there are more
// headings than slots. Returns "" when there is no usable structure.
func outlineOf(text string, hidden [2]int) string {
	type entry struct {
		line      int
		text      string
		hiddenone bool
	}
	var found []entry
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !isOutlineHeading(trimmed) {
			continue
		}
		// Redact before surfacing, and BEFORE the length cap. The entries come
		// from already-redacted text today, so this is belt-and-braces — but it
		// is the guard that keeps a heading like "# OPENAI_API_KEY=…" out of the
		// marker if this is ever called with raw content, which is how the leak
		// arose in the first place: the outline lifts text out of the part
		// nobody was going to see and puts it in front of the provider.
		//
		// Order matters: the patterns are length-anchored (AKIA + exactly 16
		// chars, a JWT's three segments), so cutting the heading first can end
		// a secret one char short of its own pattern — the masker then finds
		// nothing and the surviving prefix ships in the clear. Mask the whole
		// heading, then trim what is already inert.
		trimmed = redact.String(trimmed)
		if len(trimmed) > outlineEntryMaxChars {
			trimmed = textutil.TruncateBytes(trimmed, outlineEntryMaxChars) + "…"
		}
		n := i + 1
		found = append(found, entry{line: n, text: trimmed, hiddenone: n >= hidden[0] && n <= hidden[1]})
	}
	if len(found) < outlineMinEntries {
		return ""
	}

	// Headings the model cannot otherwise see come first; ties keep file order.
	kept := make([]entry, 0, len(found))
	for _, e := range found {
		if e.hiddenone {
			kept = append(kept, e)
		}
	}
	truncated := len(kept) > outlineMaxEntries
	if len(kept) > outlineMaxEntries {
		kept = kept[:outlineMaxEntries]
	}
	for _, e := range found {
		if len(kept) >= outlineMaxEntries {
			truncated = truncated || !e.hiddenone
			continue
		}
		if !e.hiddenone {
			kept = append(kept, e)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].line < kept[j].line })

	parts := make([]string, 0, len(kept)+1)
	for _, e := range kept {
		parts = append(parts, fmt.Sprintf("%d: %s", e.line, e.text))
	}
	if truncated {
		parts = append(parts, "…")
	}
	return "\n스필 구조 (offset으로 열기): " + strings.Join(parts, " · ")
}

// isOutlineHeading recognizes the section markers that actually show up in
// spilled tool output: markdown headings and the `=== x ===` / `--- x ---`
// banners shell tooling prints.
func isOutlineHeading(trimmed string) bool {
	switch {
	case trimmed == "":
		return false
	case strings.HasPrefix(trimmed, "#") && strings.Contains(trimmed, " "):
		return true
	case (strings.HasPrefix(trimmed, "===") && strings.HasSuffix(trimmed, "===")) ||
		(strings.HasPrefix(trimmed, "---") && strings.HasSuffix(trimmed, "---")):
		return len(strings.Trim(trimmed, "=- ")) > 0
	default:
		return false
	}
}

// CompactPriorToolResults shrinks tool_result content blocks in messages from
// completed turns so that subsequent LLM calls carry less baggage. Only
// messages before lastTurnStartIdx are eligible (the current turn's results
// are kept at full size so the LLM can reason about them). Returns the number
// of blocks that were actually compacted.
func CompactPriorToolResults(messages []llm.Message, lastTurnStartIdx int) int {
	compacted := 0
	for i := range messages[:lastTurnStartIdx] {
		if messages[i].Role != "user" {
			continue
		}
		// Try to parse as content blocks (tool result messages are block arrays).
		var blocks []llm.ContentBlock
		if err := json.Unmarshal(messages[i].Content.Bytes(), &blocks); err != nil {
			continue // plain text message, skip
		}

		changed := false
		for j := range blocks {
			if blocks[j].Type != "tool_result" {
				continue
			}
			if len(blocks[j].Content) <= CompactedMaxOutput {
				continue
			}
			blocks[j].Content = RankLines(blocks[j].Content, CompactedMaxOutput)
			changed = true
			compacted++
		}

		if changed {
			raw, _ := json.Marshal(blocks)
			messages[i].Content = llm.FlexibleFromRaw(raw)
		}
	}
	return compacted
}
