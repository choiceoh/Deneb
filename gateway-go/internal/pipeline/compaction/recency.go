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
		FormatContextFence(
			"polaris-recency",
			"drop-notice",
			fmt.Sprintf("Polaris recency compaction: %d oldest messages dropped", evicted),
			"### 불확실한 메모 (Uncertain Notes)\n- [오래됨] 일부 오래된 메시지는 요약 없이 제거되었다. 남은 최신 원문 메시지를 우선하고, 누락된 과거 맥락은 확신하지 마라.",
		)))
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
