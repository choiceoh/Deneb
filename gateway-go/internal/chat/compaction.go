package chat

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/pilot"
	compaction2 "github.com/choiceoh/deneb/gateway-go/internal/chat/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// Compaction defaults.
const (
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
	cbOnce         sync.Once
	circuitBreaker *compaction2.CompactionCircuitBreaker
}

// getCompactionCircuitBreaker returns the lazily-initialized circuit breaker.
// Uses sync.Once to ensure thread-safe single initialization.
func getCompactionCircuitBreaker() *compaction2.CompactionCircuitBreaker {
	proactiveCompaction.cbOnce.Do(func() {
		proactiveCompaction.circuitBreaker = compaction2.NewCompactionCircuitBreaker()
	})
	return proactiveCompaction.circuitBreaker
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
	if getCompactionCircuitBreaker().IsTripped() {
		logger.Debug("proactive compaction: circuit breaker tripped, skipping")
		return
	}

	// Cooldown: skip if a sweep completed recently.
	if lastMs := proactiveCompaction.lastRun.Load(); lastMs > 0 {
		if time.Since(time.UnixMilli(lastMs)) < proactiveCompactionCooldown {
			return
		}
	}

	// Threshold check via Rust evaluation (single source of truth).
	storedTokens, err := deps.auroraStore.FetchTokenCount(1)
	if err != nil || storedTokens == 0 {
		return
	}
	shouldCompact, _, err := aurora.EvaluateCompaction(
		deps.compactionCfg, storedTokens, 0, deps.contextCfg.TokenBudget,
	)
	if err != nil || !shouldCompact {
		return
	}

	// Prevent concurrent sweeps.
	if !proactiveCompaction.running.CompareAndSwap(false, true) {
		return
	}

	logger.Info("proactive compaction: stored tokens exceed threshold, starting background sweep",
		"storedTokens", storedTokens,
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
			tripped := getCompactionCircuitBreaker().RecordFailure()
			logger.Warn("proactive compaction: sweep failed",
				"error", compErr,
				"consecutiveFailures", getCompactionCircuitBreaker().ConsecutiveFailures(),
				"circuitTripped", tripped)
			return
		}
		getCompactionCircuitBreaker().RecordSuccess()
		proactiveCompaction.lastRun.Store(time.Now().UnixMilli())
		logger.Info("proactive compaction: background sweep completed, next assembly will include summaries")
	}()
}

// midLoopCompactionThreshold is the fraction of the token budget at which
// mid-loop compaction triggers. Matches the proactive threshold (0.80) so
// that mid-loop checks are consistent with background compaction.
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

// handleContextOverflowAurora handles context overflow using the Aurora compaction system.
// Runs a full hierarchical sweep via Rust FFI.
// Returns the compacted messages, an optional system prompt addition from Aurora
// (guidance text for the LLM about summarized context), and any error.
func handleContextOverflowAurora(
	ctx context.Context,
	deps runDeps,
	params RunParams,
	llmClient *llm.Client,
	logger *slog.Logger,
) ([]llm.Message, string, error) {
	if deps.auroraStore == nil {
		return nil, "", fmt.Errorf("aurora compaction: store not available")
	}

	logger.Info("aurora: running compaction sweep on overflow")

	// Use compactionCfg directly — it's now aurora.SweepConfig.
	sweepCfg := deps.compactionCfg

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
			estimatedTokens := uint64(utf8.RuneCountInString(summaryContent) / runesPerToken)
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
		return nil, "", fmt.Errorf("aurora compaction sweep: %w", err)
	}

	if result == nil || !result.ActionTaken {
		return nil, "", nil
	}

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
