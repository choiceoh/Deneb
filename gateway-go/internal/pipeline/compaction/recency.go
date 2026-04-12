package compaction

import (
	"fmt"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// RecencyCompact is the last-resort compaction fallback.
// When both LLM summarization and embedding+MMR are unavailable, simply keep
// the most recent messages within the token budget.
//
// Unlike emergency compaction (which fires on huge single inputs), recency
// compaction fires when context exceeds threshold and no smarter compaction
// method is available.
func RecencyCompact(
	cfg Config,
	messages []llm.Message,
	logger *slog.Logger,
) ([]llm.Message, bool) {
	threshold := int(float64(cfg.ContextBudget) * DefaultLLMThresholdPct)
	total := EstimateMessagesTokens(messages)
	if total <= threshold {
		return messages, false
	}

	// Keep messages from the end until we fill the budget.
	// Target: fit within 80% of context budget to leave headroom.
	target := int(float64(cfg.ContextBudget) * 0.80)
	kept := 0
	startIdx := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		msgTokens := EstimateTokens(string(messages[i].Content)) + 4
		if kept+msgTokens > target {
			break
		}
		kept += msgTokens
		startIdx = i
	}

	if startIdx <= 0 {
		return messages, false // keeping everything already
	}

	evicted := startIdx
	remaining := messages[startIdx:]

	compacted := make([]llm.Message, 0, 1+len(remaining))
	compacted = append(compacted, llm.NewTextMessage("user",
		fmt.Sprintf("[Polaris recency compaction: %d oldest messages dropped]", evicted)))
	compacted = append(compacted, remaining...)

	if logger != nil {
		logger.Info("polaris: recency compaction applied",
			"dropped", evicted,
			"kept", len(remaining),
			"tokensBefore", total,
			"tokensAfter", EstimateMessagesTokens(compacted))
	}
	return compacted, true
}
