package chat

import (
	"fmt"
	"log/slog"
	"math"
	"os"
	"strconv"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
)

// Context assembly defaults.
const (
	defaultMemoryTokenBudget  = 170_000
	defaultSystemPromptBudget = 45_000
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
// the long tail. The system-prompt share includes the ambient tier-1 wiki
// memory block, so it must leave enough room above the static prompt for that
// block to arrive intact instead of being trimmed away after assembly. An
// override that leaves no real history headroom above the system-prompt budget
// is ignored (see minMemoryBudgetHeadroom).
func DefaultContextConfig() ContextConfig {
	cfg := ContextConfig{
		MemoryTokenBudget:  defaultMemoryTokenBudget,
		SystemPromptBudget: defaultSystemPromptBudget,
		FreshTailCount:     defaultFreshTailCount,
	}
	if raw := os.Getenv("DENEB_MEMORY_TOKEN_BUDGET"); raw != "" {
		// ParseInt (not ParseUint) so the value is already bounded to signed
		// int64 — CodeQL's incorrect-integer-conversion tracks ParseUint→int.
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v < 0 || uint64(v) < cfg.SystemPromptBudget+minMemoryBudgetHeadroom || v > math.MaxInt {
			slog.Warn("DENEB_MEMORY_TOKEN_BUDGET ignored",
				"value", raw, "minimum", cfg.SystemPromptBudget+minMemoryBudgetHeadroom, "error", err)
		} else {
			cfg.MemoryTokenBudget = uint64(v) //nolint:gosec // G115 — v checked against MaxInt above
		}
	}
	return cfg
}

// uint64ToInt converts v to int only after an explicit MaxInt upper bound check
// (CodeQL go/incorrect-integer-conversion sanitizer shape).
func uint64ToInt(v uint64) (int, error) {
	if v > uint64(math.MaxInt) {
		return 0, fmt.Errorf("%d exceeds int range", v)
	}
	return int(v), nil
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
	memBudget, err := uint64ToInt(cfg.MemoryTokenBudget)
	if err != nil {
		return nil, fmt.Errorf("chat: memory token budget: %w", err)
	}
	freshTail, err := uint64ToInt(uint64(cfg.FreshTailCount))
	if err != nil {
		return nil, fmt.Errorf("chat: fresh tail count: %w", err)
	}
	result, err := bridge.AssembleContext(
		sessionKey,
		memBudget,
		freshTail,
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
