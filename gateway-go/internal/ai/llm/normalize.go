// normalize.go — Pre-send message normalization for API compliance.
//
// Some LLM providers (notably Anthropic) require strict user/assistant
// message alternation. Mid-loop compaction or restoration can produce
// consecutive same-role messages. This module merges them defensively
// before the API call, keeping the caller's original slice untouched.
//
// Inspired by Claude Code's normalizeMessagesForAPI pattern.
package llm

import "encoding/json"

// NormalizeMessages merges consecutive messages with the same role into a
// single message. Content blocks are concatenated; plain text strings are
// promoted to text blocks first so the merged result is always a block array.
//
// The input slice is not modified — a new slice is returned.
func NormalizeMessages(messages []Message) []Message {
	if len(messages) <= 1 {
		return messages
	}

	// Fast path: scan for any consecutive same-role pair.
	needsMerge := false
	for i := 1; i < len(messages); i++ {
		if messages[i].Role == messages[i-1].Role {
			needsMerge = true
			break
		}
	}
	if !needsMerge {
		return messages
	}

	result := make([]Message, 0, len(messages))
	result = append(result, messages[0])

	for i := 1; i < len(messages); i++ {
		last := &result[len(result)-1]
		if last.Role != messages[i].Role {
			result = append(result, messages[i])
			continue
		}
		// Same role — merge content blocks.
		last.Content = mergeContent(last.Content, messages[i].Content)
	}

	return result
}

// mergeContent combines two json.RawMessage content values into one block
// array. Each value may be a JSON string (plain text) or a JSON array of
// ContentBlock objects.
func mergeContent(a, b json.RawMessage) json.RawMessage {
	blocksA := contentToBlocks(a)
	blocksB := contentToBlocks(b)
	merged := make([]ContentBlock, 0, len(blocksA)+len(blocksB))
	merged = append(merged, blocksA...)
	merged = append(merged, blocksB...)
	raw, _ := json.Marshal(merged)
	return raw
}

// contentToBlocks parses Content into blocks. A plain text string becomes
// a single text block; an array of blocks is returned as-is.
func contentToBlocks(content json.RawMessage) []ContentBlock {
	if len(content) == 0 {
		return nil
	}
	// Try array of blocks first (most common for tool_result messages).
	var blocks []ContentBlock
	if err := json.Unmarshal(content, &blocks); err == nil && len(blocks) > 0 {
		return blocks
	}
	// Plain text string → single text block.
	var text string
	if err := json.Unmarshal(content, &text); err == nil && text != "" {
		return []ContentBlock{{Type: "text", Text: text}}
	}
	return nil
}
