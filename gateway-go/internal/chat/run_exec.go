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
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/typing"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/streaming"
	hookspkg "github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/plugin"
	"github.com/choiceoh/deneb/gateway-go/internal/skills"
)

// cachedSkillsPrompt caches the workspace skills prompt at startup to avoid
// re-scanning the filesystem on every chat message. Single-user deployment:
// skills don't change at runtime.
var (
	cachedSkillsPrompt     string
	cachedSkillsPromptOnce sync.Once
)

// loadCachedSkillsPrompt returns the cached skills prompt, building it on first call.
func loadCachedSkillsPrompt(workspaceDir string) string {
	cachedSkillsPromptOnce.Do(func() {
		snapshot := skills.BuildWorkspaceSkillSnapshot(skills.SnapshotConfig{
			DiscoverConfig: skills.DiscoverConfig{
				WorkspaceDir: workspaceDir,
			},
		})
		if snapshot != nil {
			cachedSkillsPrompt = snapshot.Prompt
		}
	})
	return cachedSkillsPrompt
}

// executeAgentRun performs the core agent execution: persist user msg, assemble context,
// run agent loop, persist result.
func executeAgentRun(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	broadcaster *streaming.Broadcaster,
	typingSignaler *typing.FullTypingSignaler,
	statusCtrl *telegram.StatusReactionController,
	logger *slog.Logger,
	runLog *agentlog.RunLogger,
) (*agent.AgentResult, error) {
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
		userMsg := ChatMessage{
			Role:      "user",
			Content:   params.Message,
			Timestamp: time.Now().UnixMilli(),
		}
		if err := deps.transcript.Append(params.SessionKey, userMsg); err != nil {
			logger.Error("failed to persist user message", "error", err)
		}
		if deps.emitTranscriptFn != nil {
			deps.emitTranscriptFn(params.SessionKey, userMsg, "")
		}
	}
	// Sync to Aurora store for compaction tracking.
	// Queue Aurora observer compaction for non-empty user messages.
	if deps.auroraStore != nil && params.Message != "" {
		tokenCount := uint64(estimateTokens(params.Message))
		if _, err := deps.auroraStore.SyncMessage(1, "user", params.Message, tokenCount); err != nil {
			logger.Warn("aurora: failed to sync user message", "error", err)
		}
	}

	workspaceDir := params.WorkspaceDir
	if workspaceDir == "" {
		workspaceDir = resolveWorkspaceDirForPrompt()
	}

	// 2. Resolve model and provider early (no IO — pure config/registry lookups).
	// Must happen before the parallel section so apiType is known when building
	// the system prompt in the parallel goroutine below.
	//
	// Agent tools pass role names ("main", "lightweight", "pilot", "fallback").
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
	if model == "" {
		model = deps.defaultModel
	}
	if model == "" && deps.registry != nil {
		model = deps.registry.FullModelID(modelrole.RoleMain)
	}
	// If the request has image attachments, prefer the lightweight model.
	if deps.registry != nil && len(params.Attachments) > 0 && hasImageAttachment(params.Attachments) {
		lwCfg := deps.registry.Config(modelrole.RoleLightweight)
		if lwCfg.Model != "" {
			model = deps.registry.FullModelID(modelrole.RoleLightweight)
			initialRole = modelrole.RoleLightweight
		}
	}
	// Parse provider prefix (e.g., "google/gemini-3.0-flash" → provider="google", model="gemini-3.0-flash").
	providerID, modelName := parseModelID(model)
	model = modelName

	runLog.LogStart(agentlog.RunStartData{
		Model:    model,
		Provider: providerID,
		Message:  params.Message,
		Channel:  deliveryChannel(params.Delivery),
	})

	// 3. Resolve LLM client (no IO — reads in-memory config/auth store).
	// apiType must be known before the parallel section to build the system prompt
	// in the correct format (Anthropic ContentBlock array vs plain string).
	client, apiType := resolveClient(deps, providerID, logger)
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

	// 4b. Kick off memory recall as a parallel goroutine (fallback model).
	// Runs alongside the main LLM: if it finishes before turn 0, the result
	// is injected into the system prompt. If it arrives during or after the
	// agent run, a follow-up reply is sent with the recalled context.
	recallCh := make(chan string, 1)
	if params.Message != "" && deps.registry != nil && deps.memoryStore != nil {
		go func() {
			recallClient := deps.registry.Client(modelrole.RoleFallback)
			recallModel := deps.registry.FullModelID(modelrole.RoleFallback)
			if recallClient == nil {
				recallCh <- ""
				return
			}
			base := deps.shutdownCtx
			if base == nil {
				base = context.Background()
			}
			recallCh <- memory.Recall(base, deps.memoryStore, deps.memoryEmbedder,
				recallClient, recallModel, params.Message, memory.DefaultRecallConfig(), logger)
		}()
	} else {
		recallCh <- ""
	}

	prepStart := time.Now()
	// 5. Run knowledge prefetch, context assembly, and system prompt build in parallel.
	// All three are now independent: apiType is known (step 3) so the system prompt
	// can be built in the correct format without waiting for sequential resolution.
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
			kDeps := KnowledgeDeps{
				VegaBackend:  deps.vegaBackend,
				WorkspaceDir: workspaceDir,
			}
			{
				kDeps.MemoryStore = deps.memoryStore
				kDeps.MemoryEmbedder = deps.memoryEmbedder
				kDeps.UnifiedStore = deps.unifiedStore
			}
			knowledgeAddition = PrefetchKnowledge(ctx, params.Message, kDeps)
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

	// System prompt build (parallel — apiType resolved early so this no longer
	// needs to be deferred until after the sequential model/client resolution).
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
		spp := prompt.SystemPromptParams{
			WorkspaceDir: workspaceDir,
			ToolDefs:     toPromptToolDefs(deps.tools.Definitions()),
			UserTimezone: tz,
			ContextFiles: prompt.LoadContextFiles(workspaceDir,
				append(memoryContextOpts(deps), prompt.WithSessionSnapshot(params.SessionKey))...),
			RuntimeInfo:  prompt.BuildDefaultRuntimeInfo(params.Model, deps.defaultModel),
			Channel:      ch,
			SkillsPrompt: loadCachedSkillsPrompt(workspaceDir),
		}
		// Telegram is the coding-specialized channel: use the coding
		// system prompt which strips non-coding sections and emphasizes
		// the vibe-coder workflow (no raw code, Korean explanations).
		if ch == "telegram" {
			if apiType == "anthropic" {
				systemPrompt = llm.SystemBlocks(prompt.BuildCodingSystemPromptBlocks(spp))
			} else {
				systemPrompt = llm.SystemString(prompt.BuildCodingSystemPrompt(spp))
			}
		} else {
			if apiType == "anthropic" {
				systemPrompt = llm.SystemBlocks(prompt.BuildSystemPromptBlocks(spp))
			} else {
				systemPrompt = llm.SystemString(prompt.BuildSystemPrompt(spp))
			}
		}
	}()

	prepWg.Wait()
	logger.Info("pipeline: parallel prep done (knowledge+context+sysprompt)", "ms", time.Since(prepStart).Milliseconds())

	if contextErr != nil {
		logger.Warn("context assembly failed, using message only", "error", contextErr)
	}

	// Proactive compaction: if stored tokens exceed the threshold, fire a
	// background sweep. The sweep writes summaries into the Aurora DB; the NEXT
	// request's normal assembly will include them. The current request proceeds
	// with its already-assembled context (no blocking, no stale cache).
	triggerProactiveCompaction(deps.shutdownCtx, deps, params, client, logger)

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

	// 7. Append knowledge and Aurora systemAddition to the built system prompt.
	systemPrompt = llm.AppendSystemTexts(systemPrompt, knowledgeAddition, auroraSystemAddition)

	// 7b. Try to inject recall early (non-blocking). If the fallback model
	// finished recall before the main agent starts, we inject it now so the
	// first response already benefits from recalled memory.
	var recallConsumed bool
	select {
	case recallText := <-recallCh:
		if recallText != "" {
			systemPrompt = llm.AppendSystemText(systemPrompt, recallText)
			recallConsumed = true
			logger.Info("recall: early injection (before turn 0)", "chars", len(recallText))
		} else {
			recallConsumed = true // empty result, nothing to follow up
		}
	default:
		// Recall still running — will be checked via DeferredSystemText or post-run.
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
	var tools []llm.Tool
	if deps.tools != nil {
		tools = deps.tools.LLMTools()
	}

	// For Anthropic: mark the last tool for prompt caching.
	if apiType == "anthropic" && len(tools) > 0 {
		tools[len(tools)-1].CacheControl = &llm.CacheControl{Type: "ephemeral"}
	}

	// 9. Build agent config.
	maxTokens := deps.maxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	// RunCache lives for the entire agent run (across all turns) and caches
	// idempotent tool results (find, tree). Invalidated on mutation tools.
	runCache := NewRunCache()

	// Resolve thinking config from the session's ThinkingLevel setting.
	var thinkingCfg *llm.ThinkingConfig
	if sess := deps.sessions.Get(params.SessionKey); sess != nil && sess.ThinkingLevel != "" {
		thinkingCfg = resolveThinkingConfig(sess.ThinkingLevel)
	}

	cfg := agent.AgentConfig{
		MaxTurns:  defaultMaxTurns,
		Timeout:   defaultAgentTimeout,
		Model:     model,
		System:    systemPrompt,
		Tools:     tools,
		MaxTokens: maxTokens,
		APIType:   apiType,
		Thinking:  thinkingCfg,
		// Drop base64 image bytes from the message history after turn 0 so that
		// subsequent tool-call turns don't retransmit the full image payload.
		// Each inline image is ~1600 tokens; stripping saves that cost per turn
		// from turn 1 onward for multi-turn runs that start with an image.
		StripImagesAfterFirstTurn: hasImageAttachment(params.Attachments),
		// Deferred context injection on turn 1+: proactive hints and/or recall.
		// Non-blocking: returns whatever is ready, skips what isn't.
		DeferredSystemText: deferredMultiHint(proactiveCh, recallCh, &recallConsumed, proactiveStart, logger),
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
			return ctx
		},
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
		hooks.OnToolStart = func(_ string, _ string) {
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
		hooks.OnToolStart = func(name, reason string) {
			if prevOnToolStart != nil {
				prevOnToolStart(name, reason)
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
		hooks.OnToolStart = func(name, reason string) {
			if prevOnToolStart != nil {
				prevOnToolStart(name, reason)
			}
			deps.toolProgressFn(ctx, delivery, ToolProgressEvent{Type: "start", Name: name, Reason: reason})
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
		hooks.OnToolStart = func(name, reason string) {
			if prevOnToolStart != nil {
				prevOnToolStart(name, reason)
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

	// Plugin typed hook: fire after_tool_call event after each tool completes.
	if deps.pluginHookRunner != nil {
		prevOnToolResult := hooks.OnToolResult
		hooks.OnToolResult = func(name, toolUseID, result string, isErr bool) {
			if prevOnToolResult != nil {
				prevOnToolResult(name, toolUseID, result, isErr)
			}
			go deps.pluginHookRunner.RunVoidHook(context.Background(), plugin.HookAfterToolCall, map[string]any{
				"toolName":   name,
				"toolCallId": toolUseID,
				"sessionKey": params.SessionKey,
				"runId":      params.ClientRunID,
				"isError":    isErr,
			})
		}
	}

	// User-defined hook registry: fire tool.use event after each tool completes.
	if deps.hookRegistry != nil {
		prevOnToolResult := hooks.OnToolResult
		hooks.OnToolResult = func(name, toolUseID, result string, isErr bool) {
			if prevOnToolResult != nil {
				prevOnToolResult(name, toolUseID, result, isErr)
			}
			go deps.hookRegistry.Fire(context.Background(), hookspkg.EventToolUse, map[string]string{
				"DENEB_TOOL":        name,
				"DENEB_TOOL_USE_ID": toolUseID,
				"DENEB_IS_ERROR":    fmt.Sprintf("%t", isErr),
				"DENEB_SESSION_KEY": params.SessionKey,
			})
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
		// After stopping the loop, delete the draft message from Telegram so the
		// user doesn't see both the partial draft and the final reply.
		defer func() {
			if draftCtrl != nil {
				draftCtrl.StopForClear()
			}
			// Delete the draft message if one was sent.
			if deps.draftDeleteFn != nil {
				draftMu.Lock()
				msgID := draftMsgID
				draftMu.Unlock()
				if msgID != "" {
					delCtx, delCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
					defer delCancel()
					if err := deps.draftDeleteFn(delCtx, delivery, msgID); err != nil {
						logger.Warn("draft stream delete failed", "msgId", msgID, "error", err)
					}
				}
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

		prevOnDelta := hooks.OnTextDelta
		hooks.OnTextDelta = func(text string) {
			if prevOnDelta != nil {
				prevOnDelta(text)
			}
			accum.WriteString(text)
			draftCtrl.Update(accum.String())
		}

		// On tool start, stop the draft loop so partial text is flushed before
		// the tool runs. The final reply will be delivered by the normal pipeline.
		prevOnToolStart := hooks.OnToolStart
		hooks.OnToolStart = func(name, reason string) {
			draftCtrl.StopForClear()
			if prevOnToolStart != nil {
				prevOnToolStart(name, reason)
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
				compactedMsgs, sysAddition, compErr := handleContextOverflowAurora(
					ctx, deps, params, client, logger,
				)
				if compErr != nil {
					return nil, fmt.Errorf("compaction failed: %w (original: %w)", compErr, runErr)
				}
				messages = compactedMsgs
				if sysAddition != "" {
					cfg.System = llm.AppendSystemText(origSystem, sysAddition)
				}
				continue
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
					agentCfg.APIType = fbCfg.APIType
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
		break
	}

	agentMs := time.Since(agentStart).Milliseconds()
	totalMs := time.Since(runStart).Milliseconds()
	logger.Info("pipeline: agent loop complete",
		"agentMs", agentMs,
		"totalMs", totalMs,
		"turns", agentResult.Turns,
		"inputTokens", agentResult.Usage.InputTokens,
		"outputTokens", agentResult.Usage.OutputTokens)

	// Post-run recall follow-up: if recall wasn't consumed during the agent
	// run (e.g., single-turn Q&A where DeferredSystemText never fires), check
	// now. If it has meaningful content, append it to the agent result so the
	// reply pipeline can deliver it as a follow-up or edit.
	if !recallConsumed {
		select {
		case recallText := <-recallCh:
			recallConsumed = true
			if recallText != "" && agentResult != nil && agentResult.Text != "" {
				logger.Info("recall: post-run follow-up available", "chars", len(recallText))
				agentResult.RecallFollowUp = recallText
			}
		default:
			// Recall still not ready — skip (5s timeout will clean it up).
		}
	}

	// Fire agent_end plugin hook (void, non-blocking).
	if deps.pluginHookRunner != nil {
		go deps.pluginHookRunner.RunVoidHook(context.Background(), plugin.HookAgentEnd, map[string]any{
			"sessionKey": params.SessionKey,
			"runId":      params.ClientRunID,
			"model":      model,
			"turns":      agentResult.Turns,
			"durationMs": totalMs,
			"success":    agentResult.StopReason == "end_turn",
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
