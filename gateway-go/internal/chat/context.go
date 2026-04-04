package chat

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/tokenutil"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// Context assembly defaults.
const (
	defaultMemoryTokenBudget  = 30_000  // Aurora transcript history injection limit
	defaultLiveTokenBudget    = 120_000 // total agent loop token budget (system + tools + memory + live messages)
	defaultSystemPromptBudget = 30_000
	defaultFreshTailCount     = 48
	defaultMaxMessages        = 100
	// runesPerToken re-exports the shared constant for local callers
	// (compaction, sweep, etc.) that use it directly in math expressions.
	runesPerToken = tokenutil.RunesPerToken
	// charsPerToken is the byte-based divisor used by knowledge.go for
	// incremental budget tracking on strings.Builder output (sb.Len() bytes).
	// English: ~4 bytes/token (1 byte/char × 4 chars/token) — accurate.
	// Korean: ~6 bytes/token (3 bytes/rune × 2 runes/token) — underestimates,
	// so knowledge budget fills faster; acceptable conservative margin.
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
	MemoryTokenBudget  uint64 // max tokens for Aurora context (transcript history)
	LiveTokenBudget    uint64 // total agent loop token budget (system + tools + memory + live)
	SystemPromptBudget uint64 // max tokens for system prompt fragments
	FreshTailCount     uint32 // messages protected from eviction
	MaxMessages        int    // fallback limit when FFI unavailable
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
// Delegates to tokenutil.EstimateTokens (shared across chat subsystem).
func estimateTokens(s string) int {
	return tokenutil.EstimateTokens(s)
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

	handle, err := ffi.ContextAssemblyNew(conversationID, cfg.MemoryTokenBudget, cfg.FreshTailCount)
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

	// Respect MaxMessages limit: only feed the tail N messages to the engine.
	if cfg.MaxMessages > 0 && len(allMsgs) > cfg.MaxMessages {
		allMsgs = allMsgs[len(allMsgs)-cfg.MaxMessages:]
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
			var envelope struct {
				Result struct {
					EstimatedTokens int      `json:"estimatedTokens"`
					SelectedIDs     []string `json:"selectedItemIds"`
					SummaryCount    int      `json:"summaryCount"`
				} `json:"result"`
			}
			if err := json.Unmarshal(cmdJSON, &envelope); err != nil {
				return nil, fmt.Errorf("parse assembly result: %w", err)
			}
			result := envelope.Result

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
				"tokenCount":  estimateTokens(msg.TextContent()),
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
