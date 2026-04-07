// run_exec.go contains the core agent execution loop: user message persistence,
// context assembly, LLM invocation with compaction retry and model fallback.
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	compact "github.com/choiceoh/deneb/gateway-go/internal/chat/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/coordinator"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolpreset"
	"github.com/choiceoh/deneb/gateway-go/internal/chatport"
	hookspkg "github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/rlm/repl"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
)

// skillsPromptCache is a version-aware cache for the workspace skills prompt.
// Invalidated when the skills watcher bumps the version (file changes detected).
var skillsCache struct {
	mu       sync.RWMutex
	prompt   string
	snapshot *skills.FullSkillSnapshot
	version  int64
	built    bool
}

// skillsWatcher is the shared watcher that monitors SKILL.md file changes.
// Initialized once by InitSkillsWatcher.
var skillsWatcher *skills.Watcher

// InitSkillsWatcher creates and starts the skills watcher for a workspace.
// Call once at server startup. The watcher invalidates the skills prompt cache
// when SKILL.md files change on disk.
func InitSkillsWatcher(workspaceDir string) {
	if skillsWatcher != nil {
		return
	}
	skillsWatcher = skills.NewWatcher(nil)
	skillsWatcher.RegisterChangeListener(func(event skills.SkillsChangeEvent) {
		skillsCache.mu.Lock()
		skillsCache.built = false
		skillsCache.mu.Unlock()
	})
	skillsWatcher.EnsureWatcher(workspaceDir, nil, 250)
}

// loadCachedSkillsPrompt returns the cached skills prompt, rebuilding it when
// the watcher version changes or on first call.
// availableToolNames is used for conditional activation (requires_tools/fallback_for_tools).
func loadCachedSkillsPrompt(workspaceDir string, availableToolNames []string) string {
	skillsCache.mu.RLock()
	if skillsCache.built {
		prompt := skillsCache.prompt
		skillsCache.mu.RUnlock()
		return prompt
	}
	skillsCache.mu.RUnlock()

	skillsCache.mu.Lock()
	defer skillsCache.mu.Unlock()

	// Double-check after acquiring write lock.
	if skillsCache.built {
		return skillsCache.prompt
	}

	// Build available tools map for conditional activation.
	availableTools := make(map[string]bool, len(availableToolNames))
	for _, name := range availableToolNames {
		availableTools[name] = true
	}

	cfg := skills.SnapshotConfig{
		DiscoverConfig: skills.DiscoverConfig{
			WorkspaceDir: workspaceDir,
		},
		Eligibility: skills.EligibilityContext{
			EnvVars:        skills.EnvSnapshotFromOS(),
			SkillConfigs:   make(map[string]skills.SkillConfig),
			AvailableTools: availableTools,
		},
	}
	// Discover entries first so we can cache them for slash command routing.
	allEntries := skills.DiscoverWorkspaceSkills(cfg.DiscoverConfig)
	SetCachedSkillEntries(allEntries, 0)

	snapshot := skills.BuildWorkspaceSkillSnapshot(cfg)
	if snapshot != nil {
		skillsCache.prompt = snapshot.Prompt
		skillsCache.snapshot = snapshot
	} else {
		skillsCache.prompt = ""
		skillsCache.snapshot = nil
	}
	skillsCache.built = true
	return skillsCache.prompt
}

// GetCachedSkillsSnapshot returns the last-built skills snapshot, or nil.
func GetCachedSkillsSnapshot() *skills.FullSkillSnapshot {
	skillsCache.mu.RLock()
	defer skillsCache.mu.RUnlock()
	return skillsCache.snapshot
}

// InvalidateSkillsCache forces the skills prompt to be rebuilt on next access.
func InvalidateSkillsCache() {
	skillsCache.mu.Lock()
	skillsCache.built = false
	skillsCache.mu.Unlock()
}

// availableToolNames returns sorted tool names from the registry, or nil if nil.
func availableToolNames(tools *ToolRegistry) []string {
	if tools == nil {
		return nil
	}
	return tools.SortedNames()
}

// chatRunResult wraps the agent result with chat-layer continuation info.
type chatRunResult struct {
	*agent.AgentResult
	// ContSignal is non-nil when the continue_run tool was available.
	// Check ContSignal.Requested() to see if the LLM requested a continuation.
	ContSignal *ContinuationSignal
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
	if deps.emitAgentFn != nil {
		deps.emitAgentFn("run.start", params.SessionKey, params.ClientRunID, map[string]any{
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
		if deps.emitTranscriptFn != nil {
			deps.emitTranscriptFn(params.SessionKey, userMsg, "")
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
	// Agent tools pass role names ("main", "lightweight", "fallback").
	// /model command or RPC may pass model IDs ("google/gemini-3.1-pro") — these
	// are treated as direct overrides (no fallback chain).
	model := params.Model
	initialRole := modelrole.RoleMain

	if deps.registry != nil && model != "" {
		// Role name → resolve to actual model ID with fallback chain.
		if resolved, role, ok := deps.registry.ResolveModel(model); ok {
			model = resolved
			initialRole = role
		}
		// Raw model ID → no role mapping, no fallback chain (direct override).
	}
	if model == "" && cachedSession != nil && cachedSession.SpawnedBy != "" {
		// Sub-agent: use explicit session model if set at spawn time,
		// otherwise fall back to the configured subagent default model.
		if cachedSession.Model != "" {
			model = cachedSession.Model
		} else if deps.subagentDefaultModel != "" {
			model = deps.subagentDefaultModel
		}
	}
	if model == "" {
		model = deps.defaultModel
	}
	if model == "" && deps.registry != nil {
		model = deps.registry.FullModelID(modelrole.RoleMain)
	}
	// Second-pass role resolution: fallback values (defaultModel, subagentDefaultModel,
	// sess.Model) may contain role names like "main" that need registry resolution.
	if deps.registry != nil && model != "" {
		if resolved, role, ok := deps.registry.ResolveModel(model); ok {
			model = resolved
			initialRole = role
		}
	}
	// Parse provider prefix (e.g., "google/gemini-3.0-flash" → provider="google", model="gemini-3.0-flash").
	providerID, modelName := parseModelID(model)
	model = modelName

	// Sub-agent provider remapping: if this session was spawned by another
	// agent and a "<provider>-subagent" config exists, use the alternate
	// API key. This allows main and sub-agents to use different accounts
	// on the same provider (separate rate limits).
	if cachedSession != nil && cachedSession.SpawnedBy != "" && providerID != "" {
		alt := providerID + "-subagent"
		if deps.providerConfigs != nil {
			if _, ok := deps.providerConfigs[alt]; ok {
				logger.Info("subagent provider remap", "from", providerID, "to", alt)
				providerID = alt
			}
		}
	}

	// Snapshot immutable query config once. All subsequent code uses qCfg
	// for consistent values (model, budget, limits) rather than re-reading.
	qCfg := BuildQueryConfig(params, model, providerID, workspaceDir)

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

	prepStart := time.Now()
	// 5. Run knowledge prefetch, context assembly, and system prompt build in parallel.
	var knowledgeAddition string
	var messages []llm.Message
	var contextErr error
	var systemPrompt json.RawMessage

	var prepWg sync.WaitGroup

	// Knowledge prefetch (parallel).
	prepWg.Add(1)
	go func() {
		defer prepWg.Done()
		if params.Message != "" {
			kDeps := knowledge.Deps{
				WorkspaceDir: workspaceDir,
			}
			knowledgeAddition = knowledge.Prefetch(ctx, params.Message, kDeps)
		}
	}()

	// Context assembly (parallel).
	// Transcript-only fallback (Aurora removed).
	prepWg.Add(1)
	go func() {
		defer prepWg.Done()

		if deps.transcript != nil {
			result, err := assembleContext(deps.transcript, params.SessionKey, deps.contextCfg, logger)
			if err != nil {
				contextErr = err
			} else {
				messages = result.Messages
			}
		}
	}()

	// Resolve session tool preset early (needed for both system prompt and tool list).
	var sessionToolPreset string
	if cachedSession != nil {
		sessionToolPreset = cachedSession.ToolPreset
	}

	// System prompt build (parallel).
	prepWg.Add(1)
	go func() {
		defer prepWg.Done()
		if params.System != "" {
			systemPrompt = llm.SystemString(params.System)
			return
		}
		if deps.defaultSystem != "" {
			systemPrompt = llm.SystemString(deps.defaultSystem)
			return
		}
		if deps.tools == nil {
			return
		}
		tz, _ := prompt.LoadCachedTimezone()
		ch := deliveryChannel(params.Delivery)
		// Session memory: pre-format for prompt injection.
		var sessionMemoryText string
		if deps.sessionMemory != nil {
			if content := deps.sessionMemory.Get(params.SessionKey); content != "" {
				sessionMemoryText = FormatForPrompt(content)
			}
		}

		// Build tool defs — filtered if a preset is active.
		allowed := toolpreset.AllowedTools(toolpreset.Preset(sessionToolPreset))
		toolDefs := toPromptToolDefs(deps.tools.FilteredDefinitions(allowed))

		// Deferred tool summaries for system prompt listing.
		deferredSummaries := deps.tools.DeferredSummaries()
		var deferredToolInfos []prompt.DeferredToolInfo
		for _, ds := range deferredSummaries {
			// Skip deferred tools not in the allowed preset (if preset is active).
			if len(allowed) > 0 && !allowed[ds.Name] {
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
			ContextFiles: prompt.LoadContextFiles(workspaceDir,
				append(memoryContextOpts(deps), prompt.WithSessionSnapshot(params.SessionKey))...),
			RuntimeInfo:   prompt.BuildDefaultRuntimeInfo(params.Model, deps.defaultModel),
			Channel:       ch,
			SessionMemory: sessionMemoryText,
			SkillsPrompt:  loadCachedSkillsPrompt(workspaceDir, availableToolNames(deps.tools)),
			ToolPreset:    sessionToolPreset,
		}

		// Coordinator mode: use the coordinator-specific system prompt.
		if sessionToolPreset == string(toolpreset.PresetCoordinator) {
			scratchpadDir := coordinator.ResolveScratchpadDir(params.SessionKey)
			systemPrompt = llm.SystemString(prompt.BuildCoordinatorSystemPrompt(spp, scratchpadDir))
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
		systemPrompt = llm.SystemBlocks(blocks)
	}()

	prepWg.Wait()
	logger.Info("pipeline: parallel prep done (knowledge+context+sysprompt)", "ms", time.Since(prepStart).Milliseconds())

	// Microcompact: prune old tool results to save tokens before the LLM call.
	// Runs after context assembly but before prompt finalization.
	// Low-cost (no LLM call) — replaces old tool_result blocks with compact stubs.
	if len(messages) > 0 {
		mcMessages, mcResult := compact.MicrocompactMessages(messages, time.Now())
		if mcResult.PrunedCount > 0 {
			messages = mcMessages
			logger.Info("microcompact: pruned old tool results",
				"pruned", mcResult.PrunedCount,
				"estimatedTokensSaved", mcResult.EstimatedSaved)
		}
	}

	if contextErr != nil {
		logger.Error("context assembly failed, proceeding with degraded context",
			"sessionKey", params.SessionKey, "error", contextErr)
	}

	// If the caller provided pre-built messages (e.g., OpenAI-compatible HTTP API
	// with full conversation history), use those instead of transcript context.
	if len(params.PrebuiltMessages) > 0 {
		messages = params.PrebuiltMessages
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

	// 6. Proactive context: no blocking wait. The hint is injected via
	// DeferredSystemText on turn 1+. By then the goroutine has had the full
	// duration of turn 0 (LLM response + tool execution) to complete — typically
	// several seconds — giving effectively 100% hit rate with zero user wait.

	// 7. Budget-optimize variable prompt additions before appending.
	// The base system prompt (identity, tools, skills, context files) is fixed;
	// variable additions (knowledge, aurora, recall) are optimized by priority.
	promptBudget := prompt.PromptBudget{Total: deps.contextCfg.SystemPromptBudget}
	baseTokens := prompt.EstimateTokens(string(systemPrompt))
	var remainingBudget uint64
	if promptBudget.Total > baseTokens {
		remainingBudget = promptBudget.Total - baseTokens
	}
	additionBudget := prompt.PromptBudget{Total: remainingBudget}

	var additionFragments []prompt.PromptFragment
	if knowledgeAddition != "" {
		additionFragments = append(additionFragments, prompt.NewFragment("memory", knowledgeAddition))
	}

	// Optimize and append surviving fragments.
	optimized := additionBudget.Optimize(additionFragments)
	for _, f := range optimized {
		systemPrompt = llm.AppendSystemText(systemPrompt, f.Content)
	}
	if len(additionFragments) > len(optimized) {
		logger.Info("prompt budget: trimmed additions",
			"original", len(additionFragments),
			"surviving", len(optimized),
			"budgetTokens", remainingBudget)
	}

	// 7b. Auto-suggest coordinator mode if the message looks like a multi-file task
	// and the session is not already in coordinator mode.
	if sessionToolPreset == "" && params.Message != "" && coordinator.ShouldSuggestCoordinator(params.Message) {
		hint := "\n\n[System hint: this request appears to involve multiple files. " +
			"Consider suggesting coordinator mode (/coordinator) for structured multi-agent orchestration.]\n"
		systemPrompt = llm.AppendSystemText(systemPrompt, hint)
	}

	logger.Info("pipeline: system prompt finalized",
		"chars", len(systemPrompt),
		"knowledgeChars", len(knowledgeAddition))

	runLog.LogPrep(agentlog.RunPrepData{
		SystemPromptChars: len(systemPrompt),
		ContextMessages:   len(messages),
		KnowledgeChars:    len(knowledgeAddition),
		PrepMs:            time.Since(runStart).Milliseconds(),
	})

	// 8. Build tool list from registry (uses stored descriptions and schemas).
	// If a tool preset is active, filter the tool list to only include allowed tools.
	// Then partition into builtin prefix + dynamic suffix for prompt cache stability.
	var tools []llm.Tool
	if deps.tools != nil {
		allowed := toolpreset.AllowedTools(toolpreset.Preset(sessionToolPreset))
		rawTools := deps.tools.FilteredLLMTools(allowed)

		// Cache-stable ordering: built-in tools form a sorted prefix,
		// dynamic tools (plugins, MCP) are sorted separately and appended.
		// Changes to dynamic tools only invalidate cache from the boundary onward.
		builtinNames := make(map[string]bool, len(deps.tools.Names()))
		for _, name := range deps.tools.Names() {
			builtinNames[name] = true
		}
		partition := PartitionTools(rawTools, builtinNames)
		tools = partition.MergedTools()
	}

	// 9. Build agent config.
	maxTokens := deps.maxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	// RunCache lives for the entire agent run (across all turns) and caches
	// idempotent tool results (find, tree). Invalidated on mutation tools.
	runCache := NewRunCache()

	// FileCache lives for the entire agent run and deduplicates repeated file reads.
	// When the same file is read again (unchanged mtime/size), a compact "already read"
	// message is returned instead of the full content, saving context tokens.
	fileCache := agent.NewFileCache(agent.DefaultFileCacheMaxItems)

	// REPL environment: Starlark execution context for the repl tool.
	// Created once per run so variables persist across tool calls.
	// Conversation history is injected as the `context` variable, and
	// llm_query() calls go through the sub-agent path with session memory.
	replEnv := buildREPLEnv(ctx, messages, client, model, deps, params)

	// ContinuationSignal: shared across turns so continue_run tool can set it.
	// Read by runAgentAsync after the agent loop returns.
	// Only created for async paths where continuation is actually supported;
	// sync paths (OpenAI HTTP) leave it nil so the continue_run tool returns
	// "not available" instead of silently accepting and never following up.
	var contSignal *ContinuationSignal
	if deps.continuationEnabled {
		contSignal = NewContinuationSignal()
	}

	// DeferredActivation: tracks which deferred tools have been activated via
	// fetch_tools during this run. The executor reads it each turn to inject
	// newly activated tool schemas into the ChatRequest.
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

	cfg := agent.AgentConfig{
		MaxTurns:         maxTurns,
		Timeout:          agentTimeout,
		Model:            model,
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
		// Each inline image is ~1600 tokens; stripping saves that cost per turn
		// from turn 1 onward for multi-turn runs that start with an image.
		StripImagesAfterFirstTurn: hasImageAttachment(params.Attachments),
		// Deferred context injection on turn 1+: subagent completion
		// notifications via non-blocking channel reads.
		DeferredSystemText: deferredSubagentNotifications(deps.subagentNotifyCh),
		// Emit heartbeat at each turn so WS clients know the agent is alive.
		OnTurn: func(turn int, accumulatedTokens int) {
			if deps.emitAgentFn != nil {
				deps.emitAgentFn("heartbeat", params.SessionKey, params.ClientRunID, map[string]any{
					"turn":   turn,
					"tokens": accumulatedTokens,
					"ts":     time.Now().UnixMilli(),
				})
			}
		},
		// Inject a fresh TurnContext at the start of each turn so that tools
		// executing in parallel within the same turn can share results via $ref.
		// RunCache is injected once and persists across turns.
		OnTurnInit: func(ctx context.Context) context.Context {
			ctx = WithTurnContext(ctx, NewTurnContext())
			ctx = WithRunCache(ctx, runCache)
			ctx = WithFileCache(ctx, fileCache)
			ctx = WithToolPreset(ctx, sessionToolPreset)
			ctx = WithDeferredActivation(ctx, deferredActivation)
			if contSignal != nil {
				ctx = WithContinuationSignal(ctx, contSignal)
			}
			if replEnv != nil {
				ctx = repl.WithEnv(ctx, replEnv)
			}
			return ctx
		},
		DynamicToolsProvider: func() []llm.Tool {
			names := deferredActivation.ActivatedNames()
			if len(names) == 0 {
				return nil
			}
			return deps.tools.DeferredLLMTools(names)
		},
		NudgeBudget:                 nudgeBudget,
		MaxOutputTokensRecovery:     maxOutputRecovery,
		MaxOutputTokensScaleFactors: maxOutputScaleFactors,
		// Suppress nudge when continue_run was already called — the explicit
		// continuation will start a fresh run, so nudging wastes turns.
		ContinuationRequested: func() bool {
			return contSignal != nil && contSignal.Requested()
		},
		StreamingToolExecution: true,
		ToolLoopDetector:       agent.NewToolLoopDetector(agent.DefaultToolLoopConfig(), logger),
		// Per-turn message persistence: persist each assistant and tool_result
		// message immediately to transcript so intermediate findings survive
		// across runs (fixes the "short-term memory loss" bug).
		OnMessagePersist: buildMessagePersister(deps, params, logger),
	}

	// Mid-run memory extraction removed: it used placeholder context ("[mid-run turn N, M tokens]")
	// producing low-quality facts. End-of-run extraction (below) has full response text.

	// 10. Set up stream hooks via compositor: fan-out dispatch for each hook type.
	var hc agent.HookCompositor

	// Broadcaster: WebSocket streaming deltas.
	if broadcaster != nil {
		hc.OnTextDelta(broadcaster.EmitDelta)
		hc.OnToolEmit(broadcaster.EmitToolStart)
		hc.OnToolResult(func(name, toolUseID, result string, isErr bool) {
			broadcaster.EmitToolResult(name, toolUseID, result, isErr)
			if deps.broadcast != nil {
				deps.broadcast("session.tool", map[string]any{
					"sessionKey": params.SessionKey,
					"runId":      params.ClientRunID,
					"tool":       name,
					"toolUseId":  toolUseID,
					"isError":    isErr,
				})
			}
		})
	}

	// Typing signaler: UI typing indicators.
	if typingSignaler != nil {
		hc.OnTextDelta(typingSignaler.SignalTextDelta)
		hc.OnThinking(typingSignaler.SignalReasoningDelta)
		hc.OnToolStart(func(_ string, _ string, _ []byte) {
			typingSignaler.SignalToolStart()
		})
	}

	// Status controller: Telegram emoji reactions.
	if statusCtrl != nil {
		hc.OnThinking(statusCtrl.SetThinking)
		hc.OnToolStart(func(name, _ string, _ []byte) { statusCtrl.SetTool(name) })
		// First text delta means we moved past thinking — set thinking
		// emoji if not already in a tool phase.
		hc.OnTextDelta(func(_ string) { statusCtrl.SetThinking() })
	}

	// Tool progress tracking for channel integrations.
	if deps.toolProgressFn != nil && params.Delivery != nil {
		delivery := params.Delivery
		hc.OnToolStart(func(name, reason string, input []byte) {
			deps.toolProgressFn(ctx, delivery, ToolProgressEvent{Type: "start", Name: name, Reason: reason, Input: input})
		})
		hc.OnToolResult(func(name, _, _ string, isErr bool) {
			deps.toolProgressFn(ctx, delivery, ToolProgressEvent{Type: "complete", Name: name, IsError: isErr})
		})
	}

	// Gateway event subscription: emit tool.start / tool.end for WebSocket clients.
	if deps.emitAgentFn != nil {
		hc.OnToolStart(func(name, _ string, _ []byte) {
			deps.emitAgentFn("tool.start", params.SessionKey, params.ClientRunID, map[string]any{
				"tool": name,
				"ts":   time.Now().UnixMilli(),
			})
		})
		hc.OnToolResult(func(name, _, _ string, isErr bool) {
			deps.emitAgentFn("tool.end", params.SessionKey, params.ClientRunID, map[string]any{
				"tool":    name,
				"isError": isErr,
				"ts":      time.Now().UnixMilli(),
			})
		})
	}

	// Internal hook registry: fire tool.use event after each tool completes.
	if deps.internalHookRegistry != nil {
		hc.OnToolResult(func(name, toolUseID, _ string, isErr bool) {
			env := map[string]string{
				"DENEB_TOOL":        name,
				"DENEB_TOOL_USE_ID": toolUseID,
				"DENEB_IS_ERROR":    fmt.Sprintf("%t", isErr),
				"DENEB_SESSION_KEY": params.SessionKey,
			}
			go deps.internalHookRegistry.TriggerFromEvent(deps.shutdownCtx, hookspkg.EventToolUse, params.SessionKey, env)
		})
	}

	// Draft stream hook: real-time message editing during LLM streaming.
	// Creates a throttled draft loop that sends/edits a Telegram message as
	// text deltas arrive, giving the user immediate visual feedback.
	var draftCtrl *telegram.DraftStreamLoop
	if deps.draftEditFn != nil && params.Delivery != nil && params.Delivery.Channel == "telegram" {
		delivery := params.Delivery
		var draftMu sync.Mutex
		var draftMsgID string // tracks the sent message ID across edits
		var accum strings.Builder

		// Defer cleanup so the draft is stopped on all exit paths (success, error, fallback).
		// Finalize flushes any pending text (so the user sees the complete streamed output),
		// then stores the draft message ID on the delivery context. The reply pipeline
		// will edit this message in-place instead of deleting it and sending a new one,
		// preventing the "disappear then reappear" flicker on completion.
		defer func() {
			if draftCtrl != nil {
				// Stop the draft loop without flushing. The reply pipeline will
				// edit the draft message in-place with the final processed text.
				// Flushing here would cause a double-edit: first with SanitizeDraftText
				// (code blocks stripped), then immediately after with the final reply
				// text (differently processed), producing a visible content flash
				// or "clear and resend" flicker on Telegram.
				draftCtrl.StopForClear()
			}
			// Store the draft message ID on the delivery context so the reply
			// pipeline can edit the existing message instead of sending a new one.
			draftMu.Lock()
			msgID := draftMsgID
			draftMu.Unlock()
			if msgID != "" && delivery != nil {
				delivery.DraftMsgID = msgID
			}
		}()

		draftCtrl = telegram.NewDraftStreamLoop(800, func(text string) (bool, error) {
			draftMu.Lock()
			currentID := draftMsgID
			draftMu.Unlock()

			editCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()

			newID, err := deps.draftEditFn(editCtx, delivery, currentID, text)
			if err != nil {
				logger.Warn("draft stream send/edit failed", "error", err)
				return false, err
			}
			draftMu.Lock()
			draftMsgID = newID
			draftMu.Unlock()
			return true, nil
		})

		// Section-based streaming: update on paragraph breaks or 500+ char accumulation.
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
				if deps.sanitizeDraft != nil {
					sanitized = deps.sanitizeDraft(current)
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
	}

	hooks := hc.Build()

	logger.Info("pipeline: prep complete, starting agent loop",
		"prepMs", time.Since(runStart).Milliseconds(),
		"model", model, "provider", providerID,
		"messages", len(messages), "tools", len(tools))

	// 11. Execute agent loop with compaction retry and model fallback chain.
	agentStart := time.Now()
	var agentResult *agent.AgentResult

	// Budget tracker: detect diminishing returns across compaction retries.
	budgetTracker := NewBudgetTracker()

	// Track the final transition reason for telemetry/debugging.
	var lastTransition QueryTransition

	for attempt := 0; attempt <= maxCompactionRetries; attempt++ {
		if attempt > 0 {
			logger.Info("retrying agent run after compaction", "attempt", attempt)
		}

		var runErr error
		agentResult, runErr = agent.RunAgent(ctx, cfg, messages, client, deps.tools, hooks, logger, runLog)
		if runErr != nil {
			// Context overflow: summarize middle messages and retry.
			if isContextOverflow(runErr) && attempt < maxCompactionRetries {
				logger.Info("context overflow, compacting messages", "error", runErr)
				if len(messages) > 10 {
					const keepHead, keepTail = 2, 8
					if len(messages) > keepHead+keepTail {
						dropped := len(messages) - keepHead - keepTail
						middle := messages[keepHead : len(messages)-keepTail]

						// Try to summarize the middle before dropping.
						summary := emergencySummarize(ctx, client, model, middle, logger)

						kept := make([]llm.Message, 0, keepHead+1+keepTail)
						kept = append(kept, messages[:keepHead]...)
						if summary != "" {
							kept = append(kept, llm.NewTextMessage("user",
								"[Compacted conversation summary]\n"+summary))
						}
						kept = append(kept, messages[len(messages)-keepTail:]...)
						messages = kept
						logger.Info("context overflow: emergency compaction",
							"dropped", dropped, "summarized", summary != "",
							"remaining", len(messages))
					}
				}
				messages = compact.StripImageBlocks(messages)
				lastTransition = NewContinue(ContinueCompactRetry)
				continue
			}

			// Transient HTTP retry: 502/503/521/429 → wait 2.5s, retry once.
			if deps.isTransientError != nil && deps.isTransientError(runErr.Error()) && ctx.Err() == nil {
				logger.Warn("transient HTTP error, retrying once", "error", runErr)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(2500 * time.Millisecond):
				}
				agentResult, runErr = agent.RunAgent(ctx, cfg, messages, client, deps.tools, hooks, logger, runLog)
				if runErr == nil {
					break
				}
				logger.Warn("transient retry also failed", "error", runErr)
			}

			// Model fallback chain: try each subsequent role in the chain.
			// e.g., Main → Lightweight → Fallback
			if deps.registry != nil && ctx.Err() == nil {
				chain := deps.registry.FallbackChain(initialRole)
				fallbackSucceeded := false
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
						fallbackSucceeded = true
						break
					}
					logger.Error("fallback also failed",
						"role", string(fbRole), "model", fbCfg.Model, "error", runErr)
				}
				if fallbackSucceeded {
					break
				}
			}

			return nil, runErr
		}
		// Check budget tracker for diminishing returns across turns.
		totalTokens := agentResult.Usage.InputTokens + agentResult.Usage.OutputTokens
		decision := budgetTracker.CheckBudget("", int(qCfg.LiveTokenBudget), totalTokens)
		if decision.Action == "stop" && decision.DiminishingReturns {
			lastTransition = NewTerminal(TerminalDiminishingReturn, nil)
			logger.Info("budget tracker: diminishing returns detected, stopping",
				"continuations", decision.ContinuationCount,
				"pct", decision.Pct)
			break
		}
		lastTransition = NewTerminal(TerminalCompleted, nil)
		break
	}

	agentMs := time.Since(agentStart).Milliseconds()
	totalMs := time.Since(runStart).Milliseconds()
	logger.Info("pipeline: agent loop complete",
		"agentMs", agentMs,
		"totalMs", totalMs,
		"turns", agentResult.Turns,
		"inputTokens", agentResult.Usage.InputTokens,
		"outputTokens", agentResult.Usage.OutputTokens,
		"transition", lastTransition.Reason())

	// Emit agent run.end event to gateway subscriptions.
	if deps.emitAgentFn != nil {
		deps.emitAgentFn("run.end", params.SessionKey, params.ClientRunID, map[string]any{
			"model":        model,
			"turns":        agentResult.Turns,
			"durationMs":   totalMs,
			"inputTokens":  agentResult.Usage.InputTokens,
			"outputTokens": agentResult.Usage.OutputTokens,
			"stopReason":   agentResult.StopReason,
		})
	}

	return &chatRunResult{AgentResult: agentResult, ContSignal: contSignal}, nil
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
