package compaction

import (
	"context"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

const (
	// BootstrapMaxOutput caps the LLM output tokens for bootstrap summaries.
	// Higher than regular compaction (4096) because bootstrap summarizes a
	// potentially very long conversation history (hundreds of messages).
	BootstrapMaxOutput = 8192
)

// BootstrapCompact creates a summary of older messages for initial DAG bootstrap.
// Used when no summaries exist yet and assembly truncated the context via freshTailCount.
// Returns the summary text, or empty string if compaction was unnecessary or failed.
func BootstrapCompact(
	ctx context.Context,
	messages []llm.Message,
	summarizer Summarizer,
	logger *slog.Logger,
) string {
	if len(messages) == 0 || summarizer == nil {
		return ""
	}

	text := serializeMessages(messages)
	if EstimateTokens(text) < 500 {
		return "" // too little to bother
	}

	summary, err := summarizer.Summarize(ctx, compactionSystemPrompt, text, BootstrapMaxOutput)
	if err != nil {
		if logger != nil {
			logger.Warn("polaris: bootstrap compaction failed", "error", err)
		}
		return ""
	}
	return summary
}
