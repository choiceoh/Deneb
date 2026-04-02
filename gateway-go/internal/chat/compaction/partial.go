// compaction_partial.go implements direction-aware partial compaction.
//
// Full compaction replaces the entire conversation history with a summary.
// Partial compaction summarizes only a portion, preserving the rest verbatim:
//
//   - "from" direction: summarize later messages, preserve earlier ones.
//     This MAINTAINS the prompt cache prefix (system prompt + early context).
//
//   - "up_to" direction: summarize earlier messages, preserve later ones.
//     This INVALIDATES the prompt cache (prefix changes) but preserves
//     recent detailed context.
//
// "from" is the recommended default: it keeps the prompt cache warm while
// compressing the less-valuable recent tool results and intermediate steps.
//
// Inspired by Claude Code's partialCompactConversation pattern.
package compaction

import (
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// CompactionDirection controls which part of the conversation is summarized.
type CompactionDirection string

const (
	// CompactFrom summarizes messages from a pivot point to the end.
	// Preserves earlier messages (maintains prompt cache).
	CompactFrom CompactionDirection = "from"

	// CompactUpTo summarizes messages from the start up to a pivot point.
	// Preserves later messages (invalidates prompt cache).
	CompactUpTo CompactionDirection = "up_to"
)

// PartialCompactConfig configures a partial compaction operation.
type PartialCompactConfig struct {
	// Direction determines which portion is summarized.
	Direction CompactionDirection

	// PivotIndex is the message index that separates preserved from summarized.
	// For "from": messages [0, pivot) are preserved, [pivot, end) are summarized.
	// For "up_to": messages [0, pivot) are summarized, [pivot, end) are preserved.
	PivotIndex int

	// MaxSummaryTokens caps the size of the generated summary.
	MaxSummaryTokens int
}

// DefaultPartialCompactConfig returns a config that compresses the latter
// half of the conversation (cache-friendly).
func DefaultPartialCompactConfig(messageCount int) PartialCompactConfig {
	pivot := messageCount / 2
	if pivot < 4 {
		pivot = 4
	}
	if pivot > messageCount {
		pivot = messageCount
	}
	return PartialCompactConfig{
		Direction:        CompactFrom,
		PivotIndex:       pivot,
		MaxSummaryTokens: 4096,
	}
}

// PartialCompactResult describes the outcome.
type PartialCompactResult struct {
	PreservedCount  int    // messages kept verbatim
	SummarizedCount int    // messages that were summarized
	SummaryTokens   int    // estimated tokens in the summary
	CachePreserved  bool   // true if prompt cache prefix is intact
	Direction       CompactionDirection
}

// SplitForPartialCompaction splits messages according to the partial compact
// config, returning the portion to preserve and the portion to summarize.
func SplitForPartialCompaction(messages []llm.Message, cfg PartialCompactConfig) (preserved, toSummarize []llm.Message) {
	if cfg.PivotIndex <= 0 || cfg.PivotIndex >= len(messages) {
		// Invalid pivot — return all as preserved, nothing to summarize.
		return messages, nil
	}

	switch cfg.Direction {
	case CompactFrom:
		// Preserve early messages, summarize later ones.
		preserved = messages[:cfg.PivotIndex]
		toSummarize = messages[cfg.PivotIndex:]
	case CompactUpTo:
		// Summarize early messages, preserve later ones.
		toSummarize = messages[:cfg.PivotIndex]
		preserved = messages[cfg.PivotIndex:]
	default:
		return messages, nil
	}
	return preserved, toSummarize
}

// ReassembleAfterPartialCompaction merges preserved messages with the
// compaction summary, placing them in the correct order based on direction.
func ReassembleAfterPartialCompaction(
	preserved []llm.Message,
	summaryText string,
	cfg PartialCompactConfig,
) []llm.Message {
	if summaryText == "" {
		return preserved
	}

	summaryMsg := llm.NewTextMessage("user", "[Compacted conversation summary]\n"+summaryText)

	switch cfg.Direction {
	case CompactFrom:
		// preserved (early) + summary (replaces later messages)
		result := make([]llm.Message, 0, len(preserved)+1)
		result = append(result, preserved...)
		result = append(result, summaryMsg)
		return result
	case CompactUpTo:
		// summary (replaces early messages) + preserved (later)
		result := make([]llm.Message, 0, len(preserved)+1)
		result = append(result, summaryMsg)
		result = append(result, preserved...)
		return result
	default:
		return preserved
	}
}
