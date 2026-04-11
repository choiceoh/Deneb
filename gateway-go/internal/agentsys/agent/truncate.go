package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
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
	head := content[:half]
	tail := content[len(content)-half:]

	// Count lines in the discarded middle for the marker.
	middle := content[half : len(content)-half]
	truncatedLines := strings.Count(middle, "\n")

	var marker string
	if spillID != "" {
		marker = fmt.Sprintf(
			"\n\n... [%d lines truncated — use read_spillover(%q) for full content] ...\n\n",
			truncatedLines, spillID)
	} else {
		marker = fmt.Sprintf("\n\n... [%d lines truncated] ...\n\n", truncatedLines)
	}

	return head + marker + tail
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
		if err := json.Unmarshal(messages[i].Content, &blocks); err != nil {
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
			blocks[j].Content = TruncateHeadTail(blocks[j].Content, CompactedMaxOutput, "")
			changed = true
			compacted++
		}

		if changed {
			raw, _ := json.Marshal(blocks)
			messages[i].Content = raw
		}
	}
	return compacted
}
