package chat

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
	compact "github.com/choiceoh/deneb/gateway-go/internal/chat/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
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
	circuitBreaker *compact.CompactionCircuitBreaker
}

// getCompactionCircuitBreaker returns the lazily-initialized circuit breaker.
// Uses sync.Once to ensure thread-safe single initialization.
func getCompactionCircuitBreaker() *compact.CompactionCircuitBreaker {
	proactiveCompaction.cbOnce.Do(func() {
		proactiveCompaction.circuitBreaker = compact.NewCompactionCircuitBreaker()
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
	// RLM always active: no Aurora compaction — the loop manages its own context.
	if true {
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
	// RLM: only microcompaction (free tool-result pruning), no Aurora sweep.
	return func(ctx context.Context, turn int, messages []llm.Message, accTokens int) ([]llm.Message, string, error) {
		compacted, _ := compact.MicrocompactMessages(messages, time.Now())
		return compacted, "", nil
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

	// Fact extractor disabled: memory store replaced by wiki.
	var factExtractor aurora.FactExtractor

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

