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
	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
	compact "github.com/choiceoh/deneb/gateway-go/internal/chat/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/coordinator"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolpreset"
	"github.com/choiceoh/deneb/gateway-go/internal/chatport"
	hookspkg "github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/plugin"
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
func loadCachedSkillsPrompt(workspaceDir string) string {
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

	cfg := skills.SnapshotConfig{
		DiscoverConfig: skills.DiscoverConfig{
			WorkspaceDir: workspaceDir,
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
	// Sync to Aurora store for compaction tracking.
	// Queue Aurora observer compaction for non-empty user messages.
	// Skip for system sessions (e.g. diary-heartbeat) — their messages must not
	// enter the shared Aurora context or they contaminate the user's conversation.
	if deps.auroraStore != nil && params.Message != "" && !isSystemSession(params.SessionKey) {
		tokenCount := uint64(estimateTokens(params.Message))
		if _, err := deps.auroraStore.SyncMessage(1, "user", params.Message, tokenCount); err != nil {
			logger.Warn("aurora: failed to sync user message", "error", err)
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

	// Plugin hook: allow plugins to override model/provider selection.
	if deps.pluginHookRunner != nil {
		if mrResult := deps.pluginHookRunner.RunBeforeModelResolve(ctx, map[string]any{
			"currentModel": model,
			"provider":     providerID,
			"sessionKey":   params.SessionKey,
			"runId":        params.ClientRunID,
		}); mrResult != nil {
			if mrResult.ModelOverride != "" {
				model = mrResult.ModelOverride
			}
			if mrResult.ProviderOverride != "" {
				providerID = mrResult.ProviderOverride
			}
			logger.Info("plugin: model override applied", "model", model, "provider", providerID)
		}
	}

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
		// Use provider-specific missing-auth message when available.
		if deps.providerRuntime != nil && providerID != "" {
			if msg := deps.providerRuntime.BuildMissingAuthMessage(providerID); msg != "" {
				return nil, fmt.Errorf("%s", msg)
			}
		}
		return nil, fmt.Errorf("no LLM client available (provider=%q, model=%q)", providerID, model)
	}

	// 4. Kick off proactive context as a fire-and-forget goroutine.
	// The local sglang model analyzes the user message and returns a context
	// hint that reduces the agent's first-turn exploration (saves 1-3 turns).
	// We check for the result non-blocking after the parallel section: if the
	// hint is ready it gets injected; if not, we skip it rather than stalling.
	proactiveCh := make(chan string, 1)
	proactiveStart := time.Now()
	if params.Message != "" && len(params.Message) >= proactiveMinMsgLen {
		go func() {
			// Use shutdownCtx instead of the request ctx so the proactive
			// goroutine is not canceled when the current request completes.
			// The hint is consumed via DeferredSystemText on turn 1+, so it
			// must outlive the initial request. Its own timeout (25s/20s)
			// still bounds the LLM call.
			proactiveCh <- buildProactiveContext(deps.shutdownCtx, params.Message, workspaceDir, logger)
		}()
	} else {
		proactiveCh <- "" // no-op: skip for short messages
	}

	// Memory recall is now agent-driven: the agent calls memory(action=recall)
	// as a tool when it needs past context. No parallel goroutine needed.

	prepStart := time.Now()
	// 5. Run knowledge prefetch, context assembly, and system prompt build in parallel.
	var knowledgeAddition string
	var auroraSystemAddition string
	var messages []llm.Message
	var contextErr error
	var systemPrompt json.RawMessage

	var prepWg sync.WaitGroup

	// Knowledge prefetch (parallel).
	// Load memory recall for incoming messages and
	// don't benefit from conversational memory. Vega (project knowledge) still runs.
	prepWg.Add(1)
	go func() {
		defer prepWg.Done()
		if params.Message != "" {
			// When recall engine is active (parallel goroutine), skip
			// memory SearchFacts here to avoid duplicate DB queries.
			recallActive := deps.registry != nil && deps.memoryStore != nil
			kDeps := knowledge.Deps{
				VegaBackend:      deps.vegaBackend,
				WorkspaceDir:     workspaceDir,
				SkipMemorySearch: recallActive,
			}
			{
				kDeps.MemoryStore = deps.memoryStore
				kDeps.MemoryEmbedder = deps.memoryEmbedder
				kDeps.UnifiedStore = deps.unifiedStore
			}
			knowledgeAddition = knowledge.Prefetch(ctx, params.Message, kDeps)
		}
	}()

	// Context assembly (parallel).
	// Primary: Aurora store (includes summaries from compaction).
	// Fallback: file transcript (no compaction awareness).
	prepWg.Add(1)
	go func() {
		defer prepWg.Done()
		if deps.auroraStore != nil {
			asmCfg := aurora.AssemblyConfig{
				TokenBudget:    deps.contextCfg.TokenBudget,
				FreshTailCount: deps.contextCfg.FreshTailCount,
				MaxMessages:    deps.contextCfg.MaxMessages,
			}
			asmResult, err := aurora.Assemble(deps.auroraStore, 1, asmCfg, logger)
			if err != nil {
				logger.Warn("aurora context assembly failed, falling back to transcript", "error", err)
			} else if len(asmResult.Messages) > 0 {
				messages = asmResult.Messages
				auroraSystemAddition = asmResult.SystemPromptAddition
			}
		}
		if len(messages) == 0 && deps.transcript != nil {
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
			SkillsPrompt:  loadCachedSkillsPrompt(workspaceDir),
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

		sp := prompt.BuildSystemPrompt(spp)
		if workerAddition != "" {
			sp += "\n" + workerAddition
		}
		systemPrompt = llm.SystemString(sp)
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

	// Plugin hook: allow plugins to modify/extend the system prompt.
	if deps.pluginHookRunner != nil {
		if pbResult := deps.pluginHookRunner.RunBeforePromptBuild(ctx, map[string]any{
			"sessionKey": params.SessionKey,
			"channel":    deliveryChannel(params.Delivery),
		}); pbResult != nil {
			if pbResult.SystemPrompt != "" {
				systemPrompt = llm.SystemString(pbResult.SystemPrompt)
			}
			additions := pbResult.PrependSystemContext + pbResult.AppendSystemContext
			if additions != "" {
				systemPrompt = llm.AppendSystemText(systemPrompt, additions)
			}
		}
	}

	if contextErr != nil {
		logger.Error("context assembly failed, proceeding with degraded context",
			"sessionKey", params.SessionKey, "error", contextErr)
	}

	// Proactive compaction: if stored tokens exceed the threshold, fire a
	// background sweep. The sweep writes summaries into the Aurora DB; the NEXT
	// request's normal assembly will include them. The current request proceeds
	// with its already-assembled context (no blocking, no stale cache).
	triggerProactiveCompaction(deps.shutdownCtx, deps, params, client, logger)

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
	if auroraSystemAddition != "" {
		additionFragments = append(additionFragments, prompt.NewFragment("aurora_summary", auroraSystemAddition))
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

	// 7c. Auto-suggest coordinator mode if the message looks like a multi-file task
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

	// Deep work mode: extend per-run limits for long autonomous sessions.
	maxTurns := defaultMaxTurns
	agentTimeout := defaultAgentTimeout
	nudgeConts := 5
	if params.DeepWork {
		maxTurns = 50
		agentTimeout = 30 * time.Minute
		nudgeConts = 7
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
		// Deferred context injection on turn 1+: proactive hints only.
		// Recall is now agent-driven via memory(action=recall) tool.
		DeferredSystemText: deferredProactiveHint(proactiveCh, proactiveStart, logger),
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
			return ctx
		},
		DynamicToolsProvider: func() []llm.Tool {
			names := deferredActivation.ActivatedNames()
			if len(names) == 0 {
				return nil
			}
			return deps.tools.DeferredLLMTools(names)
		},
		// Enable nudge budget continuation and max-tokens recovery.
		NudgeBudget: &agent.NudgeBudgetConfig{
			MaxContinuations: nudgeConts,
			BudgetThreshold:  0.9,
			MinDeltaTokens:   300,
		},
		MaxOutputTokensRecovery:     3,
		MaxOutputTokensScaleFactors: []float64{1.5, 2.0, 2.0},
		// Suppress nudge when continue_run was already called — the explicit
		// continuation will start a fresh run, so nudging wastes turns.
		ContinuationRequested: func() bool {
			return contSignal != nil && contSignal.Requested()
		},
		StreamingToolExecution: true,
		// Mid-loop compaction: evaluate context size after each tool turn and
		// compact proactively before the LLM hits context_length_exceeded.
		OnMidLoopCompact: buildMidLoopCompactor(deps, params, logger),
		// Per-turn message persistence: persist each assistant and tool_result
		// message immediately to transcript so intermediate findings survive
		// across runs (fixes the "short-term memory loss" bug).
		OnMessagePersist: buildMessagePersister(deps, params, logger),
	}

	// Mid-run memory extraction removed: it used placeholder context ("[mid-run turn N, M tokens]")
	// producing low-quality facts. End-of-run extraction (below) has full response text.

	// 10. Set up stream hooks: compose broadcaster (WS deltas) + typing + status reactions.
	// (Numbered 10 to preserve the agent loop label below as 11.)
	var hooks agent.StreamHooks
	if broadcaster != nil {
		hooks.OnTextDelta = broadcaster.EmitDelta
		hooks.OnToolEmit = broadcaster.EmitToolStart
		hooks.OnToolResult = func(name, toolUseID, result string, isErr bool) {
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
		}
	}
	if typingSignaler != nil {
		prevOnDelta := hooks.OnTextDelta
		hooks.OnTextDelta = func(text string) {
			if prevOnDelta != nil {
				prevOnDelta(text)
			}
			typingSignaler.SignalTextDelta(text)
		}
		hooks.OnThinking = func() {
			typingSignaler.SignalReasoningDelta()
		}
		hooks.OnToolStart = func(_ string, _ string, _ []byte) {
			typingSignaler.SignalToolStart()
		}
	}
	if statusCtrl != nil {
		prevOnThinking := hooks.OnThinking
		hooks.OnThinking = func() {
			if prevOnThinking != nil {
				prevOnThinking()
			}
			statusCtrl.SetThinking()
		}
		prevOnToolStart := hooks.OnToolStart
		hooks.OnToolStart = func(name, reason string, input []byte) {
			if prevOnToolStart != nil {
				prevOnToolStart(name, reason, input)
			}
			statusCtrl.SetTool(name)
		}
		prevOnDelta := hooks.OnTextDelta
		hooks.OnTextDelta = func(text string) {
			if prevOnDelta != nil {
				prevOnDelta(text)
			}
			// First text delta means we moved past thinking — set thinking
			// emoji if not already in a tool phase.
			statusCtrl.SetThinking()
		}
	}

	// Tool progress tracking hook for channel integrations
	// so the ProgressTracker can update the progress embed in real-time.
	if deps.toolProgressFn != nil && params.Delivery != nil {
		delivery := params.Delivery
		prevOnToolStart := hooks.OnToolStart
		hooks.OnToolStart = func(name, reason string, input []byte) {
			if prevOnToolStart != nil {
				prevOnToolStart(name, reason, input)
			}
			deps.toolProgressFn(ctx, delivery, ToolProgressEvent{Type: "start", Name: name, Reason: reason, Input: input})
		}
		prevOnToolResult := hooks.OnToolResult
		hooks.OnToolResult = func(name, toolUseID, result string, isErr bool) {
			if prevOnToolResult != nil {
				prevOnToolResult(name, toolUseID, result, isErr)
			}
			deps.toolProgressFn(ctx, delivery, ToolProgressEvent{Type: "complete", Name: name, IsError: isErr})
		}
	}

	// Gateway event subscription hooks: emit tool.start / tool.end so WebSocket
	// clients receive real-time agent activity events via the event bus.
	if deps.emitAgentFn != nil {
		prevOnToolStart := hooks.OnToolStart
		hooks.OnToolStart = func(name, reason string, input []byte) {
			if prevOnToolStart != nil {
				prevOnToolStart(name, reason, input)
			}
			deps.emitAgentFn("tool.start", params.SessionKey, params.ClientRunID, map[string]any{
				"tool": name,
				"ts":   time.Now().UnixMilli(),
			})
		}
		prevOnToolResult := hooks.OnToolResult
		hooks.OnToolResult = func(name, toolUseID, result string, isErr bool) {
			if prevOnToolResult != nil {
				prevOnToolResult(name, toolUseID, result, isErr)
			}
			deps.emitAgentFn("tool.end", params.SessionKey, params.ClientRunID, map[string]any{
				"tool":    name,
				"isError": isErr,
				"ts":      time.Now().UnixMilli(),
			})
		}
	}

	// Plugin typed hook: allow blocking tool calls before execution.
	if deps.pluginHookRunner != nil {
		hooks.OnBeforeToolCall = func(name, toolCallID string, input []byte) (bool, string) {
			result := deps.pluginHookRunner.RunBeforeToolCall(ctx, map[string]any{
				"toolName":   name,
				"toolCallId": toolCallID,
				"sessionKey": params.SessionKey,
				"runId":      params.ClientRunID,
			})
			if result != nil && result.Cancel {
				return true, result.CancelReason
			}
			return false, ""
		}
	}

	// Plugin typed hook: fire after_tool_call event after each tool completes.
	if deps.pluginHookRunner != nil {
		prevOnToolResult := hooks.OnToolResult
		hooks.OnToolResult = func(name, toolUseID, result string, isErr bool) {
			if prevOnToolResult != nil {
				prevOnToolResult(name, toolUseID, result, isErr)
			}
			go deps.pluginHookRunner.RunVoidHook(deps.shutdownCtx, plugin.HookAfterToolCall, map[string]any{
				"toolName":   name,
				"toolCallId": toolUseID,
				"sessionKey": params.SessionKey,
				"runId":      params.ClientRunID,
				"isError":    isErr,
				"result":     result,
			})
		}
	}

	// User-defined hook registry: fire tool.use event after each tool completes.
	// Uses FireProgressive for real-time progress tracking (per-hook duration,
	// started/completed/failed phases) instead of the blocking Fire().
	if deps.hookRegistry != nil || deps.internalHookRegistry != nil {
		prevOnToolResult := hooks.OnToolResult
		hooks.OnToolResult = func(name, toolUseID, result string, isErr bool) {
			if prevOnToolResult != nil {
				prevOnToolResult(name, toolUseID, result, isErr)
			}
			env := map[string]string{
				"DENEB_TOOL":        name,
				"DENEB_TOOL_USE_ID": toolUseID,
				"DENEB_IS_ERROR":    fmt.Sprintf("%t", isErr),
				"DENEB_SESSION_KEY": params.SessionKey,
			}
			if deps.hookRegistry != nil {
				// Use progressive emission: hook progress events are logged
				// for observability. The channel is drained in a goroutine
				// so tool execution is not blocked.
				go func() {
					ch := deps.hookRegistry.FireProgressive(deps.shutdownCtx, hookspkg.EventToolUse, env)
					for p := range ch {
						if p.Phase == "failed" {
							logger.Warn("tool.use hook failed",
								"hookId", p.HookID,
								"error", p.Error,
								"durationMs", p.DurationMs)
						}
					}
				}()
			}
			if deps.internalHookRegistry != nil {
				go deps.internalHookRegistry.TriggerFromEvent(deps.shutdownCtx, hookspkg.EventToolUse, params.SessionKey, env)
			}
		}
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

		// Section-based streaming display: instead of updating on every small
		// delta (which causes constant flickering), accumulate text and only
		// push an update when a meaningful section boundary is reached
		// (paragraph break) or enough text has accumulated (500+ chars).
		// This gives the user a calmer reading experience where completed
		// sections appear at once rather than character-by-character.
		var lastUpdateLen int
		prevOnDelta := hooks.OnTextDelta
		hooks.OnTextDelta = func(text string) {
			if prevOnDelta != nil {
				prevOnDelta(text)
			}
			accum.WriteString(text)
			current := accum.String()
			delta := len(current) - lastUpdateLen
			if delta < 100 {
				return // too small to bother updating
			}
			newContent := current[lastUpdateLen:]
			if strings.Contains(newContent, "\n\n") || delta >= 500 {
				// Sanitize draft text: strip leaked tool call markup and
				// fenced code blocks so commands/code are never shown in
				// the Telegram streaming draft (vibe coder constraint).
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
		}

		// On tool start, stop the draft loop so no more edits are pushed.
		// Keep the draft message alive — the deferred cleanup will store
		// its ID on the delivery context so the final reply pipeline can
		// edit it in-place. SanitizeDraftText already strips tool call
		// markup and code blocks during streaming, so deletion is not needed.
		prevOnToolStart := hooks.OnToolStart
		hooks.OnToolStart = func(name, reason string, input []byte) {
			draftCtrl.StopForClear()
			if prevOnToolStart != nil {
				prevOnToolStart(name, reason, input)
			}
		}
	}

	logger.Info("pipeline: prep complete, starting agent loop",
		"prepMs", time.Since(runStart).Milliseconds(),
		"model", model, "provider", providerID,
		"messages", len(messages), "tools", len(tools))

	// 11. Execute agent loop with compaction retry and model fallback chain.
	agentStart := time.Now()
	var agentResult *agent.AgentResult
	origSystem := cfg.System // preserve for compaction retries to avoid duplicate appends

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
			// Check for context overflow error.
			// Retry with context compaction when provider reports overflow
			// don't maintain Aurora state, so compaction would be a no-op.
			if isContextOverflow(runErr) && attempt < maxCompactionRetries {
				logger.Info("context overflow, attempting compaction", "error", runErr)

				// Pre-compaction fact extraction: extract learnings from the
				// conversation before compaction trims older messages.
				if deps.memoryStore != nil && deps.registry != nil {
					lwClient := deps.registry.Client(modelrole.RoleLightweight)
					lwModel := deps.registry.Model(modelrole.RoleLightweight)
					if lwClient != nil && lwModel != "" {
						snapshot := messages // capture before compaction
						go func() {
							extractCtx, extractCancel := context.WithTimeout(deps.shutdownCtx, 30*time.Second)
							defer extractCancel()
							var userParts, assistantParts []string
							for _, m := range snapshot {
								// Extract text content from the message (may be
								// a JSON string or a []ContentBlock array).
								var text string
								if len(m.Content) > 0 && m.Content[0] == '"' {
									_ = json.Unmarshal(m.Content, &text)
								} else {
									var blocks []llm.ContentBlock
									if json.Unmarshal(m.Content, &blocks) == nil {
										for _, b := range blocks {
											if b.Type == "text" && b.Text != "" {
												text += b.Text + "\n"
											}
										}
									}
								}
								if text == "" {
									continue
								}
								switch m.Role {
								case "user":
									userParts = append(userParts, text)
								case "assistant":
									assistantParts = append(assistantParts, text)
								}
							}
							if len(userParts) == 0 && len(assistantParts) == 0 {
								return
							}
							facts, err := memory.ExtractFacts(
								extractCtx, lwClient, lwModel,
								strings.Join(userParts, "\n"),
								strings.Join(assistantParts, "\n"),
								logger,
							)
							if err != nil {
								logger.Warn("pre-compaction fact extraction failed", "error", err)
								return
							}
							if len(facts) > 0 {
								memory.InsertExtractedFacts(extractCtx, deps.memoryStore, deps.memoryEmbedder, facts, logger)
								logger.Info("pre-compaction facts extracted", "count", len(facts))
							}
						}()
					}
				}

				// Strip images before compaction — they waste tokens in the
				// summarization call and can cause prompt-too-long errors.
				preCompactMsgs := compact.StripImageBlocks(messages)

				// Extract recent file reads before compaction destroys them.
				recentFiles := compact.ExtractRecentFileReads(preCompactMsgs)

				compactedMsgs, sysAddition, compErr := handleContextOverflowAurora(
					ctx, deps, params, client, logger,
				)
				if compErr != nil {
					// Record compaction failure for circuit breaker.
					getCompactionCircuitBreaker().RecordFailure()
					return nil, fmt.Errorf("compaction failed: %w (original: %w)", compErr, runErr)
				}
				getCompactionCircuitBreaker().RecordSuccess()
				messages = compactedMsgs

				// Post-compaction file restoration: re-inject recently accessed
				// file contents so the agent retains working memory of files
				// it was actively editing. Stays within token budget.
				if restored := compact.BuildRestorationMessages(recentFiles); len(restored) > 0 {
					messages = append(messages, restored...)
					logger.Info("compaction: restored recent file reads",
						"count", len(restored))
				}

				if sysAddition != "" {
					cfg.System = llm.AppendSystemText(origSystem, sysAddition)
				}
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

			lastTransition = NewTerminal(TerminalModelError, runErr)
			return nil, runErr
		}
		// Check budget tracker for diminishing returns across turns.
		totalTokens := agentResult.Usage.InputTokens + agentResult.Usage.OutputTokens
		decision := budgetTracker.CheckBudget("", int(qCfg.TokenBudget), totalTokens)
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

	// Fire agent_end plugin hook (void, non-blocking).
	if deps.pluginHookRunner != nil {
		errMsg := ""
		if agentResult.StopReason != "end_turn" && agentResult.StopReason != "" {
			errMsg = "stop_reason: " + agentResult.StopReason
		}
		go deps.pluginHookRunner.RunVoidHook(deps.shutdownCtx, plugin.HookAgentEnd, map[string]any{
			"sessionKey": params.SessionKey,
			"runId":      params.ClientRunID,
			"model":      model,
			"turns":      agentResult.Turns,
			"durationMs": totalMs,
			"success":    agentResult.StopReason == "end_turn",
			"error":      errMsg,
		})
	}

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
		// Sync to Aurora for compaction awareness.
		// Use formatRichContent to preserve tool_use/tool_result structure
		// so the compaction summarizer can build accurate <timeline> sections.
		if deps.auroraStore != nil && !isSystemSession(params.SessionKey) {
			text := formatRichContent(chatMsg.Content)
			if text != "" {
				tokenCount := uint64(estimateTokens(text))
				if _, err := deps.auroraStore.SyncMessage(1, msg.Role, text, tokenCount); err != nil {
					logger.Warn("per-turn aurora sync failed", "role", msg.Role, "error", err)
				}
			}
		}
	}
}
