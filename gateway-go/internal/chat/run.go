package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
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
	defaultMaxTokens        = 8192
	defaultMaxTurns         = 25
	defaultAgentTimeout     = 10 * time.Minute
	defaultModel            = "zai/glm-5-turbo"
	maxCompactionRetries    = 2
)

// runDeps holds the dependencies the async run needs from the Handler.
// Optional fields (may be nil): transcript, tools, authManager,
// broadcast, broadcastRaw, jobTracker. Required: sessions, logger.
type runDeps struct {
	sessions     *session.Manager       // required
	llmClient    *llm.Client            // optional; resolved from authManager if nil
	transcript   TranscriptStore        // optional; history unavailable without it
	tools        *ToolRegistry          // optional; no tool use if nil
	authManager  *provider.AuthManager  // optional; uses pre-configured client if nil
	broadcast    BroadcastFunc          // optional
	broadcastRaw BroadcastRawFunc       // optional
	jobTracker   *agent.JobTracker      // optional
	replyFunc       ReplyFunc              // optional; delivers response to originating channel
	providerConfigs map[string]ProviderConfig // optional; config-based provider credentials
	logger          *slog.Logger             // required (defaults to slog.Default)

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
	// 1. Persist user message to transcript.
	if deps.transcript != nil && params.Message != "" {
		userMsg := ChatMessage{
			Role:      "user",
			Content:   params.Message,
			Timestamp: time.Now().UnixMilli(),
		}
		if err := deps.transcript.Append(params.SessionKey, userMsg); err != nil {
			logger.Error("failed to persist user message", "error", err)
			// Continue anyway — don't block the agent run.
		}
	}

	// 2. Assemble system prompt (supports both string and content block array).
	var systemPrompt json.RawMessage
	if params.System != "" {
		systemPrompt = llm.SystemString(params.System)
	} else if deps.defaultSystem != "" {
		systemPrompt = llm.SystemString(deps.defaultSystem)
	}
	if len(systemPrompt) == 0 && deps.tools != nil {
		// Build system prompt from tool definitions, workspace context, and runtime info.
		workspaceDir := resolveWorkspaceDirForPrompt()
		built := BuildSystemPrompt(SystemPromptParams{
			WorkspaceDir: workspaceDir,
			ToolDefs:     deps.tools.Definitions(),
			UserTimezone: resolveTimezone(),
			ContextFiles: LoadContextFiles(workspaceDir),
			RuntimeInfo:  BuildDefaultRuntimeInfo(params.Model, deps.defaultModel),
			Channel:      deliveryChannel(params.Delivery),
		})
		systemPrompt = llm.SystemString(built)
	}

	// 3. Assemble context (token-budgeted history).
	var messages []llm.Message
	if deps.transcript != nil {
		result, err := assembleContext(deps.transcript, params.SessionKey, deps.contextCfg, logger)
		if err != nil {
			logger.Warn("context assembly failed, using message only", "error", err)
		} else {
			messages = result.Messages
		}
	}

	// If context assembly failed or had no history, build user message.
	if len(messages) == 0 && params.Message != "" {
		// Include attachments as multimodal content blocks if present.
		if len(params.Attachments) > 0 {
			blocks := []llm.ContentBlock{{Type: "text", Text: params.Message}}
			for _, att := range params.Attachments {
				if att.URL != "" && att.Type == "image" {
					blocks = append(blocks, llm.ContentBlock{
						Type: "image",
						Source: &llm.ImageSource{
							Type:      "url",
							MediaType: att.MimeType,
							Data:      att.URL,
						},
					})
				}
			}
			messages = []llm.Message{llm.NewBlockMessage("user", blocks)}
		} else {
			messages = []llm.Message{llm.NewTextMessage("user", params.Message)}
		}
	}

	// 3. Resolve model and provider.
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

	// 4. Resolve LLM client from provider config, auth manager, or pre-configured client.
	client, apiType := resolveClient(deps, providerID, logger)
	if client == nil {
		return nil, fmt.Errorf("no LLM client available (provider=%q, model=%q)", providerID, model)
	}

	// 5. Build tool list from registry (uses stored descriptions and schemas).
	var tools []llm.Tool
	if deps.tools != nil {
		tools = deps.tools.LLMTools()
	}

	// 6. Build agent config.
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

	// 7. Set up delta emitter for streaming.
	var emitDelta func(string)
	if broadcaster != nil {
		emitDelta = broadcaster.EmitDelta
	}

	// 8. Execute agent loop with compaction retry.
	var agentResult *AgentResult

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
				compactedMsgs, compErr := handleContextOverflow(
					deps.transcript, params.SessionKey,
					deps.contextCfg, deps.compactionCfg, logger,
				)
				if compErr != nil {
					return nil, fmt.Errorf("compaction failed: %w (original: %w)", compErr, runErr)
				}
				messages = compactedMsgs
				continue
			}
			return nil, runErr
		}
		break
	}

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
	// Persist assistant message to transcript.
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
	default:
		return ""
	}
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
func resolveWorkspaceDirForPrompt() string {
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return "/tmp"
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
