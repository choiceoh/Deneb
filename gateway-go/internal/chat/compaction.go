package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/pilot"
	compaction2 "github.com/choiceoh/deneb/gateway-go/internal/chat/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// Compaction defaults.
const (
	defaultContextThreshold = 0.75
	// proactiveCompactionCooldown is the minimum interval between proactive sweeps.
	// Prevents repeated LLM summarization calls on consecutive messages.
	proactiveCompactionCooldown = 5 * time.Minute
)

// proactiveCompaction tracks cooldown and in-flight status for proactive compaction.
// The sweep modifies the Aurora DB (creates summaries, replaces context_items);
// subsequent requests benefit automatically via normal assembly — no message
// caching needed.
var proactiveCompaction struct {
	lastRun        atomic.Int64 // epoch millis of last completed sweep
	running        atomic.Bool  // prevents concurrent sweeps
	circuitBreaker *compaction2.CompactionCircuitBreaker
}

func init() {
	proactiveCompaction.circuitBreaker = compaction2.NewCompactionCircuitBreaker()
}

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

// triggerProactiveCompaction fires a background Aurora sweep if stored tokens
// exceed the compaction threshold. The sweep writes summaries into the Aurora DB;
// subsequent requests benefit automatically via normal assembly. The current
// request continues with its already-assembled context (no blocking).
func triggerProactiveCompaction(
	shutdownCtx context.Context,
	deps runDeps,
	params RunParams,
	client *llm.Client,
	logger *slog.Logger,
) {
	if deps.auroraStore == nil {
		return
	}

	// Circuit breaker: skip if compaction has failed too many times consecutively.
	if proactiveCompaction.circuitBreaker.IsTripped() {
		logger.Debug("proactive compaction: circuit breaker tripped, skipping")
		return
	}

	// Cooldown: skip if a sweep completed recently.
	if lastMs := proactiveCompaction.lastRun.Load(); lastMs > 0 {
		if time.Since(time.UnixMilli(lastMs)) < proactiveCompactionCooldown {
			return
		}
	}

	// Threshold check.
	storedTokens, err := deps.auroraStore.FetchTokenCount(1)
	if err != nil || storedTokens == 0 {
		return
	}
	threshold := uint64(deps.compactionCfg.ContextThreshold * float64(deps.contextCfg.TokenBudget))
	if storedTokens <= threshold {
		return
	}

	// Prevent concurrent sweeps.
	if !proactiveCompaction.running.CompareAndSwap(false, true) {
		return
	}

	logger.Info("proactive compaction: stored tokens exceed threshold, starting background sweep",
		"storedTokens", storedTokens,
		"threshold", threshold,
		"budget", deps.contextCfg.TokenBudget,
	)

	// Use shutdownCtx (server lifecycle) instead of request ctx so the sweep
	// survives after the current request completes.
	go func() {
		defer proactiveCompaction.running.Store(false)

		// Only the sweep matters — we discard the reassembled messages because
		// the next request's normal assembly will pick up the new summaries.
		_, _, compErr := handleContextOverflowAurora(
			shutdownCtx, deps, params, client, logger,
		)
		if compErr != nil {
			tripped := proactiveCompaction.circuitBreaker.RecordFailure()
			logger.Warn("proactive compaction: sweep failed",
				"error", compErr,
				"consecutiveFailures", proactiveCompaction.circuitBreaker.ConsecutiveFailures(),
				"circuitTripped", tripped)
			return
		}
		proactiveCompaction.circuitBreaker.RecordSuccess()
		proactiveCompaction.lastRun.Store(time.Now().UnixMilli())
		logger.Info("proactive compaction: background sweep completed, next assembly will include summaries")
	}()
}

// midLoopCompactionThreshold is the fraction of the token budget at which
// mid-loop compaction triggers. Higher than proactive (0.75) to avoid
// unnecessary compaction, but low enough to prevent context_length_exceeded.
const midLoopCompactionThreshold = 0.80

// estimateMessagesTokens returns a rough token count for an entire message history.
func estimateMessagesTokens(messages []llm.Message) int {
	total := 0
	for _, msg := range messages {
		// Each message has ~4 tokens of overhead (role, delimiters).
		total += 4
		total += estimateTokens(string(msg.Content))
	}
	return total
}

// buildMidLoopCompactor returns an OnMidLoopCompact callback that evaluates
// context size after each tool turn and compacts proactively. The strategy is:
//
//  1. Microcompact (prune old tool results) — free, no LLM call.
//  2. Strip image blocks from older messages.
//  3. If still over threshold, sync to Aurora and run full compaction sweep.
//
// This prevents the 3-5 minute context exhaustion pattern where the agent
// accumulates tool results during the loop with no compaction checks.
func buildMidLoopCompactor(
	deps runDeps,
	params RunParams,
	logger *slog.Logger,
) func(ctx context.Context, turn int, messages []llm.Message, accTokens int) ([]llm.Message, string, error) {
	budget := deps.contextCfg.TokenBudget
	threshold := uint64(midLoopCompactionThreshold * float64(budget))

	// Track last compaction turn to avoid compacting on consecutive turns.
	var lastCompactTurn int = -10
	// Track the number of messages already synced to Aurora to avoid duplicates.
	var auroraSyncedCount int

	return func(ctx context.Context, turn int, messages []llm.Message, accTokens int) ([]llm.Message, string, error) {
		// Skip if we just compacted recently (within 2 turns).
		if turn-lastCompactTurn < 2 {
			return nil, "", nil
		}

		// Estimate current context size.
		liveTokens := estimateMessagesTokens(messages)
		if uint64(liveTokens) < threshold {
			return nil, "", nil
		}

		logger.Info("mid-loop compaction: token threshold exceeded",
			"turn", turn,
			"liveTokens", liveTokens,
			"threshold", threshold,
			"budget", budget,
		)

		// Step 1: Microcompact (prune old tool results — zero cost).
		mcMessages, mcResult := compaction2.MicrocompactMessages(messages, time.Now())
		if mcResult.PrunedCount > 0 {
			messages = mcMessages
			liveTokens -= mcResult.EstimatedSaved
			logger.Info("mid-loop compaction: microcompact applied",
				"pruned", mcResult.PrunedCount,
				"savedTokens", mcResult.EstimatedSaved,
				"remainingTokens", liveTokens,
			)
			if uint64(liveTokens) < threshold {
				lastCompactTurn = turn
				return messages, "", nil
			}
		}

		// Step 2: Strip base64 image blocks from all but the last 2 messages.
		messages = compaction2.StripImageBlocks(messages)
		liveTokens = estimateMessagesTokens(messages)
		if uint64(liveTokens) < threshold {
			lastCompactTurn = turn
			return messages, "", nil
		}

		// Step 3: Aurora compaction sweep (uses lightweight local LLM for summaries).
		if deps.auroraStore == nil {
			// No Aurora store — return microcompacted messages as best effort.
			lastCompactTurn = turn
			if mcResult.PrunedCount > 0 {
				return messages, "", nil
			}
			return nil, "", nil
		}

		// Sync only new messages to Aurora (avoid duplicates).
		if len(messages) > auroraSyncedCount {
			syncMessagesToAurora(deps.auroraStore, messages[auroraSyncedCount:], logger)
			auroraSyncedCount = len(messages)
		}

		compactedMsgs, sysAddition, err := handleContextOverflowAurora(
			ctx, deps, params, deps.llmClient, logger,
		)
		if err != nil {
			logger.Warn("mid-loop compaction: aurora sweep failed, using microcompact result",
				"error", err)
			lastCompactTurn = turn
			// Return microcompacted messages as fallback.
			if mcResult.PrunedCount > 0 {
				return messages, "", nil
			}
			return nil, "", nil
		}

		lastCompactTurn = turn
		auroraSyncedCount = 0 // Reset: compaction may have changed Aurora state.
		logger.Info("mid-loop compaction: aurora sweep completed",
			"turn", turn,
			"beforeTokens", liveTokens,
			"afterMsgs", len(compactedMsgs),
		)
		return compactedMsgs, sysAddition, nil
	}
}

// syncMessagesToAurora persists in-memory messages to the Aurora store so that
// the compaction sweep has up-to-date data.
func syncMessagesToAurora(store *aurora.Store, messages []llm.Message, logger *slog.Logger) {
	for _, msg := range messages {
		if msg.Role != "assistant" && msg.Role != "user" {
			continue
		}
		content := string(msg.Content)
		tokenCount := uint64(estimateTokens(content))
		if _, err := store.SyncMessage(1, msg.Role, content, tokenCount); err != nil {
			logger.Warn("mid-loop aurora sync failed", "role", msg.Role, "error", err)
		}
	}
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

		// Use lightweight model for cost-efficient compaction summaries.
		lwClient := pilot.GetLightweightClient()
		lwModel := pilot.GetLightweightModel()
		summarizer := aurora.NewLLMSummarizer(lwClient, lwModel)

		// Build inline fact extractor: replaces the async flushMemory/transferSummary
		// bridge with synchronous extraction during the sweep persist step.
		// Facts are extracted from condensed summaries (depth >= 1) and stored
		// directly in the memory store. Extraction failure is non-fatal.
		var factExtractor aurora.FactExtractor
		if deps.memoryStore != nil {
			factExtractor = func(summaryContent string, depth uint32) error {
				// Estimate token count using rune-based divisor consistent with
				// estimateTokens() (runesPerToken=2, calibrated for Korean).
				estimatedTokens := uint64(len([]rune(summaryContent)) / runesPerToken)
				summary := aurora.SummaryRecord{
					Content:    summaryContent,
					Depth:      depth,
					Kind:       "condensed",
					TokenCount: estimatedTokens,
				}
				if !aurora.ShouldTransfer(summary, aurora.DefaultMemoryTransferConfig()) {
					logger.Debug("aurora-transfer: summary below transfer threshold",
						"depth", depth, "tokens", estimatedTokens)
					return nil
				}
				return aurora.TransferSummaryToMemory(
					ctx,
					summary,
					deps.auroraStore,
					deps.memoryStore,
					deps.memoryEmbedder,
					lwClient, lwModel,
					logger,
				)
			}
		}

		result, err := aurora.RunSweep(
			deps.auroraStore,
			1, // single-user conversation ID
			deps.contextCfg.TokenBudget,
			sweepCfg,
			summarizer,
			true, // force (overflow already detected)
			true, // hard trigger
			logger,
			factExtractor,
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
				if runeText := []rune(text); len(runeText) > 200 {
					text = string(runeText[:200]) + "..."
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
