package chat

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
	compaction2 "github.com/choiceoh/deneb/gateway-go/internal/chat/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
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

	// Only main sessions (e.g. "telegram:123") may trigger compaction.
	// Dev-test, sub-task, and cron sessions must not modify the shared Aurora store.
	if !isMainSession(params.SessionKey) {
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
		deps.compactionCfg, storedTokens, 0, deps.contextCfg.MemoryTokenBudget,
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
		"budget", deps.contextCfg.MemoryTokenBudget,
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

// midLoopCompactionDefault is the baseline compaction threshold ratio when
// messages are small/uniform (0.60 = trigger at 60% of live token budget).
const midLoopCompactionDefault = 0.60

// adaptiveMidLoopThreshold adjusts the compaction trigger ratio based on
// average message size. Tool-heavy sessions (code analysis, large results)
// get a lower threshold to compress earlier; short conversational sessions
// preserve more context at the default ratio.
//
// Inspired by OpenClaw's adaptive chunk ratio:
//   - avgRatio > 0.1 (each msg ~10%+ of budget) → compress earlier (0.15–0.40)
//   - avgRatio ≤ 0.1 (small messages)            → keep default (0.60)
func adaptiveMidLoopThreshold(liveTokens int, msgCount int, budget uint64) float64 {
	if msgCount == 0 || budget == 0 {
		return midLoopCompactionDefault
	}
	avgRatio := (float64(liveTokens) / float64(msgCount)) * 1.2 / float64(budget)
	if avgRatio > 0.1 {
		reduction := math.Min(avgRatio*2, 0.25)
		return math.Max(0.15, 0.4-reduction)
	}
	return midLoopCompactionDefault
}

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
	budget := deps.contextCfg.LiveTokenBudget

	// Track last compaction turn to avoid compacting on consecutive turns.
	var lastCompactTurn int = -10
	// Track the number of messages already synced to Aurora to avoid duplicates.
	var auroraSyncedCount int

	return func(ctx context.Context, turn int, messages []llm.Message, accTokens int) ([]llm.Message, string, error) {
		// Skip if we just compacted recently (within 2 turns).
		if turn-lastCompactTurn < 2 {
			return nil, "", nil
		}

		// Estimate current context size and compute adaptive threshold.
		// Tool-heavy sessions (large avg message) trigger earlier; conversational
		// sessions preserve more context at the default 0.60 ratio.
		liveTokens := estimateMessagesTokens(messages)
		ratio := adaptiveMidLoopThreshold(liveTokens, len(messages), budget)
		threshold := uint64(ratio * float64(budget))
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

		// Step 2.5: Emergency drop — if context is way over budget (>2x threshold),
		// messages are arriving faster than compaction can process. Drop the oldest
		// messages (keeping the first 2 + fresh tail) to bring context under control.
		// This prevents the scenario where rapid filler messages fill context before
		// the LLM-based compaction sweep can run.
		emergencyThreshold := threshold * 2
		if uint64(liveTokens) > emergencyThreshold && len(messages) > 10 {
			// Keep first 2 messages (initial context/facts) + last 8 (fresh tail).
			const keepHead = 2
			const keepTail = 8
			if len(messages) > keepHead+keepTail {
				dropped := len(messages) - keepHead - keepTail
				kept := make([]llm.Message, 0, keepHead+keepTail)
				kept = append(kept, messages[:keepHead]...)
				kept = append(kept, messages[len(messages)-keepTail:]...)
				messages = kept
				liveTokens = estimateMessagesTokens(messages)
				logger.Info("mid-loop compaction: emergency drop applied",
					"dropped", dropped,
					"remainingMsgs", len(messages),
					"remainingTokens", liveTokens,
				)
				if uint64(liveTokens) < threshold {
					lastCompactTurn = turn
					return messages, "", nil
				}
			}
		}

		// Step 2.75: Pre-compaction memory flush — extract facts from messages that
		// are about to be dropped (inspired by OpenClaw's memoryFlush). This ensures
		// important context survives compaction by persisting to long-term memory.
		if deps.memoryStore != nil && pilot.CheckLocalAIHealth() {
			flushPreCompactionMemory(ctx, deps, messages, logger)
		}

		// Step 3: Aurora compaction sweep (uses lightweight local LLM for summaries).
		// Only main sessions may use Aurora sweep; others get microcompact only.
		if deps.auroraStore == nil || !isMainSession(params.SessionKey) {
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
	// Prefer hub-routed summarizer for token budget management.
	// On failure, fall back to the next model in the registry chain.
	lwClient := pilot.GetLightweightClient()
	lwModel := pilot.GetLightweightModel()
	var summarizer aurora.Summarizer
	if sHub := pilot.GetLocalAIHub(); sHub != nil {
		summarizer = aurora.NewHubSummarizer(sHub)
	} else {
		summarizer = aurora.NewLLMSummarizer(lwClient, lwModel)
	}
	// Wrap with fallback: if primary summarizer fails, try fallback model.
	if deps.registry != nil {
		chain := deps.registry.FallbackChain(modelrole.RoleLightweight)
		for _, role := range chain[1:] {
			fbClient := deps.registry.Client(role)
			fbModel := deps.registry.Model(role)
			if fbClient != nil && fbModel != "" {
				summarizer = aurora.WithFallback(summarizer, aurora.NewLLMSummarizer(fbClient, fbModel))
				break
			}
		}
	}

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
		deps.contextCfg.MemoryTokenBudget,
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
		TokenBudget:    deps.contextCfg.MemoryTokenBudget,
		FreshTailCount: deps.contextCfg.FreshTailCount,
		MaxMessages:    deps.contextCfg.MaxMessages,
	}
	asmResult, err := aurora.Assemble(ctx, deps.auroraStore, 1, asmCfg, logger)
	if err != nil {
		return nil, "", fmt.Errorf("aurora reassemble after compaction: %w", err)
	}
	return asmResult.Messages, asmResult.SystemPromptAddition, nil
}

// flushPreCompactionMemory extracts facts from older messages that are about to
// be dropped by compaction. This preserves important context in long-term memory
// before the conversation history is truncated.
//
// Strategy: scan messages from the middle (likely-to-be-dropped zone), build a
// condensed text of user+assistant exchanges, and run fact extraction.
func flushPreCompactionMemory(ctx context.Context, deps runDeps, messages []llm.Message, logger *slog.Logger) {
	if deps.memoryStore == nil || len(messages) < 6 {
		return
	}

	// Focus on messages in the middle — the first 2 (system/initial) and last 4
	// (recent context) are either preserved or already extracted from.
	const keepHead = 2
	const keepTail = 4
	if len(messages) <= keepHead+keepTail {
		return
	}

	dropZone := messages[keepHead : len(messages)-keepTail]

	// Build a condensed conversation summary from the drop zone.
	var sb strings.Builder
	const maxFlushChars = 8000
	for _, msg := range dropZone {
		if sb.Len() >= maxFlushChars {
			break
		}
		content := string(msg.Content)
		if len(content) == 0 {
			continue
		}
		// Only process user and assistant messages (skip tool results — they're
		// mostly raw data that doesn't contain memory-worthy information).
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		// Truncate individual messages to keep the flush lightweight.
		if len(content) > 2000 {
			content = content[:2000]
		}
		sb.WriteString(msg.Role)
		sb.WriteString(": ")
		sb.WriteString(content)
		sb.WriteString("\n\n")
	}

	conversationText := sb.String()
	if len(conversationText) < 50 {
		return
	}

	// Extract using the lightweight model (same path as end-of-run extraction).
	flushCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	lwClient := pilot.GetLightweightClient()
	lwModel := pilot.GetLightweightModel()
	facts, err := memory.ExtractFacts(flushCtx, lwClient, lwModel,
		"[pre-compaction flush]", conversationText, logger)
	if err != nil {
		logger.Debug("pre-compaction memory flush: extraction failed", "error", err)
		return
	}
	if len(facts) > 0 {
		memory.InsertExtractedFactsAs(flushCtx, deps.memoryStore, deps.memoryEmbedder,
			facts, "compaction_flush", logger)
		logger.Info("pre-compaction memory flush: saved facts before compaction",
			"count", len(facts))
	}
}
