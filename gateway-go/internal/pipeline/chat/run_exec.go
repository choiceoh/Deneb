// run_exec.go contains the core agent execution loop: user message persistence,
// context assembly, LLM invocation with model fallback.
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/metrics"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolpreset"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	compact "github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
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
		return nil, fmt.Errorf("no LLM client available (provider=%q, model=%q)", providerID, model)
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

	// Stage 3: Finalize system prompt (budget optimization, coordinator suggestion, tier-1 injection).
	systemPrompt := finalizePrompt(prep.SystemPrompt, prep.RecallMemory, prep.Tier1Wiki, deps.contextCfg, sessionToolPreset, params.Message)

	logger.Info("pipeline: system prompt finalized",
		"chars", len(systemPrompt))

	runLog.LogPrep(agentlog.RunPrepData{
		SystemPromptChars: len(systemPrompt),
		ContextMessages:   len(messages),
		PrepMs:            time.Since(runStart).Milliseconds(),
		RecallChars:       len(prep.RecallMemory),
	})

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
	agentResult, actualModel, fellBack, err := runAgentWithFallback(ctx, cfg, messages, client, deps, providerID, initialRole, hooks, logger, runLog)
	if err != nil {
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
	logger.Info("pipeline: agent loop complete",
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
		deps.callbacks.emitAgentFn("run.end", params.SessionKey, params.ClientRunID, map[string]any{
			"model":        model,
			"turns":        agentResult.Turns,
			"durationMs":   totalMs,
			"inputTokens":  agentResult.Usage.InputTokens,
			"outputTokens": agentResult.Usage.OutputTokens,
			"stopReason":   agentResult.StopReason,
		})
	}

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

// ---------------------------------------------------------------------------
// Extracted stages: prepareContextAndPrompt, assembleMessages, finalizePrompt,
// buildAgentConfig. These are called sequentially from executeAgentRun.
// ---------------------------------------------------------------------------

// prepResult holds the output of the parallel context/prompt preparation stage.
type prepResult struct {
	Messages     []llm.Message
	SystemPrompt json.RawMessage
	RecallMemory string
	Tier1Wiki    string
	ContextErr   error
}

// prepareContextAndPrompt runs wiki injection, context assembly, and system prompt
// build in parallel. Returns the combined results.
//
// statusCtrl is optional: when non-nil, the recall goroutine signals
// SetRecalling on a true cache miss (cue present + no cached evidence) so
// the user sees 🧠 only when memory search is actually happening, not for
// every prep call.
func prepareContextAndPrompt(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	workspaceDir string,
	sessionToolPreset string,
	statusCtrl statusReactor,
	logger *slog.Logger,
) prepResult {
	var result prepResult
	var resultMu sync.Mutex
	var prepWg sync.WaitGroup

	// Tier-1 wiki auto-injection (parallel).
	prepWg.Add(1)
	safego.GoWithSlog(logger, "prep-tier1-wiki", func() {
		defer prepWg.Done()
		var tier1 string
		if deps.wikiStore != nil {
			cfg := wiki.ConfigFromEnv()
			tier1 = knowledge.FormatTier1(deps.wikiStore, cfg.Tier1MinImportance)
		}
		resultMu.Lock()
		result.Tier1Wiki = tier1
		resultMu.Unlock()
	})

	// Recall preflight (parallel): inject focused memory before the LLM call.
	//
	// Two modes feed one <recall-context> block:
	//   - Cue-gated sources (wiki/diary/transcript/polaris) run only when the
	//     user message implies past context. Their result is cached per
	//     (session, cue-fingerprint) so repeat questions on the same topic
	//     reuse the ~6s of parallel search timeouts.
	//   - Hindsight auto-recall runs every turn when configured (the Hermes
	//     auto_recall model): the memory bank is queried with the current
	//     message regardless of cue. No-cue turns are not cached — each
	//     turn's message is a distinct query the "" fingerprint cannot
	//     disambiguate. /reset clears every slot. See chat/recall_cache.go.
	prepWg.Add(1)
	safego.GoWithSlog(logger, "prep-recall", func() {
		defer prepWg.Done()
		// Ephemeral turns (autonomous heartbeat self-triggers) never run
		// recall — there is no real user message to recall against.
		if params.EphemeralUser {
			return
		}
		fingerprint := recallCueFingerprint(params.Message)
		hasCue := fingerprint != ""
		// Hermes-style auto_recall: run the preflight every turn, not just cue turns.
		// buildRecallPreflight searches wiki/diary/polaris/transcript/hindsight and returns
		// "" silently when there's no evidence, so non-cue turns add latency but no noise.
		if hasCue {
			if cached, ok := cachedRecallMemory(params.SessionKey, fingerprint); ok {
				resultMu.Lock()
				result.RecallMemory = cached
				resultMu.Unlock()
				return
			}
			// Explicit recall: surface the 🧠 phase so the user sees the
			// wiki/diary/transcript search instead of a frozen 📚. Silent
			// auto-recall on no-cue turns stays invisible.
			emitPhase(deps, params, "recalling", time.Now())
			if statusCtrl != nil {
				statusCtrl.SetRecalling()
			}
		}
		recallMemory := buildRecallPreflight(ctx, params, deps, logger)
		if hasCue && recallMemoryHasEvidence(recallMemory) {
			storeRecallMemory(params.SessionKey, fingerprint, recallMemory)
		}
		resultMu.Lock()
		result.RecallMemory = recallMemory
		resultMu.Unlock()
	})

	// Context assembly (parallel).
	prepWg.Add(1)
	safego.GoWithSlog(logger, "prep-context", func() {
		defer prepWg.Done()

		var messages []llm.Message
		var contextErr error
		if bridge, ok := deps.transcript.(*polaris.Bridge); ok {
			ctxResult, err := assembleContext(bridge, params.SessionKey, deps.contextCfg, logger)
			if err != nil {
				contextErr = err
			} else {
				messages = ctxResult.Messages
				// Log-only telemetry for truncation. Do NOT inject a synthetic
				// notice message here: bootstrapIfNeeded (inside CompactAndPersist)
				// recovers dropped messages by computing olderEnd from len(messages),
				// so any synthetic prepend inflates the count and orphans the
				// fresh-tail boundary message, causing "right-after-compaction
				// previous turn forgotten" regressions.
				if !ctxResult.WasCompacted && ctxResult.TotalMessages > len(ctxResult.Messages) && len(ctxResult.Messages) > 0 {
					logger.Warn("context truncated without summaries (bootstrap will recover)",
						"total", ctxResult.TotalMessages,
						"loaded", len(ctxResult.Messages),
						"dropped", ctxResult.TotalMessages-len(ctxResult.Messages),
						"session", params.SessionKey)
				}
			}
		}
		resultMu.Lock()
		result.Messages = messages
		result.ContextErr = contextErr
		resultMu.Unlock()
	})

	// System prompt build (parallel).
	prepWg.Add(1)
	safego.GoWithSlog(logger, "prep-sysprompt", func() {
		defer prepWg.Done()
		var systemPrompt json.RawMessage
		if params.System != "" {
			systemPrompt = llm.SystemString(params.System)
			resultMu.Lock()
			result.SystemPrompt = systemPrompt
			resultMu.Unlock()
			return
		}
		if deps.defaultSystem != "" {
			systemPrompt = llm.SystemString(deps.defaultSystem)
			resultMu.Lock()
			result.SystemPrompt = systemPrompt
			resultMu.Unlock()
			return
		}
		if deps.tools == nil {
			return
		}
		tz, _ := prompt.LoadCachedTimezone()
		ch := deliveryChannel(params.Delivery)
		// Build tool defs — filtered if a preset is active.
		allowed := toolpreset.AllowedTools(toolpreset.Preset(sessionToolPreset))
		toolDefs := toPromptToolDefs(deps.tools.FilteredDefinitions(allowed))

		// Deferred tool summaries for system prompt listing.
		deferredSummaries := deps.tools.DeferredSummaries()
		var deferredToolInfos []prompt.DeferredToolInfo
		for _, ds := range deferredSummaries {
			// Skip deferred tools not in the allowed preset (if preset is active).
			if _, ok := allowed[ds.Name]; len(allowed) > 0 && !ok {
				continue
			}
			deferredToolInfos = append(deferredToolInfos, prompt.DeferredToolInfo{
				Name:        ds.Name,
				Description: ds.Description,
			})
		}

		// P4: read CompactionFired from session right before assembly so
		// the system prompt's one-time compaction reminder appears from
		// the turn after first compaction onward. Sticky flag — once set
		// it stays set, keeping the dynamic block byte-stable for the
		// trailing message cache markers' prefix matching.
		compactionFired := false
		if deps.sessions != nil {
			if sess := deps.sessions.Get(params.SessionKey); sess != nil {
				compactionFired = sess.CompactionFired
			}
		}

		// Per-topic knowledge: map the forum threadID (from the delivery
		// context) to a topic key, then load <dir>/<key>.md (frozen per
		// session). The content joins the Static cache block; topicCacheKey
		// keys that cache per topic + content hash so topics never collide and
		// edits invalidate. Unmapped/missing → empty (no injection, no cache
		// key change → topic-less Static cache stays shared).
		var topicKnowledge, topicCacheKey, topicKnowledgePath string
		if deps.topicResolver != nil && params.Delivery != nil {
			if key := deps.topicResolver.TopicKey(params.Delivery.ThreadID); key != "" {
				tk := prompt.LoadTopicKnowledge(workspaceDir, deps.topicResolver.Dir(), key, params.SessionKey)
				if tk.Content != "" {
					topicKnowledge = tk.Content
					topicCacheKey = tk.Key + ":" + tk.Hash
					topicKnowledgePath = tk.Path
				}
			}
		}

		// Ambient calendar glance for the dynamic block. The provider freezes
		// it per day, so this is a cheap cache hit on all but the first turn of
		// the day; "" when no calendar source or no upcoming events.
		var calendarGlance string
		if deps.calendarGlanceFn != nil {
			calendarGlance = deps.calendarGlanceFn(ctx, params.SessionKey, tz)
		}

		spp := prompt.SystemPromptParams{
			WorkspaceDir:        workspaceDir,
			ToolDefs:            toolDefs,
			DeferredTools:       deferredToolInfos,
			UserTimezone:        tz,
			ContextFiles:        prompt.LoadContextFiles(workspaceDir, prompt.WithSessionSnapshot(params.SessionKey)),
			RuntimeInfo:         prompt.BuildDefaultRuntimeInfo(params.Model, deps.callbacks.defaultModel),
			Channel:             ch,
			SkillsPrompt:        loadCachedSkillsPrompt(workspaceDir, availableToolNames(deps.tools)),
			ToolPreset:          sessionToolPreset,
			CompactionFired:     compactionFired,
			AutoDeliveredOutput: params.AutoDeliveredOutput,
			HindsightEnabled:    deps.hindsightClient != nil,
			CalendarGlance:      calendarGlance,
			TopicKnowledge:      topicKnowledge,
			TopicCacheKey:       topicCacheKey,
			TopicKnowledgePath:  topicKnowledgePath,
			SupportsRichUI:      richUIChannel(ch),
		}

		systemPrompt = llm.SystemBlocks(prompt.BuildSystemPromptBlocks(spp))
		resultMu.Lock()
		result.SystemPrompt = systemPrompt
		resultMu.Unlock()
	})

	prepWg.Wait()
	return result
}

// compactionHooks holds optional callbacks for the STW compaction phase.
// When LLM compaction fires, these hooks provide user-visible feedback
// (status emoji + typing keepalive) so the user knows the system is working.
type compactionHooks struct {
	onStart  func() // called when LLM compaction begins (e.g. set ✍ emoji)
	typingFn func() // sends typing indicator every 5s during compaction
}

// minCompactionBudget is the floor below which an effective context budget is
// treated as a history-suppression sentinel rather than a real budget. Real
// budgets are tens of thousands of tokens (boot passes 30K, defaults are
// 140K+); the only sub-floor caller is the skill-review fork's
// MaxHistoryTokens=1. A single protected turn already exceeds such a budget,
// so compaction cannot succeed by construction.
const minCompactionBudget = 1024

// skipCompactionBudget reports whether the effective context budget is a
// deliberate history-suppression sentinel, in which case Polaris compaction
// is skipped entirely instead of burning every tier and warning each run.
// Zero means "no budget configured" and keeps the legacy behavior.
func skipCompactionBudget(budget int) bool {
	return budget > 0 && budget < minCompactionBudget
}

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
	messages := prep.Messages

	// If the caller provided pre-built messages (e.g., OpenAI-compatible HTTP API
	// with full conversation history), use those instead of transcript context.
	if len(params.PrebuiltMessages) > 0 {
		// Copy to avoid aliasing the caller's backing array. Without the copy,
		// append may write into shared capacity, corrupting the original slice.
		messages = append([]llm.Message(nil), params.PrebuiltMessages...)
		// When the caller also supplies a Message, append it so the LLM sees
		// it without re-loading the entire transcript.
		if params.Message != "" && len(params.Attachments) == 0 {
			messages = append(messages, llm.NewTextMessage("user", params.Message))
		}
	}

	// Build or augment user message with attachments.
	if len(messages) == 0 && params.Message != "" {
		// No history — build the user message from scratch.
		if len(params.Attachments) > 0 {
			blocks := buildAttachmentBlocks(params.Message, params.Attachments)
			messages = []llm.Message{llm.NewBlockMessage("user", blocks)}
		} else {
			messages = []llm.Message{llm.NewTextMessage("user", params.Message)}
		}
	} else if len(messages) > 0 && len(params.Attachments) > 0 {
		// History exists but current message has attachments — replace the
		// last user message (which was persisted as text-only) with a
		// multimodal version that includes the image/video content blocks.
		messages = appendAttachmentsToHistory(messages, params.Message, params.Attachments)
	}

	// Model marked non-vision (provider config `vision: false`): replace image
	// blocks with text stubs up front instead of letting the provider reject
	// the request. Only fires on an explicit override — unknown models are
	// assumed vision-capable.
	if modelCapability(deps, mr.providerID, mr.model).NoVision {
		messages = compact.StripImageBlocks(messages)
	}

	// Polaris compaction: tiered context compression.
	// Applied after message assembly, before prompt finalization.
	// STW (Stop-the-World): when LLM compaction fires, the user sees a
	// ✍ status emoji and typing keepalive until compaction completes.
	// No LLM call is made until context is compressed — incoming messages
	// are already queued by PendingQueue during the active run.
	if len(messages) > 0 {
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

		polarisCtx, polarisCancel := context.WithTimeout(ctx, 2*time.Minute)
		var summarizer compact.Summarizer
		if pilotHub := pilot.LocalAIHub(); pilotHub != nil {
			summarizer = &localAISummarizer{}
		}

		// STW: pre-check if LLM compaction will likely fire.
		// Signal the user before the (potentially slow) summarization starts.
		var compactTypingDone chan struct{}
		var compactStart time.Time
		if hooks != nil && summarizer != nil {
			currentTokens := compact.EstimateMessagesTokens(messages)
			threshold := int(float64(contextBudget) * compact.DefaultLLMThresholdPct)
			if currentTokens > threshold {
				compactStart = time.Now()
				if hooks.onStart != nil {
					hooks.onStart()
				}
				logger.Info("pipeline: STW compaction starting",
					"tokens", currentTokens, "budget", contextBudget,
					"ratio", fmt.Sprintf("%.1f%%", float64(currentTokens)/float64(contextBudget)*100))
				if hooks.typingFn != nil {
					compactTypingDone = make(chan struct{})
					typingFn := hooks.typingFn
					typingLogger := logger
					go func() {
						defer func() {
							if r := recover(); r != nil {
								typingLogger.Error("panic in compaction typing loop", "panic", r)
							}
						}()
						ticker := time.NewTicker(5 * time.Second)
						defer ticker.Stop()
						for {
							select {
							case <-compactTypingDone:
								return
							case <-ctx.Done():
								return
							case <-ticker.C:
								typingFn()
							}
						}
					}()
				}
			}
		}

		var polarisResult compact.Result
		if bridge, ok := deps.transcript.(*polaris.Bridge); ok {
			engine := bridge.Engine()
			if deps.embeddingClient != nil {
				engine.SetEmbedder(deps.embeddingClient)
			}
			engine.SetAnchorKeywords(buildAnchorKeywords(deps.wikiStore))
			messages, polarisResult = engine.CompactAndPersist(polarisCtx, params.SessionKey, messages, summarizer, contextBudget)

			// Proactive condensation: when a new leaf summary was persisted,
			// trigger background condensation to merge leaves into higher-level nodes.
			// Runs in its own goroutine with a bounded timeout so it cannot
			// outlive sensible lifetime and cannot take down the process on panic.
			if polarisResult.LLMCompacted && summarizer != nil {
				condSummarizer := summarizer // capture for goroutine
				sessionKey := params.SessionKey
				condLogger := logger
				// Decouple from the request ctx so Condense outlives the agent turn,
				// but derive from the server shutdown ctx so a graceful shutdown
				// cancels it. Falls back to Background if shutdownCtx isn't wired
				// yet (e.g. in tests) — still bounded by the timeout below.
				parentCtx := deps.callbacks.shutdownCtx
				if parentCtx == nil {
					parentCtx = context.Background()
				}
				go func() { //nolint:gosec // G118 — decoupled from request ctx on purpose; bounded timeout below
					defer func() {
						if r := recover(); r != nil {
							condLogger.Error("panic in background condense", "session", sessionKey, "panic", r)
						}
					}()
					// Bounded by a 5-minute timeout so it cannot leak forever.
					condCtx, cancel := context.WithTimeout(parentCtx, 5*time.Minute)
					defer cancel()
					if err := engine.Condense(condCtx, sessionKey, condSummarizer); err != nil {
						condLogger.Warn("background condense failed", "session", sessionKey, "error", err)
					}
				}()
			}
		} else {
			cfg := compact.NewConfig(contextBudget)
			cfg.Embedder = deps.embeddingClient
			// Incremental recompaction: feed the prior summary so the LLM tier
			// UPDATES it (In Progress → Done) instead of re-summarizing from
			// scratch, then store the new summary for next time. In-memory on
			// the session; a /reset (new Session) or restart clears it.
			var compSession *session.Session
			if deps.sessions != nil {
				compSession = deps.sessions.Get(params.SessionKey)
			}
			if compSession != nil {
				cfg.PreviousSummary = compSession.PreviousCompactionSummary
			}
			messages, polarisResult = compact.Compact(polarisCtx, cfg, messages, summarizer, logger)
			if compSession != nil && polarisResult.Summary != "" {
				compSession.PreviousCompactionSummary = polarisResult.Summary
			}
		}
		polarisCancel()

		if compactTypingDone != nil {
			close(compactTypingDone)
		}
		if !compactStart.IsZero() {
			logger.Info("pipeline: STW compaction done",
				"durationMs", time.Since(compactStart).Milliseconds())
		}

		if polarisResult.MicroPruned > 0 || polarisResult.LLMCompacted || polarisResult.EmbeddingCompacted || polarisResult.RecencyCompacted || polarisResult.EmergencyEvicted > 0 {
			var tier string
			switch {
			case polarisResult.EmergencyEvicted > 0:
				tier = "emergency"
			case polarisResult.LLMCompacted:
				tier = "tier1-llm"
			case polarisResult.EmbeddingCompacted:
				tier = "tier2-embedding-mmr"
			case polarisResult.RecencyCompacted:
				tier = "tier3-recency"
			default:
				tier = "micro"
			}
			attrs := []any{"tokensBefore", polarisResult.TokensBefore, "tokensAfter", polarisResult.TokensAfter}
			if polarisResult.MicroPruned > 0 {
				attrs = append(attrs, "pruned", polarisResult.MicroPruned)
			}
			if polarisResult.EmergencyEvicted > 0 {
				attrs = append(attrs, "evicted", polarisResult.EmergencyEvicted)
			}
			logger.Info("polaris "+tier+" compaction", attrs...)
		}

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
		if contextBudget > 0 && polarisResult.TokensBefore > contextBudget && polarisResult.TokensAfter > contextBudget {
			logger.Warn("polaris: compaction failed to reduce below budget",
				"session", params.SessionKey,
				"tokensBefore", polarisResult.TokensBefore,
				"tokensAfter", polarisResult.TokensAfter,
				"budget", contextBudget)
			if deps.broadcast != nil {
				deps.broadcast("chat.compaction_degraded", ChatCompactionDegradedEvent{
					Session:      params.SessionKey,
					TokensBefore: polarisResult.TokensBefore,
					TokensAfter:  polarisResult.TokensAfter,
					Budget:       contextBudget,
				})
			}
		}
	}

	return messages
}

// finalizePrompt applies budget optimization, tier-1 wiki injection, and
// coordinator suggestion to the system prompt.
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
		systemPrompt = llm.AppendSystemText(systemPrompt, recallAddition)
	}

	if tier1Addition != "" {
		promptBudget := prompt.PromptBudget{Total: contextCfg.SystemPromptBudget}
		baseTokens := uint64(tokenest.Estimate(string(systemPrompt)))
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
			systemPrompt = llm.AppendSystemText(systemPrompt, f.Content)
		}
	}

	return systemPrompt
}

// agentConfigDeps holds dependencies specifically needed by buildAgentConfig.
type agentConfigDeps struct {
	Tools            *ToolRegistry
	MaxTokens        int
	SubagentNotifyCh <-chan string
	EmitAgentFn      func(kind, sessionKey, runID string, payload map[string]any)
	Transcript       TranscriptStore
	// SkillNudger fires background skill reviews after every N tool
	// invocations. Nil disables iteration-based nudging.
	SkillNudger SkillNudger
	// SkillUsageRecorder attributes each turn's outcome to the skills consulted
	// that turn, feeding the genesis Evolver's success-rate gate. Nil disables.
	SkillUsageRecorder SkillUsageRecorder
}

// buildAgentConfig constructs the agent.AgentConfig, building tool lists and
// wiring all turn-level hooks. Returns the config along with the spawn flag
// for the run orchestrator.
func buildAgentConfig(
	params RunParams,
	deps runDeps,
	cachedSession *session.Session,
	systemPrompt json.RawMessage,
	sessionToolPreset string,
	acd agentConfigDeps,
	logger *slog.Logger,
) (cfg agent.AgentConfig, spawnFlag *SpawnFlag) {
	// Build tool list from registry (uses stored descriptions and schemas).
	// If a tool preset is active, filter the tool list to only include allowed tools.
	var tools []llm.Tool
	if acd.Tools != nil {
		allowed := toolpreset.AllowedTools(toolpreset.Preset(sessionToolPreset))
		rawTools := acd.Tools.FilteredLLMTools(allowed)

		// Cache-stable ordering: built-in tools form a sorted prefix,
		// dynamic tools (plugins, MCP) are sorted separately and appended.
		builtinNames := make(map[string]struct{}, len(acd.Tools.Names()))
		for _, name := range acd.Tools.Names() {
			builtinNames[name] = struct{}{}
		}
		partition := PartitionTools(rawTools, builtinNames)
		tools = partition.MergedTools()
	}

	maxTokens := acd.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	// RunCache lives for the entire agent run (across all turns) and caches
	// idempotent tool results (find, tree). Invalidated on mutation tools.
	runCache := NewRunCache()

	// skillConsults records which skills the agent reads during this run so the
	// post-turn hook can attribute each turn's outcome to them (genesis usage
	// signal). Run-scoped; shared with the skills tool via OnTurnInit.
	skillConsults := NewSkillConsultLog()

	// FileCache lives for the entire agent run and deduplicates repeated file reads.
	fileCache := agent.NewFileCache(agent.DefaultFileCacheMaxItems)

	// SpawnFlag: tracks whether sessions_spawn was called during this run.
	spawnFlag = NewSpawnFlag()

	// Verification gate state: armed by successful write/edit, disarmed by a
	// successful build/test exec; consulted when the model tries to finish.
	verifyGate := &verifyGateState{}

	// DeferredActivation: tracks which deferred tools have been activated via
	// fetch_tools during this run.
	deferredActivation := NewDeferredActivation()

	// Resolve thinking config from the session's ThinkingLevel setting.
	var thinkingCfg *llm.ThinkingConfig
	if cachedSession != nil && cachedSession.ThinkingLevel != "" {
		thinkingCfg = resolveThinkingConfig(cachedSession.ThinkingLevel)
	}
	// Interleaved thinking is an additive flag: it requires extended thinking
	// to be enabled (otherwise there's nothing to interleave). When
	// thinkingCfg is nil the interleaved bit has no effect.
	if thinkingCfg != nil && cachedSession != nil && cachedSession.InterleavedThinking != nil && *cachedSession.InterleavedThinking {
		thinkingCfg.Interleaved = true
	}

	// Override max tokens if the caller (e.g., OpenAI HTTP endpoint) specified one.
	if params.MaxTokens != nil && *params.MaxTokens > 0 {
		maxTokens = *params.MaxTokens
	}

	// Mode-aware agent config: Chat mode gets reduced limits for quick
	// conversational replies; other modes use the default agent capabilities.
	// Cron runs get the most generous budget — they can (a) deliver the
	// primary message, (b) post one or two short progress updates via
	// message.send so the user is not silently waiting, and (c) still update
	// wikis / projects without truncating. 50 turns is the current ceiling;
	// keep this high only while the cron-side progress-reporting rule in the
	// job prompts stays active, otherwise the user perceives the run as hung.
	maxTurns := defaultMaxTurns         // 25
	agentTimeout := defaultAgentTimeout // 60min
	if cachedSession != nil {
		switch {
		case cachedSession.Mode == session.ModeChat:
			maxTurns = 10
			agentTimeout = 10 * time.Minute
		case cachedSession.SpawnedBy != "":
			// Sub-agents are scoped delegations, not open-ended sessions: a
			// stuck child should fail fast and report back instead of holding
			// a local vLLM slot for the full 60-minute default.
			agentTimeout = 15 * time.Minute
		case cachedSession.Kind == session.KindCron:
			maxTurns = 50
		}
	}

	maxOutputRecovery := 1
	maxOutputScaleFactors := []float64{1.5}

	// Skill-nudger hook state: tracks per-run tool activity so we can
	// hand a clean snapshot to the background review goroutine. Zero cost
	// when acd.SkillNudger is nil or disabled.
	skillNudgerEnabled := shouldEnableSkillNudger(acd.SkillNudger, params, sessionToolPreset)
	var nudgerMu sync.Mutex
	var nudgerActivities []SkillNudgeToolActivity
	var nudgerTurns int

	cfg = agent.AgentConfig{
		MaxTurns:         maxTurns,
		Timeout:          agentTimeout,
		Model:            "", // set by caller after model resolution
		System:           systemPrompt,
		Tools:            tools,
		MaxTokens:        maxTokens,
		Thinking:         thinkingCfg,
		Temperature:      params.Temperature,
		TopP:             params.TopP,
		FrequencyPenalty: params.FrequencyPenalty,
		PresencePenalty:  params.PresencePenalty,
		StopSequences:    params.Stop,
		ResponseFormat:   params.ResponseFormat,
		ToolChoice:       params.ToolChoice,
		// Drop base64 image bytes from the message history after turn 0 so that
		// subsequent tool-call turns don't retransmit the full image payload.
		StripImagesAfterFirstTurn: hasImageAttachment(params.Attachments),
		// Deferred context injection on turn 1+: subagent completion
		// notifications via non-blocking channel reads.
		DeferredSystemText: deferredSubagentNotifications(acd.SubagentNotifyCh),
		// Emit heartbeat at each turn so WS clients know the agent is alive.
		OnTurn: func(turn int, accumulatedTokens int) {
			if acd.EmitAgentFn != nil {
				acd.EmitAgentFn("heartbeat", params.SessionKey, params.ClientRunID, map[string]any{
					"turn":   turn,
					"tokens": accumulatedTokens,
					"ts":     time.Now().UnixMilli(),
				})
			}
		},
		// Post-turn hook: (1) attribute this turn's outcome to the skills
		// consulted during it (genesis usage signal), then (2) feed the skill
		// nudger. Both are cheap no-ops when their dependency is nil.
		OnToolTurn: func(turn int, activities []agent.ToolActivity) {
			recordTurnSkillUsage(acd.SkillUsageRecorder, skillConsults, activities, params.SessionKey)
			if !skillNudgerEnabled {
				return
			}
			nudgerMu.Lock()
			nudgerTurns = turn
			for _, a := range activities {
				nudgerActivities = append(nudgerActivities, SkillNudgeToolActivity{
					Name:    a.Name,
					IsError: a.IsError,
				})
			}
			if len(activities) == 0 {
				nudgerMu.Unlock()
				return
			}
			snapshot := SkillNudgeSnapshot{
				Turns:          nudgerTurns,
				ToolActivities: append([]SkillNudgeToolActivity(nil), nudgerActivities...),
				Label:          params.SessionKey,
				Model:          params.Model,
			}
			nudgerMu.Unlock()
			acd.SkillNudger.OnToolCalls(context.Background(), params.SessionKey, len(activities), snapshot)
		},
		// Inject a fresh TurnContext at the start of each turn so that tools
		// executing in parallel within the same turn can share results via $ref.
		OnTurnInit: func(ctx context.Context) context.Context {
			ctx = WithTurnContext(ctx, NewTurnContext())
			ctx = WithRunCache(ctx, runCache)
			ctx = WithSkillConsultLog(ctx, skillConsults)
			ctx = WithFileCache(ctx, fileCache)
			ctx = WithToolPreset(ctx, sessionToolPreset)
			ctx = WithDeferredActivation(ctx, deferredActivation)
			ctx = WithSpawnFlag(ctx, spawnFlag)
			ctx = WithVerifyGate(ctx, verifyGate)
			// Cron/scheduled runs deliver their final text via the run-completion
			// layer, so an in-loop message-tool send is a benign no-op rather than
			// an outage. Without this flag on the tool context, the message tool
			// returns an error the model translates into a "전송이 안 됐네요, 직접
			// 전달드릴게요" apology that then leaks into the delivered report.
			// runAgentAsync sets this on its own ctx, but the SendSync/cron path
			// reaches RunAgent only through this OnTurnInit — so it must be set
			// here too. See message.go's AutoDeliveryFromContext branch.
			if params.AutoDeliveredOutput {
				ctx = WithAutoDelivery(ctx)
			}
			return ctx
		},
		DynamicToolsProvider: func() []llm.Tool {
			names := deferredActivation.ActivatedNames()
			if len(names) == 0 {
				return nil
			}
			return acd.Tools.DeferredLLMTools(names)
		},
		MaxOutputTokensRecovery:     maxOutputRecovery,
		MaxOutputTokensScaleFactors: maxOutputScaleFactors,
		SpawnDetected:               spawnFlag.IsSet,
		ToolLoopDetector:            agent.NewToolLoopDetector(agent.DefaultToolLoopConfig(), logger),
		// Per-turn message persistence: persist each assistant and tool_result
		// message immediately to transcript so intermediate findings survive
		// across runs (fixes the "short-term memory loss" bug).
		OnMessagePersist: buildMessagePersister(deps, params, logger),
	}

	// Reasoning sandwich (docs/research/ideal-agent-environment-harness.md §11):
	// when enabled, boost the planning (turn 0) thinking budget and keep later
	// turns at baseline. Opt-in via DENEB_REASONING_SANDWICH and only when the
	// session already has thinking enabled, so default behavior is unchanged.
	// Thinking is a request-level param, so per-turn variation is cache-safe.
	if thinkingCfg != nil && reasoningSandwichEnabled() {
		cfg.ThinkingModulator = planningSandwichThinking(thinkingCfg, cfg.MaxTokens)
	}

	// Verification gate (docs/research/ideal-agent-environment-harness.md §10):
	// a run that wrote/edited files must run a verification command before its
	// finish is accepted; the gate injects one demand prompt, then yields.
	// Default ON (inert for non-mutating runs); DENEB_VERIFY_GATE=0 disables.
	if verifyGateEnabled() {
		cfg.FinalizeGate = func(int) string { return verifyGate.finalizePrompt() }
	}

	return cfg, spawnFlag
}

// recordTurnSkillUsage attributes one turn's outcome to the skills consulted
// during it, feeding the genesis Evolver real success-rate signal instead of
// empty stats. The turn counts as a failure for every consulted skill if any
// tool in it errored. No-op when no recorder is wired or nothing was consulted.
func recordTurnSkillUsage(rec SkillUsageRecorder, log *SkillConsultLog, activities []agent.ToolActivity, sessionKey string) {
	if rec == nil || log == nil {
		return
	}
	consulted := log.DrainNew()
	if len(consulted) == 0 {
		return
	}
	errMsg := ""
	for _, a := range activities {
		if a.IsError {
			errMsg = "turn failed: tool " + a.Name + " errored"
			break
		}
	}
	for _, name := range consulted {
		rec.RecordSkillUse(sessionKey, name, errMsg == "", errMsg)
	}
}

func shouldEnableSkillNudger(nudger SkillNudger, params RunParams, sessionToolPreset string) bool {
	if nudger == nil || !nudger.Enabled() {
		return false
	}
	if params.EphemeralUser || params.EphemeralAssistant {
		return false
	}
	if sessionToolPreset == string(toolpreset.PresetSelfReview) {
		return false
	}
	return !strings.HasPrefix(params.SessionKey, "system:")
}

// ---------------------------------------------------------------------------
// errModelStalled marks a turn that timed out without producing any output (an
// LLM stream stall). It is synthesized inside runAgentWithFallback so a stall
// engages the model fallback chain the same way a hard error does.
var errModelStalled = errors.New("model produced no output before timeout (stream stall)")

// stallFallbackBudget bounds the fallback attempt when a stall has already
// consumed the per-turn deadline. A stall surfaces as a timeout result only
// after the parent ctx is spent, so the fallback needs a fresh budget — but a
// bounded one, so a wedged turn can't run unbounded. Single-user: a slightly
// late answer from a healthy model beats silence.
const stallFallbackBudget = 90 * time.Second

// errModelCircuitOpen marks a turn whose initial model was skipped because its
// circuit breaker is open (repeated recent failures — see modelrole/health.go).
// Synthesized instead of calling RunAgent so the fallback chain engages
// immediately and the user is spared the dead model's stall timeout.
var errModelCircuitOpen = errors.New("model circuit open: skipped after repeated recent failures")

// healthyFallbackExists reports whether the fallback chain for role offers at
// least one model that is distinct from failedModel and whose breaker is
// closed. The initial-model skip only happens when this holds — when every
// candidate is unhealthy, trying the requested model is still the best move.
func healthyFallbackExists(reg *modelrole.Registry, role modelrole.Role, failedModel string) bool {
	chain := reg.FallbackChain(role)
	for i := 1; i < len(chain); i++ {
		cfg := reg.Config(chain[i])
		if cfg.Model == "" || cfg.Model == failedModel {
			continue
		}
		if !reg.ModelUnhealthy(cfg.Model) {
			return true
		}
	}
	return false
}

// isStalledResult reports whether an agent run timed out without emitting any
// text — the signature of a stalled LLM stream (the inner per-run timeout fired
// before a single token arrived). A turn that timed out after producing text is
// left alone: the user already got a partial answer, so falling back would only
// discard it.
func isStalledResult(r *agent.AgentResult) bool {
	return r != nil && r.StopReason == "timeout" && strings.TrimSpace(r.AllText) == ""
}

// Stage 3: runAgentWithFallback — agent loop with compaction retry + model fallback.
// ---------------------------------------------------------------------------

// runAgentWithFallback executes the agent loop with mid-loop compaction retries
// on context overflow, transient HTTP error retries, and model fallback chain.
func runAgentWithFallback(
	ctx context.Context,
	cfg agent.AgentConfig,
	messages []llm.Message,
	client *llm.Client,
	deps runDeps,
	providerID string,
	initialRole modelrole.Role,
	hooks agent.StreamHooks,
	logger *slog.Logger,
	runLog *agentlog.RunLogger,
) (*agent.AgentResult, string, bool, error) {
	const maxCompactionRetries = 2

	contextBudget := effectiveContextBudget(deps, providerID, cfg.Model, logger)

	// Anti-thrashing state (see compact_guard.go):
	//   - lastCompactInputHash detects idempotent compaction — if the
	//     prior attempt's input slice hashes to the same value, another
	//     compact.Compact call will produce the same output and we'll
	//     retry the same failure in a loop.
	//   - protectedZoneExceedsBudget detects physically-impossible
	//     sessions where even with a zero-byte middle, the head+tail
	//     protected zones alone exceed budget.
	// On either condition we bail with stopReasonCompressionStuck so the
	// user sees a Korean "can't compress further, try /reset" message
	// instead of another cryptic "context overflow" from the provider.
	var lastCompactInputHash string

	var agentResult *agent.AgentResult
	var runErr error
	// Track which model actually answered. Starts as the requested model and
	// only changes if the fallback chain below fires.
	actualModel := cfg.Model
	fellBack := false
	// stalledResult holds the original empty timeout result when the main model
	// stalled. If no fallback model recovers, we return this rather than the
	// fallback's error — preserving the pre-fallback "stall = empty reply"
	// behavior instead of surfacing a downstream error.
	var stalledResult *agent.AgentResult
	// Circuit breaker: when the requested model has failed repeatedly within
	// the cooldown window AND the chain offers a healthy alternative, skip the
	// initial attempt and go straight to the fallback chain. Saves the user
	// the dead model's stall timeout on every turn while it is down; the
	// cooldown re-admits the model automatically once it has aged out.
	skipInitial := deps.registry != nil &&
		deps.registry.ModelUnhealthy(cfg.Model) &&
		healthyFallbackExists(deps.registry, initialRole, cfg.Model)
	if skipInitial {
		logger.Warn("model circuit open; skipping straight to fallback chain",
			"model", cfg.Model, "role", string(initialRole))
	}
	for compactAttempt := 0; compactAttempt <= maxCompactionRetries; compactAttempt++ {
		if skipInitial {
			runErr = errModelCircuitOpen
		} else {
			agentResult, runErr = agent.RunAgent(ctx, cfg, messages, client, deps.tools, hooks, logger, runLog)
		}
		// A stall (timed out with zero output) returns no error but an empty
		// timeout result. Treat it as a failure so the fallback chain below gets
		// a shot with a different model. Only the inner per-run timeout fired,
		// not the parent ctx, so fallback attempts still have budget.
		if runErr == nil && isStalledResult(agentResult) {
			logger.Warn("model stalled (no output before timeout); engaging fallback chain",
				"model", cfg.Model, "stopReason", agentResult.StopReason)
			runErr = errModelStalled
			stalledResult = agentResult
		}
		if runErr == nil {
			break
		}

		// Mid-loop compaction retry: on context overflow, strip images and
		// emergency-summarize to reduce context before retrying.
		if isContextOverflow(runErr) && compactAttempt < maxCompactionRetries && ctx.Err() == nil {
			// Early-abort guard A: head + tail protected zone already
			// exceeds budget. Compaction cannot reduce below budget even
			// with a zero-byte middle, so skip straight to the user-visible
			// stuck message.
			if protectedZoneExceedsBudget(messages, contextBudget) {
				logger.Warn("compaction skipped: protected zone exceeds budget",
					"messageCount", len(messages),
					"budget", contextBudget,
					"attempt", compactAttempt+1)
				if deps.broadcast != nil {
					deps.broadcast("chat.compaction_stuck", ChatCompactionStuckEvent{
						Reason:       "protected_zone_exceeds_budget",
						MessageCount: len(messages),
						Budget:       contextBudget,
					})
				}
				return &agent.AgentResult{
					StopReason:    stopReasonCompressionStuck,
					FinalMessages: messages,
				}, cfg.Model, false, nil
			}

			// Early-abort guard B: input hash matches the prior attempt.
			// The cheap-first shrink pipeline + LLM summarizer already ran
			// and produced a slice that's byte-identical to what we fed in
			// last time. Another compact.Compact call will not do anything
			// new, so stop burning the retry budget.
			inputHash := hashMessages(messages)
			if lastCompactInputHash != "" && inputHash == lastCompactInputHash {
				logger.Warn("compaction skipped: identical input as prior attempt",
					"messageCount", len(messages),
					"inputHash", inputHash,
					"attempt", compactAttempt+1)
				if deps.broadcast != nil {
					deps.broadcast("chat.compaction_stuck", ChatCompactionStuckEvent{
						Reason:       "idempotent_compaction",
						MessageCount: len(messages),
						InputHash:    inputHash,
					})
				}
				return &agent.AgentResult{
					StopReason:    stopReasonCompressionStuck,
					FinalMessages: messages,
				}, cfg.Model, false, nil
			}
			lastCompactInputHash = inputHash

			logger.Warn("context overflow, attempting mid-loop compaction",
				"attempt", compactAttempt+1,
				"maxRetries", maxCompactionRetries,
				"messageCount", len(messages),
				"error", runErr)

			// Cheap-first shrink pipeline (no LLM calls):
			// 1) Structurally truncate long tool-call argument strings.
			//    Protects against naive byte-slice truncation producing
			//    invalid JSON that providers reject non-retryably.
			// 2) Replace image blocks with text stubs.
			messages = compact.TruncateToolCallArgs(messages, 400)
			messages = compact.StripImageBlocks(messages)

			// Emergency summarize: keep head 2 + tail 8, summarize the middle.
			if len(messages) > 10 {
				var summarizer compact.Summarizer
				if pilotHub := pilot.LocalAIHub(); pilotHub != nil {
					summarizer = &localAISummarizer{}
				}
				if summarizer != nil {
					compactCfg := compact.NewConfig(contextBudget)
					compactCtx, compactCancel := context.WithTimeout(ctx, 30*time.Second)
					messages, _ = compact.Compact(compactCtx, compactCfg, messages, summarizer, logger)
					compactCancel()
				}
			}
			continue
		}

		// Transient HTTP retry: 500/502/503/521/529/429 → wait 2.5s, retry once.
		//
		// Classification is delegated to llmerr so the decision shares the
		// same taxonomy as isContextOverflow above and the autoreply runner.
		// We deliberately whitelist the narrow set of reasons the prior
		// string-based IsTransientError matched (5xx server errors, overload,
		// rate limits) plus transport timeouts — keeping ReasonUnknown and
		// non-HTTP signals out so we don't over-retry on truly unknown
		// failures or auth/billing issues.
		if ctx.Err() == nil && isTransientLLMError(runErr) {
			logger.Warn("transient HTTP error, retrying once", "error", runErr)
			select {
			case <-ctx.Done():
				return nil, "", false, ctx.Err()
			case <-time.After(2500 * time.Millisecond):
			}
			agentResult, runErr = agent.RunAgent(ctx, cfg, messages, client, deps.tools, hooks, logger, runLog)
			if runErr != nil {
				logger.Warn("transient retry also failed", "error", runErr)
			}
		}

		// Model fallback chain: try each subsequent role in the chain.
		// e.g., Main → Lightweight → Fallback
		if runErr != nil && deps.registry != nil {
			// Choose the context for fallback attempts. A hard error leaves the
			// parent ctx alive (budget remains, so reuse it). A stall, however,
			// only surfaces once the per-turn deadline is already spent waiting
			// on the dead model — so give the fallback a fresh, bounded budget,
			// otherwise the user gets silence instead of an answer from a
			// healthy model. A user abort yields StopReason "aborted" (not
			// "timeout"), so it never reaches this stall branch.
			fbCtx, fbCancel := ctx, context.CancelFunc(nil)
			runFallback := true
			if ctx.Err() != nil {
				if errors.Is(runErr, errModelStalled) {
					fbCtx, fbCancel = context.WithTimeout(context.WithoutCancel(ctx), stallFallbackBudget)
				} else {
					runFallback = false // parent canceled for another reason — respect it
				}
			}
			if runFallback {
				// Feed the circuit breaker: a hard error or stall counts against
				// the model's health. Context overflow does not (input-size
				// problem, not a model fault) and neither does the synthetic
				// circuit-open sentinel (the model was never tried).
				if !isContextOverflow(runErr) && !errors.Is(runErr, errModelCircuitOpen) {
					deps.registry.RecordModelFailure(cfg.Model)
				}
				chain := deps.registry.FallbackChain(initialRole)
				// Skip models already attempted — the chain can list the same model
				// for multiple roles (e.g. main == lightweight), and re-running a
				// model that just stalled only burns the budget.
				triedModels := map[string]bool{cfg.Model: true}
				for i := 1; i < len(chain); i++ {
					if fbCtx.Err() != nil {
						break
					}
					fbRole := chain[i]
					// Re-discover what the local vLLM is serving before targeting
					// the role (rate-limited; no-op for non-vllm roles). Without
					// this, a model swapped on the server after gateway startup
					// 404s every fallback until a restart.
					fbCfg := deps.registry.RefreshVllmRole(fbRole)
					fbClient := deps.registry.Client(fbRole)
					if fbClient == nil || triedModels[fbCfg.Model] {
						continue
					}
					triedModels[fbCfg.Model] = true
					logger.Warn("model failed, trying fallback",
						"failedRole", string(chain[i-1]),
						"nextRole", string(fbRole),
						"nextModel", fbCfg.Model,
						"error", runErr)
					agentCfg := cfg
					agentCfg.Model = fbCfg.Model
					agentResult, runErr = agent.RunAgent(fbCtx, agentCfg, messages, fbClient, deps.tools, hooks, logger, runLog)
					// A stalled fallback (empty timeout) is also a failure — advance
					// to the next role instead of returning its empty result.
					if runErr == nil && isStalledResult(agentResult) {
						runErr = errModelStalled
					}
					if runErr == nil {
						actualModel = fbCfg.Model
						fellBack = true
						break
					}
					if fbCtx.Err() == nil {
						// Only count failures the model itself produced — a spent
						// fallback budget says nothing about the model's health.
						deps.registry.RecordModelFailure(fbCfg.Model)
					}
					logger.Error("fallback also failed",
						"role", string(fbRole), "model", fbCfg.Model, "error", runErr)
				}
			}
			if fbCancel != nil {
				fbCancel()
			}
		}

		if runErr != nil {
			// The main model stalled and no fallback model produced an answer
			// (it stalled too, or errored — e.g. a provider rejecting the
			// history). Degrade to the original empty timeout result rather than
			// surfacing the fallback's error, matching the prior behavior from
			// before stalls engaged the fallback chain.
			if stalledResult != nil && !fellBack {
				return stalledResult, actualModel, false, nil
			}
			// Surface unrecoverable context overflow so operators/UI see it.
			// Without this the only signal was a Warn log in the retry loop
			// and the final error return — easy to miss when diagnosing
			// "why did the bot suddenly stop on long sessions".
			if isContextOverflow(runErr) && deps.broadcast != nil {
				deps.broadcast("chat.context_overflow_unrecoverable", ChatContextOverflowEvent{
					Model:        cfg.Model,
					MessageCount: len(messages),
					Attempts:     maxCompactionRetries + 1,
					Error:        runErr.Error(),
				})
				logger.Error("context overflow: all compaction retries exhausted",
					"model", cfg.Model,
					"messageCount", len(messages),
					"attempts", maxCompactionRetries+1,
					"error", runErr)
			}
			return nil, "", false, runErr
		}
		break // success via transient retry or fallback
	}

	// Close the answering model's breaker. Failures were already recorded at
	// the fallback-engagement points above; the stalled-degrade and
	// compression-stuck paths return earlier and never reach here.
	if deps.registry != nil {
		deps.registry.RecordModelSuccess(actualModel)
	}
	return agentResult, actualModel, fellBack, nil
}

// resolveThinkingConfig maps a session ThinkingLevel string to an llm.ThinkingConfig.
// Returns nil for "off", empty, or unrecognized levels (disables extended thinking).
func resolveThinkingConfig(level string) *llm.ThinkingConfig {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "minimal":
		return &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 1024}
	case "low":
		return &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 4096}
	case "medium":
		return &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 10240}
	case "high":
		return &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 32768}
	case "xhigh":
		return &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 65536}
	case "adaptive":
		return &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 16384}
	default:
		return nil
	}
}

// reasoningSandwichEnabled reports whether the planning-phase reasoning boost
// (the "reasoning sandwich" — docs/research/ideal-agent-environment-harness.md
// §11) is turned on. Off by default: it changes per-turn thinking budget, and
// the latency/quality trade-off should be validated live before defaulting on.
// Enable with DENEB_REASONING_SANDWICH=1 (or true/on/yes).
func reasoningSandwichEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DENEB_REASONING_SANDWICH"))) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// thinkingBudgetLadder lists the NAMED extended-thinking tiers in increasing
// order: minimal/low/medium/high/xhigh (see resolveThinkingConfig).
// boostThinkingBudget walks one step up this ladder. Note "adaptive" (16384) is
// intentionally NOT a ladder entry — it is not a named tier, so boosting from
// adaptive lands on the next tier above it (high=32768). Do not insert 16384
// here; that would change the boost semantics.
var thinkingBudgetLadder = []int{1024, 4096, 10240, 32768, 65536}

// boostThinkingBudget returns the next budget tier strictly above b, capped at
// the top of the ladder. A value already at or above the top is unchanged.
func boostThinkingBudget(b int) int {
	for _, step := range thinkingBudgetLadder {
		if step > b {
			return step
		}
	}
	return b
}

// minThinkingResponseHeadroom is the token room the planning turn must retain
// for its own output after the (boosted) thinking budget is carved out of
// max_tokens. Anthropic extended thinking requires budget_tokens < max_tokens,
// so a boost that would not leave this margin is dropped rather than risking a
// rejected request.
const minThinkingResponseHeadroom = 4096

// planningSandwichThinking returns a per-turn thinking selector that boosts the
// first (planning) turn one budget tier above the session baseline and uses the
// baseline for every later turn — the "front of the reasoning sandwich" (§11).
// Planning is where extra reasoning pays off most, while keeping middle
// tool-execution turns at baseline avoids the timeout cost of max-everywhere
// reasoning.
//
// The boost is applied only when the larger budget still leaves response
// headroom under maxTokens; otherwise the planning turn falls back to the
// baseline so it is never more likely to be rejected than a normal turn
// (Anthropic requires budget_tokens < max_tokens). maxTokens <= 0 means
// "unknown" and keeps the boost. Returns nil when base is nil so the caller
// leaves Thinking as-is.
func planningSandwichThinking(base *llm.ThinkingConfig, maxTokens int) func(turn int) *llm.ThinkingConfig {
	if base == nil {
		return nil
	}
	boostedBudget := boostThinkingBudget(base.BudgetTokens)
	boosted := base
	if boostedBudget > base.BudgetTokens && (maxTokens <= 0 || boostedBudget <= maxTokens-minThinkingResponseHeadroom) {
		boosted = &llm.ThinkingConfig{
			Type:         base.Type,
			BudgetTokens: boostedBudget,
			Interleaved:  base.Interleaved,
		}
	}
	return func(turn int) *llm.ThinkingConfig {
		if turn == 0 {
			return boosted
		}
		return base
	}
}

// buildMessagePersister returns a callback that persists each message to the
// transcript store immediately. This ensures intermediate assistant text and
// tool results survive across runs — fixing the "short-term memory loss" bug
// where the agent forgot discoveries made in earlier turns.
//
// Assistant messages are sanitized via sanitizeAssistantForTranscript before
// persistence: the silent-reply token (NO_REPLY) is stripped from text blocks,
// and messages that end up with no substance (all empty text, no tool_use /
// tool_result / thinking / image blocks) are dropped entirely. Without this,
// an assistant turn whose only text was "NO_REPLY" would be persisted with
// that literal token, and the model on the next turn would see it in history
// and hallucinate that it had replied — the "대답 안 하고 대답했다고 생각하는
// 경향" bug.
func buildMessagePersister(
	deps runDeps,
	params RunParams,
	logger *slog.Logger,
) func(msg llm.Message) {
	// EphemeralAssistant turns suppress assistant + tool_result persistence:
	// returning nil here disables the executor's per-turn persist callback.
	// Heartbeat sets this true so autonomous progress ticks do not pollute the
	// user's short-term transcript; heartbeat state is kept in HEARTBEAT.md.
	if deps.transcript == nil || params.EphemeralAssistant {
		return nil
	}
	return func(msg llm.Message) {
		content := msg.Content
		if msg.Role == "assistant" {
			sanitized, skip := sanitizeAssistantForTranscript(content)
			if skip {
				logger.Info("skipping persist of empty assistant turn",
					"session", params.SessionKey,
					"reason", "no user-visible content after silent-token strip")
				return
			}
			content = sanitized
		}
		chatMsg := ChatMessage{
			Role:      msg.Role,
			Content:   content, // json.RawMessage — rich blocks preserved
			Timestamp: time.Now().UnixMilli(),
		}
		if err := deps.transcript.Append(params.SessionKey, chatMsg); err != nil {
			logger.Warn("per-turn message persist failed", "role", msg.Role, "error", err)
		}
	}
}

// sanitizeAssistantForTranscript strips NO_REPLY from assistant text blocks
// and reports whether the resulting message has enough substance to persist.
// Returns (content, skip). When skip=true, the caller must not persist the
// message at all — it would only pollute transcript history and confuse the
// model into thinking it replied when it did not.
//
// "Substance" = any non-text block (tool_use, tool_result, thinking, image),
// or a text block with non-empty content after stripping.
func sanitizeAssistantForTranscript(content json.RawMessage) (json.RawMessage, bool) {
	// Text-form message: Content is a JSON-encoded string.
	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		stripped := StripSilentToken(text)
		if stripped == "" {
			return nil, true
		}
		if stripped == text {
			return content, false
		}
		raw, err := json.Marshal(stripped)
		if err != nil {
			return content, false
		}
		return raw, false
	}
	// Block-form message: Content is a JSON array of ContentBlocks.
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return content, false
	}
	changed := false
	hasSubstance := false
	for i := range blocks {
		if blocks[i].Type == "text" {
			stripped := StripSilentToken(blocks[i].Text)
			if stripped != blocks[i].Text {
				blocks[i].Text = stripped
				changed = true
			}
			if stripped != "" {
				hasSubstance = true
			}
			continue
		}
		// tool_use, tool_result, thinking, image — any non-text block counts
		// as substance worth preserving in history.
		hasSubstance = true
	}
	if !hasSubstance {
		return nil, true
	}
	if !changed {
		return content, false
	}
	raw, err := json.Marshal(blocks)
	if err != nil {
		return content, false
	}
	return raw, false
}

// Compile-time interface compliance.
var _ compact.Summarizer = (*localAISummarizer)(nil)

// localAISummarizer adapts pilot.CallLocalLLM to the compaction.Summarizer interface.
type localAISummarizer struct{}

func (s *localAISummarizer) Summarize(ctx context.Context, system, conversation string, maxOutputTokens int) (string, error) {
	return pilot.CallAnalysisLLM(ctx, system, conversation, maxOutputTokens)
}

// formatToolHist renders a tool-count histogram as "name:count,name:count" in
// descending count order so the busiest tool surfaces first in the log line.
// Returns "" for an empty map — slog will drop empty string values cleanly.
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
