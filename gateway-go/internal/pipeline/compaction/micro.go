package compaction

import (
	"encoding/json"
	"regexp"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// codeBlockRE matches fenced code blocks (``` ... ```).
var codeBlockRE = regexp.MustCompile("(?s)```[^\n]*\n.*?```")

// MicroCompact strips code fences from tool_result content blocks that are
// older than turnThreshold assistant turns. This is a zero-cost operation
// (no LLM call) that typically saves 30-60% of tool result tokens.
//
// Returns modified messages and count of tool_result blocks that were pruned.
func MicroCompact(messages []llm.Message, turnThreshold int) ([]llm.Message, int) {
	if len(messages) == 0 || turnThreshold <= 0 {
		return messages, 0
	}

	// Find assistant message positions to establish turn boundaries.
	var assistantIdx []int
	for i, m := range messages {
		if m.Role == "assistant" {
			assistantIdx = append(assistantIdx, i)
		}
	}
	if len(assistantIdx) <= turnThreshold {
		return messages, 0
	}

	// Cutoff: everything before the (turnThreshold)th-to-last assistant msg.
	cutoff := assistantIdx[len(assistantIdx)-turnThreshold]

	protected := protectedToolResultIDs(messages)

	pruned := 0
	result := make([]llm.Message, len(messages))
	copy(result, messages)

	for i := 0; i < cutoff; i++ {
		var blocks []llm.ContentBlock
		if err := json.Unmarshal(messages[i].Content.Bytes(), &blocks); err != nil {
			continue
		}

		changed := false
		for j := range blocks {
			if blocks[j].Type != "tool_result" || blocks[j].Content == "" {
				continue
			}
			if protected[blocks[j].ToolUseID] {
				continue // tool schema payload — pruning it forces a re-fetch
			}
			// A spilled result's read_spillover pointer can sit between the
			// head's opening ``` and the tail's closing ```; codeBlockRE would
			// swallow the pointer along with the fence. Skip it — the pointer
			// is preserved (TruncateOldToolResults clears it later, keeping the
			// ref), and the full output stays reachable on disk.
			if spilloverRef(blocks[j].Content) != "" {
				continue
			}
			stripped := stripCodeFences(blocks[j].Content)
			if stripped != blocks[j].Content {
				blocks[j].Content = stripped
				changed = true
				pruned++
			}
		}

		if changed {
			if raw, err := json.Marshal(blocks); err == nil {
				result[i] = llm.Message{Role: messages[i].Role, Content: llm.FlexibleFromRaw(raw)}
			}
		}
	}

	return result, pruned
}

// stripCodeFences removes fenced code blocks, replacing each with a placeholder.
func stripCodeFences(text string) string {
	return codeBlockRE.ReplaceAllString(text, "[code omitted]")
}
