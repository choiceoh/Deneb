// compaction_micro.go implements lightweight tool result pruning to reduce
// token count WITHOUT an expensive LLM summarization call.
//
// "Microcompact" runs before full compaction: it removes old tool_result
// content from messages, replacing them with compact stubs. This reclaims
// tokens at near-zero cost (no API call) and often avoids or delays the
// need for a full compaction sweep.
//
// Inspired by Claude Code's microCompact.ts pattern.
package compaction

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/tokenutil"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// Microcompact defaults.
const (
	// microcompactKeepRecent is the number of most recent tool results
	// to always preserve (even if stale).
	microcompactKeepRecent = 8

	// microcompactStubText is the replacement text for pruned tool results.
	microcompactStubText = "_(tool result pruned to save context tokens)_"
)

// MicrocompactResult describes what microcompact did.
type MicrocompactResult struct {
	PrunedCount    int    // number of tool results pruned
	EstimatedSaved int    // estimated tokens saved
	Reason         string // why it ran (or why it was skipped)
}

// MicrocompactMessages prunes old tool_result content blocks from messages
// to reduce token count without an LLM call. Returns the modified messages
// (a new slice; original is not mutated) and a result summary.
//
// The algorithm:
//  1. Find the timestamp of the last assistant message.
//  2. If the gap since then exceeds the stale threshold, tool results in
//     older messages are eligible for pruning.
//  3. Walk messages oldest-first, replacing tool_result content with a stub.
//  4. Always preserve the most recent N tool results.
func MicrocompactMessages(messages []llm.Message, now time.Time) ([]llm.Message, MicrocompactResult) {
	if len(messages) == 0 {
		return messages, MicrocompactResult{Reason: "no_messages"}
	}

	// Find tool_result positions and the last assistant message index.
	type toolResultPos struct {
		msgIdx   int
		blockIdx int
		tokens   int // estimated tokens in the content
	}
	var positions []toolResultPos

	for i, msg := range messages {
		// Parse content blocks for user messages (tool_result blocks live
		// in user messages in the Anthropic API format).
		if msg.Role != "user" {
			continue
		}
		var blocks []llm.ContentBlock
		if err := json.Unmarshal(msg.Content, &blocks); err != nil {
			// Content is a plain string, not blocks — skip.
			continue
		}
		for j, block := range blocks {
			if block.Type == "tool_result" && block.Content != "" {
				est := estimateTokens(block.Content)
				positions = append(positions, toolResultPos{
					msgIdx:   i,
					blockIdx: j,
					tokens:   est,
				})
			}
		}
	}

	// Nothing to prune.
	if len(positions) == 0 {
		return messages, MicrocompactResult{Reason: "no_tool_results"}
	}

	// Staleness gate: only prune tool results from completed turns.
	// Tool results that appear after the last assistant message are from the
	// current active turn and must be preserved for context fidelity.
	lastAssistantIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			lastAssistantIdx = i
			break
		}
	}

	// Filter to only stale positions (before the last assistant message).
	var stalePositions []toolResultPos
	for _, pos := range positions {
		if pos.msgIdx < lastAssistantIdx {
			stalePositions = append(stalePositions, pos)
		}
	}
	if len(stalePositions) == 0 {
		return messages, MicrocompactResult{Reason: "active_turn"}
	}

	// Determine how many stale results to prune: all except the most recent N.
	pruneCount := len(stalePositions) - microcompactKeepRecent
	if pruneCount <= 0 {
		return messages, MicrocompactResult{Reason: "below_keep_threshold"}
	}

	// Use stale positions (oldest first) for pruning.
	positions = stalePositions

	// Build a set of (msgIdx, blockIdx) pairs to prune.
	type key struct{ msg, block int }
	pruneSet := make(map[key]bool, pruneCount)
	estimatedSaved := 0
	for _, pos := range positions[:pruneCount] {
		pruneSet[key{pos.msgIdx, pos.blockIdx}] = true
		estimatedSaved += pos.tokens
	}

	// Create new messages with pruned content.
	result := make([]llm.Message, len(messages))
	for i, msg := range messages {
		if msg.Role != "user" {
			result[i] = msg
			continue
		}

		var blocks []llm.ContentBlock
		if err := json.Unmarshal(msg.Content, &blocks); err != nil {
			result[i] = msg
			continue
		}

		modified := false
		newBlocks := make([]llm.ContentBlock, len(blocks))
		copy(newBlocks, blocks)
		for j := range newBlocks {
			if pruneSet[key{i, j}] {
				newBlocks[j].Content = fmt.Sprintf("%s (was %d chars)", microcompactStubText, len(blocks[j].Content))
				modified = true
			}
		}

		if modified {
			raw, _ := json.Marshal(newBlocks)
			result[i] = llm.Message{Role: msg.Role, Content: raw}
		} else {
			result[i] = msg
		}
	}

	return result, MicrocompactResult{
		PrunedCount:    pruneCount,
		EstimatedSaved: estimatedSaved,
		Reason:         "pruned",
	}
}

// estimateTokens provides a rough token estimate using rune count.
// Delegates to tokenutil.EstimateTokens (shared across chat subsystem).
func estimateTokens(s string) int {
	return tokenutil.EstimateTokens(s)
}
