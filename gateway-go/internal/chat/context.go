package chat

import (
	"fmt"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/polaris"
	"github.com/choiceoh/deneb/gateway-go/internal/tokenest"
)

// Context assembly defaults.
const (
	defaultMemoryTokenBudget  = 30_000
	defaultLiveTokenBudget    = 120_000 // total agent loop token budget (system + tools + memory + live messages)
	defaultSystemPromptBudget = 30_000
	defaultFreshTailCount     = 48
	defaultMaxMessages        = 100
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
	LiveTokenBudget    uint64 // total agent loop token budget (system + tools + memory + live)
	SystemPromptBudget uint64 // max tokens for system prompt fragments
	FreshTailCount     uint32 // messages protected from eviction
	MaxMessages        int    // max messages to load from transcript
}

// DefaultContextConfig returns sensible defaults.
func DefaultContextConfig() ContextConfig {
	return ContextConfig{
		MemoryTokenBudget:  defaultMemoryTokenBudget,
		LiveTokenBudget:    defaultLiveTokenBudget,
		SystemPromptBudget: defaultSystemPromptBudget,
		FreshTailCount:     defaultFreshTailCount,
		MaxMessages:        defaultMaxMessages,
	}
}

// estimateTokens returns a rough token count for a string.
// Delegates to tokenest.Estimate (script-aware, Korean-weighted).
func estimateTokens(s string) int {
	return tokenest.Estimate(s)
}

// assembleContext selects transcript messages that fit within the token budget.
// When the store is an LCM Bridge with summary data, uses DAG-based assembly
// (summaries + recent raw messages). Otherwise falls back to simple tail-N.
func assembleContext(
	store TranscriptStore,
	sessionKey string,
	cfg ContextConfig,
	logger *slog.Logger,
) (*AssemblyResult, error) {
	limit := cfg.MaxMessages
	if limit <= 0 {
		limit = defaultMaxMessages
	}

	// LCM-aware assembly: use summary DAG when available.
	if bridge, ok := store.(*polaris.Bridge); ok {
		result, err := polaris.AssembleContext(
			bridge.Store(), bridge,
			sessionKey,
			int(cfg.MemoryTokenBudget),
			int(cfg.FreshTailCount),
			limit,
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

	// Legacy tail-N assembly.
	msgs, total, err := store.Load(sessionKey, limit)
	if err != nil {
		return nil, fmt.Errorf("load transcript: %w", err)
	}

	// Tail-N truncation.
	if len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}

	// Token-budget truncation: drop oldest messages until history fits.
	if budget := cfg.MemoryTokenBudget; budget > 0 && len(msgs) > 0 {
		msgs = trimToTokenBudget(msgs, int(budget))
	}

	return &AssemblyResult{
		Messages:      transcriptToMessages(msgs),
		TotalMessages: total,
	}, nil
}

// trimToTokenBudget drops the oldest messages until the total estimated
// token count fits within budget. Always keeps at least the last message.
func trimToTokenBudget(msgs []ChatMessage, budget int) []ChatMessage {
	total := 0
	for _, m := range msgs {
		total += estimateTokens(string(m.Content))
	}
	if total <= budget {
		return msgs
	}
	// Drop from the front (oldest) until we fit.
	for len(msgs) > 1 && total > budget {
		total -= estimateTokens(string(msgs[0].Content))
		msgs = msgs[1:]
	}
	return msgs
}

// transcriptToMessages converts ChatMessage transcript entries to LLM messages.
// System prompt is injected via ChatRequest.System, not as a message here.
// Content is passed through directly as json.RawMessage so rich content
// (block arrays with tool_use, tool_result, thinking) is preserved.
func transcriptToMessages(transcript []ChatMessage) []llm.Message {
	msgs := make([]llm.Message, 0, len(transcript))
	for _, t := range transcript {
		role := t.Role
		if role == "" {
			role = "user"
		}
		// Pass Content directly — both ChatMessage.Content and llm.Message.Content
		// are json.RawMessage, so rich block arrays are preserved without re-encoding.
		msgs = append(msgs, llm.Message{Role: role, Content: t.Content})
	}
	return msgs
}
