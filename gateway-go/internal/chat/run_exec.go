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

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
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
) (*agent.AgentResult, error) {
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
		return nil, fmt.Errorf("no LLM client available (provider=%q, model=%q)", providerID, model)
	}

	// 4. Kick off proactive context as a fire-and-forget goroutine.
	// The local sglang model analyzes the user message and returns a context
	// hint that reduces the agent's first-turn exploration (saves 1-3 turns).
	// We check for the result non-blocking after the parallel section: if the
	// hint is ready it gets injected; if not, we skip it rather than stalling.
	type proactiveResult struct{ hint string }
	proactiveCh := make(chan proactiveResult, 1)
	if params.Message != "" && len(params.Message) >= proactiveMinMsgLen {
		go func() {
			hint := buildProactiveContext(ctx, params.Message, workspaceDir, logger)
			proactiveCh <- proactiveResult{hint: hint}
		}()
	} else {
		proactiveCh <- proactiveResult{} // no-op: skip for short messages
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
		spp := prompt.SystemPromptParams{
			WorkspaceDir: workspaceDir,
			ToolDefs:     toPromptToolDefs(deps.tools.Definitions()),
			UserTimezone: tz,
			ContextFiles: prompt.LoadContextFiles(workspaceDir,
				append(memoryContextOpts(deps), prompt.WithSessionSnapshot(params.SessionKey))...),
			RuntimeInfo: prompt.BuildDefaultRuntimeInfo(params.Model, deps.defaultModel),
			Channel:     deliveryChannel(params.Delivery),
		}
		if apiType == "anthropic" {
			systemPrompt = llm.SystemBlocks(prompt.BuildSystemPromptBlocks(spp))
		} else {
			systemPrompt = llm.SystemString(prompt.BuildSystemPrompt(spp))
		}
	}()

	prepWg.Wait()
	logger.Info("pipeline: parallel prep done (knowledge+context+sysprompt)", "ms", time.Since(prepStart).Milliseconds())

	if contextErr != nil {
		logger.Warn("context assembly failed, using message only", "error", contextErr)
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

	// 6. Bounded wait for proactive context hint.
	// The goroutine runs concurrently with the parallel prep above; after prep
	// completes we wait briefly to improve hit-rate without adding noticeable
	// latency. If still not ready, skip it to avoid stalling the run.
	// Hit rate is logged here to inform future tuning decisions.
	var proactiveHint string
	select {
	case proactive := <-proactiveCh:
		if proactive.hint != "" {
			logger.Info("proactive context hit", "chars", len(proactive.hint))
			proactiveHint = "\n## Context Hint (from local analysis)\n" + proactive.hint
		} else {
			logger.Debug("proactive context miss (N/A or filtered)")
		}
	default:
		const proactiveGraceWait = 120 * time.Millisecond
		select {
		case proactive := <-proactiveCh:
			if proactive.hint != "" {
				logger.Info("proactive context hit (after grace wait)",
					"chars", len(proactive.hint),
					"waitMs", proactiveGraceWait.Milliseconds())
				proactiveHint = "\n## Context Hint (from local analysis)\n" + proactive.hint
			} else {
				logger.Debug("proactive context miss (N/A or filtered)")
			}
		case <-time.After(proactiveGraceWait):
			logger.Debug("proactive context not ready, skipping (fire-and-forget)",
				"graceWaitMs", proactiveGraceWait.Milliseconds())
		}
	}

	// 7. Append knowledge, proactive hint, and Aurora systemAddition to the built system prompt.
	systemPrompt = llm.AppendSystemTexts(systemPrompt, knowledgeAddition, proactiveHint, auroraSystemAddition)
	logger.Info("pipeline: system prompt finalized",
		"chars", len(systemPrompt),
		"knowledgeChars", len(knowledgeAddition),
		"proactiveInjected", proactiveHint != "")

	runLog.LogPrep(agentlog.RunPrepData{
		SystemPromptChars: len(systemPrompt),
		ContextMessages:   len(messages),
		KnowledgeChars:    len(knowledgeAddition),
		PrepMs:            time.Since(runStart).Milliseconds(),
	})

	// 8. Build tool list from registry (uses stored descriptions and schemas).
	// Profile selection reduces per-turn schema token cost:
	//   telegram → "chat" profile for general conversation, or full tools on coding triggers.
	//   other    → full set (safe default).
	var tools []llm.Tool
	if deps.tools != nil {
		switch deliveryChannel(params.Delivery) {
		case "telegram":
			tools = deps.tools.LLMToolsForProfile(classifyMessageProfile(params.Message))
		default:
			tools = deps.tools.LLMTools()
		}
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

	cfg := agent.AgentConfig{
		MaxTurns:  defaultMaxTurns,
		Timeout:   defaultAgentTimeout,
		Model:     model,
		System:    systemPrompt,
		Tools:     tools,
		MaxTokens: maxTokens,
		APIType:   apiType,
		// Drop base64 image bytes from the message history after turn 0 so that
		// subsequent tool-call turns don't retransmit the full image payload.
		// Each inline image is ~1600 tokens; stripping saves that cost per turn
		// from turn 1 onward for multi-turn runs that start with an image.
		StripImagesAfterFirstTurn: hasImageAttachment(params.Attachments),
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

	logger.Info("pipeline: agent loop complete",
		"agentMs", time.Since(agentStart).Milliseconds(),
		"totalMs", time.Since(runStart).Milliseconds(),
		"turns", agentResult.Turns,
		"inputTokens", agentResult.Usage.InputTokens,
		"outputTokens", agentResult.Usage.OutputTokens)

	return agentResult, nil
}
