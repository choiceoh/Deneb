package compaction

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// EmergencyCompact handles the case where a single user input is ≥30K tokens.
//
// The message list is split into three zones:
//
//	[evicted (dropped)] [non-evicted old → summarized] [recent (preserved)]
//
// Strategy:
//  1. Evict the oldest messages to make room for the large input
//  2. Summarize the non-evicted old messages (between evicted and recent)
//  3. Preserve recent messages and the large input intact
func EmergencyCompact(
	ctx context.Context,
	cfg Config,
	messages []llm.Message,
	inputTokens int,
	summarizer Summarizer,
	logger *slog.Logger,
) ([]llm.Message, int) {
	if len(messages) <= 2 {
		return messages, 0
	}

	// Preserve at least the last few messages (the large input + recent context).
	const recentKeep = 4
	if len(messages) <= recentKeep {
		return messages, 0
	}

	// Split: recent tail is always preserved.
	recentStart := len(messages) - recentKeep
	recent := messages[recentStart:]

	// Evict oldest messages from the old portion until budget fits.
	old := messages[:recentStart]
	evicted := 0
	for evicted < len(old) {
		totalAfter := EstimateMessagesTokens(old[evicted:]) + EstimateMessagesTokens(recent)
		if totalAfter+inputTokens <= cfg.ContextBudget {
			break
		}
		evicted++
	}

	if evicted == 0 {
		return messages, 0
	}

	// Non-evicted old messages: the zone between evicted and recent.
	nonEvicted := old[evicted:]

	// Summarize non-evicted old messages so the agent retains context.
	if summarizer != nil && len(nonEvicted) > 0 {
		text := serializeMessages(nonEvicted)
		// Cap output at 2048: emergency summaries should be brief since
		// we already evicted the bulkiest messages.
		maxOutput := int(float64(cfg.ContextBudget) * 0.10)
		if maxOutput > 2048 {
			maxOutput = 2048
		}
		summary, err := summarizer.Summarize(ctx, compactionSystemPrompt, text, maxOutput)
		if err == nil && summary != "" {
			result := make([]llm.Message, 0, 1+len(recent))
			result = append(result, llm.NewTextMessage("user",
				fmt.Sprintf("[Emergency compaction: %d messages evicted, %d messages summarized]\n\n%s",
					evicted, len(nonEvicted), summary)))
			result = append(result, recent...)

			if logger != nil {
				logger.Info("polaris: emergency compaction",
					"evicted", evicted,
					"summarized", len(nonEvicted),
					"summaryTokens", EstimateTokens(summary),
					"recentKept", len(recent))
			}
			return result, evicted
		}
	}

	// Fallback: drop evicted, keep non-evicted + recent without summary.
	result := make([]llm.Message, 0, len(nonEvicted)+len(recent))
	result = append(result, nonEvicted...)
	result = append(result, recent...)

	if logger != nil {
		logger.Info("polaris: emergency compaction (no summary)",
			"evicted", evicted,
			"remaining", len(result))
	}
	return result, evicted
}
