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
	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
)

// cachedWorkspaceDir caches the resolved workspace directory at startup
// to avoid disk I/O (config.LoadConfigFromDefaultPath) on every chat message.
// Single-user deployment: config doesn't change at runtime.
var (
	cachedWorkspaceDir     string
	cachedWorkspaceDirOnce sync.Once
)

// RunParams holds all parameters for an async agent run.
type RunParams struct {
	SessionKey  string
	Message     string
	Attachments []ChatAttachment
	Model       string
	System      string // system prompt override
	ClientRunID string
	Delivery    *DeliveryContext
}

// Agent run defaults.
const (
	defaultMaxTokens     = 8192
	defaultMaxTurns      = 25
	defaultAgentTimeout  = 10 * time.Minute
	defaultModel         = "zai/glm-5-turbo"
	maxCompactionRetries = 2
)

// runDeps holds the dependencies the async run needs from the Handler.
// Optional fields (may be nil): transcript, tools, authManager,
// broadcast, broadcastRaw, jobTracker. Required: sessions, logger.
type runDeps struct {
	sessions        *session.Manager          // required
	llmClient       *llm.Client               // optional; resolved from authManager if nil
	transcript      TranscriptStore           // optional; history unavailable without it
	tools           *ToolRegistry             // optional; no tool use if nil
	authManager     *provider.AuthManager     // optional; uses pre-configured client if nil
	broadcast       BroadcastFunc             // optional
	broadcastRaw    BroadcastRawFunc          // optional
	jobTracker      *agent.JobTracker         // optional
	replyFunc       ReplyFunc                 // optional; delivers response to originating channel
	mediaSendFn     MediaSendFunc             // optional; delivers files to originating channel
	providerConfigs map[string]ProviderConfig // optional; config-based provider credentials
	logger          *slog.Logger              // required (defaults to slog.Default)

	auroraStore     *aurora.Store            // optional; enables Aurora compaction
	vegaBackend     vega.Backend            // optional; enables knowledge prefetch
	memoryStore     *memory.Store           // optional; structured memory (Honcho-style)
	memoryEmbedder  *memory.Embedder        // optional; fact embedding
	dreamingTrigger *memory.DreamingTrigger // optional; dreaming trigger
	contextCfg    ContextConfig
	compactionCfg CompactionConfig
	defaultModel  string
	defaultSystem string
	maxTokens     int
}

// runAgentAsync is the background goroutine that executes an agent run.
// It persists the user message, assembles context, calls the LLM agent loop,
// persists the result, and broadcasts completion events.
func runAgentAsync(ctx context.Context, params RunParams, deps runDeps) {
	logger := deps.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With(
		"session", params.SessionKey,
		"runId", params.ClientRunID,
	)

	// Emit lifecycle start event for agent job tracker.
	if deps.jobTracker != nil {
		deps.jobTracker.OnLifecycleEvent(agent.LifecycleEvent{
			RunID: params.ClientRunID,
			Phase: "start",
			Ts:    time.Now().UnixMilli(),
		})
	}

	// Create streaming broadcaster for this run.
	var broadcaster *streamBroadcaster
	if deps.broadcastRaw != nil {
		broadcaster = newStreamBroadcaster(deps.broadcastRaw, params.SessionKey, params.ClientRunID)
		broadcaster.EmitStarted()
	}

	// Inject delivery context and reply function into ctx so tools
	// (especially the message tool) can send proactive messages.
	if params.Delivery != nil {
		ctx = WithDeliveryContext(ctx, params.Delivery)
	}
	if deps.replyFunc != nil {
		ctx = WithReplyFunc(ctx, deps.replyFunc)
	}
	if deps.mediaSendFn != nil {
		ctx = WithMediaSendFunc(ctx, deps.mediaSendFn)
	}
	ctx = WithSessionKey(ctx, params.SessionKey)

	// Run the agent and capture result.
	result, err := executeAgentRun(ctx, params, deps, broadcaster, logger)

	// Handle completion.
	now := time.Now().UnixMilli()
	if err != nil {
		handleRunError(ctx, params, deps, broadcaster, logger, err, now)
		return
	}

	handleRunSuccess(ctx, params, deps, broadcaster, logger, result, now)
}

// executeAgentRun performs the core agent execution: persist user msg, assemble context,
// run agent loop, persist result.
func executeAgentRun(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	broadcaster *streamBroadcaster,
	logger *slog.Logger,
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
	if deps.auroraStore != nil && params.Message != "" {
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
	workspaceDir := resolveWorkspaceDirForPrompt()
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
	var systemPromptParams *SystemPromptParams // non-nil when dynamic build is needed
	if params.System != "" {
		systemPrompt = llm.SystemString(params.System)
	} else if deps.defaultSystem != "" {
		systemPrompt = llm.SystemString(deps.defaultSystem)
	}
	if len(systemPrompt) == 0 && deps.tools != nil {
		tz, _ := loadCachedTimezone()
		spp := SystemPromptParams{
			WorkspaceDir: workspaceDir,
			ToolDefs:     deps.tools.Definitions(),
			UserTimezone: tz,
			ContextFiles: LoadContextFiles(workspaceDir),
			RuntimeInfo:  BuildDefaultRuntimeInfo(params.Model, deps.defaultModel),
			Channel:      deliveryChannel(params.Delivery),
		}
		systemPromptParams = &spp
		// Defer format choice: only build the format we'll actually use.
		// BuildSystemPrompt (string) is the default; overridden to blocks
		// for Anthropic API after apiType is resolved below.
		systemPrompt = llm.SystemString(BuildSystemPrompt(spp))
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
	prepWg.Add(1)
	go func() {
		defer prepWg.Done()
		if params.Message != "" {
			kDeps := KnowledgeDeps{
				VegaBackend:    deps.vegaBackend,
				WorkspaceDir:   workspaceDir,
				MemoryStore:    deps.memoryStore,
				MemoryEmbedder: deps.memoryEmbedder,
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
	model := params.Model
	if model == "" {
		model = deps.defaultModel
	}
	if model == "" {
		model = defaultModel
	}

	// Parse provider prefix from model (e.g., "zai/glm-5-turbo" → provider="zai", model="glm-5-turbo").
	providerID, modelName := parseModelID(model)
	model = modelName

	// 7. Resolve LLM client from provider config, auth manager, or pre-configured client.
	client, apiType := resolveClient(deps, providerID, logger)
	if client == nil {
		return nil, fmt.Errorf("no LLM client available (provider=%q, model=%q)", providerID, model)
	}

	// 8. Build tool list from registry (uses stored descriptions and schemas).
	var tools []llm.Tool
	if deps.tools != nil {
		tools = deps.tools.LLMTools()
	}

	// For Anthropic API: rebuild system prompt as ContentBlock array with
	// cache_control breakpoints, and mark the last tool for caching.
	if apiType == "anthropic" {
		if systemPromptParams != nil {
			systemPrompt = llm.SystemBlocks(BuildSystemPromptBlocks(*systemPromptParams))
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

	cfg := AgentConfig{
		MaxTurns:  defaultMaxTurns,
		Timeout:   defaultAgentTimeout,
		Model:     model,
		System:    systemPrompt,
		Tools:     tools,
		MaxTokens: maxTokens,
		APIType:   apiType,
	}

	// Mid-conversation memory extraction (Honcho Neuromancer-style).
	// Every ~1000 tokens, asynchronously extract facts from accumulated context.
	if deps.memoryStore != nil {
		var lastExtractTokens int
		cfg.OnTurn = func(turn int, accumulatedTokens int) {
			delta := accumulatedTokens - lastExtractTokens
			if delta >= memory.TokenThreshold {
				lastExtractTokens = accumulatedTokens
				go func() {
					extractCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					sglangClient := getSglangClient()
					// Use the user message + accumulated context for mid-run extraction.
					facts, err := memory.ExtractFacts(extractCtx, sglangClient, sglangModel,
						params.Message, fmt.Sprintf("[mid-run turn %d, %d tokens]", turn, accumulatedTokens), logger)
					if err == nil && len(facts) > 0 {
						memory.InsertExtractedFacts(extractCtx, deps.memoryStore, deps.memoryEmbedder, facts, logger)
						logger.Debug("mid-run memory extraction", "turn", turn, "facts", len(facts))
					}
				}()
			}
		}
	}

	// 10. Set up delta emitter for streaming.
	var emitDelta func(string)
	if broadcaster != nil {
		emitDelta = broadcaster.EmitDelta
	}

	logger.Info("pipeline: prep complete, starting agent loop",
		"prepMs", time.Since(runStart).Milliseconds(),
		"model", model, "provider", providerID,
		"messages", len(messages), "tools", len(tools))

	// 11. Execute agent loop with compaction retry.
	agentStart := time.Now()
	var agentResult *AgentResult
	origSystem := cfg.System // preserve for compaction retries to avoid duplicate appends

	for attempt := 0; attempt <= maxCompactionRetries; attempt++ {
		if attempt > 0 {
			logger.Info("retrying agent run after compaction", "attempt", attempt)
		}

		var runErr error
		agentResult, runErr = RunAgent(ctx, cfg, messages, client, deps.tools, emitDelta, logger)
		if runErr != nil {
			// Check for context overflow error.
			if isContextOverflow(runErr) && attempt < maxCompactionRetries {
				logger.Info("context overflow, attempting compaction", "error", runErr)
				compactedMsgs, sysAddition, compErr := handleContextOverflowAurora(
					deps, params, client, logger,
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

			// Fallback to local sglang when a known remote provider fails
			// (skip if already sglang, context cancelled, or unknown provider).
			if providerID != "sglang" && providerID != "" && ctx.Err() == nil {
				logger.Warn("primary model failed, falling back to sglang",
					"provider", providerID, "model", model, "error", runErr)
				fbClient := getSglangClient()
				fbCfg := cfg
				fbCfg.Model = sglangModel
				fbCfg.APIType = "openai"
				agentResult, runErr = RunAgent(ctx, fbCfg, messages, fbClient, deps.tools, emitDelta, logger)
				if runErr == nil {
					break
				}
				logger.Error("sglang fallback also failed", "error", runErr)
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

// handleRunSuccess processes a successful agent run completion.
func handleRunSuccess(
	_ context.Context,
	params RunParams,
	deps runDeps,
	broadcaster *streamBroadcaster,
	logger *slog.Logger,
	result *AgentResult,
	now int64,
) {
	// Persist assistant message to transcript + Aurora store.
	if deps.transcript != nil && result.Text != "" {
		assistantMsg := ChatMessage{
			Role:      "assistant",
			Content:   result.Text,
			Timestamp: now,
		}
		if err := deps.transcript.Append(params.SessionKey, assistantMsg); err != nil {
			logger.Error("failed to persist assistant message", "error", err)
		}
	}
	if deps.auroraStore != nil && result.Text != "" {
		tokenCount := uint64(estimateTokens(result.Text))
		if _, err := deps.auroraStore.SyncMessage(1, "assistant", result.Text, tokenCount); err != nil {
			logger.Warn("aurora: failed to sync assistant message", "error", err)
		}
	}

	if broadcaster != nil {
		broadcaster.EmitComplete(result.Text, result.Usage)
	}

	// Deliver response back to the originating channel (e.g., Telegram).
	// Suppress delivery if the LLM returned the silent reply token (NO_REPLY).
	if deps.replyFunc != nil && params.Delivery != nil && result.Text != "" {
		if IsSilentReply(result.Text) {
			logger.Info("suppressing silent reply (NO_REPLY)")
		} else {
			replyText := StripSilentToken(result.Text)
			if replyText != "" {
				replyCtx, replyCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer replyCancel()
				if err := deps.replyFunc(replyCtx, params.Delivery, replyText); err != nil {
					logger.Error("channel reply failed", "error", err, "channel", params.Delivery.Channel)
				}
			}
		}
	}

	finishRun(deps, params, session.PhaseEnd, "completed", "done", now)
	emitJobEvent(deps, params.ClientRunID, "end", false, "", now)

	// Auto-memory: extract key learnings asynchronously via local sglang.
	// When structured memory store is available, use Honcho-style importance extraction.
	// Falls back to legacy MEMORY.md append otherwise.
	if params.Message != "" && result.Text != "" {
		go func() {
			memCtx, memCancel := context.WithTimeout(context.Background(), autoMemoryTimeout)
			defer memCancel()

			if deps.memoryStore != nil {
				// Structured extraction: extract facts with importance scoring.
				sglangClient := getSglangClient()
				facts, err := memory.ExtractFacts(memCtx, sglangClient, sglangModel, params.Message, result.Text, logger)
				if err != nil {
					logger.Debug("structured memory extraction failed, falling back", "error", err)
				}
				if len(facts) > 0 {
					memory.InsertExtractedFacts(memCtx, deps.memoryStore, deps.memoryEmbedder, facts, logger)
					// Debounced MEMORY.md export (export every 10 facts).
					if count, _ := deps.memoryStore.ActiveFactCount(memCtx); count%10 == 0 {
						workspaceDir := resolveWorkspaceDirForPrompt()
						if err := deps.memoryStore.ExportToFile(memCtx, workspaceDir); err != nil {
							logger.Debug("memory export failed", "error", err)
						}
					}
				}

				// Check dreaming trigger.
				if deps.dreamingTrigger != nil {
					deps.dreamingTrigger.IncrementTurnAndCheck(memCtx)
				}
			} else {
				// Legacy: append bullet points to MEMORY.md.
				notes := extractAutoMemory(memCtx, params.Message, result.Text, logger)
				if notes != "" {
					workspaceDir := resolveWorkspaceDirForPrompt()
					appendToMemoryFile(workspaceDir, notes, logger)
				}
			}
		}()
	}

	logger.Info("agent run completed",
		"stopReason", result.StopReason,
		"turns", result.Turns,
		"inputTokens", result.Usage.InputTokens,
		"outputTokens", result.Usage.OutputTokens,
	)
}

// handleRunError processes a failed or aborted agent run.
func handleRunError(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	broadcaster *streamBroadcaster,
	logger *slog.Logger,
	err error,
	now int64,
) {
	aborted := ctx.Err() != nil

	if aborted {
		logger.Info("agent run aborted", "error", err)
		if broadcaster != nil {
			broadcaster.EmitAborted("")
		}
		finishRun(deps, params, session.PhaseEnd, "aborted", "killed", now)
		emitJobEvent(deps, params.ClientRunID, "end", true, err.Error(), now)
	} else {
		logger.Error("agent run failed", "error", err)
		if broadcaster != nil {
			broadcaster.EmitError(err.Error())
		}
		finishRun(deps, params, session.PhaseError, "error", "failed", now)
		emitJobEvent(deps, params.ClientRunID, "error", false, err.Error(), now)
	}
}

// finishRun transitions the session out of running and broadcasts the change.
func finishRun(deps runDeps, params RunParams, phase session.LifecyclePhase, reason, status string, ts int64) {
	deps.sessions.ApplyLifecycleEvent(params.SessionKey, session.LifecycleEvent{
		Phase: phase,
		Ts:    ts,
	})
	if deps.broadcast != nil {
		deps.broadcast("sessions.changed", map[string]any{
			"sessionKey": params.SessionKey,
			"reason":     reason,
			"status":     status,
		})
	}
}

// emitJobEvent notifies the job tracker of a lifecycle phase change.
func emitJobEvent(deps runDeps, runID, phase string, aborted bool, errMsg string, ts int64) {
	if deps.jobTracker == nil {
		return
	}
	deps.jobTracker.OnLifecycleEvent(agent.LifecycleEvent{
		RunID:   runID,
		Phase:   phase,
		Aborted: aborted,
		Error:   errMsg,
		Ts:      ts,
	})
}

// parseModelID splits a "provider/model" string into provider and model name.
// If no prefix, returns empty provider and the original model string.
func parseModelID(model string) (providerID, modelName string) {
	if i := strings.IndexByte(model, '/'); i > 0 {
		return model[:i], model[i+1:]
	}
	return "", model
}

// resolveClient creates an LLM client from provider configs, auth manager,
// or falls back to the pre-configured client. Returns the client and API type
// ("anthropic" or "openai").
func resolveClient(deps runDeps, providerID string, logger *slog.Logger) (*llm.Client, string) {
	// 1. Try provider config from deneb.json.
	if deps.providerConfigs != nil && providerID != "" {
		if cfg, ok := deps.providerConfigs[providerID]; ok && cfg.APIKey != "" {
			apiType := cfg.API
			if apiType == "" {
				apiType = inferAPIType(providerID)
			}
			client := llm.NewClient(cfg.BaseURL, cfg.APIKey, llm.WithLogger(logger))
			logger.Info("using provider from config", "provider", providerID, "apiType", apiType)
			return client, apiType
		}
	}

	// 2. Try auth manager.
	if deps.authManager != nil {
		target := providerID
		if target == "" {
			target = "zai" // Default provider: Z.ai Coding Plan (OpenAI-compatible).
		}
		cred := deps.authManager.Resolve(target, "")
		if cred != nil && !cred.IsExpired() && cred.APIKey != "" {
			base := cred.BaseURL
			apiType := inferAPIType(target)
			if base == "" {
				base = resolveDefaultBaseURL(target)
			}
			return llm.NewClient(base, cred.APIKey, llm.WithLogger(logger)), apiType
		}
	}

	// 3. Fall back to pre-configured client (OpenAI-compatible by default).
	if deps.llmClient != nil {
		return deps.llmClient, "openai"
	}

	return nil, ""
}

// Default base URLs for known providers (used when config doesn't specify one).
const (
	// Z.ai Coding Plan global endpoint (OpenAI-compatible).
	// Matches ZAI_CODING_GLOBAL_BASE_URL in src/plugins/provider-model-definitions.ts.
	defaultZaiBaseURL = "https://api.z.ai/api/coding/paas/v4"

	// Local sglang server (OpenAI-compatible). Used as fallback and for lightweight tasks.
	defaultSglangBaseURL = "http://127.0.0.1:30000/v1"
	sglangModel          = "Qwen/Qwen3.5-35B-A3B"
)

// inferAPIType guesses the API type from the provider ID.
// OpenAI-compatible is the default; Anthropic is special-cased.
func inferAPIType(providerID string) string {
	switch providerID {
	case "anthropic":
		return "anthropic"
	default:
		// Default: OpenAI-compatible API (openai, zai, sglang, deepseek, etc.)
		return "openai"
	}
}

// executeAgentRunWithDelta is a variant of executeAgentRun that accepts a direct
// onDelta callback for streaming text to HTTP clients.
func executeAgentRunWithDelta(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	onDelta func(string),
	logger *slog.Logger,
) (*AgentResult, error) {
	deltaRaw := BroadcastRawFunc(func(event string, data []byte) int {
		if onDelta == nil || event != "chat.delta" {
			return 0
		}
		var envelope struct {
			Payload struct {
				Delta string `json:"delta"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(data, &envelope); err == nil && envelope.Payload.Delta != "" {
			onDelta(envelope.Payload.Delta)
		}
		return 1
	})
	broadcaster := newStreamBroadcaster(deltaRaw, params.SessionKey, params.ClientRunID)
	return executeAgentRun(ctx, params, deps, broadcaster, logger)
}

// resolveDefaultBaseURL returns the default API base URL for a known provider
// when no explicit base URL is configured.
func resolveDefaultBaseURL(providerID string) string {
	switch providerID {
	case "anthropic":
		return llm.DefaultAnthropicBaseURL
	case "zai":
		return defaultZaiBaseURL
	case "sglang":
		return defaultSglangBaseURL
	default:
		return ""
	}
}

// buildAttachmentBlocks creates a multimodal content block array from text and
// attachments. Images with base64 Data get Anthropic-native ImageSource blocks;
// images with URL get URL-referenced blocks.
func buildAttachmentBlocks(text string, attachments []ChatAttachment) []llm.ContentBlock {
	blocks := make([]llm.ContentBlock, 0, len(attachments)+1)
	if text != "" {
		blocks = append(blocks, llm.ContentBlock{Type: "text", Text: text})
	}
	for _, att := range attachments {
		switch att.Type {
		case "image":
			if att.Data != "" {
				// Base64-encoded inline image (from Telegram download).
				blocks = append(blocks, llm.ContentBlock{
					Type: "image",
					Source: &llm.ImageSource{
						Type:      "base64",
						MediaType: att.MimeType,
						Data:      att.Data,
					},
				})
			} else if att.URL != "" {
				blocks = append(blocks, llm.ContentBlock{
					Type: "image",
					Source: &llm.ImageSource{
						Type:      "url",
						MediaType: att.MimeType,
						Data:      att.URL,
					},
				})
			}

		case "document_text":
			// Text extracted from a document (PDF, Office, etc.) via LiteParse.
			label := att.Name
			if label == "" {
				label = "document"
			}
			blocks = append(blocks, llm.ContentBlock{
				Type: "text",
				Text: fmt.Sprintf("[%s]\n\n%s", label, att.Data),
			})
		}
	}
	return blocks
}

// appendAttachmentsToHistory finds the last user message in the history and
// replaces it with a multimodal version that includes attachment content blocks.
// This is needed because transcript persistence stores text only; the
// attachments must be re-injected before sending to the LLM.
func appendAttachmentsToHistory(messages []llm.Message, text string, attachments []ChatAttachment) []llm.Message {
	// Find the last user message.
	lastUserIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		var role struct {
			Role string `json:"role"`
		}
		if err := json.Unmarshal(messages[i].Content, &role); err == nil && role.Role == "" {
			// Content is a string, not structured. Check role from the Message.
		}
		if messages[i].Role == "user" {
			lastUserIdx = i
			break
		}
	}

	if lastUserIdx < 0 {
		// No user message in history; append a new multimodal message.
		blocks := buildAttachmentBlocks(text, attachments)
		return append(messages, llm.NewBlockMessage("user", blocks))
	}

	// Replace the last user message with a multimodal version.
	// Extract existing text from the message.
	existingText := extractTextFromMessage(messages[lastUserIdx])
	if existingText == "" {
		existingText = text
	}

	blocks := buildAttachmentBlocks(existingText, attachments)
	result := make([]llm.Message, len(messages))
	copy(result, messages)
	result[lastUserIdx] = llm.NewBlockMessage("user", blocks)
	return result
}

// extractTextFromMessage extracts the text content from a Message.
// Handles both string content and structured content block arrays.
func extractTextFromMessage(msg llm.Message) string {
	// Try as plain string first.
	var s string
	if err := json.Unmarshal(msg.Content, &s); err == nil {
		return s
	}
	// Try as content block array.
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err == nil {
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				return b.Text
			}
		}
	}
	return ""
}

// isContextOverflow checks if an error indicates a context window overflow.
func isContextOverflow(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "context_length_exceeded") ||
		strings.Contains(msg, "context_too_long") ||
		strings.Contains(msg, "prompt is too long") ||
		strings.Contains(msg, "maximum context length")
}

// stopReasonFromCtx determines the stop reason from a context error.
func stopReasonFromCtx(ctx context.Context) string {
	if ctx.Err() == context.DeadlineExceeded {
		return "timeout"
	}
	return "aborted"
}

// resolveWorkspaceDirForPrompt returns the workspace directory for system prompt assembly.
// Reads agents.defaults.workspace / agents.list[].workspace from config,
// falling back to ~/.deneb/workspace (matching TS resolveAgentWorkspaceDir).
func resolveWorkspaceDirForPrompt() string {
	cachedWorkspaceDirOnce.Do(func() {
		snap, err := config.LoadConfigFromDefaultPath()
		if err == nil && snap != nil {
			dir := config.ResolveAgentWorkspaceDir(&snap.Config)
			if dir != "" {
				cachedWorkspaceDir = dir
				return
			}
		}
		cachedWorkspaceDir = config.ResolveAgentWorkspaceDir(nil)
	})
	return cachedWorkspaceDir
}

// deliveryChannel extracts the channel name from a delivery context.
func deliveryChannel(d *DeliveryContext) string {
	if d == nil {
		return ""
	}
	return d.Channel
}

// Definitions returns all registered tool definitions (for system prompt assembly).
func (r *ToolRegistry) Definitions() []ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]ToolDef, 0, len(r.order))
	for _, name := range r.order {
		defs = append(defs, r.tools[name])
	}
	return defs
}
