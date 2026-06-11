package compaction

import (
	"context"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// BootstrapCompact creates a summary of older messages for initial DAG bootstrap.
// Used when no summaries exist yet and assembly truncated the context via
// freshTailCount.
//
// The mechanism is identical to regular LLM compaction: same
// cfg.ContextBudget × DefaultLLMTargetPct output budget, same chunkMaxTokens
// threshold, and the same bounded parallel chunked summarization path. Only
// the trigger is bootstrap-specific (recovering messages the assembly
// dropped); the summarization itself is shared with LLMCompact.
//
// Returns the summary text and the number of leading messages it covers —
// a huge backlog is digested maxChunksPerPass chunks per pass, so covered may
// be smaller than len(messages); the caller must persist the partial range
// only. Returns ("", 0) if compaction was unnecessary (no messages, no
// summarizer, too little content) or failed.
func BootstrapCompact(
	ctx context.Context,
	cfg Config,
	messages []llm.Message,
	summarizer Summarizer,
	logger *slog.Logger,
) (string, int) {
	if len(messages) == 0 || summarizer == nil {
		return "", 0
	}
	return summarizeOldMessages(ctx, cfg, messages, summarizer, logger)
}
