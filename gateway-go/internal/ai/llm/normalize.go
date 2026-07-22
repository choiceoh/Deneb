// normalize.go — Pre-send message normalization for API compliance.
//
// Some LLM providers (notably Anthropic) require strict user/assistant
// message alternation. Mid-loop compaction or restoration can produce
// consecutive same-role messages. This module merges them defensively
// before the API call, keeping the caller's original slice untouched.
//
// Inspired by Claude Code's normalizeMessagesForAPI pattern.
package llm

import (
	"encoding/json"
	"strings"
)

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

// RepairToolPairing heals tool_use/tool_result pairing that history pruning or
// compaction can break. Strict backends reject both defects (observed live:
// kimi 400 "tool_call_ids did not have response messages" on a 151-message
// session, which silently demoted every main-role turn to the fallback model):
//
//   - forward orphan — an assistant tool_use whose result message was pruned:
//     a synthetic tool_result ("unavailable") is inserted right after the
//     assistant message, preserving what the model said.
//   - reverse orphan — a tool_result whose originating tool_use was pruned:
//     the block is dropped (a result may not precede or float without its
//     use); a message left empty is removed by DropEmptyMessages.
//
// Run it BEFORE NormalizeMessages so an inserted user message merges into an
// adjacent one. The input slice is not modified.
func RepairToolPairing(messages []Message) []Message {
	// Pass 1: index each tool_use by id, and mark ids whose result appears in
	// a LATER message (the only placement strict backends accept).
	useAt := make(map[string]int)
	resolved := make(map[string]bool)
	reverseOrphans := false
	for i := range messages {
		for _, b := range ContentToBlocks(messages[i].Content) {
			switch b.Type {
			case "tool_use":
				if b.ID != "" {
					useAt[b.ID] = i
				}
			case "tool_result":
				if at, ok := useAt[b.ToolUseID]; ok && at < i {
					resolved[b.ToolUseID] = true
				} else {
					reverseOrphans = true
				}
			}
		}
	}
	forwardOrphans := len(resolved) < len(useAt)
	if !forwardOrphans && !reverseOrphans {
		return messages
	}

	out := make([]Message, 0, len(messages)+1)
	for i := range messages {
		msg := messages[i]
		blocks := ContentToBlocks(msg.Content)
		if reverseOrphans {
			kept := make([]ContentBlock, 0, len(blocks))
			for _, b := range blocks {
				if b.Type == "tool_result" {
					if at, ok := useAt[b.ToolUseID]; !ok || at >= i {
						continue // orphaned result: its tool_use is gone
					}
				}
				kept = append(kept, b)
			}
			if len(kept) != len(blocks) {
				if len(kept) == 0 {
					// Stripped to nothing: drop the message outright — the
					// OpenAI path has no DropEmptyMessages pass, and an empty
					// message is its own rejection risk.
					continue
				}
				blocks = kept
				msg.Content = marshalBlocks(kept)
			}
		}
		out = append(out, msg)
		// Synthesize results for this message's unresolved tool_use ids so the
		// pair is complete where the backend expects it: immediately after.
		var synth []ContentBlock
		for _, b := range blocks {
			if b.Type == "tool_use" && b.ID != "" && !resolved[b.ID] && useAt[b.ID] == i {
				synth = append(synth, ContentBlock{
					Type:      "tool_result",
					ToolUseID: b.ID,
					Content:   "[tool result unavailable: pruned from history]",
					IsError:   true,
				})
			}
		}
		if len(synth) > 0 {
			out = append(out, Message{Role: "user", Content: marshalBlocks(synth)})
		}
	}
	return out
}

// OrderToolResultsFirst rewrites any USER message whose tool_result blocks
// appear after other block types so the results come first (their relative
// order preserved, then everything else).
//
// Why: Anthropic requires tool_result blocks at the START of the user
// message. The shape [text, tool_result, …] arises when the user steers
// mid-tool-loop — the typed message persists between the assistant's
// tool_use and its results, and NormalizeMessages then merges the two user
// messages text-first. Real Anthropic rejects that outright; kimi's coding
// endpoint is worse — its translator silently derails and 400s blaming
// unrelated LATER calls ("tool_call_ids did not have response messages:
// web:24, web:25" while messages 111/113 were perfectly paired; live-bisected
// 2026-07-17, the poison was one [text, result, result] user message at 76,
// and reordering exactly that message made the identical request return 200).
//
// Already-ordered messages pass through byte-identical (cache-stable); the
// input slice is not modified. Run AFTER NormalizeMessages — the merge is
// what creates the bad ordering.
func OrderToolResultsFirst(messages []Message) []Message {
	needs := false
	for i := range messages {
		if messages[i].Role == "user" && needsToolResultReorder(ContentToBlocks(messages[i].Content)) {
			needs = true
			break
		}
	}
	if !needs {
		return messages
	}
	out := make([]Message, len(messages))
	copy(out, messages)
	for i := range out {
		if out[i].Role != "user" {
			continue
		}
		blocks := ContentToBlocks(out[i].Content)
		if !needsToolResultReorder(blocks) {
			continue
		}
		results := make([]ContentBlock, 0, len(blocks))
		rest := make([]ContentBlock, 0, len(blocks))
		for _, b := range blocks {
			if b.Type == "tool_result" {
				results = append(results, b)
			} else {
				rest = append(rest, b)
			}
		}
		out[i].Content = marshalBlocks(append(results, rest...))
	}
	return out
}

// needsToolResultReorder reports whether a tool_result block appears after any
// non-tool_result block.
func needsToolResultReorder(blocks []ContentBlock) bool {
	seenOther := false
	for _, b := range blocks {
		if b.Type == "tool_result" {
			if seenOther {
				return true
			}
			continue
		}
		seenOther = true
	}
	return false
}

// DropEmptyMessages removes messages that carry no usable content — no
// non-blank text and no structural block (tool_use, tool_result, image,
// thinking). Anthropic rejects such messages ("... must not be empty"); they
// are stall or compaction artifacts (e.g. a turn that timed out with zero
// output) and carry no information. Run it before NormalizeMessages so any
// adjacency the drop creates is merged away. The input slice is not modified.
func DropEmptyMessages(messages []Message) []Message {
	hasEmpty := false
	for i := range messages {
		if isContentEmpty(messages[i].Content) {
			hasEmpty = true
			break
		}
	}
	if !hasEmpty {
		return messages
	}
	result := make([]Message, 0, len(messages))
	for _, m := range messages {
		if !isContentEmpty(m.Content) {
			result = append(result, m)
		}
	}
	return result
}

// isContentEmpty reports whether a message's content has no payload that would
// survive to the Anthropic wire: no non-blank text, no tool_use / tool_result /
// image. A single empty text block (what sanitizeAnthropicContent emits for null
// content) counts as empty.
//
// Thinking blocks are judged by their wire field (`thinking`), not `text`: some
// persisted history stores reasoning in a thinking block's `text` field, which
// marshalAnthropicBlocks drops (it serializes `thinking`). Such a block reaches
// Anthropic empty, so a message made only of those is empty for our purposes and
// must be dropped — otherwise Anthropic rejects it ("... must not be empty").
func isContentEmpty(content FlexibleJSON) bool {
	for _, b := range ContentToBlocks(content) {
		switch b.Type {
		case "", "text":
			if strings.TrimSpace(b.Text) != "" {
				return false
			}
		case "thinking":
			if strings.TrimSpace(b.Thinking) != "" {
				return false
			}
		default:
			return false // tool_use / tool_result / image — meaningful
		}
	}
	return true
}

// mergeContent combines two FlexibleJSON content values into one block
// array. Each value may be a JSON string (plain text) or a JSON array of
// ContentBlock objects.
func mergeContent(a, b FlexibleJSON) FlexibleJSON {
	blocksA := ContentToBlocks(a)
	blocksB := ContentToBlocks(b)
	merged := append([]ContentBlock{}, blocksA...)
	merged = append(merged, blocksB...)
	// marshalBlocks (not a bare json.Marshal) so a block with a non-JSON
	// Input fragment can never collapse two real messages into one with
	// empty (0-byte) Content.
	return marshalBlocks(merged)
}

// ContentToBlocks parses message Content into blocks. A plain text string
// becomes a single text block; an array of blocks is returned as-is. The
// canonical content parser — reuse it instead of hand-rolling the
// "is it a block array or a bare string" dance (effort router, compaction).
func ContentToBlocks(content FlexibleJSON) []ContentBlock {
	if content.IsZero() {
		return nil
	}
	raw := content.Bytes()
	// Try array of blocks first (most common for tool_result messages).
	var blocks []ContentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil && len(blocks) > 0 {
		return blocks
	}
	// Plain text string → single text block.
	var text string
	if err := json.Unmarshal(raw, &text); err == nil && text != "" {
		return []ContentBlock{{Type: "text", Text: text}}
	}
	return nil
}
