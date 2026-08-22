package agent

import (
	"encoding/json"
	"fmt"
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
			truncatedLines, spillID, middleOutline(content, middle, len(head)),
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

// middleOutline renders the section markers found in the discarded middle,
// each with its 1-based line number in the ORIGINAL content so it feeds
// straight into read_spillover(offset=N). Returns "" when the middle has no
// usable structure.
func middleOutline(content, middle string, headLen int) string {
	// Line number of the middle's first line within the whole content.
	base := strings.Count(content[:headLen], "\n") + 1

	var entries []string
	for i, line := range strings.Split(middle, "\n") {
		trimmed := strings.TrimSpace(line)
		if !isOutlineHeading(trimmed) {
			continue
		}
		if len(trimmed) > outlineEntryMaxChars {
			trimmed = textutil.TruncateBytes(trimmed, outlineEntryMaxChars) + "…"
		}
		// Redact before surfacing. The caller hands TruncateHeadTail the RAW
		// output — only the spill file is redacted on its way to disk — so a
		// secret-bearing heading ("# OPENAI_API_KEY=…") in the discarded middle
		// would otherwise be lifted out of the part nobody was going to see and
		// put in front of the provider.
		entries = append(entries, fmt.Sprintf("%d: %s", base+i, redact.String(trimmed)))
		if len(entries) >= outlineMaxEntries {
			entries = append(entries, "…")
			break
		}
	}
	if len(entries) < outlineMinEntries {
		return ""
	}
	return "\n생략 구간 구조 (offset으로 열기): " + strings.Join(entries, " · ")
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
