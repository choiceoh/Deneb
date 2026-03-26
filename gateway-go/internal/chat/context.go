package chat

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// Context assembly defaults.
const (
	defaultTokenBudget    = 100_000
	defaultFreshTailCount = 48
	defaultMaxMessages    = 100
	// charsPerToken is the rough estimate for token counting when a
	// real tokenizer is unavailable (BPE averages ~3.5–4 chars/token).
	charsPerToken = 4
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
	TokenBudget    uint64 // max tokens for context window
	FreshTailCount uint32 // messages protected from eviction
	MaxMessages    int    // fallback limit when FFI unavailable
}

// DefaultContextConfig returns sensible defaults.
func DefaultContextConfig() ContextConfig {
	return ContextConfig{
		TokenBudget:    defaultTokenBudget,
		FreshTailCount: defaultFreshTailCount,
		MaxMessages:    defaultMaxMessages,
	}
}

// estimateTokens returns a rough token count for a string.
func estimateTokens(s string) int {
	n := len(s) / charsPerToken
	if n < 1 {
		return 1
	}
	return n
}

// assembleContext selects transcript messages that fit within the token budget.
// Uses Rust FFI context engine when available; falls back to simple tail-N.
func assembleContext(
	store TranscriptStore,
	sessionKey string,
	cfg ContextConfig,
	logger *slog.Logger,
) (*AssemblyResult, error) {
	if ffi.Available {
		return assembleContextFFI(store, sessionKey, cfg, logger)
	}
	return assembleContextFallback(store, sessionKey, cfg)
}

// assembleContextFFI uses the Rust context engine for token-budgeted selection.
func assembleContextFFI(
	store TranscriptStore,
	sessionKey string,
	cfg ContextConfig,
	logger *slog.Logger,
) (*AssemblyResult, error) {
	// For single-user deployment, use a fixed conversation ID.
	var conversationID uint64 = 1

	handle, err := ffi.ContextAssemblyNew(conversationID, cfg.TokenBudget, cfg.FreshTailCount)
	if err != nil {
		logger.Warn("context assembly FFI unavailable, falling back", "error", err)
		return assembleContextFallback(store, sessionKey, cfg)
	}
	defer ffi.ContextEngineDrop(handle)

	cmdJSON, err := ffi.ContextAssemblyStart(handle)
	if err != nil {
		return nil, fmt.Errorf("context assembly start: %w", err)
	}

	allMsgs, total, err := store.Load(sessionKey, 0)
	if err != nil {
		return nil, fmt.Errorf("load transcript for context: %w", err)
	}

	// Run the command/response loop.
	for {
		var cmd struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(cmdJSON, &cmd); err != nil {
			return nil, fmt.Errorf("parse assembly command: %w", err)
		}

		if cmd.Type == "done" {
			var result struct {
				Type            string   `json:"type"`
				EstimatedTokens int      `json:"estimatedTokens"`
				SelectedIDs     []string `json:"selectedItemIds"`
				SummaryCount    int      `json:"summaryCount"`
			}
			if err := json.Unmarshal(cmdJSON, &result); err != nil {
				return nil, fmt.Errorf("parse assembly result: %w", err)
			}

			selected := selectMessagesByIDs(allMsgs, result.SelectedIDs)
			return &AssemblyResult{
				Messages:        transcriptToMessages(selected),
				EstimatedTokens: result.EstimatedTokens,
				TotalMessages:   total,
				WasCompacted:    result.SummaryCount > 0,
			}, nil
		}

		response, err := handleAssemblyCommand(cmdJSON, allMsgs)
		if err != nil {
			return nil, fmt.Errorf("handle assembly command: %w", err)
		}

		respJSON, err := json.Marshal(response)
		if err != nil {
			return nil, fmt.Errorf("marshal assembly response: %w", err)
		}

		cmdJSON, err = ffi.ContextAssemblyStep(handle, respJSON)
		if err != nil {
			return nil, fmt.Errorf("context assembly step: %w", err)
		}
	}
}

// handleAssemblyCommand processes a command from the Rust engine and returns a response.
func handleAssemblyCommand(cmdJSON json.RawMessage, msgs []ChatMessage) (any, error) {
	var cmd struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(cmdJSON, &cmd); err != nil {
		return nil, err
	}

	switch cmd.Type {
	case "fetchContextItems":
		items := make([]map[string]any, len(msgs))
		for i, msg := range msgs {
			items[i] = map[string]any{
				"ordinal":     i,
				"itemType":    "message",
				"messageId":   i,
				"tokenCount":  estimateTokens(msg.Content),
				"depth":       0,
				"isCondensed": false,
			}
		}
		return map[string]any{
			"type":  "contextItems",
			"items": items,
		}, nil

	default:
		return map[string]any{"type": "empty"}, nil
	}
}

// selectMessagesByIDs picks messages matching "msg_{index}" IDs from the assembly result.
func selectMessagesByIDs(msgs []ChatMessage, ids []string) []ChatMessage {
	if len(ids) == 0 {
		return msgs
	}
	idxSet := make(map[int]bool, len(ids))
	for _, id := range ids {
		var idx int
		if _, err := fmt.Sscanf(id, "msg_%d", &idx); err == nil {
			idxSet[idx] = true
		}
	}
	if len(idxSet) == 0 {
		return msgs
	}
	selected := make([]ChatMessage, 0, len(idxSet))
	for i, msg := range msgs {
		if idxSet[i] {
			selected = append(selected, msg)
		}
	}
	return selected
}

// assembleContextFallback loads the most recent messages up to MaxMessages.
func assembleContextFallback(
	store TranscriptStore,
	sessionKey string,
	cfg ContextConfig,
) (*AssemblyResult, error) {
	limit := cfg.MaxMessages
	if limit <= 0 {
		limit = defaultMaxMessages
	}
	msgs, total, err := store.Load(sessionKey, limit)
	if err != nil {
		return nil, fmt.Errorf("load transcript: %w", err)
	}
	return &AssemblyResult{
		Messages:      transcriptToMessages(msgs),
		TotalMessages: total,
	}, nil
}

// transcriptToMessages converts ChatMessage transcript entries to LLM messages.
// System prompt is injected via ChatRequest.System, not as a message here.
func transcriptToMessages(transcript []ChatMessage) []llm.Message {
	msgs := make([]llm.Message, 0, len(transcript))
	for _, t := range transcript {
		role := t.Role
		if role == "" {
			role = "user"
		}
		msgs = append(msgs, llm.NewTextMessage(role, t.Content))
	}
	return msgs
}
