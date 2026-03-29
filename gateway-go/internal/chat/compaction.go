package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
)

// Compaction defaults.
const (
	defaultContextThreshold = 0.85
	// summarizationModel — local sglang Qwen for cost-efficient compaction summaries.
	summarizationModel = "Qwen/Qwen3.5-35B-A3B"
)

// CompactionConfig configures compaction behavior.
type CompactionConfig struct {
	ContextThreshold float64 `json:"contextThreshold"` // fraction of budget (default 0.75)
	FreshTailCount   int     `json:"freshTailCount"`   // messages to protect (default 32)
}

// DefaultCompactionConfig returns sensible defaults.
func DefaultCompactionConfig() CompactionConfig {
	return CompactionConfig{
		ContextThreshold: defaultContextThreshold,
		FreshTailCount:   defaultFreshTailCount,
	}
}

// CompactionDecision is the parsed result from compaction evaluation.
type CompactionDecision struct {
	ShouldCompact bool   `json:"shouldCompact"`
	Reason        string `json:"reason"`
	CurrentTokens uint64 `json:"currentTokens"`
	Threshold     uint64 `json:"threshold"`
}

// evaluateCompaction checks whether context compaction is needed.
func evaluateCompaction(cfg CompactionConfig, storedTokens, liveTokens, tokenBudget uint64) (*CompactionDecision, error) {
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal compaction config: %w", err)
	}

	resultJSON, err := ffi.CompactionEvaluate(string(configJSON), storedTokens, liveTokens, tokenBudget)
	if err != nil {
		return nil, fmt.Errorf("compaction evaluate: %w", err)
	}

	var decision CompactionDecision
	if err := json.Unmarshal(resultJSON, &decision); err != nil {
		return nil, fmt.Errorf("parse compaction decision: %w", err)
	}
	return &decision, nil
}

// handleContextOverflowAurora handles context overflow using the Aurora compaction system.
// When the Aurora store is available, it runs a full hierarchical sweep via Rust FFI.
// Falls back to legacy transcript-based compaction otherwise.
// Returns the compacted messages, an optional system prompt addition from Aurora
// (guidance text for the LLM about summarized context), and any error.
func handleContextOverflowAurora(
	ctx context.Context,
	deps runDeps,
	params RunParams,
	llmClient *llm.Client,
	logger *slog.Logger,
) ([]llm.Message, string, error) {
	// Try Aurora compaction first.
	if deps.auroraStore != nil {
		logger.Info("aurora: running compaction sweep on overflow")
		sweepCfg := aurora.DefaultSweepConfig()
		sweepCfg.ContextThreshold = deps.compactionCfg.ContextThreshold
		sweepCfg.FreshTailCount = uint32(deps.compactionCfg.FreshTailCount)

		// Flush important facts to memory before compacting messages away.
		// Runs synchronously so facts are saved before Aurora summarizes them.
		if deps.memoryStore != nil {
			flushMemoryBeforeCompaction(ctx, deps.auroraStore, deps.memoryStore, deps.memoryEmbedder,
				deps.compactionCfg.FreshTailCount, logger)
		}

		// Use local sglang for cost-efficient compaction summaries.
		sglangClient := getSglangClient()
		summarizer := aurora.NewLLMSummarizer(sglangClient, summarizationModel, "openai")

		result, err := aurora.RunSweep(
			deps.auroraStore,
			1, // single-user conversation ID
			deps.contextCfg.TokenBudget,
			sweepCfg,
			summarizer,
			true, // force (overflow already detected)
			true, // hard trigger
			logger,
		)
		if err != nil {
			logger.Warn("aurora sweep failed, falling back", "error", err)
		} else if result != nil && result.ActionTaken {
			// Reassemble context from Aurora store.
			asmCfg := aurora.AssemblyConfig{
				TokenBudget:    deps.contextCfg.TokenBudget,
				FreshTailCount: deps.contextCfg.FreshTailCount,
				MaxMessages:    deps.contextCfg.MaxMessages,
			}
			asmResult, err := aurora.Assemble(deps.auroraStore, 1, asmCfg, logger)
			if err != nil {
				return nil, "", fmt.Errorf("aurora reassemble after compaction: %w", err)
			}
			return asmResult.Messages, asmResult.SystemPromptAddition, nil
		}
	}

	// Legacy fallback: use transcript store directly.
	msgs, err := handleContextOverflowLegacy(
		deps.transcript, params.SessionKey,
		deps.contextCfg, deps.compactionCfg, logger,
	)
	return msgs, "", err
}

// handleContextOverflowLegacy is the original overflow handler using transcript-based compaction.
func handleContextOverflowLegacy(
	store TranscriptStore,
	sessionKey string,
	ctxCfg ContextConfig,
	compCfg CompactionConfig,
	logger *slog.Logger,
) ([]llm.Message, error) {
	// Estimate stored tokens as 120% of budget — accounts for messages that
	// arrived between the last compaction and the current overflow.
	estimatedStored := ctxCfg.TokenBudget + ctxCfg.TokenBudget/5
	decision, err := evaluateCompaction(compCfg, estimatedStored, estimatedStored, ctxCfg.TokenBudget)
	if err != nil {
		logger.Warn("compaction evaluation failed", "error", err)
	}

	if decision != nil && decision.ShouldCompact {
		swept, err := runCompactionSweepLegacy(store, sessionKey, compCfg, ctxCfg.TokenBudget, logger)
		if err != nil {
			logger.Warn("compaction sweep failed", "error", err)
		}
		if swept {
			result, err := assembleContext(store, sessionKey, ctxCfg, logger)
			if err != nil {
				return nil, fmt.Errorf("reassemble after compaction: %w", err)
			}
			return result.Messages, nil
		}
	}

	// Fallback: halve the context window and message limit to fit within the
	// LLM's context. This is a last resort when compaction didn't free enough space.
	reducedCfg := ctxCfg
	reducedCfg.TokenBudget /= 2
	if reducedCfg.MaxMessages > 10 {
		reducedCfg.MaxMessages /= 2
	}
	result, err := assembleContext(store, sessionKey, reducedCfg, logger)
	if err != nil {
		return nil, fmt.Errorf("reassemble with reduced budget: %w", err)
	}
	return result.Messages, nil
}

// runCompactionSweepLegacy is the original stub-based sweep for backward compatibility.
func runCompactionSweepLegacy(
	store TranscriptStore,
	sessionKey string,
	cfg CompactionConfig,
	tokenBudget uint64,
	logger *slog.Logger,
) (bool, error) {
	if !ffi.Available {
		logger.Info("compaction sweep skipped: FFI unavailable")
		return false, nil
	}

	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return false, fmt.Errorf("marshal compaction config: %w", err)
	}

	var conversationID uint64 = 1
	nowMs := time.Now().UnixMilli()

	handle, err := ffi.CompactionSweepNew(
		string(configJSON), conversationID, tokenBudget,
		false, true, nowMs,
	)
	if err != nil {
		return false, fmt.Errorf("create compaction sweep: %w", err)
	}
	defer ffi.CompactionSweepDrop(handle)

	cmdJSON, err := ffi.CompactionSweepStart(handle)
	if err != nil {
		return false, fmt.Errorf("start compaction sweep: %w", err)
	}

	allMsgs, _, err := store.Load(sessionKey, 0)
	if err != nil {
		return false, fmt.Errorf("load transcript for sweep: %w", err)
	}

	// Pull-based coroutine protocol with the Rust compaction state machine:
	// 1. Rust yields a SweepCommand (e.g., FetchMessages, Summarize).
	// 2. Go executes the command and marshals a SweepResponse.
	// 3. Go feeds the response back via CompactionSweepStep, which returns the next command.
	// 4. Repeat until Rust yields a "done" command with the final result.
	for {
		var cmd struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(cmdJSON, &cmd); err != nil {
			return false, fmt.Errorf("parse sweep command: %w", err)
		}

		if cmd.Type == "done" {
			logger.Info("compaction sweep completed", "session", sessionKey)
			var doneCmd struct {
				Result struct {
					ActionTaken bool `json:"actionTaken"`
				} `json:"result"`
			}
			if err := json.Unmarshal(cmdJSON, &doneCmd); err == nil {
				return doneCmd.Result.ActionTaken, nil
			}
			return false, nil
		}

		response, err := handleSweepCommandLegacy(cmdJSON, allMsgs, logger)
		if err != nil {
			return false, fmt.Errorf("handle sweep command: %w", err)
		}

		respJSON, err := json.Marshal(response)
		if err != nil {
			return false, fmt.Errorf("marshal sweep response: %w", err)
		}

		cmdJSON, err = ffi.CompactionSweepStep(handle, respJSON)
		if err != nil {
			return false, fmt.Errorf("compaction sweep step: %w", err)
		}
	}
}

// flushMemoryBeforeCompaction extracts and persists important facts from the messages
// that are about to be compressed by the Aurora compaction sweep. This ensures
// high-value information survives summarization in long-term structured memory.
//
// Only messages outside the fresh tail are considered — those are the ones that
// will actually be summarized away. Errors are logged and silently ignored so
// they never block the compaction path.
func flushMemoryBeforeCompaction(
	ctx context.Context,
	store *aurora.Store,
	memStore *memory.Store,
	embedder *memory.Embedder,
	freshTailCount int,
	logger *slog.Logger,
) {
	const (
		conversationID = uint64(1)
		flushTimeout   = 35 * time.Second
		// maxFlushTokens is an approximate character budget for the combined text
		// sent to the extraction LLM (user 4000 + assistant 8000 from ExtractFacts).
		maxUserChars      = 3800
		maxAssistantChars = 7600
	)

	flushCtx, cancel := context.WithTimeout(ctx, flushTimeout)
	defer cancel()

	// Fetch all ordered context items for the conversation.
	items, err := store.FetchContextItems(conversationID)
	if err != nil {
		logger.Warn("memory flush: failed to fetch context items", "error", err)
		return
	}

	// Collect only raw message items (exclude summaries already persisted by past sweeps).
	type msgItem struct {
		ordinal uint64
		id      uint64
	}
	var msgItems []msgItem
	for _, ci := range items {
		if ci.ItemType == "message" && ci.MessageID != nil {
			msgItems = append(msgItems, msgItem{ordinal: ci.Ordinal, id: *ci.MessageID})
		}
	}
	sort.Slice(msgItems, func(i, j int) bool { return msgItems[i].ordinal < msgItems[j].ordinal })

	// Exclude the fresh tail — those messages won't be compacted.
	if len(msgItems) <= freshTailCount {
		logger.Debug("memory flush: all messages in fresh tail, nothing to flush")
		return
	}
	flushable := msgItems[:len(msgItems)-freshTailCount]

	// Fetch message records.
	ids := make([]uint64, len(flushable))
	for i, mi := range flushable {
		ids[i] = mi.id
	}
	records, err := store.FetchMessages(ids)
	if err != nil {
		logger.Warn("memory flush: failed to fetch messages", "error", err)
		return
	}

	// Build ordered user/assistant text blocks for the extraction prompt.
	// Walk in ordinal order to preserve conversation sequence.
	var userParts, assistantParts []string
	for _, mi := range flushable {
		rec, ok := records[mi.id]
		if !ok {
			continue
		}
		switch rec.Role {
		case "user":
			userParts = append(userParts, rec.Content)
		case "assistant":
			assistantParts = append(assistantParts, rec.Content)
		}
	}

	userText := strings.Join(userParts, "\n\n---\n\n")
	assistantText := strings.Join(assistantParts, "\n\n---\n\n")
	if userText == "" && assistantText == "" {
		return
	}

	// Trim to ExtractFacts budget so we don't overflow the extraction LLM.
	userRunes := []rune(userText)
	if len(userRunes) > maxUserChars {
		userText = string(userRunes[len(userRunes)-maxUserChars:]) // keep recent tail
	}
	assistantRunes := []rune(assistantText)
	if len(assistantRunes) > maxAssistantChars {
		assistantText = string(assistantRunes[len(assistantRunes)-maxAssistantChars:])
	}

	logger.Info("memory flush: extracting facts before compaction",
		"flushableMessages", len(flushable),
		"userChars", len([]rune(userText)),
		"assistantChars", len([]rune(assistantText)),
	)

	sglangClient := getSglangClient()
	facts, err := memory.ExtractFacts(flushCtx, sglangClient, sglangModel, userText, assistantText, logger)
	if err != nil {
		logger.Warn("memory flush: extraction failed", "error", err)
		return
	}
	if len(facts) == 0 {
		logger.Debug("memory flush: no facts extracted")
		return
	}

	memory.InsertExtractedFacts(flushCtx, memStore, embedder, facts, logger)
	logger.Info("memory flush: completed", "factsExtracted", len(facts))
}

// handleSweepCommandLegacy is the original stub handler (kept for no-Aurora fallback).
func handleSweepCommandLegacy(cmdJSON json.RawMessage, msgs []ChatMessage, logger *slog.Logger) (any, error) {
	var cmd struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(cmdJSON, &cmd); err != nil {
		return nil, err
	}

	switch cmd.Type {
	case "fetchCandidates":
		items := make([]map[string]any, len(msgs))
		for i, msg := range msgs {
			tokenCount := estimateTokens(msg.Content)
			items[i] = map[string]any{
				"ordinal":    i,
				"messageId":  i,
				"role":       msg.Role,
				"tokenCount": tokenCount,
				"timestamp":  msg.Timestamp,
			}
		}
		return map[string]any{
			"type":  "candidates",
			"items": items,
		}, nil

	case "summarize":
		var summarizeCmd struct {
			MessageIDs []int `json:"messageIds"`
		}
		if err := json.Unmarshal(cmdJSON, &summarizeCmd); err != nil {
			logger.Warn("failed to parse summarize command", "error", err)
		}

		var parts []string
		for _, id := range summarizeCmd.MessageIDs {
			if id >= 0 && id < len(msgs) {
				text := msgs[id].Content
				if len(text) > 200 {
					text = text[:200] + "..."
				}
				parts = append(parts, fmt.Sprintf("[%s] %s", msgs[id].Role, text))
			}
		}
		summary := strings.Join(parts, "\n")
		return map[string]any{
			"type":       "summary",
			"text":       summary,
			"tokenCount": estimateTokens(summary),
		}, nil

	default:
		return map[string]any{"type": "empty"}, nil
	}
}
