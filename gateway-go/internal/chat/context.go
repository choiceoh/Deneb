package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/tokenutil"
	"github.com/choiceoh/deneb/gateway-go/internal/embedding"
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

// AssemblyHints provides optional hints for smarter context assembly.
// When both QueryText and Embedder are set, the fallback path uses
// semantic ranking instead of simple tail-N truncation.
type AssemblyHints struct {
	QueryText string             // current user message for semantic ranking
	Embedder  embedding.Embedder // optional; enables semantic ranking
}

// Semantic ranking weights and limits.
const (
	semanticWeight    = 0.6              // cosine similarity contribution to final score
	recencyWeight     = 0.4              // linear recency decay contribution
	embedBatchTimeout = 3 * time.Second  // timeout for batch embedding call
)

// estimateTokens returns a rough token count for a string.
// Delegates to tokenutil.EstimateTokens (shared across chat subsystem).
func estimateTokens(s string) int {
	return tokenutil.EstimateTokens(s)
}

// assembleContext selects transcript messages that fit within the token budget.
// Uses Rust FFI context engine when available; falls back to simple tail-N
// (or semantic ranking when AssemblyHints provides an embedder).
func assembleContext(
	store TranscriptStore,
	sessionKey string,
	cfg ContextConfig,
	logger *slog.Logger,
	hints ...AssemblyHints,
) (*AssemblyResult, error) {
	if ffi.Available {
		return assembleContextFFI(store, sessionKey, cfg, logger)
	}
	var h AssemblyHints
	if len(hints) > 0 {
		h = hints[0]
	}
	return assembleContextFallback(store, sessionKey, cfg, h, logger)
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
		return assembleContextFallback(store, sessionKey, cfg, AssemblyHints{}, logger)
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
// When hints provide an embedder and query text, and the transcript exceeds
// MaxMessages, it uses semantic ranking to select the most relevant older
// messages rather than simple tail-N truncation.
func assembleContextFallback(
	store TranscriptStore,
	sessionKey string,
	cfg ContextConfig,
	hints AssemblyHints,
	logger *slog.Logger,
) (*AssemblyResult, error) {
	limit := cfg.MaxMessages
	if limit <= 0 {
		limit = defaultMaxMessages
	}

	canRank := hints.Embedder != nil && hints.QueryText != ""

	// When semantic ranking is available, load all messages so we can rank
	// beyond the tail-N window. Otherwise, load only the tail for efficiency.
	loadLimit := limit
	if canRank {
		loadLimit = 0 // load entire transcript
	}
	msgs, total, err := store.Load(sessionKey, loadLimit)
	if err != nil {
		return nil, fmt.Errorf("load transcript: %w", err)
	}

	// If the transcript exceeds the budget and semantic ranking is available,
	// rank older messages by relevance instead of discarding them.
	freshTail := int(cfg.FreshTailCount)
	if freshTail <= 0 {
		freshTail = defaultFreshTailCount
	}
	if canRank && len(msgs) > limit {
		ranked, err := semanticRankMessages(msgs, limit, freshTail, hints, logger)
		if err != nil {
			// Graceful fallback: log and use tail-N.
			if logger != nil {
				logger.Debug("semantic ranking failed, using tail-N fallback", "error", err)
			}
		} else {
			msgs = ranked
		}
	}

	// Standard tail-N truncation for messages still exceeding the limit
	// (either semantic ranking was not available, or it already trimmed).
	if len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}

	return &AssemblyResult{
		Messages:      transcriptToMessages(msgs),
		TotalMessages: total,
	}, nil
}

// semanticRankMessages selects the most relevant messages using embedding similarity.
// It protects the freshTail most recent messages and ranks older messages by a
// weighted combination of cosine similarity (0.6) and linear recency (0.4).
func semanticRankMessages(
	msgs []ChatMessage,
	budget int,
	freshTail int,
	hints AssemblyHints,
	logger *slog.Logger,
) ([]ChatMessage, error) {
	if freshTail >= budget {
		freshTail = budget
	}
	if freshTail >= len(msgs) {
		// All messages fit in the fresh tail — nothing to rank.
		return msgs, nil
	}

	// Split: older candidates vs protected fresh tail.
	olderCount := len(msgs) - freshTail
	olderMsgs := msgs[:olderCount]
	freshMsgs := msgs[olderCount:]

	// How many older messages can we keep?
	olderBudget := budget - freshTail
	if olderBudget <= 0 {
		return freshMsgs, nil
	}
	if olderBudget >= len(olderMsgs) {
		// All older messages fit — no ranking needed.
		return msgs, nil
	}

	// Collect text from older messages for batch embedding.
	texts := make([]string, len(olderMsgs))
	for i, m := range olderMsgs {
		texts[i] = m.TextContent()
	}

	// Build the batch: query + all older message texts.
	batchTexts := make([]string, 0, 1+len(texts))
	batchTexts = append(batchTexts, hints.QueryText)
	batchTexts = append(batchTexts, texts...)

	// Embed with a 3-second timeout.
	ctx, cancel := context.WithTimeout(context.Background(), embedBatchTimeout)
	defer cancel()

	vectors, err := hints.Embedder.EmbedBatch(ctx, batchTexts)
	if err != nil {
		return nil, fmt.Errorf("batch embed for semantic ranking: %w", err)
	}
	if len(vectors) != len(batchTexts) {
		return nil, fmt.Errorf("embed batch returned %d vectors, expected %d", len(vectors), len(batchTexts))
	}

	queryVec := vectors[0]
	msgVecs := vectors[1:]

	// Score each older message: 0.6 * cosine_similarity + 0.4 * recency_weight.
	// Recency decays linearly from 1.0 (most recent older message) to 0.0 (oldest).
	type scored struct {
		index int
		score float64
	}
	scores := make([]scored, len(olderMsgs))
	for i := range olderMsgs {
		sim := vecCosineSimilarity(queryVec, msgVecs[i])
		// Linear recency: index 0 is oldest (weight ~0), index olderCount-1 is
		// the most recent older message (weight ~1).
		recency := 0.0
		if olderCount > 1 {
			recency = float64(i) / float64(olderCount-1)
		} else {
			recency = 1.0
		}
		scores[i] = scored{
			index: i,
			score: semanticWeight*sim + recencyWeight*recency,
		}
	}

	// Sort descending by score to pick top-K.
	sort.Slice(scores, func(a, b int) bool {
		return scores[a].score > scores[b].score
	})

	// Select top olderBudget indices, then re-sort by original index
	// to preserve chronological order.
	selected := scores[:olderBudget]
	sort.Slice(selected, func(a, b int) bool {
		return selected[a].index < selected[b].index
	})

	// Merge: selected older messages (chronological) + fresh tail (in order).
	result := make([]ChatMessage, 0, olderBudget+freshTail)
	for _, s := range selected {
		result = append(result, olderMsgs[s.index])
	}
	result = append(result, freshMsgs...)

	if logger != nil {
		logger.Debug("semantic ranking applied",
			"total_older", len(olderMsgs),
			"selected_older", olderBudget,
			"fresh_tail", len(freshMsgs),
			"top_score", scores[0].score,
		)
	}

	return result, nil
}

// vecCosineSimilarity computes cosine similarity between two float32 vectors.
// Duplicated from memory/store.go since that function is unexported.
func vecCosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
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
