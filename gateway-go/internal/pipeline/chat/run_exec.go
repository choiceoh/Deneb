// run_exec.go contains the core agent execution loop: user message persistence,
// context assembly, LLM invocation with model fallback.
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/coordinator"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolpreset"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	compact "github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// chatRunResult wraps the agent result with chat-layer continuation info.
type chatRunResult struct {
	*agent.AgentResult
	// ContSignal is non-nil when the continue_run tool was available.
	// Check ContSignal.Requested() to see if the LLM requested a continuation.
	ContSignal *ContinuationSignal
	// SpawnFlag is non-nil; IsSet() returns true when sessions_spawn was called.
	SpawnFlag *SpawnFlag
}

// executeAgentRun performs the core agent execution: persist user msg, assemble context,
// run agent loop, persist result.
func executeAgentRun(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	broadcaster *streaming.Broadcaster,
	typingSignaler chatport.TypingSignaler,
	statusCtrl *telegram.StatusReactionController,
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

	// 1. Persist user message to transcript + Aurora store.
	if deps.transcript != nil && params.Message != "" {
		userMsg := NewTextChatMessage("user", params.Message, time.Now().UnixMilli())
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

	runLog.LogStart(agentlog.RunStartData{
		Model:    model,
		Provider: providerID,
		Message:  params.Message,
		Channel:  deliveryChannel(params.Delivery),
	})

	// 3. Resolve LLM client (no IO — reads in-memory config/auth store).
	client := resolveClient(deps, providerID, logger)
	if client == nil {
		return nil, fmt.Errorf("no LLM client available (provider=%q, model=%q)", providerID, model)
	}

	// Memory recall is now agent-driven: the agent calls memory(action=recall)
	// as a tool when it needs past context. No parallel goroutine needed.

	// Resolve session tool preset early (needed for both system prompt and tool list).
	var sessionToolPreset string
	if cachedSession != nil {
		sessionToolPreset = cachedSession.ToolPreset
	}

	// Stage 1: Parallel context + prompt preparation.
	prepStart := time.Now()
	prep := prepareContextAndPrompt(params, deps, workspaceDir, sessionToolPreset, logger)
	logger.Info("pipeline: parallel prep done (context+sysprompt)", "ms", time.Since(prepStart).Milliseconds())

	if prep.ContextErr != nil {
		logger.Error("context assembly failed, proceeding with degraded context",
			"sessionKey", params.SessionKey, "error", prep.ContextErr)
	}

	// Stage 2: Assemble final message list (prebuilt, attachments, Polaris compaction).
	messages := assembleMessages(ctx, params, deps, prep, logger)

	// Stage 3: Finalize system prompt (budget optimization, coordinator suggestion, tier-1 injection).
	systemPrompt := finalizePrompt(prep.SystemPrompt, prep.Tier1Wiki, deps.contextCfg, sessionToolPreset, params.Message)

	logger.Info("pipeline: system prompt finalized",
		"chars", len(systemPrompt))

	runLog.LogPrep(agentlog.RunPrepData{
		SystemPromptChars: len(systemPrompt),
		ContextMessages:   len(messages),
		PrepMs:            time.Since(runStart).Milliseconds(),
	})

	// Stage 4: Build tool list and agent config.
	acd := agentConfigDeps{
		Tools:               deps.tools,
		MaxTokens:           deps.maxTokens,
		ContinuationEnabled: deps.continuationEnabled,
		SubagentNotifyCh:    deps.subagentNotifyCh,
		EmitAgentFn:         deps.callbacks.emitAgentFn,
		Transcript:          deps.transcript,
	}
	cfg, contSignal, spawnFlag := buildAgentConfig(params, deps, cachedSession, systemPrompt, sessionToolPreset, acd, logger)
	cfg.Model = model // set the resolved model

	// Set up stream hooks via compositor: fan-out dispatch for each hook type.
	var hc agent.HookCompositor
	wireStreamHooks(&hc, params, deps, broadcaster, typingSignaler, statusCtrl)

	// Draft stream hook: real-time message editing during LLM streaming.
	var draftCtrl *telegram.DraftStreamLoop
	var draftMsgIDFn func() string // retrieves current draft message ID
	if deps.callbacks.draftEditFn != nil && params.Delivery != nil && params.Delivery.Channel == "telegram" {
		draftCtrl, draftMsgIDFn = wireDraftStreamHook(ctx, &hc, params, deps, logger)
	}
	hooks := hc.Build()

	// Defer cleanup so the draft is stopped on all exit paths.
	if draftCtrl != nil {
		defer func() {
			draftCtrl.StopForClear()
			if msgID := draftMsgIDFn(); msgID != "" && params.Delivery != nil {
				params.Delivery.DraftMsgID = msgID
			}
		}()
	}

	logger.Info("pipeline: prep complete, starting agent loop",
		"prepMs", time.Since(runStart).Milliseconds(),
		"model", model, "provider", providerID,
		"messages", len(messages), "tools", len(cfg.Tools))

	// Execute agent loop with model fallback chain.
	agentStart := time.Now()
	agentResult, err := runAgentWithFallback(ctx, cfg, messages, client, deps, initialRole, hooks, logger, runLog)
	if err != nil {
		return nil, err
	}

	agentMs := time.Since(agentStart).Milliseconds()
	totalMs := time.Since(runStart).Milliseconds()
	logger.Info("pipeline: agent loop complete",
		"agentMs", agentMs,
		"totalMs", totalMs,
		"turns", agentResult.Turns,
		"inputTokens", agentResult.Usage.InputTokens,
		"outputTokens", agentResult.Usage.OutputTokens,
		"stopReason", agentResult.StopReason)

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

	return &chatRunResult{AgentResult: agentResult, ContSignal: contSignal, SpawnFlag: spawnFlag}, nil
}

// ---------------------------------------------------------------------------
// Extracted stages: prepareContextAndPrompt, assembleMessages, finalizePrompt,
// buildAgentConfig. These are called sequentially from executeAgentRun.
// ---------------------------------------------------------------------------

// prepResult holds the output of the parallel context/prompt preparation stage.
type prepResult struct {
	Messages     []llm.Message
	SystemPrompt json.RawMessage
	Tier1Wiki    string
	ContextErr   error
}

// prepareContextAndPrompt runs wiki injection, context assembly, and system prompt
// build in parallel. Returns the combined results.
func prepareContextAndPrompt(
	params RunParams,
	deps runDeps,
	workspaceDir string,
	sessionToolPreset string,
	logger *slog.Logger,
) prepResult {
	var result prepResult
	var prepWg sync.WaitGroup

	// Tier-1 wiki auto-injection (parallel).
	prepWg.Add(1)
	go func() {
		defer prepWg.Done()
		if deps.wikiStore != nil {
			cfg := wiki.ConfigFromEnv()
			result.Tier1Wiki = knowledge.FormatTier1(deps.wikiStore, cfg.Tier1MinImportance)
		}
	}()

	// Context assembly (parallel).
	prepWg.Add(1)
	go func() {
		defer prepWg.Done()

		if bridge, ok := deps.transcript.(*polaris.Bridge); ok {
			ctxResult, err := assembleContext(bridge, params.SessionKey, deps.contextCfg, logger)
			if err != nil {
				result.ContextErr = err
			} else {
				result.Messages = ctxResult.Messages
				// When messages were silently truncated (no summaries covering them),
				// inject a notice so the AI knows its context is incomplete.
				if !ctxResult.WasCompacted && ctxResult.TotalMessages > len(ctxResult.Messages) && len(ctxResult.Messages) > 0 {
					dropped := ctxResult.TotalMessages - len(ctxResult.Messages)
					notice := fmt.Sprintf(
						"[컨텍스트 알림: 이 세션의 전체 대화 %d개 메시지 중 최근 %d개만 포함되어 있습니다. "+
							"이전 %d개 메시지는 컨텍스트 제한으로 생략되었습니다. "+
							"이전 대화 내용을 정확히 기억하지 못할 수 있으니 유저에게 솔직히 알려주세요.]",
						ctxResult.TotalMessages, len(ctxResult.Messages), dropped)
					result.Messages = append([]llm.Message{llm.NewTextMessage("user", notice)}, result.Messages...)
					logger.Warn("context truncated without summaries",
						"total", ctxResult.TotalMessages,
						"loaded", len(ctxResult.Messages),
						"dropped", dropped,
						"session", params.SessionKey)
				}
			}
		}
	}()

	// System prompt build (parallel).
	prepWg.Add(1)
	go func() {
		defer prepWg.Done()
		if params.System != "" {
			result.SystemPrompt = llm.SystemString(params.System)
			return
		}
		if deps.defaultSystem != "" {
			result.SystemPrompt = llm.SystemString(deps.defaultSystem)
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

		spp := prompt.SystemPromptParams{
			WorkspaceDir:  workspaceDir,
			ToolDefs:      toolDefs,
			DeferredTools: deferredToolInfos,
			UserTimezone:  tz,
			ContextFiles:  prompt.LoadContextFiles(workspaceDir, prompt.WithSessionSnapshot(params.SessionKey)),
			RuntimeInfo:   prompt.BuildDefaultRuntimeInfo(params.Model, deps.callbacks.defaultModel),
			Channel:       ch,
			SkillsPrompt:  loadCachedSkillsPrompt(workspaceDir, availableToolNames(deps.tools)),
			ToolPreset:    sessionToolPreset,
		}

		// Coordinator mode: use the coordinator-specific system prompt.
		if sessionToolPreset == string(toolpreset.PresetCoordinator) {
			scratchpadDir := coordinator.ResolveScratchpadDir(params.SessionKey)
			result.SystemPrompt = llm.SystemString(prompt.BuildCoordinatorSystemPrompt(spp, scratchpadDir))
			return
		}

		// Worker sessions with a tool preset: append role-specific instructions.
		workerAddition := ""
		if sessionToolPreset != "" {
			scratchpadDir := coordinator.ResolveScratchpadDir(params.SessionKey)
			workerAddition = prompt.WorkerPromptAddition(sessionToolPreset, scratchpadDir)
		}

		blocks := prompt.BuildSystemPromptBlocks(spp)
		if workerAddition != "" {
			// Append worker instructions to the last (dynamic) block.
			last := &blocks[len(blocks)-1]
			last.Text += "\n" + workerAddition
		}
		result.SystemPrompt = llm.SystemBlocks(blocks)
	}()

	prepWg.Wait()
	return result
}

// assembleMessages builds the final message list from prebuilt messages, transcript
// context, attachments, and Polaris compaction.
func assembleMessages(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	prep prepResult,
	logger *slog.Logger,
) []llm.Message {
	messages := prep.Messages

	// If the caller provided pre-built messages (e.g., OpenAI-compatible HTTP API
	// with full conversation history, or continuation runs passing the previous
	// run's final message array), use those instead of transcript context.
	if len(params.PrebuiltMessages) > 0 {
		// Copy to avoid aliasing the caller's backing array (e.g., FinalMessages
		// from a previous continuation run). Without the copy, append may write
		// into shared capacity, corrupting the original slice.
		messages = append([]llm.Message(nil), params.PrebuiltMessages...)
		// Continuation runs set both PrebuiltMessages (previous run's context)
		// and Message (continuation system message). Append the message so the
		// LLM sees it without re-loading the entire transcript.
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

	// Polaris compaction: tiered context compression.
	// Applied after message assembly, before prompt finalization.
	if len(messages) > 0 {
		polarisCtx, polarisCancel := context.WithTimeout(ctx, 30*time.Second)
		var summarizer compact.Summarizer
		if pilotHub := pilot.LocalAIHub(); pilotHub != nil {
			summarizer = &localAISummarizer{}
		}
		// Derive compaction budget from context assembly budgets so they stay in sync.
		contextBudget := int(deps.contextCfg.MemoryTokenBudget - deps.contextCfg.SystemPromptBudget) //nolint:gosec // G115
		var polarisResult compact.Result
		if bridge, ok := deps.transcript.(*polaris.Bridge); ok {
			engine := bridge.Engine()
			messages, polarisResult = engine.CompactAndPersist(polarisCtx, params.SessionKey, messages, summarizer, contextBudget)

			// Proactive condensation: when a new leaf summary was persisted,
			// trigger background condensation to merge leaves into higher-level nodes.
			if polarisResult.LLMCompacted && summarizer != nil {
				condSummarizer := summarizer // capture for goroutine
				go engine.Condense(context.Background(), params.SessionKey, condSummarizer)
			}
		} else {
			messages, polarisResult = compact.Compact(polarisCtx, compact.NewConfig(contextBudget), messages, summarizer, logger)
		}
		polarisCancel()
		if polarisResult.MicroPruned > 0 || polarisResult.LLMCompacted || polarisResult.EmergencyEvicted > 0 {
			var tier string
			switch {
			case polarisResult.EmergencyEvicted > 0:
				tier = "emergency"
			case polarisResult.LLMCompacted:
				tier = "llm"
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
	}

	return messages
}

// finalizePrompt applies budget optimization, tier-1 wiki injection, and
// coordinator suggestion to the system prompt.
func finalizePrompt(
	systemPrompt json.RawMessage,
	tier1Addition string,
	contextCfg ContextConfig,
	sessionToolPreset string,
	message string,
) json.RawMessage {
	// Budget-optimize variable prompt additions before appending.
	if tier1Addition != "" {
		promptBudget := prompt.PromptBudget{Total: contextCfg.SystemPromptBudget}
		baseTokens := uint64(tokenest.Estimate(string(systemPrompt)))
		var remainingBudget uint64
		if promptBudget.Total > baseTokens {
			remainingBudget = promptBudget.Total - baseTokens
		}
		additionBudget := prompt.PromptBudget{Total: remainingBudget}

		additionFragments := []prompt.PromptFragment{
			prompt.NewFragment("tier1", tier1Addition),
		}
		optimized := additionBudget.Optimize(additionFragments)
		for _, f := range optimized {
			systemPrompt = llm.AppendSystemText(systemPrompt, f.Content)
		}
	}

	// Auto-suggest coordinator mode if the message looks like a multi-file task
	// and the session is not already in coordinator mode.
	if sessionToolPreset == "" && message != "" && coordinator.ShouldSuggestCoordinator(message) {
		hint := "\n\n[System hint: this request appears to involve multiple files. " +
			"Consider suggesting coordinator mode (/coordinator) for structured multi-agent orchestration.]\n"
		systemPrompt = llm.AppendSystemText(systemPrompt, hint)
	}

	return systemPrompt
}

// agentConfigDeps holds dependencies specifically needed by buildAgentConfig.
type agentConfigDeps struct {
	Tools               *ToolRegistry
	MaxTokens           int
	ContinuationEnabled bool
	SubagentNotifyCh    <-chan string
	EmitAgentFn         func(kind, sessionKey, runID string, payload map[string]any)
	Transcript          TranscriptStore
}

// buildAgentConfig constructs the agent.AgentConfig, building tool lists and
// wiring all turn-level hooks. Returns the config along with the continuation
// signal and spawn flag for the run orchestrator.
func buildAgentConfig(
	params RunParams,
	deps runDeps,
	cachedSession *session.Session,
	systemPrompt json.RawMessage,
	sessionToolPreset string,
	acd agentConfigDeps,
	logger *slog.Logger,
) (cfg agent.AgentConfig, contSignal *ContinuationSignal, spawnFlag *SpawnFlag) {
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

	// FileCache lives for the entire agent run and deduplicates repeated file reads.
	fileCache := agent.NewFileCache(agent.DefaultFileCacheMaxItems)

	// ContinuationSignal: shared across turns so continue_run tool can set it.
	if acd.ContinuationEnabled {
		contSignal = NewContinuationSignal()
	}

	// SpawnFlag: tracks whether sessions_spawn was called during this run.
	spawnFlag = NewSpawnFlag()

	// DeferredActivation: tracks which deferred tools have been activated via
	// fetch_tools during this run.
	deferredActivation := NewDeferredActivation()

	// Resolve thinking config from the session's ThinkingLevel setting.
	var thinkingCfg *llm.ThinkingConfig
	if cachedSession != nil && cachedSession.ThinkingLevel != "" {
		thinkingCfg = resolveThinkingConfig(cachedSession.ThinkingLevel)
	}

	// Override max tokens if the caller (e.g., OpenAI HTTP endpoint) specified one.
	if params.MaxTokens != nil && *params.MaxTokens > 0 {
		maxTokens = *params.MaxTokens
	}

	// Mode-aware agent config: Work mode gets full agent capabilities;
	// Normal/Chat modes get reduced limits for quick interactions.
	isWorkMode := (cachedSession != nil && cachedSession.Mode == session.ModeWork) || params.DeepWork

	maxTurns := 10
	agentTimeout := 10 * time.Minute
	if isWorkMode {
		maxTurns = defaultMaxTurns         // 25
		agentTimeout = defaultAgentTimeout // 60min
	}
	if params.DeepWork {
		maxTurns = 50
		agentTimeout = 30 * time.Minute
	}

	// Work-only features: nudge budget, output recovery.
	var nudgeBudget *agent.NudgeBudgetConfig
	maxOutputRecovery := 1 // minimal recovery for all modes
	maxOutputScaleFactors := []float64{1.5}
	if isWorkMode {
		nudgeConts := 5
		if params.DeepWork {
			nudgeConts = 7
		}
		nudgeBudget = &agent.NudgeBudgetConfig{
			MaxContinuations: nudgeConts,
			BudgetThreshold:  0.9,
			MinDeltaTokens:   300,
		}
		maxOutputRecovery = 3
		maxOutputScaleFactors = []float64{1.5, 2.0, 2.0}
	}

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
		// Inject a fresh TurnContext at the start of each turn so that tools
		// executing in parallel within the same turn can share results via $ref.
		OnTurnInit: func(ctx context.Context) context.Context {
			ctx = WithTurnContext(ctx, NewTurnContext())
			ctx = WithRunCache(ctx, runCache)
			ctx = WithFileCache(ctx, fileCache)
			ctx = WithToolPreset(ctx, sessionToolPreset)
			ctx = WithDeferredActivation(ctx, deferredActivation)
			if contSignal != nil {
				ctx = WithContinuationSignal(ctx, contSignal)
			}
			ctx = WithSpawnFlag(ctx, spawnFlag)
			return ctx
		},
		DynamicToolsProvider: func() []llm.Tool {
			names := deferredActivation.ActivatedNames()
			if len(names) == 0 {
				return nil
			}
			return acd.Tools.DeferredLLMTools(names)
		},
		NudgeBudget:                 nudgeBudget,
		MaxOutputTokensRecovery:     maxOutputRecovery,
		MaxOutputTokensScaleFactors: maxOutputScaleFactors,
		// Suppress nudge when continue_run was already called.
		ContinuationRequested: func() bool {
			return contSignal != nil && contSignal.Requested()
		},
		SpawnDetected:          spawnFlag.IsSet,
		StreamingToolExecution: true,
		ToolLoopDetector:       agent.NewToolLoopDetector(agent.DefaultToolLoopConfig(), logger),
		// Per-turn message persistence: persist each assistant and tool_result
		// message immediately to transcript so intermediate findings survive
		// across runs (fixes the "short-term memory loss" bug).
		OnMessagePersist: buildMessagePersister(deps, params, logger),
	}

	return cfg, contSignal, spawnFlag
}

// wireDraftStreamHook sets up the draft stream loop on the compositor and returns
// the DraftStreamLoop controller. The caller must defer Ctrl.StopForClear().
func wireDraftStreamHook(
	ctx context.Context,
	hc *agent.HookCompositor,
	params RunParams,
	deps runDeps,
	logger *slog.Logger,
) (*telegram.DraftStreamLoop, func() string) {
	delivery := params.Delivery
	var draftMu sync.Mutex
	var draftMsgID string

	draftCtrl := telegram.NewDraftStreamLoop(800, func(text string) (bool, error) {
		draftMu.Lock()
		currentID := draftMsgID
		draftMu.Unlock()

		editCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()

		newID, err := deps.callbacks.draftEditFn(editCtx, delivery, currentID, text)
		if err != nil {
			logger.Warn("draft stream send/edit failed", "error", err)
			return false, err
		}
		draftMu.Lock()
		draftMsgID = newID
		draftMu.Unlock()
		return true, nil
	})

	getMsgID := func() string {
		draftMu.Lock()
		defer draftMu.Unlock()
		return draftMsgID
	}

	// Section-based streaming: update on paragraph breaks or 500+ char accumulation.
	var accum strings.Builder
	var lastUpdateLen int
	hc.OnTextDelta(func(text string) {
		accum.WriteString(text)
		current := accum.String()
		delta := len(current) - lastUpdateLen
		if delta < 100 {
			return
		}
		newContent := current[lastUpdateLen:]
		if strings.Contains(newContent, "\n\n") || delta >= 500 {
			sanitized := current
			if deps.chatport.SanitizeDraft != nil {
				sanitized = deps.chatport.SanitizeDraft(current)
			}
			if sanitized == "" {
				return
			}
			draftCtrl.Update(sanitized)
			lastUpdateLen = len(current)
		}
	})

	// Stop draft loop on tool start so no more edits are pushed.
	hc.OnToolStart(func(_ string, _ string, _ []byte) {
		draftCtrl.StopForClear()
	})

	return draftCtrl, getMsgID
}

// ---------------------------------------------------------------------------
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
	initialRole modelrole.Role,
	hooks agent.StreamHooks,
	logger *slog.Logger,
	runLog *agentlog.RunLogger,
) (*agent.AgentResult, error) {
	const maxCompactionRetries = 2

	var agentResult *agent.AgentResult
	var runErr error
	for compactAttempt := 0; compactAttempt <= maxCompactionRetries; compactAttempt++ {
		agentResult, runErr = agent.RunAgent(ctx, cfg, messages, client, deps.tools, hooks, logger, runLog)
		if runErr == nil {
			break
		}

		// Mid-loop compaction retry: on context overflow, strip images and
		// emergency-summarize to reduce context before retrying.
		if isContextOverflow(runErr) && compactAttempt < maxCompactionRetries && ctx.Err() == nil {
			logger.Warn("context overflow, attempting mid-loop compaction",
				"attempt", compactAttempt+1,
				"maxRetries", maxCompactionRetries,
				"messageCount", len(messages),
				"error", runErr)

			// Strip image blocks first (cheap, no LLM call).
			messages = compact.StripImageBlocks(messages)

			// Emergency summarize: keep head 2 + tail 8, summarize the middle.
			if len(messages) > 10 {
				var summarizer compact.Summarizer
				if pilotHub := pilot.LocalAIHub(); pilotHub != nil {
					summarizer = &localAISummarizer{}
				}
				if summarizer != nil {
					contextBudget := int(deps.contextCfg.MemoryTokenBudget - deps.contextCfg.SystemPromptBudget) //nolint:gosec // G115
					compactCfg := compact.NewConfig(contextBudget)
					compactCtx, compactCancel := context.WithTimeout(ctx, 30*time.Second)
					messages, _ = compact.Compact(compactCtx, compactCfg, messages, summarizer, logger)
					compactCancel()
				}
			}
			continue
		}

		// Transient HTTP retry: 502/503/521/429 → wait 2.5s, retry once.
		if deps.chatport.IsTransientError != nil && deps.chatport.IsTransientError(runErr.Error()) && ctx.Err() == nil {
			logger.Warn("transient HTTP error, retrying once", "error", runErr)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(2500 * time.Millisecond):
			}
			agentResult, runErr = agent.RunAgent(ctx, cfg, messages, client, deps.tools, hooks, logger, runLog)
			if runErr != nil {
				logger.Warn("transient retry also failed", "error", runErr)
			}
		}

		// Model fallback chain: try each subsequent role in the chain.
		// e.g., Main → Lightweight → Fallback
		if runErr != nil && deps.registry != nil && ctx.Err() == nil {
			chain := deps.registry.FallbackChain(initialRole)
			for i := 1; i < len(chain); i++ {
				fbRole := chain[i]
				fbCfg := deps.registry.Config(fbRole)
				fbClient := deps.registry.Client(fbRole)
				if fbClient == nil {
					continue
				}
				logger.Warn("model failed, trying fallback",
					"failedRole", string(chain[i-1]),
					"nextRole", string(fbRole),
					"nextModel", fbCfg.Model,
					"error", runErr)
				agentCfg := cfg
				agentCfg.Model = fbCfg.Model
				agentResult, runErr = agent.RunAgent(ctx, agentCfg, messages, fbClient, deps.tools, hooks, logger, runLog)
				if runErr == nil {
					break
				}
				logger.Error("fallback also failed",
					"role", string(fbRole), "model", fbCfg.Model, "error", runErr)
			}
		}

		if runErr != nil {
			return nil, runErr
		}
		break // success via transient retry or fallback
	}

	return agentResult, nil
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

// buildMessagePersister returns a callback that persists each message to the
// transcript store immediately. This ensures intermediate assistant text and
// tool results survive across runs — fixing the "short-term memory loss" bug
// where the agent forgot discoveries made in earlier turns.
func buildMessagePersister(
	deps runDeps,
	params RunParams,
	logger *slog.Logger,
) func(msg llm.Message) {
	if deps.transcript == nil {
		return nil
	}
	return func(msg llm.Message) {
		chatMsg := ChatMessage{
			Role:      msg.Role,
			Content:   msg.Content, // json.RawMessage — rich blocks preserved
			Timestamp: time.Now().UnixMilli(),
		}
		if err := deps.transcript.Append(params.SessionKey, chatMsg); err != nil {
			logger.Warn("per-turn message persist failed", "role", msg.Role, "error", err)
		}
	}
}

// Compile-time interface compliance.
var _ compact.Summarizer = (*localAISummarizer)(nil)

// localAISummarizer adapts pilot.CallLocalLLM to the compaction.Summarizer interface.
type localAISummarizer struct{}

func (s *localAISummarizer) Summarize(ctx context.Context, system, conversation string, maxOutputTokens int) (string, error) {
	return pilot.CallLocalLLM(ctx, system, conversation, maxOutputTokens)
}
