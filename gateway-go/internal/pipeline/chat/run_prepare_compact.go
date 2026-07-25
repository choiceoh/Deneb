// run_prepare_compact.go — message assembly, Polaris compaction, prompt
// finalization, and the local-AI summarizer. Extracted from run_prepare.go
// under the 700-LOC rule. Called sequentially after the context/prompt
// preparation stages in run_prepare.go.
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	compact "github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
)

// assembleMessages builds the final message list from prebuilt messages, transcript
// context, attachments, and Polaris compaction. mr identifies the resolved
// provider/model so compaction budgets and content handling can respect the
// model's capabilities (context window, vision).
func assembleMessages(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	prep prepResult,
	mr modelResolution,
	logger *slog.Logger,
	hooks *compactionHooks,
) []llm.Message {
	messages := assembleTurnMessages(ctx, params, deps, prep, mr)

	// Polaris compaction: tiered context compression — see compactTurnMessages.
	if len(messages) > 0 {
		messages = compactTurnMessages(ctx, params, deps, mr, messages, logger, hooks)
	}
	return messages
}

// assembleTurnMessages builds the turn's raw message list: transcript
// history (or caller-prebuilt messages), the current user message, extracted
// document attachments, and the non-vision image strip.
func assembleTurnMessages(ctx context.Context, params RunParams, deps runDeps, prep prepResult, mr modelResolution) []llm.Message {
	messages := prep.Messages

	// Extract raw document attachments (PDF/Office/CSV the native client sends as
	// base64 bytes with no explicit type) into text up front, so the block
	// builders below render their content instead of silently dropping them.
	// Local-AI digestion of oversized documents is gated off in briefcase mode,
	// mirroring the compaction summarizer gate; oversized originals are archived
	// to the wiki capture store so the digest map's line references stay openable.
	var save captureSaver
	if ws := deps.memory.Wiki; ws != nil {
		save = ws.SaveCaptureAt
	}
	attachments := prepareDocumentAttachments(ctx, params.Attachments, !deps.briefcaseMode, save)

	// If the caller provided pre-built messages (e.g., OpenAI-compatible HTTP API
	// with full conversation history), use those instead of transcript context.
	if len(params.PrebuiltMessages) > 0 {
		// Copy to avoid aliasing the caller's backing array. Without the copy,
		// append may write into shared capacity, corrupting the original slice.
		messages = append([]llm.Message(nil), params.PrebuiltMessages...)
		// When the caller also supplies a Message, append it so the LLM sees
		// it without re-loading the entire transcript.
		if params.Message != "" && len(attachments) == 0 {
			messages = append(messages, llm.NewTextMessage("user", params.Message))
		}
	}

	// Build or augment user message with attachments.
	switch {
	case len(messages) == 0 && params.Message != "":
		// No history — build the user message from scratch.
		if len(attachments) > 0 {
			blocks := buildAttachmentBlocks(params.Message, attachments)
			messages = []llm.Message{llm.NewBlockMessage("user", blocks)}
		} else {
			messages = []llm.Message{llm.NewTextMessage("user", params.Message)}
		}
	case params.AppendCurrentMessage && params.Message != "":
		// The current turn's message is not in the loaded history — its
		// persist was deferred to the enrichment join, or it is an ephemeral
		// trigger that never persists (run_exec.go). Append it as a NEW last
		// user message; the replace-last branch below would corrupt the
		// PREVIOUS turn's message here.
		if len(attachments) > 0 {
			messages = append(messages, llm.NewBlockMessage("user", buildAttachmentBlocks(params.Message, attachments)))
		} else {
			messages = append(messages, llm.NewTextMessage("user", params.Message))
		}
	case len(messages) > 0 && len(attachments) > 0:
		// History exists but current message has attachments — replace the
		// last user message (which was persisted as text-only) with a
		// multimodal version that includes the image/video content blocks.
		messages = appendAttachmentsToHistory(messages, params.Message, attachments)
	}

	// Model marked non-vision (provider config `vision: false`): replace image
	// blocks with text stubs up front instead of letting the provider reject
	// the request. Only fires on an explicit override — unknown models are
	// assumed vision-capable.
	if modelCapability(deps, mr.providerID, mr.model).NoVision {
		messages = compact.StripImageBlocks(messages)
	}
	return messages
}

// Polaris compaction: tiered context compression.
// Applied after message assembly, before prompt finalization.
// STW (Stop-the-World): when LLM compaction fires, the user sees a
// ✍ status emoji and typing keepalive until compaction completes.
// No LLM call is made until context is compressed — incoming messages
// are already queued by PendingQueue during the active run.
func compactTurnMessages(ctx context.Context, params RunParams, deps runDeps, mr modelResolution, messages []llm.Message, logger *slog.Logger, hooks *compactionHooks) []llm.Message {
	// Derive compaction budget from context assembly budgets so they stay
	// in sync, clamped to the model's context window when it is known.
	contextBudget := effectiveContextBudget(deps, mr.providerID, mr.model, logger)

	// History-suppressed runs (skill-review forks pass MaxHistoryTokens=1
	// to exclude transcript history) yield a sub-floor budget no compaction
	// can meet: the protected current turn alone exceeds it, so every tier
	// runs for nothing and the "failed to reduce below budget" warning
	// fires on each run. Budget 0 means "no budget configured" and keeps
	// the legacy run-everything behavior.
	if skipCompactionBudget(contextBudget) {
		logger.Debug("polaris: budget below compaction floor; skipping compaction",
			"session", params.SessionKey, "budget", contextBudget)
		return messages
	}

	// syncCompactionStall bounds the in-turn (STW) compaction — the backstop
	// for cases that cannot defer to the background pass (first compaction,
	// models with no window headroom, the hard ceiling). Raised 2m→3m so the
	// parallel chunk summaries have room to finish when the main-role model is
	// slow under GPU contention (the "polaris: chunk summarization failed …
	// context deadline exceeded" warnings), rather than failing and re-running
	// the same first compaction every turn. Stays well under the 5m turn
	// deadline. Trade-off: a turn that triggers synchronous compaction can
	// stall up to this long before replying; the deferred background path
	// (5m, off the critical path) already absorbs the common case.
	const syncCompactionStall = 3 * time.Minute
	polarisCtx, polarisCancel := context.WithTimeout(ctx, syncCompactionStall)
	var summarizer compact.Summarizer
	if pilotHub := pilot.LocalAIHub(); pilotHub != nil && !deps.briefcaseMode {
		summarizer = &localAISummarizer{}
	}

	// Off-critical-path compaction (defer the STW): when the assembled raw
	// history is over the LLM threshold but still fits the model's known context
	// window with headroom — or, for unknown-window providers, still fits the
	// configured history budget that assembly already trusts — run THIS turn on
	// the raw context and summarize in the BACKGROUND instead of blocking the
	// agent loop on a multi-second STW summarization. The next turn assembles
	// the background-persisted summary. The synchronous path below stays the
	// backstop for: the first compaction (no summaries yet → AssembleContext
	// truncated the tail, which CompactAndPersist must recover), models whose
	// known window has no headroom over the budget, and the hard ceiling where
	// the raw history would not fit the window/budget. Re-prefill behaviour is
	// unchanged (the summary still lands one turn later); only the STW is
	// removed. See polaris.Engine.CompactInBackground and prompt-cache.md §1.5.
	if deferredMessages, deferred := deferCompactionToBackground(
		params, deps, mr, messages, summarizer, contextBudget, logger,
	); deferred {
		polarisCancel()
		return deferredMessages
	}

	// STW: pre-check if LLM compaction will likely fire.
	// Signal the user before the (potentially slow) summarization starts.
	compactTypingDone, compactStart := startCompactionStatus(
		ctx, messages, contextBudget, summarizer, hooks, logger,
	)

	messages, polarisResult := runPolarisCompaction(
		polarisCtx, params, deps, messages, summarizer, contextBudget, logger,
	)
	polarisCancel()

	finishCompactionStatus(compactTypingDone, compactStart, logger)

	logCompactionResult(logger, polarisResult)

	// P4: mark the session so the next turn's system prompt includes
	// a one-time reminder that summaries are present in history.
	// Cheap-pruning-only results (Micro, TruncateOldToolResults) do
	// not trigger this — see compactionProducedSummary in
	// chat/compaction_marker.go.
	markCompactionFired(deps, params.SessionKey, polarisResult)

	// Compaction ran (triggered by tokens > budget) but did not bring
	// tokens back within budget — degraded context state. Agent will
	// likely hit provider-side overflow; surface to operator now so we
	// know why a turn later fails, rather than blaming only the LLM.
	// Skip when budget is unset/zero (e.g. boot session, subagent) —
	// the inequality is trivially true and the warning becomes noise.
	reportCompactionDegraded(params, deps, contextBudget, polarisResult, logger)
	return messages
}

func deferCompactionToBackground(
	params RunParams,
	deps runDeps,
	mr modelResolution,
	messages []llm.Message,
	summarizer compact.Summarizer,
	contextBudget int,
	logger *slog.Logger,
) ([]llm.Message, bool) {
	bridge, ok := deps.transcript.(*polaris.Bridge)
	if !ok || summarizer == nil {
		return nil, false
	}
	engine := bridge.Engine()
	currentTokens := compact.EstimateMessagesTokens(messages)
	softThreshold := int(float64(contextBudget) * compact.DefaultLLMThresholdPct)
	ceiling, canDefer := compactionDeferralCeiling(deps, mr.providerID, mr.model, contextBudget)
	eligible := currentTokens > softThreshold &&
		canDefer &&
		currentTokens <= ceiling &&
		engine.HasSummaries(params.SessionKey)
	if !eligible {
		return nil, false
	}

	engine.CompactInBackground(
		deps.callbacks.shutdownCtx,
		params.SessionKey,
		summarizer,
		contextBudget,
		deps.memory.Embedding,
		buildAnchorKeywords(deps.memory.Wiki),
		buildLearnedGuidelines(),
	)
	// Never ship an orphan tool pair at the assembly coverage boundary. This
	// operates on the current turn only and is byte-identical when balanced.
	balanced := compact.BalanceToolBlocks(messages)
	logger.Info("polaris: deferred compaction to background (turn runs on raw context)",
		"session", params.SessionKey,
		"tokens", currentTokens,
		"budget", contextBudget,
		"ceiling", ceiling)
	return balanced, true
}

func compactionDeferralCeiling(deps runDeps, providerID, model string, contextBudget int) (int, bool) {
	ceiling := contextWindowCeiling(deps, providerID, model)
	if contextBudget <= 0 {
		return ceiling, false
	}
	if ceiling > contextBudget {
		return ceiling, true
	}
	if ceiling == 0 {
		return contextBudget, true
	}
	return ceiling, false
}

func startCompactionStatus(
	ctx context.Context,
	messages []llm.Message,
	contextBudget int,
	summarizer compact.Summarizer,
	hooks *compactionHooks,
	logger *slog.Logger,
) (chan struct{}, time.Time) {
	if hooks == nil || summarizer == nil {
		return nil, time.Time{}
	}
	currentTokens := compact.EstimateMessagesTokens(messages)
	threshold := int(float64(contextBudget) * compact.DefaultLLMThresholdPct)
	if currentTokens <= threshold {
		return nil, time.Time{}
	}
	started := time.Now()
	logger.Info("pipeline: STW compaction starting",
		"tokens", currentTokens,
		"budget", contextBudget,
		"ratio", fmt.Sprintf("%.1f%%", float64(currentTokens)/float64(contextBudget)*100))
	if hooks.typingFn == nil {
		return nil, started
	}
	done := make(chan struct{})
	go runCompactionTypingLoop(ctx, done, hooks.typingFn, logger)
	return done, started
}

func runCompactionTypingLoop(
	ctx context.Context,
	done <-chan struct{},
	typingFn func(),
	logger *slog.Logger,
) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.Error("panic in compaction typing loop", "panic", recovered)
		}
	}()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			typingFn()
		}
	}
}

func finishCompactionStatus(done chan struct{}, started time.Time, logger *slog.Logger) {
	if done != nil {
		close(done)
	}
	if !started.IsZero() {
		logger.Info("pipeline: STW compaction done", "durationMs", time.Since(started).Milliseconds())
	}
}

func runPolarisCompaction(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	messages []llm.Message,
	summarizer compact.Summarizer,
	contextBudget int,
	logger *slog.Logger,
) ([]llm.Message, compact.Result) {
	if bridge, ok := deps.transcript.(*polaris.Bridge); ok {
		return compactWithPolarisBridge(
			ctx, bridge.Engine(), params, deps, messages, summarizer, contextBudget, logger,
		)
	}
	return compactWithoutPolarisBridge(ctx, params, deps, messages, summarizer, contextBudget, logger)
}

func compactWithPolarisBridge(
	ctx context.Context,
	engine *polaris.Engine,
	params RunParams,
	deps runDeps,
	messages []llm.Message,
	summarizer compact.Summarizer,
	contextBudget int,
	logger *slog.Logger,
) ([]llm.Message, compact.Result) {
	configurePolarisEngine(engine, deps)
	compacted, result := engine.CompactAndPersist(
		ctx, params.SessionKey, messages, summarizer, contextBudget,
	)
	if result.LLMCompacted && summarizer != nil {
		schedulePolarisCondensation(deps.callbacks.shutdownCtx, engine, params.SessionKey, summarizer, logger)
	}
	return compacted, result
}

func configurePolarisEngine(engine *polaris.Engine, deps runDeps) {
	if deps.memory.Embedding != nil {
		engine.SetEmbedder(deps.memory.Embedding)
	}
	if deps.briefcaseMode {
		engine.SetAnchorKeywords(nil)
		engine.SetLearnedGuidelines(nil)
		return
	}
	engine.SetAnchorKeywords(buildAnchorKeywords(deps.memory.Wiki))
	engine.SetLearnedGuidelines(buildLearnedGuidelines())
}

func schedulePolarisCondensation(
	parentCtx context.Context,
	engine *polaris.Engine,
	sessionKey string,
	summarizer compact.Summarizer,
	logger *slog.Logger,
) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	go condensePolarisInBackground(parentCtx, engine, sessionKey, summarizer, logger) //nolint:gosec // G118 -- shutdown-derived and timeout-bounded
}

func condensePolarisInBackground(
	parentCtx context.Context,
	engine *polaris.Engine,
	sessionKey string,
	summarizer compact.Summarizer,
	logger *slog.Logger,
) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.Error("panic in background condense", "session", sessionKey, "panic", recovered)
		}
	}()
	ctx, cancel := context.WithTimeout(parentCtx, 5*time.Minute)
	defer cancel()
	if err := engine.Condense(ctx, sessionKey, summarizer); err != nil {
		logger.Warn("background condense failed", "session", sessionKey, "error", err)
	}
}

func compactWithoutPolarisBridge(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	messages []llm.Message,
	summarizer compact.Summarizer,
	contextBudget int,
	logger *slog.Logger,
) ([]llm.Message, compact.Result) {
	cfg := compact.NewConfig(contextBudget)
	cfg.Embedder = deps.memory.Embedding
	compSession := compactionSession(deps, params.SessionKey)
	if compSession != nil {
		cfg.PreviousSummary = compSession.PreviousCompactionSummary
	}
	compacted, result := compact.Compact(ctx, cfg, messages, summarizer, logger)
	if compSession != nil && result.Summary != "" {
		compSession.PreviousCompactionSummary = result.Summary
	}
	return compacted, result
}

func compactionSession(deps runDeps, sessionKey string) *session.Session {
	if deps.sessions == nil {
		return nil
	}
	return deps.sessions.Get(sessionKey)
}

func logCompactionResult(logger *slog.Logger, result compact.Result) {
	tier, ok := compactionTier(result)
	if !ok {
		return
	}
	attrs := []any{"tokensBefore", result.TokensBefore, "tokensAfter", result.TokensAfter}
	if result.MicroPruned > 0 {
		attrs = append(attrs, "pruned", result.MicroPruned)
	}
	if result.EmergencyEvicted > 0 {
		attrs = append(attrs, "evicted", result.EmergencyEvicted)
	}
	logger.Info("polaris "+tier+" compaction", attrs...)
}

func compactionTier(result compact.Result) (string, bool) {
	switch {
	case result.EmergencyEvicted > 0:
		return "emergency", true
	case result.LLMCompacted:
		return "tier1-llm", true
	case result.EmbeddingCompacted:
		return "tier2-embedding-mmr", true
	case result.RecencyCompacted:
		return "tier3-recency", true
	case result.MicroPruned > 0:
		return "micro", true
	default:
		return "", false
	}
}

func reportCompactionDegraded(
	params RunParams,
	deps runDeps,
	contextBudget int,
	result compact.Result,
	logger *slog.Logger,
) {
	if contextBudget <= 0 || result.TokensBefore <= contextBudget || result.TokensAfter <= contextBudget {
		return
	}
	logger.Warn("polaris: compaction failed to reduce below budget",
		"session", params.SessionKey,
		"tokensBefore", result.TokensBefore,
		"tokensAfter", result.TokensAfter,
		"budget", contextBudget)
	if deps.broadcast != nil {
		broadcastPayload(deps.broadcast, "chat.compaction_degraded", ChatCompactionDegradedEvent{
			Session:      params.SessionKey,
			TokensBefore: result.TokensBefore,
			TokensAfter:  result.TokensAfter,
			Budget:       contextBudget,
		})
	}
}

// finalizePrompt applies budget optimization and tier-1 wiki injection to the
// system prompt. recallAddition is normally "" — per-turn recall rides the
// last user message now (run_tail_inject.go) so the system prompt stays a
// stable vLLM APC prefix; it is only non-empty on the degenerate
// no-user-message fallback path.
func finalizePrompt(
	systemPrompt json.RawMessage,
	recallAddition string,
	tier1Addition string,
	contextCfg ContextConfig,
	sessionToolPreset string,
	message string,
) json.RawMessage {
	// Budget-optimize variable prompt additions before appending.
	if recallAddition != "" {
		// Current-turn recall evidence is compact and more relevant than
		// always-on tier-1 memory, so keep it even when the static prompt is
		// already at its nominal budget.
		systemPrompt = json.RawMessage(llm.AppendSystemText(llm.FlexibleFromRaw(systemPrompt), recallAddition).Bytes())
	}

	if tier1Addition != "" {
		promptBudget := prompt.PromptBudget{Total: contextCfg.SystemPromptBudget}
		baseTokens := uint64(prompt.EstimateTokens(string(systemPrompt)))
		var remainingBudget uint64
		if promptBudget.Total > baseTokens {
			remainingBudget = promptBudget.Total - baseTokens
		}
		if promptBudget.Total > 0 && remainingBudget == 0 {
			return systemPrompt
		}
		additionBudget := prompt.PromptBudget{Total: remainingBudget}

		additionFragments := []prompt.PromptFragment{prompt.NewFragment("memory", tier1Addition)}
		optimized := additionBudget.Optimize(additionFragments)
		for _, f := range optimized {
			systemPrompt = json.RawMessage(llm.AppendSystemText(llm.FlexibleFromRaw(systemPrompt), f.Content).Bytes())
		}
	}

	return systemPrompt
}

// agentConfigDeps holds dependencies specifically needed by buildAgentConfig.

var _ compact.Summarizer = (*localAISummarizer)(nil)

// localAISummarizer adapts the LOCAL lightweight model to the
// compaction.Summarizer interface. Compaction summarization had been routed to
// the analysis role, which now resolves to a cloud model (glm-5.2) — making each
// chunk summary a ~20s network round-trip that both burns subscription credits
// and is the dominant cause of the "polaris: chunk summarization failed …
// context deadline exceeded" timeouts (the sync 2m budget vs several chunks).
// The lightweight role (local qwen3.6-35b) summarizes the same chunk in ~3s with
// equal fact-fidelity and a more concise (better-compressed) result, so the
// background path is free and the sync path no longer times out.
type localAISummarizer struct{}

// Summarize produces a bounded summary of the supplied conversation.
func (s *localAISummarizer) Summarize(ctx context.Context, system, conversation string, maxOutputTokens int) (string, error) {
	return pilot.CallLocalLLM(ctx, system, conversation, maxOutputTokens)
}
