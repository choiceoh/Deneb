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
	}
	// Sync to Aurora store for compaction tracking.
	// Skip for Discord: ephemeral coding sessions don't need compaction.
	if deps.auroraStore != nil && params.Message != "" && !isDiscordDelivery(params.Delivery) {
		tokenCount := uint64(estimateTokens(params.Message))
		if _, err := deps.auroraStore.SyncMessage(1, "user", params.Message, tokenCount); err != nil {
			logger.Warn("aurora: failed to sync user message", "error", err)
		}
	}

	// 2. Kick off proactive context in parallel with prompt + history assembly.
	// The local sglang model analyzes the user message and returns a context
	// hint that reduces the agent's first-turn exploration (saves 1-3 turns).
	type proactiveResult struct{ hint string }
	proactiveCh := make(chan proactiveResult, 1)
	workspaceDir := params.WorkspaceDir
	if workspaceDir == "" {
		workspaceDir = resolveWorkspaceDirForPrompt()
	}
	if params.Message != "" && len(params.Message) >= proactiveMinMsgLen {
		go func() {
			hint := buildProactiveContext(ctx, params.Message, workspaceDir, logger)
			proactiveCh <- proactiveResult{hint: hint}
		}()
	} else {
		proactiveCh <- proactiveResult{} // no-op: skip for short messages
	}

	promptStart := time.Now()
	// 3. Assemble system prompt (supports both string and content block array).
	// The prompt format is deferred: if Anthropic is the provider, we use
	// ContentBlock arrays with cache_control breakpoints; otherwise plain string.
	var systemPrompt json.RawMessage
	var systemPromptParams *prompt.SystemPromptParams // non-nil when dynamic build is needed
	if params.System != "" {
		systemPrompt = llm.SystemString(params.System)
	} else if deps.defaultSystem != "" {
		systemPrompt = llm.SystemString(deps.defaultSystem)
	}
	if len(systemPrompt) == 0 && deps.tools != nil {
		tz, _ := prompt.LoadCachedTimezone()
		spp := prompt.SystemPromptParams{
			WorkspaceDir: workspaceDir,
			ToolDefs:     toPromptToolDefs(deps.tools.Definitions()),
			UserTimezone: tz,
			ContextFiles: prompt.LoadContextFiles(workspaceDir),
			RuntimeInfo:  prompt.BuildDefaultRuntimeInfo(params.Model, deps.defaultModel),
			Channel:      deliveryChannel(params.Delivery),
		}
		systemPromptParams = &spp
		// Defer format choice: only build the format we'll actually use.
		// BuildSystemPrompt (string) is the default; overridden to blocks
		// for Anthropic API after apiType is resolved below.
		// Discord channel uses the coding-focused system prompt.
		if spp.Channel == "discord" {
			systemPrompt = llm.SystemString(prompt.BuildCodingSystemPrompt(spp))
		} else {
			systemPrompt = llm.SystemString(prompt.BuildSystemPrompt(spp))
		}
	}

	logger.Info("pipeline: system prompt built", "ms", time.Since(promptStart).Milliseconds())

	prepStart := time.Now()
	// 3.5 + 4. Run knowledge prefetch and context assembly in parallel.
	// Both are independent: knowledge searches Vega/Memory, context loads transcript history.
	var knowledgeAddition string
	var messages []llm.Message
	var contextErr error

	var prepWg sync.WaitGroup

	// Knowledge prefetch (parallel).
	// For Discord, skip memory recall — coding sessions are ephemeral and
	// don't benefit from conversational memory. Vega (project knowledge) still runs.
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

	// Context assembly (parallel).
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

	// 5. Collect proactive context hint and inject into system prompt.
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

	// 6. Resolve model and provider.
	// Agent tools pass role names ("main", "lightweight", "fallback", "image").
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

	// If no explicit model, use handler default or registry main.
	if model == "" {
		model = deps.defaultModel
	}
	if model == "" && deps.registry != nil {
		model = deps.registry.FullModelID(modelrole.RoleMain)
	}

	// If the request has image attachments, prefer the image model.
	if deps.registry != nil && len(params.Attachments) > 0 && hasImageAttachment(params.Attachments) {
		imgCfg := deps.registry.Config(modelrole.RoleImage)
		if imgCfg.Model != "" {
			model = deps.registry.FullModelID(modelrole.RoleImage)
			initialRole = modelrole.RoleImage
		}
	}

	// Parse provider prefix from model (e.g., "google/gemini-3.0-flash" → provider="google", model="gemini-3.0-flash").
	providerID, modelName := parseModelID(model)
	model = modelName

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

	// 7. Resolve LLM client from provider config, auth manager, or pre-configured client.
	client, apiType := resolveClient(deps, providerID, logger)
	if client == nil {
		return nil, fmt.Errorf("no LLM client available (provider=%q, model=%q)", providerID, model)
	}

	// 8. Build tool list from registry (uses stored descriptions and schemas).
	// Discord channel uses the coding profile (subset of tools).
	var tools []llm.Tool
	if deps.tools != nil {
		if deliveryChannel(params.Delivery) == "discord" {
			tools = deps.tools.LLMToolsForProfile("coding")
		} else {
			tools = deps.tools.LLMTools()
		}
	}

	// For Anthropic API: rebuild system prompt as ContentBlock array with
	// cache_control breakpoints, and mark the last tool for caching.
	if apiType == "anthropic" {
		if systemPromptParams != nil {
			if systemPromptParams.Channel == "discord" {
				systemPrompt = llm.SystemBlocks(prompt.BuildCodingSystemPromptBlocks(*systemPromptParams))
			} else {
				systemPrompt = llm.SystemBlocks(prompt.BuildSystemPromptBlocks(*systemPromptParams))
			}
			// Re-apply knowledge prefetch (the rebuild above replaces the prompt).
			if knowledgeAddition != "" {
				systemPrompt = llm.AppendSystemText(systemPrompt, knowledgeAddition)
			}
		}
		// Re-inject proactive hint (Anthropic rebuild overwrites the string version).
		if proactive.hint != "" {
			systemPrompt = llm.AppendSystemText(systemPrompt,
				"\n## Context Hint (from local analysis)\n"+proactive.hint)
		}
		if len(tools) > 0 {
			tools[len(tools)-1].CacheControl = &llm.CacheControl{Type: "ephemeral"}
		}
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
