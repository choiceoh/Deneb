package chat

import (
	"log/slog"
	"os"
	"strconv"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
)

// Context assembly defaults.
const (
	defaultMemoryTokenBudget  = 170_000
	defaultSystemPromptBudget = 30_000
	defaultFreshTailCount     = 24

	// minMemoryBudgetHeadroom is the smallest history allowance a
	// MemoryTokenBudget override must leave above the system-prompt budget.
	// Below this the override is ignored: effectiveContextBudget computes
	// memory-minus-system on uint64s, so a budget at or under the system
	// share would underflow into a near-infinite history budget.
	minMemoryBudgetHeadroom = 4_096
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
//
// DENEB_MEMORY_TOKEN_BUDGET overrides the total history+system token budget.
// The 170K default was sized for large-window remote models; on the local
// bandwidth-bound DGX serve, decode speed degrades sharply with context size
// (measured on step3p7/GB10: ~20 tok/s under 60K input vs ~5 tok/s at 110K+),
// so a deployment that prefers latency over raw in-context history sets a
// smaller budget here and lets Polaris compaction + recall preflight carry
// the long tail. An override that leaves no real history headroom above the
// system-prompt budget is ignored (see minMemoryBudgetHeadroom).
func DefaultContextConfig() ContextConfig {
	cfg := ContextConfig{
		MemoryTokenBudget:  defaultMemoryTokenBudget,
		SystemPromptBudget: defaultSystemPromptBudget,
		FreshTailCount:     defaultFreshTailCount,
	}
	if raw := os.Getenv("DENEB_MEMORY_TOKEN_BUDGET"); raw != "" {
		v, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || v < cfg.SystemPromptBudget+minMemoryBudgetHeadroom {
			slog.Warn("DENEB_MEMORY_TOKEN_BUDGET ignored",
				"value", raw, "minimum", cfg.SystemPromptBudget+minMemoryBudgetHeadroom, "error", err)
		} else {
			cfg.MemoryTokenBudget = v
		}
	}
	return cfg
}

// assembleContext builds the LLM context via the Polaris summary DAG.
//
// Flow:
//   - Summaries exist → [summary messages] + recent raw messages only (efficient).
//   - No summaries yet → full message load → compaction creates summaries →
//     next turn enters the summary path automatically.
func assembleContext(
	bridge *polaris.Bridge,
	sessionKey string,
	cfg ContextConfig,
	logger *slog.Logger,
) (*AssemblyResult, error) {
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
