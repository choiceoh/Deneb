package chat

import (
	"fmt"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
)

// Context assembly defaults.
const (
	defaultMemoryTokenBudget  = 150_000
	defaultSystemPromptBudget = 30_000
	defaultFreshTailCount     = 48
)

// AssemblyResult holds the output of context assembly.
type AssemblyResult struct {
	Messages        []llm.Message
	EstimatedTokens int
	TotalMessages   int
	WasCompacted    bool // true if summaries were used
}

// ContextConfig configures context assembly behavior.
type ContextConfig struct {
	MemoryTokenBudget  uint64 // max tokens for transcript history
	SystemPromptBudget uint64 // max tokens for system prompt fragments
	FreshTailCount     uint32 // messages protected from eviction
}

// DefaultContextConfig returns sensible defaults.
func DefaultContextConfig() ContextConfig {
	return ContextConfig{
		MemoryTokenBudget:  defaultMemoryTokenBudget,
		SystemPromptBudget: defaultSystemPromptBudget,
		FreshTailCount:     defaultFreshTailCount,
	}
}

// estimateTokens returns a rough token count for a string.
// Delegates to tokenest.Estimate (script-aware, Korean-weighted).
func estimateTokens(s string) int {
	return tokenest.Estimate(s)
}

// assembleContext builds the LLM context via Polaris summary DAG.
//
// Flow:
//   - Summaries exist → [summary messages] + recent raw messages only (efficient).
//   - No summaries yet → full message load → compaction creates summaries →
//     next turn enters the summary path automatically.
//
// The store MUST be a *polaris.Bridge; legacy (non-Bridge) assembly is removed.
func assembleContext(
	store TranscriptStore,
	sessionKey string,
	cfg ContextConfig,
	logger *slog.Logger,
) (*AssemblyResult, error) {
	bridge, ok := store.(*polaris.Bridge)
	if !ok {
		return nil, fmt.Errorf("assembleContext: store must be *polaris.Bridge, got %T", store)
	}

	result, err := bridge.AssembleContext(
		sessionKey,
		int(cfg.MemoryTokenBudget),
		int(cfg.FreshTailCount),
		logger,
	)
	if err != nil {
		return nil, err
	}
	return &AssemblyResult{
		Messages:        result.Messages,
		EstimatedTokens: result.EstimatedTokens,
		TotalMessages:   result.TotalMessages,
		WasCompacted:    result.WasCompacted,
	}, nil
}
