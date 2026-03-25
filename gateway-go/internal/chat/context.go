package chat

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
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
		TokenBudget:    100_000,
		FreshTailCount: 32,
		MaxMessages:    100,
	}
}

// assembleContext selects transcript messages that fit within the token budget.
// Uses Rust FFI context engine when available; falls back to simple tail-N.
func assembleContext(
	store TranscriptStore,
	sessionKey string,
	systemPrompt string,
	cfg ContextConfig,
	logger *slog.Logger,
) (*AssemblyResult, error) {
	if ffi.Available {
		return assembleContextFFI(store, sessionKey, systemPrompt, cfg, logger)
	}
	return assembleContextFallback(store, sessionKey, systemPrompt, cfg)
}

// assembleContextFFI uses the Rust context engine for token-budgeted selection.
func assembleContextFFI(
	store TranscriptStore,
	sessionKey string,
	systemPrompt string,
	cfg ContextConfig,
	logger *slog.Logger,
) (*AssemblyResult, error) {
	// For single-user deployment, use a fixed conversation ID derived from session.
	// The actual conversation ID only matters for DB lookups in the FFI engine.
	var conversationID uint64 = 1

	handle, err := ffi.ContextAssemblyNew(conversationID, cfg.TokenBudget, cfg.FreshTailCount)
	if err != nil {
		logger.Warn("context assembly FFI unavailable, falling back", "error", err)
		return assembleContextFallback(store, sessionKey, systemPrompt, cfg)
	}
	defer ffi.ContextEngineDrop(handle)

	// Start the engine — get the first command.
	cmdJSON, err := ffi.ContextAssemblyStart(handle)
	if err != nil {
		return nil, fmt.Errorf("context assembly start: %w", err)
	}

	// Load transcript for the FFI engine's data requests.
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
			// Parse final result.
			var result struct {
				Type            string   `json:"type"`
				EstimatedTokens int      `json:"estimatedTokens"`
				SelectedIDs     []string `json:"selectedItemIds"`
				SummaryCount    int      `json:"summaryCount"`
			}
			if err := json.Unmarshal(cmdJSON, &result); err != nil {
				return nil, fmt.Errorf("parse assembly result: %w", err)
			}

			// Select messages by index (IDs are "msg_{index}" format).
			selected := selectMessagesByIDs(allMsgs, result.SelectedIDs)
			messages := transcriptToMessages(selected, systemPrompt)

			return &AssemblyResult{
				Messages:        messages,
				EstimatedTokens: result.EstimatedTokens,
				TotalMessages:   total,
				WasCompacted:    result.SummaryCount > 0,
			}, nil
		}

		// Build response for the command.
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
		// Return all messages as context items.
		items := make([]map[string]any, len(msgs))
		for i, msg := range msgs {
			// Rough token estimate: ~4 chars per token.
			tokenCount := len(msg.Content) / 4
			if tokenCount < 1 {
				tokenCount = 1
			}
			items[i] = map[string]any{
				"ordinal":     i,
				"itemType":    "message",
				"messageId":   i,
				"tokenCount":  tokenCount,
				"depth":       0,
				"isCondensed": false,
			}
		}
		return map[string]any{
			"type":  "contextItems",
			"items": items,
		}, nil

	default:
		// Unknown command — return empty response to let the engine proceed.
		return map[string]any{"type": "empty"}, nil
	}
}

// selectMessagesByIDs picks messages matching "msg_{index}" IDs from the assembly result.
func selectMessagesByIDs(msgs []ChatMessage, ids []string) []ChatMessage {
	if len(ids) == 0 {
		return msgs
	}
	// Build index set.
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
// Used when FFI is unavailable.
func assembleContextFallback(
	store TranscriptStore,
	sessionKey string,
	systemPrompt string,
	cfg ContextConfig,
) (*AssemblyResult, error) {
	limit := cfg.MaxMessages
	if limit <= 0 {
		limit = 100
	}
	msgs, total, err := store.Load(sessionKey, limit)
	if err != nil {
		return nil, fmt.Errorf("load transcript: %w", err)
	}

	messages := transcriptToMessages(msgs, systemPrompt)
	return &AssemblyResult{
		Messages:      messages,
		TotalMessages: total,
	}, nil
}

// transcriptToMessages converts ChatMessage transcript entries to LLM messages,
// prepending a system prompt if provided.
func transcriptToMessages(transcript []ChatMessage, systemPrompt string) []llm.Message {
	msgs := make([]llm.Message, 0, len(transcript)+1)
	// System prompt is set via ChatRequest.System, not as a message.
	// Just convert transcript entries directly.
	_ = systemPrompt
	for _, t := range transcript {
		role := t.Role
		if role == "" {
			role = "user"
		}
		msgs = append(msgs, llm.NewTextMessage(role, t.Content))
	}
	return msgs
}
