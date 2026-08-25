// run_exec.go contains the core agent execution loop: user message persistence,
// context assembly, LLM invocation with model fallback.
package chat

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/metrics"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// chatRunResult wraps the agent result with chat-layer metadata.
type chatRunResult struct {
	*agent.AgentResult
	// SpawnFlag is non-nil; IsSet() returns true when sessions_spawn was called.
	SpawnFlag *SpawnFlag
	// ActualModel is the model that actually produced the answer. It differs
	// from the requested model only when the model fallback chain fired.
	ActualModel string
	// FellBack is true when runAgentWithFallback had to drop from the initial
	// role to a fallback role to get a successful turn. Surfaced to clients so
	// the UI can show which model answered.
	FellBack bool
}

// executeAgentRun performs the core agent execution: persist user msg, assemble context,
// run agent loop, persist result.
func executeAgentRun(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	broadcaster *streaming.Broadcaster,
	typingSignaler chatport.TypingSignaler,
	logger *slog.Logger,
	runLog *agentlog.RunLogger,
) (*chatRunResult, error) {
	runStart := time.Now()
	if deps.briefcaseMode {
		deps.strictErrors = &strictRunErrorSink{}
	}

	emitRunStart(deps, params, runStart)

	// Signal "preparing" phase — covers parallel context assembly, system prompt
	// build, and recall preflight setup. SSE clients receive a structured
	// phase.changed event for the transition.
	emitPhase(deps, params, "preparing", runStart)

	// 1. Persist user message to transcript + Aurora store. Deferred to the
	// enrichment join (below, after parallel prep) when a link enrichment is
	// in flight: the fetched block must be part of the persisted bytes
	// (prompt-cache rule: what the LLM saw is what history reloads), so the
	// message cannot be persisted until the fetches complete.
	if err := persistInitialUserMessage(params, deps, logger); err != nil {
		return nil, err
	}
	workspaceDir := prewarmPromptWorkspace(params, deps)

	// Cache session lookup: fetched once and reused throughout this function
	// to avoid repeated map lookups + lock acquisitions.
	cachedSession := lookupRunSession(deps, params.SessionKey)

	// 2. Resolve model and provider early (no IO — pure config/registry lookups).
	mr := resolveModel(params, deps, cachedSession)
	model := mr.model
	providerID := mr.providerID
	initialRole := mr.initialRole

	runLog.LogStart(agentlog.RunStartData{
		Model:         model,
		Provider:      providerID,
		Message:       params.Message,
		Channel:       deliveryChannel(params.Delivery),
		ThinkingLevel: sessionThinkingLevel(cachedSession),
	})

	// 3. Resolve LLM client from in-memory config/auth store. May perform
	// provider-runtime auth (e.g. token exchange) but no longer resolves
	// external secret references on the chat path.
	client := resolveClient(ctx, deps, providerID, logger)
	if client == nil {
		return nil, noLLMClientError(params, deps, providerID, model, runLog, logger)
	}

	// Recall preflight runs during context preparation: when the current
	// message hints at prior context, server-side search injects compact
	// evidence before the first LLM call instead of relying only on tool use.

	// Resolve session tool preset early (needed for both system prompt and tool list).
	var sessionToolPreset string
	if cachedSession != nil {
		sessionToolPreset = cachedSession.ToolPreset
	}

	// Stage 1: Parallel context + prompt preparation.
	prepStart := time.Now()
	prep := prepareContextAndPrompt(ctx, params, deps, workspaceDir, sessionToolPreset, logger)
	parallelPrepMs := time.Since(prepStart).Milliseconds()
	logger.Info("pipeline: parallel prep done (context+sysprompt)", "ms", parallelPrepMs)

	if err := checkContextAssembly(prep, deps, params.SessionKey, logger); err != nil {
		return nil, err
	}
	if err := deps.strictErrors.Err(); err != nil {
		return nil, fmt.Errorf("briefcase context preparation: %w", err)
	}

	enrichStart := time.Now()
	if err := joinPendingEnrichment(ctx, &params, deps, logger); err != nil {
		return nil, err
	}
	enrichJoinMs := time.Since(enrichStart).Milliseconds()
	// Ephemeral turns (heartbeat) never persist their trigger message, and on
	// a history-bearing session nothing else put it into the working list —
	// the model ran blind on pure history (latent bug: every heartbeat tick on
	// client:main). Append it wire-only, timestamped like a persisted message.
	// Fresh sessions (boot, mail-qa, event-ingest) keep the scratch-build path
	// in assembleTurnMessages, byte-identical to before.
	if ephemeralNeedsExplicitAppend(params, prep) {
		params.Message = formatTurnUserMessage(params.Message, deps.now())
		params.AppendCurrentMessage = true
	}

	// Stage 2: Assemble final message list (prebuilt, attachments, Polaris compaction).
	cHooks := buildCompactionHooks(params, deps)
	assembleStart := time.Now()
	messages := assembleMessages(ctx, params, deps, prep, mr, logger, cHooks)
	assembleMs := time.Since(assembleStart).Milliseconds()

	messages, tailForSystem, autoLoadedSkills, autoActivatedTools := applyTailAdditions(params, deps, prep, sessionToolPreset, messages)

	// Stage 2.5: difficulty model routing — an obviously-simple interactive
	// main turn rides main2 (second main-tier subscription) when configured,
	// reserving the flagship for analysis-grade turns. Decided here (not in
	// resolveModel) because the heuristic needs the assembled history, and
	// BEFORE any consumer of the resolution (APC diag, tuning, cache policy)
	// so downstream — including the mutual main2→main fallback chain — runs
	// exactly as a native main2 turn. See difficulty_route.go.
	if rt := difficultyModelRoute(deps.registry, params, sessionSpawnedBy(cachedSession), messages, initialRole, providerID, model, logger); rt != nil {
		model, providerID, initialRole = rt.model, rt.providerID, modelrole.RoleMain2
		client = rt.client
	}

	// Stage 3: Finalize system prompt (budget optimization, coordinator suggestion, tier-1 injection).
	systemPrompt := finalizePrompt(prep.SystemPrompt, tailForSystem, prep.Tier1Wiki, deps.contextCfg, sessionToolPreset, params.Message)
	if deps.auditSystemPrompt != nil {
		deps.auditSystemPrompt(params.SessionKey, append([]byte(nil), systemPrompt...))
	}

	logger.Info("pipeline: system prompt finalized",
		"chars", len(systemPrompt))

	logRunPrep(logger, runLog, params, prep, len(systemPrompt), len(messages), prepTimings{
		runStart:       runStart,
		parallelPrepMs: parallelPrepMs,
		enrichJoinMs:   enrichJoinMs,
		assembleMs:     assembleMs,
	})

	// Stage 3.5: APC prefix-stability diagnostics — classify how this run's
	// assembled prompt diverges from the session's previous run and bracket
	// the engine prefix-cache counters around the run. Deferred so the "apc
	// diag" line is emitted on error paths too. See apc_diag.go.
	var apcDiag *apcDiagRun
	if !deps.briefcaseMode {
		apcDiag = beginAPCDiag(ctx, deps, params.SessionKey, client.APIMode(), providerID, model, systemPrompt, prep.RecallMemory, messages, logger)
		defer apcDiag.finish()
	}

	// Stage 4: Build tool list and agent config.
	acd := buildAgentConfigDeps(deps, messages, sessionToolPreset, logger)
	acd.InitialDeferredTools = mergeDeferredToolNames(params.InitialDeferredTools, autoActivatedTools)
	// execStats threads into recordRunCompletion (LogEnd's RepairedToolCalls) —
	// #3117 introduced it while #3121 moved LogEnd into the completion sink.
	cfg, spawnFlag, execStats, skillConsults := buildAgentConfig(params, deps, cachedSession, systemPrompt, sessionToolPreset, acd, logger)
	if params.SoftDeadline > 0 {
		// Pin the soft preference to the end-to-end turn clock. Model fallbacks
		// reuse this absolute instant instead of accidentally granting themselves
		// another full soft-deadline window.
		cfg.SoftDeadline = params.SoftDeadline
		cfg.SoftDeadlineAt = runStart.Add(params.SoftDeadline)
		cfg.OnSoftDeadline = func() {
			emitPhase(deps, params, "wrapping_up", time.Now())
		}
	}
	autoLoadedSet := make(map[string]bool, len(autoLoadedSkills))
	for _, name := range autoLoadedSkills {
		skillConsults.Add(name)
		autoLoadedSet[name] = true
	}
	cfg.Model = model // set the resolved model
	// Content-prefix-cache providers (kimi) do exact-prefix matching over the
	// raw request: the mid-run tool-result shrink (CompactPriorToolResults)
	// mutates already-sent history bytes and forces a full cold re-prefill on
	// every later call — costing far more than the shrink saves. Keep the
	// history bytes stable there; marker-based providers keep the shrink.
	cfg.DisablePriorToolResultCompaction = modelCapability(deps, providerID, model).ContentPrefixCache
	// Per-model defaults (profile sampling, tuned max-tokens floor) — only
	// fills values the request left unset; request-level params, cache-safe.
	applyModelTuning(&cfg, deps, params, providerID, model)
	// Adaptive effort router: obviously-simple conversational messages on
	// dual-mode models (capability-gated, provider-aware) skip the thinking
	// phase (KV-prefix-safe). Routed runs may escalate back to thinking in
	// runAgentWithFallback, which needs the route to restore the original.
	effortRt, effortDecision := applyEffortRouter(&cfg, params, messages, routingProfileForRun(deps, providerID, model), logger)

	// BeforeAPICall hook chain (steer + trailing cache markers) and the
	// provider's cache_control policy — see wireBeforeAPICall.
	apiMode := wireBeforeAPICall(&cfg, deps, params, providerID, model, client, logger)

	// Set up stream hooks via compositor: fan-out dispatch for each hook type.
	var hc agent.HookCompositor
	deltaTranslit := wireStreamHooks(&hc, params, deps, broadcaster, typingSignaler)
	// Untrusted-origin tool gate (interactive runs only): block irreversible
	// tools once promptware enters the turn. Placed here so prep.RecallMemory is
	// available to seed the taint; composes with any gate wireStreamHooks set.
	untrustedGate := wireUntrustedToolGate(&hc, params, prep, deps, logger)
	if untrustedGate != nil {
		origTurnInit := cfg.OnTurnInit
		cfg.OnTurnInit = func(ctx context.Context) context.Context {
			if origTurnInit != nil {
				ctx = origTurnInit(ctx)
			}
			untrustedGate.bindTurnContext(TurnContextFromContext(ctx))
			return ctx
		}
	}

	hooks := hc.Build()

	logger.Info("pipeline: prep complete, starting agent loop",
		"prepMs", time.Since(runStart).Milliseconds(),
		"model", model, "provider", providerID,
		"messages", len(messages), "tools", len(cfg.Tools))
	emitPhase(deps, params, "thinking", time.Now())

	// Execute agent loop with model fallback chain.
	agentStart := time.Now()
	agentResult, actualModel, fellBack, err := runAgentWithFallback(ctx, cfg, messages, client, deps, providerID, initialRole, effortRt, hooks, logger, runLog)
	emitPhase(deps, params, "finalizing", time.Now())
	logEffortRouteFailure(logger, effortDecision, effortRt, actualModel, err)
	usageModel := actualModel
	if usageModel == "" {
		usageModel = model
	}
	if err != nil {
		recordRunSkillUsage(acd.SkillUsageRecorder, skillConsults, agentResult, err, params.SessionKey, usageModel, autoLoadedSet)
		// Log run.error here — not in the async-only completion handler — so
		// every entry path (runAgentAsync, SendSync, SendSyncStream) closes the
		// run.start it opened in the same per-session log file. The sync paths
		// historically logged start/prep but never end/error, leaving orphaned
		// runs that AggregateByModel could not count.
		runLog.LogError(agentlog.RunErrorData{
			Error:   err.Error(),
			Aborted: ctx.Err() != nil,
		})
		return nil, err
	}
	if err := deps.strictErrors.Err(); err != nil {
		runErr := fmt.Errorf("briefcase transcript persistence: %w", err)
		recordRunSkillUsage(acd.SkillUsageRecorder, skillConsults, agentResult, runErr, params.SessionKey, usageModel, autoLoadedSet)
		return nil, runErr
	}
	recordRunSkillUsage(acd.SkillUsageRecorder, skillConsults, agentResult, nil, params.SessionKey, usageModel, autoLoadedSet)

	flushDeltaTail(deltaTranslit, broadcaster)

	recordRunCompletion(runCompletionRecord{
		params:         params,
		deps:           deps,
		runLog:         runLog,
		client:         client,
		agentResult:    agentResult,
		requestedModel: model,
		actualModel:    actualModel,
		apiMode:        apiMode,
		fellBack:       fellBack,
		effortRt:       effortRt,
		effortDecision: effortDecision,
		execStats:      execStats,
		runStart:       runStart,
		agentStart:     agentStart,
	}, logger)

	return &chatRunResult{AgentResult: agentResult, SpawnFlag: spawnFlag, ActualModel: actualModel, FellBack: fellBack}, nil
}

// emitRunStart emits the agent run.start event to gateway subscriptions.
func emitRunStart(deps runDeps, params RunParams, runStart time.Time) {
	if deps.callbacks.emitAgentFn == nil {
		return
	}
	deps.callbacks.emitAgentFn("run.start", params.SessionKey, params.ClientRunID, map[string]any{
		"model": params.Model,
		"ts":    runStart.UnixMilli(),
	})
}

// persistInitialUserMessage persists the inbound user message unless the
// persist is deferred to the enrichment join (a link enrichment in flight).
func persistInitialUserMessage(params RunParams, deps runDeps, logger *slog.Logger) error {
	if params.PendingEnrichment != nil {
		return nil
	}
	persistTurnUserMessage(params, deps, logger)
	if err := deps.strictErrors.Err(); err != nil {
		return fmt.Errorf("briefcase transcript persistence: %w", err)
	}
	return nil
}

// lookupRunSession fetches the session once for reuse throughout the run.
func lookupRunSession(deps runDeps, sessionKey string) *session.Session {
	if deps.sessions == nil {
		return nil
	}
	return deps.sessions.Get(sessionKey)
}

// sessionThinkingLevel returns the session's thinking level, or "" when unset
// or explicitly off.
func sessionThinkingLevel(sess *session.Session) string {
	if sess == nil || sess.ThinkingLevel == "" || sess.ThinkingLevel == "off" {
		return ""
	}
	return sess.ThinkingLevel
}

// noLLMClientError builds the no-client run error. This failure path exits
// before the enrichment join, where the deferred persist lives — persist the
// original message here so the user's input isn't lost from history. No LLM
// saw anything this turn, so the unenriched bytes are consistent.
func noLLMClientError(params RunParams, deps runDeps, providerID, model string, runLog *agentlog.RunLogger, logger *slog.Logger) error {
	if params.PendingEnrichment != nil {
		persistTurnUserMessage(params, deps, logger)
	}
	err := fmt.Errorf("no LLM client available (provider=%q, model=%q)", providerID, model)
	runLog.LogError(agentlog.RunErrorData{Error: err.Error()})
	return err
}

// checkContextAssembly turns a context assembly failure into a run error in
// briefcase mode; interactive runs proceed with degraded context.
func checkContextAssembly(prep prepResult, deps runDeps, sessionKey string, logger *slog.Logger) error {
	if prep.ContextErr == nil {
		return nil
	}
	if deps.briefcaseMode {
		return fmt.Errorf("briefcase context assembly: %w", prep.ContextErr)
	}
	logger.Error("context assembly failed, proceeding with degraded context",
		"sessionKey", sessionKey, "error", prep.ContextErr)
	return nil
}

// joinPendingEnrichment joins the deferred link enrichment: the link fetches
// ran concurrently with the parallel prep (recall/history/sysprompt), so on
// the common path they cost no extra wall-clock. The joined (enriched)
// message is persisted HERE — after the history load, which therefore does
// not contain it — and AppendCurrentMessage tells assembleTurnMessages to
// append the exact persisted bytes explicitly, keeping next turn's history
// reload byte-identical to what this turn's LLM call sees (vLLM APC).
func joinPendingEnrichment(ctx context.Context, params *RunParams, deps runDeps, logger *slog.Logger) error {
	if params.PendingEnrichment == nil {
		return nil
	}
	params.Message = params.PendingEnrichment(ctx)
	if formatted := persistTurnUserMessage(*params, deps, logger); formatted != "" {
		params.Message = formatted
		params.AppendCurrentMessage = true
	}
	if err := deps.strictErrors.Err(); err != nil {
		return fmt.Errorf("briefcase transcript persistence: %w", err)
	}
	return nil
}

// buildCompactionHooks wires the typing signal into compaction, so the user
// sees activity during a long compaction pass. Nil when the run has no
// delivery or typing callback.
func buildCompactionHooks(params RunParams, deps runDeps) *compactionHooks {
	if deps.callbacks.typingFn == nil || params.Delivery == nil {
		return nil
	}
	delivery := params.Delivery
	typingFn := deps.callbacks.typingFn
	return &compactionHooks{
		typingFn: func() {
			tCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = typingFn(tCtx, delivery)
		},
	}
}

// prepTimings carries the per-stage prep timing breakdown to logRunPrep.
type prepTimings struct {
	runStart       time.Time
	parallelPrepMs int64
	enrichJoinMs   int64
	assembleMs     int64
}

// logRunPrep records the prep stage: the agentlog prep entry plus a Warn with
// the per-stage breakdown when prep is user-noticeably slow. prepMs alone hid
// a 60s stall (4× on one session, 2026-07-07 — the stage that ate it was
// unidentifiable post-hoc), so the breakdown names the culprit in the journal.
func logRunPrep(logger *slog.Logger, runLog *agentlog.RunLogger, params RunParams, prep prepResult, systemPromptChars, messageCount int, t prepTimings) {
	prepTotalMs := time.Since(t.runStart).Milliseconds()
	if prepTotalMs > 5000 {
		logger.Warn("pipeline: slow prep",
			"totalMs", prepTotalMs, "parallelPrepMs", t.parallelPrepMs,
			"enrichJoinMs", t.enrichJoinMs, "assembleMs", t.assembleMs,
			"sessionKey", params.SessionKey)
	}
	runLog.LogPrep(agentlog.RunPrepData{
		SystemPromptChars: systemPromptChars,
		ContextMessages:   messageCount,
		PrepMs:            prepTotalMs,
		EnrichJoinMs:      t.enrichJoinMs,
		AssembleMs:        t.assembleMs,
		RecallChars:       len(prep.RecallMemory),
	})
}

// buildAgentConfigDeps assembles the agent config dependency bag. Deferred
// tools activated in earlier runs stay active: replay the transcript's
// activation evidence into this run (deferred_replay.go).
func buildAgentConfigDeps(deps runDeps, messages []llm.Message, sessionToolPreset string, logger *slog.Logger) agentConfigDeps {
	acd := agentConfigDeps{
		Tools:               deps.tools,
		MaxTokens:           deps.maxTokens,
		SubagentNotifyCh:    deps.subagentNotifyCh,
		EmitAgentFn:         deps.callbacks.emitAgentFn,
		Transcript:          deps.transcript,
		SkillNudger:         deps.skills.Nudger,
		SkillUsageRecorder:  deps.skills.UsageRecorder,
		ReplayDeferredTools: replayActivatedTools(messages, deps.tools, sessionToolPreset),
	}
	if n := len(acd.ReplayDeferredTools); n > 0 {
		logger.Info("deferred replay: reactivating tools from transcript",
			"count", n, "tools", strings.Join(acd.ReplayDeferredTools, ","))
	}
	return acd
}

// logEffortRouteFailure records a routed run that failed. The failed-run
// record matters MOST for the label pipeline: a routed run that escalated and
// still failed is the strongest misjudgment signal. The success-path record
// rides on the "agent loop complete" line.
func logEffortRouteFailure(logger *slog.Logger, effortDecision string, effortRt *effortRoute, actualModel string, err error) {
	if err == nil || effortDecision == "" {
		return
	}
	logger.Info("effort router: run failed",
		"decision", effortDecision,
		"escalated", effortRt != nil && effortRt.escalated,
		"model", actualModel, "error", err)
}

// flushDeltaTail releases any trailing backticks the delta transliterator
// held back to disambiguate a fence marker that could have spanned deltas
// (run ended first). Emitted before the done frame so the live view's last
// tokens aren't dropped; the final/persisted text is transliterated
// separately.
func flushDeltaTail(deltaTranslit *streaming.Streamer, broadcaster *streaming.Broadcaster) {
	if deltaTranslit == nil || broadcaster == nil {
		return
	}
	if tail := deltaTranslit.Flush(); tail != "" {
		broadcaster.EmitDelta(tail)
	}
}

// runCompletionRecord bundles everything the post-loop telemetry sink needs.
type runCompletionRecord struct {
	params         RunParams
	deps           runDeps
	runLog         *agentlog.RunLogger
	client         *llm.Client
	agentResult    *agent.AgentResult
	requestedModel string // the model the run asked for
	actualModel    string // the model that answered (differs when fallback fired)
	apiMode        string
	fellBack       bool
	effortRt       *effortRoute
	effortDecision string
	execStats      *toolport.ToolExecStats
	runStart       time.Time
	agentStart     time.Time
}

// recordRunCompletion emits every post-loop success record in one place: the
// postmortem log line, the prompt-cache hit-ratio sample, the run.end gateway
// event, the agent-detail run.end entry, and the async engine APC sample.
func recordRunCompletion(rec runCompletionRecord, logger *slog.Logger) {
	params, deps := rec.params, rec.deps
	agentResult := rec.agentResult
	model, actualModel := rec.requestedModel, rec.actualModel
	apiMode, fellBack := rec.apiMode, rec.fellBack
	effortRt, effortDecision := rec.effortRt, rec.effortDecision
	runLog, client := rec.runLog, rec.client
	execStats := rec.execStats
	agentMs := time.Since(rec.agentStart).Milliseconds()
	totalMs := time.Since(rec.runStart).Milliseconds()
	// Surface run-level aggregates so a postmortem gets the shape in one line:
	// how many tool calls total, how they break down by name, how much text
	// the agent produced vs. what ended up in result.Text, and a 200-char head
	// of result.Text. Without textHead the operator had to query the transcript
	// DB to know what was actually delivered.
	finalTextHead := ""
	if txt := strings.TrimSpace(agentResult.Text); txt != "" {
		if len(txt) > 200 {
			// Rune-safe head so a multi-byte char (Korean) never splits into a
			// U+FFFD replacement char in the postmortem log line.
			finalTextHead = textutil.TruncateBytes(txt, 200) + "…"
		} else {
			finalTextHead = txt
		}
	}
	toolHist := formatToolHist(agentResult.ToolCounts)
	// Effort-router fields ride on the existing run-complete line (one
	// greppable record per run for the acceptance comparison and a future
	// learned router) instead of a near-duplicate second Info line.
	// runKind labels the population this run belongs to (chat, heartbeat, cron,
	// skill-review, …). Without it every postmortem reader has to treat a 4-hour
	// research cron and a chat turn as one population: runtime-health's latency
	// pillar scored 0.0 on EVERY window for months because automation lanes pin
	// p95 to their own budget caps (2026-08-23 measurement — interactive p95 was
	// 558s while the heartbeat lane sat exactly on its 300s fence). Classified by
	// the session package, the single source of truth for key→kind.
	logger.Info("pipeline: agent loop complete",
		"runKind", session.WorkTypeForKey(params.SessionKey),
		"effortDecision", effortDecision,
		"effortEscalated", effortRt != nil && effortRt.escalated,
		"agentMs", agentMs,
		"totalMs", totalMs,
		"llmMs", agentResult.LLMMs,
		"toolMs", agentResult.ToolMs,
		"turns", agentResult.Turns,
		"inputTokens", agentResult.Usage.InputTokens,
		"outputTokens", agentResult.Usage.OutputTokens,
		"cacheReadTokens", agentResult.Usage.CacheReadInputTokens,
		"cacheCreationTokens", agentResult.Usage.CacheCreationInputTokens,
		"stopReason", agentResult.StopReason,
		"streamAttempts", agentResult.Stream.Attempts,
		"streamRetries", agentResult.Stream.Retries,
		"streamRetryReason", agentResult.Stream.LastRetryReason,
		"streamTerminationReason", agentResult.Stream.TerminationReason,
		"totalTextChars", agentResult.TotalTextChars,
		"finalTextChars", len(agentResult.Text),
		"allTextChars", len(agentResult.AllText),
		"totalToolCalls", agentResult.TotalToolCalls,
		"toolHist", toolHist,
		"finalTextHead", finalTextHead)

	// Record this run's prompt-cache usage for the /status hit-ratio alarm —
	// but only for Anthropic-mode runs that did NOT fall back. apiMode is the
	// initial provider's mode; when runAgentWithFallback drops to a fallback
	// role (default registry makes those vLLM), the answer came from a provider
	// that never populates cache_* fields, so recording it would pollute the
	// ratio with structural "misses". Skipping fallbacks is conservative: it
	// never records a wrong provider's usage (worst case is an occasional
	// missed sample). Non-Anthropic providers never populate cache_* fields, so
	// counting them would drag the process-wide ratio down for reasons
	// unrelated to the prompt-cache doctrine. The three buckets are disjoint
	// (Anthropic usage semantics): InputTokens is the uncached remainder, not a
	// grand-total.
	if apiMode == llm.APIModeAnthropic && !fellBack {
		metrics.CacheHits.Record(
			int64(agentResult.Usage.CacheReadInputTokens),
			int64(agentResult.Usage.CacheCreationInputTokens),
			int64(agentResult.Usage.InputTokens),
		)
	}

	// Emit agent run.end event to gateway subscriptions.
	if deps.callbacks.emitAgentFn != nil {
		endData := map[string]any{
			"model":        model,
			"turns":        agentResult.Turns,
			"durationMs":   totalMs,
			"inputTokens":  agentResult.Usage.InputTokens,
			"outputTokens": agentResult.Usage.OutputTokens,
			"stopReason":   agentResult.StopReason,
		}
		if effortDecision != "" {
			endData["effortDecision"] = effortDecision
			endData["effortEscalated"] = effortRt != nil && effortRt.escalated
		}
		deps.callbacks.emitAgentFn("run.end", params.SessionKey, params.ClientRunID, endData)
	}

	// Log run.end to the agent detail log. This lives here — not in the
	// async-only handleRunSuccess — so the sync paths (SendSync, SendSyncStream)
	// pair every run.start with a run.end in the same session file. Orphaned
	// starts are invisible to agentlog.AggregateByModel (runs are counted at
	// run.end), which starved the modeltuner of all native-client interactive
	// runs. CompactionFired is re-read from the session because compaction can
	// fire during the run, after cachedSession was fetched; Proactive separates
	// autonomous/auto-delivered runs (heartbeat, cron relay) from user requests.
	compacted := false
	if deps.sessions != nil {
		if sess := deps.sessions.Get(params.SessionKey); sess != nil {
			compacted = sess.CompactionFired
		}
	}
	// RequestedModel is documented as "the model/role the run ASKED for
	// (params.Model)", but what was recorded here was the RESOLVED id, so a run
	// that asked for a role by name ("fallback", "submain") logged the model
	// that role happened to point at. Per-role usage then had to map the model
	// back to a role at read time, which fails the moment a role is retargeted —
	// the point at which someone is most likely to be looking. Record the asked
	// name when there is one; runs that asked for nothing keep logging the
	// resolved id so a session-level override stays attributable.
	runLog.LogEnd(agentlog.RunEndData{
		Model:               actualModel,
		RequestedModel:      requestedModelForLog(params.Model, model),
		StopReason:          agentResult.StopReason,
		Turns:               agentResult.Turns,
		InputTokens:         agentResult.Usage.InputTokens,
		OutputTokens:        agentResult.Usage.OutputTokens,
		TextLen:             len(agentResult.Text),
		CacheReadTokens:     agentResult.Usage.CacheReadInputTokens,
		CacheCreationTokens: agentResult.Usage.CacheCreationInputTokens,
		ToolCalls:           agentResult.TotalToolCalls,
		ToolCounts:          agentResult.ToolCounts,
		MaxTokensRecoveries: agentResult.MaxTokensRecoveries,
		Compacted:           compacted,
		Proactive:           params.AutoDeliveredOutput || params.EphemeralUser,
		EffortDecision:      effortDecision,
		EffortEscalated:     effortRt != nil && effortRt.escalated,
		RepairedToolCalls:   execStats.RepairedCounts(),
		CacheHitToolCalls:   execStats.CacheHitCounts(),
		TruncatedToolCalls:  execStats.TruncatedCounts(),
	})

	// Engine-side APC sample → run.cache event (async, best-effort). The vLLM
	// usage payload carries no cached_tokens, so the engine's global counters
	// are the only per-turn cache-hit signal on this path.
	logEngineCacheAsync(deps, runLog, client, actualModel, fellBack, logger)
}

// persistTurnUserMessage persists the inbound user message to the transcript
// (+ transcript event emit). Skipped when the turn is marked Ephemeral —
// autonomous self-triggers (heartbeat) share the user's session for context
// but must not crowd out the recent history window with their own trigger
// noise.
//
// The message is prepended with an ISO 8601 timestamp: the model gets the
// wall-clock time per-turn without relying on the system prompt (whose date
// field is day-only precision so the dynamic block stays byte-stable for
// trailing-message cache markers; see prompt-cache.md § 1). The timestamp is
// baked into the transcript so subsequent turns load a consistent history
// prefix — flipping to per-request hook injection would desync transcript
// history from what the LLM saw on prior turns and miss the cache.
// dentime.Now() (not time.Now()) so the baked offset matches the configured
// zone — on a UTC container with timezone set via deneb.json, time.Now()
// would stamp "...Z" while the system prompt and the rest of Deneb run in KST.
// Returns the formatted (timestamped) message actually persisted, or "" when
// persistence was skipped — the deferred-enrichment join uses the returned
// bytes as the wire message so transcript and LLM input stay byte-identical.
func persistTurnUserMessage(params RunParams, deps runDeps, logger *slog.Logger) string {
	if deps.transcript == nil || params.Message == "" || params.EphemeralUser {
		return ""
	}
	now := deps.now()
	formattedMessage := formatTurnUserMessage(params.Message, now)
	userMsg := NewTextChatMessage("user", formattedMessage, now.UnixMilli())
	if err := deps.transcript.Append(params.SessionKey, userMsg); err != nil {
		logger.Error("failed to persist user message", "error", err)
		deps.strictErrors.Record(err)
	}
	if deps.callbacks.emitTranscriptFn != nil {
		deps.callbacks.emitTranscriptFn(params.SessionKey, mustRawJSON(userMsg), "")
	}
	return formattedMessage
}

// formatTurnUserMessage renders the transcript form of an inbound user
// message: the ISO 8601 timestamp prefix + raw text (see the doc comment
// above for why the timestamp is baked in).
func formatTurnUserMessage(message string, now time.Time) string {
	return "[" + now.Format(time.RFC3339) + "] " + message
}

// ephemeralNeedsExplicitAppend reports whether this ephemeral turn's trigger
// message is absent from the loaded history and must be appended to the
// working message list wire-only. True only for history-bearing sessions:
// fresh sessions (empty history) are handled by assembleTurnMessages'
// scratch-build path exactly as before, and prebuilt-history API turns carry
// their own message. AppendCurrentMessage already set means the enrichment
// join handled it.
func ephemeralNeedsExplicitAppend(params RunParams, prep prepResult) bool {
	return params.EphemeralUser && !params.AppendCurrentMessage &&
		params.Message != "" && len(params.PrebuiltMessages) == 0 && len(prep.Messages) > 0
}

// applyTailAdditions collects and injects the per-turn wire-only additions
// (recall evidence / notebook grounding / skill hints / tone + delivery
// directives) into the LAST user message — NOT the system prompt. On the vLLM
// path the rendered prompt is [system][tool schemas][history] and APC is
// strict prefix matching — per-turn system-tail bytes invalidated the KV cache
// for the tools + entire history on every evidence-bearing turn (2026-06-13:
// 80.7% hit rate, 20-40s prefill tail on interactive turns). The transcript
// already persisted the clean user message, so next turn's history reload
// stays byte-identical to this turn's cached prefix. The degenerate
// no-user-message case returns the additions as tailForSystem so the caller
// falls back to the legacy system placement — evidence is never dropped. See
// run_tail_inject.go.
//
// Notebook grounding: when this session has an active notebook (opened via
// the notebook tool's open action), its pinned sources ride the same tail so
// the turn is grounded primarily in those sources (broad recall is suppressed
// for bound sessions in prepareContextAndPrompt — the notebook is the
// explicit scope). Skill context is gated on the run's effective preset: a
// preset without the skills tool (btw "conversation", code: "coding") must
// not acquire a new procedure outside its restricted role.
func applyTailAdditions(params RunParams, deps runDeps, prep prepResult, sessionToolPreset string, messages []llm.Message) ([]llm.Message, string, []string, []string) {
	notebookGrounding := ""
	if nbID, updated, ok := activeGroundingNotebook(deps, params.SessionKey); ok {
		if g, hit := cachedNotebookGrounding(params.SessionKey, nbID, updated); hit {
			notebookGrounding = g
		} else if g, gok := toolwire.BuildNotebookGrounding(&tooldeps.NotebookDeps{Store: deps.memory.Notebook, Wiki: deps.memory.Wiki}, nbID); gok {
			notebookGrounding = g
			storeNotebookGrounding(params.SessionKey, nbID, updated, g)
		}
	}
	var skillHints string
	var hintedSkills []string
	var autoLoadedSkills []string
	var autoActivatedTools []string
	if !deps.briefcaseMode {
		resolved := cachedResolvedSkills()
		skillHints, hintedSkills, autoLoadedSkills = buildSkillHints(params, sessionToolPreset, resolved)
		autoActivatedTools = skillRequiredDeferredTools(deps.tools, autoLoadedSkills, resolved, sessionToolPreset)
	}
	if len(hintedSkills) > 0 {
		// Keep the historical event name for dashboards while distinguishing
		// direct JIT loads from read-pointer fallbacks.
		deps.logger.Info("skill context injected",
			"session", params.SessionKey, "skills", strings.Join(hintedSkills, ","),
			"autoLoaded", strings.Join(autoLoadedSkills, ","),
			"autoActivatedTools", strings.Join(autoActivatedTools, ","))
		agentlog.LogTyped(deps.agentLog, params.SessionKey, "run.skillhints", map[string]any{
			"skills":             hintedSkills,
			"autoLoaded":         autoLoadedSkills,
			"autoActivatedTools": autoActivatedTools,
		})
	}
	// Re-attach the tails recorded for HISTORICAL user messages first: the
	// transcript stores clean messages (display/search never see recall
	// blocks), so without this the request bytes diverge from the previous
	// run's cache at the first tailed message and content-prefix providers
	// (kimi) re-bill the whole conversation every run (tail_register.go).
	cleanMessagesForOrdinal := messages
	if !deps.briefcaseMode {
		messages = attachPersistedTails(params.SessionKey, messages)
	}
	boardState := ""
	if !deps.briefcaseMode && !params.EphemeralUser {
		boardState = sessionBoardTail(peekSessionBlackboard(params.SessionKey))
	}
	tailAdds := buildTailAdditions(params, prep.RecallMemory, notebookGrounding, skillHints, boardState)
	messages, tailInjected, cleanTarget, tailTargetIdx := injectTailAdditionsTracked(messages, tailAdds)
	if tailInjected && shouldRecordTail(params, deps) {
		// Ordinal must be computed from clean transcript bytes — attachPersistedTails
		// mutates historical user messages in-place, and hashing tailed content
		// would collapse duplicate utterances ("확인", "ok") onto ord 0.
		recordPersistedTail(params.SessionKey, cleanTarget, userMessageHashOrdinal(cleanMessagesForOrdinal, tailTargetIdx), tailAdds)
	}
	tailForSystem := ""
	if !tailInjected {
		tailForSystem = strings.Join(tailAdds, "\n\n")
	}
	return messages, tailForSystem, autoLoadedSkills, autoActivatedTools
}

// shouldRecordTail reports whether this run's injected tail belongs in the
// persisted-tail register: only when the injection target is the current
// turn's transcript-persisted user message, i.e. the same bytes the next
// run's history reload returns. Ephemeral (heartbeat) triggers and prebuilt
// message lists are wire-only — recording them would attach tails to bytes
// that never reload.
func shouldRecordTail(params RunParams, deps runDeps) bool {
	return !deps.briefcaseMode && !params.EphemeralUser &&
		deps.transcript != nil && params.Message != "" &&
		len(params.PrebuiltMessages) == 0
}

// wireBeforeAPICall assembles cfg.BeforeAPICall and applies the provider's
// cache_control policy, returning the resolved API wire mode.
//
// The chain is built via agent.BeforeAPICallChain, which composes hooks by
// declared stage (PRE/NORMAL/POST) and rejects duplicate names — so "the
// trailing cache hook runs last" is a property it declares (HookStagePost),
// not a consequence of argument order, and a double registration is a logged
// conflict rather than a silent clobber. Nil hooks (disabled features) are
// skipped; an all-nil chain builds to nil.
//
//   - steer (NORMAL): drains SteerQueue notes into the last tool_result before
//     the call. No-op when the queue is nil (sub-agents, tests).
//   - trailingCache (POST): attaches ephemeral cache_control to the last 2
//     non-system messages (Hermes Agent's "system_and_3" pattern, scaled to
//     fit Anthropic's 4-breakpoint limit alongside our 2 system markers).
//     No-op for non-Anthropic providers.
//
// Some providers (Kimi) speak the Anthropic wire but REJECT cache_control with
// HTTP 400, so for cache-incompatible providers the system-block markers are
// stripped and the trailing-message hook skipped entirely. Mirrors OpenClaw's
// per-provider strip (extensions/kimi-coding). The builtin list lives in
// modelcaps; a `promptCache` boolean on the provider's deneb.json entry
// overrides it either way. The strip operates on the per-request cfg.System
// copy, so the prompt-cache doctrine (don't mutate cached blocks) holds.
func wireBeforeAPICall(cfg *agent.AgentConfig, deps runDeps, params RunParams, providerID, model string, client *llm.Client, logger *slog.Logger) string {
	apiMode := resolveAPIMode(deps, providerID)
	if client != nil {
		// The selected client is the wire authority. A handler-scoped client
		// (including Briefcase) can intentionally have no provider ID, and a
		// provider config can be stale relative to the already-built client.
		apiMode = client.APIMode()
	}
	trailingCache := buildTrailingCacheHook(apiMode)
	if deps.briefcaseMode {
		// Prompt-cache metadata is an endpoint capability and a performance
		// optimization, not part of the scored task. Generic Anthropic-compatible
		// endpoints may reject it, so deterministic Briefcase requests strip the
		// system markers and never add trailing markers.
		cfg.System = stripCacheControlMarkers(cfg.System)
		trailingCache = nil
	} else if modelCapability(deps, providerID, model).RejectsCacheControl {
		cfg.System = stripCacheControlMarkers(cfg.System)
		trailingCache = nil
	}
	var apc agent.BeforeAPICallChain
	apc.Add("steer", agent.HookStageNormal, buildSteerHookIfEnabled(deps.steerQueue, params.SessionKey, logger))
	apc.Add("trailing-cache", agent.HookStagePost, trailingCache)
	cfg.BeforeAPICall = apc.Build(logger)
	return apiMode
}

// emitPhase publishes a phase.changed lifecycle event so SSE
// subscribers can render live phase progression (the pattern the retired
// Telegram status controller established). Silently no-ops when the agent
// emit callback is unset (sub-agents, tests).
func emitPhase(deps runDeps, params RunParams, phase string, at time.Time) {
	if params.OnProgress != nil {
		params.OnProgress(phase)
	}
	if deps.callbacks.emitAgentFn == nil {
		return
	}
	deps.callbacks.emitAgentFn("phase.changed", params.SessionKey, params.ClientRunID, map[string]any{
		"phase": phase,
		"ts":    at.UnixMilli(),
	})
}

// activeGroundingNotebook reports the notebook a session is grounded in and its
// Updated stamp, but ONLY when the notebook exists and has at least one source.
// An empty/missing notebook (or nil store) returns ok=false so that recall
// suppression (run_prepare.go) and grounding injection stay symmetric — a
// bound-but-contentless turn keeps broad recall instead of running with
// neither. The Updated stamp is the grounding cache's content version: any
// pin/unpin/mode change bumps it (notebook.Store), invalidating the snapshot.
func activeGroundingNotebook(deps runDeps, sessionKey string) (id string, updated int64, ok bool) {
	if deps.memory.Notebook == nil {
		return "", 0, false
	}
	id = toolport.ActiveNotebook(sessionKey)
	if id == "" {
		return "", 0, false
	}
	nb, found := deps.memory.Notebook.Get(id)
	if !found || len(nb.Sources) == 0 {
		return "", 0, false
	}
	return id, nb.Updated, true
}

// requestedModelForLog picks what a run is attributed to in per-role usage.
//
// asked is params.Model as the caller wrote it — a role name ("fallback",
// "submain"), a raw model id, or "" for the default. resolved is what that
// turned into after registry resolution.
//
// The asked name wins whenever there is one. Recording the resolved id instead
// forced per-role usage to map model back to role at READ time, which fails the
// moment a role is retargeted — precisely when someone is looking at the usage
// screen. A run that asked for nothing keeps the resolved id so a session-level
// model override stays attributable rather than collapsing into main.
func requestedModelForLog(asked, resolved string) string {
	if asked != "" {
		return asked
	}
	return resolved
}
