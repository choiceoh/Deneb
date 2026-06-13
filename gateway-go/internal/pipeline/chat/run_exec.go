// run_exec.go contains the core agent execution loop: user message persistence,
// context assembly, LLM invocation with model fallback.
package chat

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/metrics"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
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
	statusCtrl statusReactor,
	logger *slog.Logger,
	runLog *agentlog.RunLogger,
) (*chatRunResult, error) {
	runStart := time.Now()

	// Emit agent run.start event to gateway subscriptions.
	if deps.callbacks.emitAgentFn != nil {
		deps.callbacks.emitAgentFn("run.start", params.SessionKey, params.ClientRunID, map[string]any{
			"model": params.Model,
			"ts":    runStart.UnixMilli(),
		})
	}

	// Signal "preparing" phase — covers parallel context assembly, system prompt
	// build, and recall preflight setup. The status controller debounces this
	// against the prior Queued state so very fast preps (<700ms) keep showing
	// 👀 instead of flickering to 📚 and back. WebSocket clients receive a
	// structured phase.changed event for the same transition.
	emitPhase(deps, params, "preparing", runStart)
	if statusCtrl != nil {
		statusCtrl.SetPreparing()
	}

	// 1. Persist user message to transcript + Aurora store. Skipped when the
	// turn is marked Ephemeral — autonomous self-triggers (heartbeat) share
	// the user's session for context but must not crowd out the recent
	// history window with their own trigger noise.
	if deps.transcript != nil && params.Message != "" && !params.EphemeralUser {
		// Prepend an ISO 8601 timestamp to the user message text. The model
		// gets the wall-clock time per-turn without relying on the system
		// prompt (whose date field is day-only precision so the dynamic
		// block stays byte-stable for trailing-message cache markers; see
		// prompt-cache.md § 1). The timestamp is baked into the transcript
		// so subsequent turns load a consistent history prefix — flipping
		// to per-request hook injection would desync transcript history
		// from what the LLM saw on prior turns and miss the cache.
		// dentime.Now() (not time.Now()) so the baked offset matches the
		// configured zone — on a UTC container with timezone set via
		// deneb.json, time.Now() would stamp "...Z" while the system prompt
		// and the rest of Deneb run in KST (see prompt-cache.md § 1).
		now := dentime.Now()
		formattedMessage := "[" + now.Format(time.RFC3339) + "] " + params.Message
		userMsg := NewTextChatMessage("user", formattedMessage, now.UnixMilli())
		if err := deps.transcript.Append(params.SessionKey, userMsg); err != nil {
			logger.Error("failed to persist user message", "error", err)
		}
		if deps.callbacks.emitTranscriptFn != nil {
			deps.callbacks.emitTranscriptFn(params.SessionKey, userMsg, "")
		}
	}
	workspaceDir := params.WorkspaceDir
	if workspaceDir == "" {
		workspaceDir = resolveWorkspaceDirForPrompt()
	}

	// Pre-warm context file snapshot for this session so disk I/O happens
	// before the parallel prep phase (no-op if already cached from a prior turn).
	prompt.LoadContextFiles(workspaceDir, prompt.WithSessionSnapshot(params.SessionKey))

	// Cache session lookup: fetched once and reused throughout this function
	// to avoid repeated map lookups + lock acquisitions.
	var cachedSession *session.Session
	if deps.sessions != nil {
		cachedSession = deps.sessions.Get(params.SessionKey)
	}

	// 2. Resolve model and provider early (no IO — pure config/registry lookups).
	mr := resolveModel(params, deps, cachedSession)
	model := mr.model
	providerID := mr.providerID
	initialRole := mr.initialRole

	thinkingLevel := ""
	if cachedSession != nil && cachedSession.ThinkingLevel != "" && cachedSession.ThinkingLevel != "off" {
		thinkingLevel = cachedSession.ThinkingLevel
	}
	runLog.LogStart(agentlog.RunStartData{
		Model:         model,
		Provider:      providerID,
		Message:       params.Message,
		Channel:       deliveryChannel(params.Delivery),
		ThinkingLevel: thinkingLevel,
	})

	// 3. Resolve LLM client (no IO — reads in-memory config/auth store).
	client := resolveClient(deps, providerID, logger)
	if client == nil {
		err := fmt.Errorf("no LLM client available (provider=%q, model=%q)", providerID, model)
		runLog.LogError(agentlog.RunErrorData{Error: err.Error()})
		return nil, err
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
	prep := prepareContextAndPrompt(ctx, params, deps, workspaceDir, sessionToolPreset, statusCtrl, logger)
	logger.Info("pipeline: parallel prep done (context+sysprompt)", "ms", time.Since(prepStart).Milliseconds())

	if prep.ContextErr != nil {
		logger.Error("context assembly failed, proceeding with degraded context",
			"sessionKey", params.SessionKey, "error", prep.ContextErr)
	}

	// Stage 2: Assemble final message list (prebuilt, attachments, Polaris compaction).
	var cHooks *compactionHooks
	if statusCtrl != nil || (deps.callbacks.typingFn != nil && params.Delivery != nil) {
		cHooks = &compactionHooks{}
		if statusCtrl != nil {
			cHooks.onStart = statusCtrl.SetCompacting
		}
		if deps.callbacks.typingFn != nil && params.Delivery != nil {
			delivery := params.Delivery
			typingFn := deps.callbacks.typingFn
			cHooks.typingFn = func() {
				tCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = typingFn(tCtx, delivery)
			}
		}
	}
	messages := assembleMessages(ctx, params, deps, prep, mr, logger, cHooks)

	// Per-turn additions (recall evidence + auto-delivery directive) ride the
	// LAST user message as a wire-only suffix, NOT the system prompt. On the
	// vLLM path the rendered prompt is [system][tool schemas][history] and APC
	// is strict prefix matching — per-turn system-tail bytes invalidated the
	// KV cache for the tools + entire history on every evidence-bearing turn
	// (2026-06-13: 80.7% hit rate, 20-40s prefill tail on interactive turns).
	// The transcript already persisted the clean user message, so next turn's
	// history reload stays byte-identical to this turn's cached prefix. The
	// degenerate no-user-message case falls back to the legacy system
	// placement so evidence is never dropped. See run_tail_inject.go.
	tailAdds := buildTailAdditions(params, prep.RecallMemory)
	messages, tailInjected := injectTailAdditions(messages, tailAdds)
	tailForSystem := ""
	if !tailInjected {
		tailForSystem = strings.Join(tailAdds, "\n\n")
	}

	// Stage 3: Finalize system prompt (budget optimization, coordinator suggestion, tier-1 injection).
	systemPrompt := finalizePrompt(prep.SystemPrompt, tailForSystem, prep.Tier1Wiki, deps.contextCfg, sessionToolPreset, params.Message)

	logger.Info("pipeline: system prompt finalized",
		"chars", len(systemPrompt))

	runLog.LogPrep(agentlog.RunPrepData{
		SystemPromptChars: len(systemPrompt),
		ContextMessages:   len(messages),
		PrepMs:            time.Since(runStart).Milliseconds(),
		RecallChars:       len(prep.RecallMemory),
	})

	// Stage 3.5: APC prefix-stability diagnostics — classify how this run's
	// assembled prompt diverges from the session's previous run and bracket
	// the engine prefix-cache counters around the run. Deferred so the "apc
	// diag" line is emitted on error paths too. See apc_diag.go.
	apcDiag := beginAPCDiag(ctx, deps, params.SessionKey, providerID, model, systemPrompt, prep.RecallMemory, messages, logger)
	defer apcDiag.finish()

	// Stage 4: Build tool list and agent config.
	acd := agentConfigDeps{
		Tools:              deps.tools,
		MaxTokens:          deps.maxTokens,
		SubagentNotifyCh:   deps.subagentNotifyCh,
		EmitAgentFn:        deps.callbacks.emitAgentFn,
		Transcript:         deps.transcript,
		SkillNudger:        deps.skillNudger,
		SkillUsageRecorder: deps.skillUsageRecorder,
	}
	cfg, spawnFlag := buildAgentConfig(params, deps, cachedSession, systemPrompt, sessionToolPreset, acd, logger)
	cfg.Model = model // set the resolved model
	// Per-model defaults (profile sampling, tuned max-tokens floor) — only
	// fills values the request left unset; request-level params, cache-safe.
	applyModelTuning(&cfg, deps, params, providerID, model)
	// Adaptive effort router: obviously-simple conversational messages on
	// dual-mode models (capability-gated, provider-aware) skip the thinking
	// phase (KV-prefix-safe). Routed runs may escalate back to thinking in
	// runAgentWithFallback, which needs the route to restore the original.
	effortRt, effortDecision := applyEffortRouter(&cfg, params, messages, routingProfileForRun(deps, providerID, model), logger)

	// BeforeAPICall hook chain: composed via agent.ComposeBeforeAPICall so
	// features can register additional pre-LLM transforms without clobbering
	// each other. ComposeBeforeAPICall filters nil entries and returns nil
	// when every slot is empty, so assignment is safe.
	//
	//  - steer: drains SteerQueue notes into the last tool_result before the
	//    call. No-op when the queue is nil (sub-agents, tests).
	//  - trailingCache: attaches ephemeral cache_control to the last 2
	//    non-system messages (Hermes Agent's "system_and_3" pattern, scaled
	//    to fit Anthropic's 4-breakpoint limit alongside our 2 system
	//    markers). No-op for non-Anthropic providers.
	apiMode := resolveAPIMode(deps, providerID)
	// Some providers (Kimi) speak the Anthropic wire but REJECT cache_control
	// with HTTP 400, so for cache-incompatible providers strip the system-block
	// markers and skip the trailing-message hook entirely. Mirrors OpenClaw's
	// per-provider strip (extensions/kimi-coding). The builtin list lives in
	// modelcaps; a `promptCache` boolean on the provider's deneb.json entry
	// overrides it either way. The strip operates on the per-request cfg.System
	// copy, so the prompt-cache doctrine (don't mutate cached blocks) holds.
	trailingCache := buildTrailingCacheHook(apiMode)
	if modelCapability(deps, providerID, model).RejectsCacheControl {
		cfg.System = stripCacheControlMarkers(cfg.System)
		trailingCache = nil
	}
	cfg.BeforeAPICall = agent.ComposeBeforeAPICall(
		buildSteerHookIfEnabled(deps.steerQueue, params.SessionKey, logger),
		trailingCache,
	)

	// Set up stream hooks via compositor: fan-out dispatch for each hook type.
	var hc agent.HookCompositor
	wireStreamHooks(&hc, params, deps, broadcaster, typingSignaler, statusCtrl)

	hooks := hc.Build()

	logger.Info("pipeline: prep complete, starting agent loop",
		"prepMs", time.Since(runStart).Milliseconds(),
		"model", model, "provider", providerID,
		"messages", len(messages), "tools", len(cfg.Tools))

	// Execute agent loop with model fallback chain.
	agentStart := time.Now()
	agentResult, actualModel, fellBack, err := runAgentWithFallback(ctx, cfg, messages, client, deps, providerID, initialRole, effortRt, hooks, logger, runLog)
	if err != nil && effortDecision != "" {
		// The failed-run record matters MOST for the label pipeline: a
		// routed run that escalated and still failed is the strongest
		// misjudgment signal. The success-path record rides on the
		// "agent loop complete" line below.
		logger.Info("effort router: run failed",
			"decision", effortDecision,
			"escalated", effortRt != nil && effortRt.escalated,
			"model", actualModel, "error", err)
	}
	if err != nil {
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

	agentMs := time.Since(agentStart).Milliseconds()
	totalMs := time.Since(runStart).Milliseconds()
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
	logger.Info("pipeline: agent loop complete",
		"effortDecision", effortDecision,
		"effortEscalated", effortRt != nil && effortRt.escalated,
		"agentMs", agentMs,
		"totalMs", totalMs,
		"turns", agentResult.Turns,
		"inputTokens", agentResult.Usage.InputTokens,
		"outputTokens", agentResult.Usage.OutputTokens,
		"cacheReadTokens", agentResult.Usage.CacheReadInputTokens,
		"cacheCreationTokens", agentResult.Usage.CacheCreationInputTokens,
		"stopReason", agentResult.StopReason,
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
	runLog.LogEnd(agentlog.RunEndData{
		Model:               actualModel,
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
	})

	// Engine-side APC sample → run.cache event (async, best-effort). The vLLM
	// usage payload carries no cached_tokens, so the engine's global counters
	// are the only per-turn cache-hit signal on this path.
	logEngineCacheAsync(deps, runLog, client, apiMode, fellBack, logger)

	return &chatRunResult{AgentResult: agentResult, SpawnFlag: spawnFlag, ActualModel: actualModel, FellBack: fellBack}, nil
}

// emitPhase publishes a phase.changed lifecycle event so WebSocket
// subscribers can render the same phase progression the Telegram status
// controller does. Silently no-ops when the agent emit callback is unset
// (sub-agents, tests).
func emitPhase(deps runDeps, params RunParams, phase string, at time.Time) {
	if deps.callbacks.emitAgentFn == nil {
		return
	}
	deps.callbacks.emitAgentFn("phase.changed", params.SessionKey, params.ClientRunID, map[string]any{
		"phase": phase,
		"ts":    at.UnixMilli(),
	})
}

func formatToolHist(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	type kv struct {
		name  string
		count int
	}
	pairs := make([]kv, 0, len(counts))
	for k, v := range counts {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].name < pairs[j].name
	})
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, fmt.Sprintf("%s:%d", p.name, p.count))
	}
	return strings.Join(parts, ",")
}
