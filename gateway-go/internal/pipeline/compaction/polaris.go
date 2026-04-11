// Polaris compaction system: tiered context compression for long-running agent sessions.
//
// Three tiers applied in order:
//  1. Emergency — single user input ≥30K tokens: evict oldest messages, compact remaining
//  2. Micro     — strip code fences from tool results older than 4 turns (no LLM call)
//  3. LLM       — at 90% of context budget: local AI summarizes old messages to 20% target
package compaction

import (
	"context"
	"encoding/json"
	"log/slog"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

const (
	DefaultMicroTurnThreshold      = 4
	DefaultLLMThresholdPct         = 0.90
	DefaultLLMTargetPct            = 0.20
	DefaultEmergencyInputThreshold = 30_000
	runesPerToken                  = 2
)

// Config holds Polaris compaction parameters.
type Config struct {
	ContextBudget     int  // effective token budget (MemoryTokenBudget - SystemPromptBudget)
	SkipLLMCompaction bool // skip LLM summarization tier (e.g. when summaries already injected)
}

// NewConfig creates a compaction config for the given context budget.
// contextBudget should be (MemoryTokenBudget - SystemPromptBudget).
func NewConfig(contextBudget int) Config {
	return Config{ContextBudget: contextBudget}
}

// Result reports what the pipeline did.
type Result struct {
	MicroPruned      int  // tool_result blocks that had code stripped
	LLMCompacted     bool // whether LLM summarization was applied
	EmergencyEvicted int  // messages evicted due to large input
	TokensBefore     int
	TokensAfter      int
}

// Summarizer provides LLM-based summarization (typically local AI).
// system is the instruction prompt, conversation is the serialized messages.
// maxOutputTokens caps the LLM response length (not input).
type Summarizer interface {
	Summarize(ctx context.Context, system, conversation string, maxOutputTokens int) (string, error)
}

// Compact applies the full Polaris pipeline to assembled context messages.
// summarizer may be nil — LLM and emergency compaction are skipped in that case.
func Compact(
	ctx context.Context,
	cfg Config,
	messages []llm.Message,
	summarizer Summarizer,
	logger *slog.Logger,
) ([]llm.Message, Result) {
	var r Result
	r.TokensBefore = EstimateMessagesTokens(messages)

	// Snapshot file reads before compaction so we can restore them afterward.
	// This preserves file contents the agent was actively editing.
	fileReads := ExtractRecentFileReads(messages)

	// Strip image blocks before summarization to prevent prompt-too-long errors.
	// The stripped copy is used only for LLM calls; file restoration uses originals.
	summarizeMessages := StripImageBlocks(messages)

	// Tier 1: Emergency — evict oldest when a real user input is huge.
	// Only fires for actual user messages, not tool_result blocks.
	// Emergency already summarizes non-evicted old messages, so skip LLM tier after it.
	emergencyFired := false
	lastInputTokens := lastUserInputTokens(messages)
	if lastInputTokens >= DefaultEmergencyInputThreshold && summarizer != nil {
		var evicted int
		summarizeMessages, evicted = EmergencyCompact(ctx, cfg, summarizeMessages, lastInputTokens, summarizer, logger)
		r.EmergencyEvicted = evicted
		emergencyFired = evicted > 0
		if emergencyFired {
			messages = summarizeMessages
		}
	}

	// Tier 2: Micro — strip code from old tool results (zero cost).
	var pruned int
	messages, pruned = MicroCompact(messages, DefaultMicroTurnThreshold)
	r.MicroPruned = pruned
	if !emergencyFired {
		summarizeMessages = messages
	}

	// Tier 3: LLM — summarize old messages when over threshold.
	// Skipped when emergency already summarized (avoids double summarization / fact loss).
	if !emergencyFired && !cfg.SkipLLMCompaction {
		threshold := int(float64(cfg.ContextBudget) * DefaultLLMThresholdPct)
		if EstimateMessagesTokens(summarizeMessages) > threshold && summarizer != nil {
			compacted, ok := LLMCompact(ctx, cfg, summarizeMessages, summarizer, logger)
			if ok {
				messages = compacted
				r.LLMCompacted = true
			}
		}
	}

	// Post-compaction file restoration: re-inject recently-read file contents
	// so the agent retains access to files it was actively working on.
	// Insert before the final user message so the LLM sees restored files
	// as prior context, not after the user's current input.
	if (r.LLMCompacted || r.EmergencyEvicted > 0) && len(fileReads) > 0 {
		if restored := BuildRestorationMessages(fileReads, restorationBudgetTokens); len(restored) > 0 {
			// Find the last user message (current turn input) and insert before it.
			insertIdx := len(messages)
			for i := len(messages) - 1; i >= 0; i-- {
				if messages[i].Role == "user" && !isToolResultMessage(messages[i].Content) {
					insertIdx = i
					break
				}
			}
			result := make([]llm.Message, 0, len(messages)+len(restored))
			result = append(result, messages[:insertIdx]...)
			result = append(result, restored...)
			result = append(result, messages[insertIdx:]...)
			messages = result
			if logger != nil {
				logger.Info("polaris: restored file reads after compaction", "files", len(fileReads))
			}
		}
	}

	r.TokensAfter = EstimateMessagesTokens(messages)
	return messages, r
}

// EstimateMessagesTokens estimates total tokens across all messages.
func EstimateMessagesTokens(messages []llm.Message) int {
	total := 0
	for _, m := range messages {
		// role overhead (~4 tokens) + content
		total += EstimateTokens(string(m.Content)) + 4
	}
	return total
}

// EstimateTokens estimates token count (Korean-calibrated: ~2 runes/token).
func EstimateTokens(s string) int {
	n := utf8.RuneCountInString(s)
	if n == 0 {
		return 0
	}
	est := n / runesPerToken
	if est < 1 {
		return 1
	}
	return est
}

// lastUserInputTokens returns estimated tokens of the last real user input message.
// Skips tool_result messages (role=user but content is tool_result blocks) since
// those are system-generated and should not trigger emergency compaction.
func lastUserInputTokens(messages []llm.Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		if isToolResultMessage(messages[i].Content) {
			continue
		}
		return EstimateTokens(string(messages[i].Content))
	}
	return 0
}

// isToolResultMessage checks if a user message contains tool_result blocks.
func isToolResultMessage(content json.RawMessage) bool {
	if len(content) == 0 || content[0] != '[' {
		return false // plain string or empty — real user input
	}
	var blocks []struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return false
	}
	for _, b := range blocks {
		if b.Type == "tool_result" {
			return true
		}
	}
	return false
}
