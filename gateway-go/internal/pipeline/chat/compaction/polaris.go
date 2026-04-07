// Polaris compaction system: tiered context compression for long-running agent sessions.
//
// Three tiers applied in order:
//  1. Emergency — single input ≥30K tokens: evict oldest messages, compact remaining
//  2. Micro     — strip code fences from tool results older than 4 turns (no LLM call)
//  3. LLM       — at 80% of context budget: local AI summarizes old messages to 20% target
package compaction

import (
	"context"
	"log/slog"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

const (
	DefaultContextBudget           = 150_000
	DefaultMicroTurnThreshold      = 4
	DefaultLLMThresholdPct         = 0.80
	DefaultLLMTargetPct            = 0.20
	DefaultEmergencyInputThreshold = 30_000
	runesPerToken                  = 2
)

// Config holds Polaris compaction parameters.
type Config struct {
	ContextBudget           int     // total token budget (default 150K)
	MicroTurnThreshold      int     // turns before code stripping (default 4)
	LLMThresholdPct         float64 // trigger LLM compaction at this fraction (default 0.80)
	LLMTargetPct            float64 // target size as fraction of budget (default 0.20)
	EmergencyInputThreshold int     // single-input token threshold (default 30K)
}

// DefaultConfig returns production defaults.
func DefaultConfig() Config {
	return Config{
		ContextBudget:           DefaultContextBudget,
		MicroTurnThreshold:      DefaultMicroTurnThreshold,
		LLMThresholdPct:         DefaultLLMThresholdPct,
		LLMTargetPct:            DefaultLLMTargetPct,
		EmergencyInputThreshold: DefaultEmergencyInputThreshold,
	}
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

	// Tier 1: Emergency — evict oldest when a single input is huge.
	// Emergency already summarizes non-evicted old messages, so skip LLM tier after it.
	emergencyFired := false
	lastInputTokens := lastUserMessageTokens(messages)
	if lastInputTokens >= cfg.EmergencyInputThreshold && summarizer != nil {
		var evicted int
		messages, evicted = EmergencyCompact(ctx, cfg, messages, lastInputTokens, summarizer, logger)
		r.EmergencyEvicted = evicted
		emergencyFired = evicted > 0
	}

	// Tier 2: Micro — strip code from old tool results (zero cost).
	var pruned int
	messages, pruned = MicroCompact(messages, cfg.MicroTurnThreshold)
	r.MicroPruned = pruned

	// Tier 3: LLM — summarize old messages when over threshold.
	// Skipped when emergency already summarized (avoids double summarization / fact loss).
	if !emergencyFired {
		threshold := int(float64(cfg.ContextBudget) * cfg.LLMThresholdPct)
		if EstimateMessagesTokens(messages) > threshold && summarizer != nil {
			compacted, ok := LLMCompact(ctx, cfg, messages, summarizer, logger)
			if ok {
				messages = compacted
				r.LLMCompacted = true
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

// lastUserMessageTokens returns estimated tokens of the last user message.
func lastUserMessageTokens(messages []llm.Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return EstimateTokens(string(messages[i].Content))
		}
	}
	return 0
}
