// run_exec.go contains the core agent execution loop: user message persistence,
// context assembly, LLM invocation with compaction retry and model fallback.
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/typing"
	"github.com/choiceoh/deneb/gateway-go/internal/channel"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
)

// executeAgentRun performs the core agent execution: persist user msg, assemble context,
// run agent loop, persist result.
func executeAgentRun(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	broadcaster *streaming.Broadcaster,
	typingSignaler *typing.FullTypingSignaler,
	statusCtrl *channel.StatusReactionController,
	logger *slog.Logger,
	runLog *agentlog.RunLogger,
) (*AgentResult, error) {
	runStart := time.Now()

	// ── Pipeline stage 0: Zen Decoder — pre-decode the user message ─────
	// Extract hints (greeting, short, attachments, keywords) that guide
	// downstream stages to skip unnecessary work.
	decoded := DecodeMessage(params.Message, params.Attachments)

	workspaceDir := params.WorkspaceDir
	if workspaceDir == "" {
		workspaceDir = resolveWorkspaceDirForPrompt()
	}

	// ── Pipeline stage 1: Early model + provider resolution ─────────────
	// CPU architecture technique: "pipeline interleaving" — resolve model/provider
	// first so we know the API type (anthropic vs openai) before building the
	// system prompt. This eliminates the Anthropic double-build (string → blocks
	// → re-inject knowledge/proactive) that previously wasted ~10-50ms.
	model := params.Model
	initialRole := modelrole.RoleMain

	if deps.registry != nil && model != "" {
		if resolved, role, ok := deps.registry.ResolveModel(model); ok {
			model = resolved
			initialRole = role
		}
	}
	if model == "" {
		model = deps.defaultModel
	}
	if model == "" && deps.registry != nil {
		model = deps.registry.FullModelID(modelrole.RoleMain)
	}
	if deps.registry != nil && decoded.HasImageAttachment {
		imgCfg := deps.registry.Config(modelrole.RoleImage)
		if imgCfg.Model != "" {
			model = deps.registry.FullModelID(modelrole.RoleImage)
			initialRole = modelrole.RoleImage
		}
	}

	providerID, modelName := parseModelID(model)
	model = modelName

	client, apiType := resolveClient(deps, providerID, logger)
	if client == nil {
		return nil, fmt.Errorf("no LLM client available (provider=%q, model=%q)", providerID, model)
	}
	isAnthropic := apiType == "anthropic"

	// ── Pipeline stage 2: Persist user message + async Aurora sync ──────
	// CPU architecture technique: "write-back buffer" — transcript.Append is
	// synchronous (write-through cache ensures context assembly sees the new
	// message), but Aurora sync is fire-and-forget since it's only needed for
	// compaction tracking and has no downstream data dependency.
	if deps.transcript != nil && params.Message != "" {
		userMsg := ChatMessage{
			Role:      "user",
			Content:   params.Message,
			Timestamp: time.Now().UnixMilli(),
		}
		if err := deps.transcript.Append(params.SessionKey, userMsg); err != nil {
			logger.Error("failed to persist user message", "error", err)
		}
	}
	if deps.auroraStore != nil && params.Message != "" && !isDiscordDelivery(params.Delivery) {
		auroraStore := deps.auroraStore
		msg := params.Message
		go func() {
			tokenCount := uint64(estimateTokens(msg))
			if _, err := auroraStore.SyncMessage(1, "user", msg, tokenCount); err != nil {
				logger.Warn("aurora: failed to sync user message", "error", err)
			}
		}()
	}

	// ── Pipeline stage 3: Proactive context (parallel goroutine) ────────
	// Zen Decoder hint: skip proactive context for greetings and short messages
	// (they don't benefit from local LLM analysis — saves 100-500ms).
	type proactiveResult struct{ hint string }
	proactiveCh := make(chan proactiveResult, 1)
	if params.Message != "" && !decoded.IsShort && !decoded.IsGreeting {
		go func() {
			hint := buildProactiveContext(ctx, params.Message, workspaceDir, logger)
			proactiveCh <- proactiveResult{hint: hint}
		}()
	} else {
		proactiveCh <- proactiveResult{}
	}

	// ── Pipeline stage 4: System prompt (L3 cache + correct format) ────
	// CPU architecture technique: L3 session cache — compiled system prompts are
	// expensive to build (context file loading, tool schema serialization, ~10-50ms).
	// Most components are stable within a session, so we cache the result keyed
	// by (channel, toolCount, workspaceDir, apiType). Cache hit → ~0ms.
	//
	// Since apiType is already known (from stage 1), we build in the right format
	// directly: Anthropic → ContentBlock array, others → plain string.
	promptStart := time.Now()
	var systemPrompt json.RawMessage
	if params.System != "" {
		systemPrompt = llm.SystemString(params.System)
	} else if deps.defaultSystem != "" {
		systemPrompt = llm.SystemString(deps.defaultSystem)
	}
	if len(systemPrompt) == 0 && deps.tools != nil {
		toolCount := len(deps.tools.Definitions())
		promptKey := PromptCacheKey(deliveryChannel(params.Delivery), toolCount, workspaceDir, apiType)

		if cached, ok := deps.sessionCache.GetPrompt(promptKey); ok {
			systemPrompt = cached
			logger.Info("pipeline: system prompt cache hit")
		} else {
			tz, _ := prompt.LoadCachedTimezone()
			spp := prompt.SystemPromptParams{
				WorkspaceDir: workspaceDir,
				ToolDefs:     toPromptToolDefs(deps.tools.Definitions()),
				UserTimezone: tz,
				ContextFiles: prompt.LoadContextFiles(workspaceDir),
				RuntimeInfo:  prompt.BuildDefaultRuntimeInfo(params.Model, deps.defaultModel),
				Channel:      deliveryChannel(params.Delivery),
			}
			if isAnthropic {
				if spp.Channel == "discord" {
					systemPrompt = llm.SystemBlocks(prompt.BuildCodingSystemPromptBlocks(spp))
				} else {
					systemPrompt = llm.SystemBlocks(prompt.BuildSystemPromptBlocks(spp))
				}
			} else {
				if spp.Channel == "discord" {
					systemPrompt = llm.SystemString(prompt.BuildCodingSystemPrompt(spp))
				} else {
					systemPrompt = llm.SystemString(prompt.BuildSystemPrompt(spp))
				}
			}
			deps.sessionCache.SetPrompt(promptKey, systemPrompt)
		}
	}

	logger.Info("pipeline: system prompt built", "ms", time.Since(promptStart).Milliseconds())

	// ── Pipeline stage 5: Knowledge + context assembly (parallel) ───────
	prepStart := time.Now()
	var knowledgeAddition string
	var messages []llm.Message
	var contextErr error

	var prepWg sync.WaitGroup

	prepWg.Add(1)
	go func() {
		defer prepWg.Done()
		if params.Message != "" {
			kDeps := KnowledgeDeps{
				VegaBackend:  deps.vegaBackend,
				WorkspaceDir: workspaceDir,
			}
			if !isDiscordDelivery(params.Delivery) {
				kDeps.MemoryStore = deps.memoryStore
				kDeps.MemoryEmbedder = deps.memoryEmbedder
			}
			knowledgeAddition = PrefetchKnowledge(ctx, params.Message, kDeps)
		}
	}()

	prepWg.Add(1)
	go func() {
		defer prepWg.Done()
		if deps.transcript != nil {
			result, err := assembleContext(deps.transcript, params.SessionKey, deps.contextCfg, logger)
			if err != nil {
				contextErr = err
			} else {
				messages = result.Messages
				// CPU technique: "branch prediction" — record token pressure so we
				// can predict context overflow before it happens. High pressure
				// sessions can pre-evaluate compaction in the next run.
				if deps.compactionPressure != nil && result.EstimatedTokens > 0 {
					deps.compactionPressure.Update(
						params.SessionKey,
						result.EstimatedTokens,
						int(deps.contextCfg.TokenBudget),
						result.TotalMessages,
					)
					deps.compactionPressure.LogPressure(params.SessionKey, logger)
				}
			}
		}
	}()

	prepWg.Wait()
	logger.Info("pipeline: knowledge+context parallel prep done", "ms", time.Since(prepStart).Milliseconds())

	if contextErr != nil {
		logger.Warn("context assembly failed, using message only", "error", contextErr)
	}
	if knowledgeAddition != "" {
		systemPrompt = llm.AppendSystemText(systemPrompt, knowledgeAddition)
	}

	// Build or augment user message with attachments.
	if len(messages) == 0 && params.Message != "" {
		if len(params.Attachments) > 0 {
			blocks := buildAttachmentBlocks(params.Message, params.Attachments)
			messages = []llm.Message{llm.NewBlockMessage("user", blocks)}
		} else {
			messages = []llm.Message{llm.NewTextMessage("user", params.Message)}
		}
	} else if len(messages) > 0 && len(params.Attachments) > 0 {
		messages = appendAttachmentsToHistory(messages, params.Message, params.Attachments)
	}

	// ── Pipeline stage 6: Collect proactive context hint ────────────────
	proactiveWaitStart := time.Now()
	proactive := <-proactiveCh
	if pw := time.Since(proactiveWaitStart).Milliseconds(); pw > 50 {
		logger.Info("pipeline: proactive context wait", "ms", pw)
	}
	if proactive.hint != "" {
		systemPrompt = llm.AppendSystemText(systemPrompt,
			"\n## Context Hint (from local analysis)\n"+proactive.hint)
		logger.Info("proactive context injected", "chars", len(proactive.hint))
	}

	// Log run.start with resolved model/provider info.
	runLog.LogStart(agentlog.RunStartData{
		Model:    model,
		Provider: providerID,
		Message:  params.Message,
		Channel:  deliveryChannel(params.Delivery),
	})

	// Log run.prep with context assembly metrics.
	runLog.LogPrep(agentlog.RunPrepData{
		SystemPromptChars: len(systemPrompt),
		ContextMessages:   len(messages),
		KnowledgeChars:    len(knowledgeAddition),
		PrepMs:            time.Since(runStart).Milliseconds(),
	})

	// ── Pipeline stage 7: Build tools + Anthropic caching ───────────────
	var tools []llm.Tool
	if deps.tools != nil {
		if deliveryChannel(params.Delivery) == "discord" {
			tools = deps.tools.LLMToolsForProfile("coding")
		} else {
			tools = deps.tools.LLMTools()
		}
	}

	if isAnthropic && len(tools) > 0 {
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

	cfg := AgentConfig{
		MaxTurns:  defaultMaxTurns,
		Timeout:   defaultAgentTimeout,
		Model:     model,
		System:    systemPrompt,
		Tools:     tools,
		MaxTokens: maxTokens,
		APIType:   apiType,
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
	var hooks StreamHooks
	if broadcaster != nil {
		hooks.OnTextDelta = broadcaster.EmitDelta
		hooks.OnToolEmit = broadcaster.EmitToolStart
		hooks.OnToolResult = func(name, toolUseID, result string, isErr bool) {
			broadcaster.EmitToolResult(name, toolUseID, result, isErr)
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

	// Discord tool progress tracking: send tool start/complete events back to Discord
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

	logger.Info("pipeline: prep complete, starting agent loop",
		"prepMs", time.Since(runStart).Milliseconds(),
		"model", model, "provider", providerID,
		"messages", len(messages), "tools", len(tools))

	// 11. Execute agent loop with compaction retry and model fallback chain.
	agentStart := time.Now()
	var agentResult *AgentResult
	origSystem := cfg.System // preserve for compaction retries to avoid duplicate appends

	for attempt := 0; attempt <= maxCompactionRetries; attempt++ {
		if attempt > 0 {
			logger.Info("retrying agent run after compaction", "attempt", attempt)
		}

		var runErr error
		agentResult, runErr = RunAgent(ctx, cfg, messages, client, deps.tools, hooks, logger, runLog)
		if runErr != nil {
			// Check for context overflow error.
			// Skip Aurora compaction for Discord: ephemeral coding sessions
			// don't maintain Aurora state, so compaction would be a no-op.
			if isContextOverflow(runErr) && attempt < maxCompactionRetries && !isDiscordDelivery(params.Delivery) {
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
					agentResult, runErr = RunAgent(ctx, agentCfg, messages, fbClient, deps.tools, hooks, logger, runLog)
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

	logger.Info("pipeline: agent loop complete",
		"agentMs", time.Since(agentStart).Milliseconds(),
		"totalMs", time.Since(runStart).Milliseconds(),
		"turns", agentResult.Turns,
		"inputTokens", agentResult.Usage.InputTokens,
		"outputTokens", agentResult.Usage.OutputTokens)

	return agentResult, nil
}
