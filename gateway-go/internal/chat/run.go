package chat

import (
	"context"
	"fmt"
	"log/slog"
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
	defaultModel            = "claude-sonnet-4-20250514"
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
	logger       *slog.Logger           // required (defaults to slog.Default)

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

	// 2. Assemble context (token-budgeted history).
	systemPrompt := params.System
	if systemPrompt == "" {
		systemPrompt = deps.defaultSystem
	}

	var messages []llm.Message
	if deps.transcript != nil {
		result, err := assembleContext(deps.transcript, params.SessionKey, deps.contextCfg, logger)
		if err != nil {
			logger.Warn("context assembly failed, using message only", "error", err)
		} else {
			messages = result.Messages
		}
	}

	// If context assembly failed or had no history, just use the current message.
	if len(messages) == 0 && params.Message != "" {
		messages = []llm.Message{
			llm.NewTextMessage("user", params.Message),
		}
	}

	// 3. Resolve model.
	model := params.Model
	if model == "" {
		model = deps.defaultModel
	}
	if model == "" {
		model = defaultModel
	}

	// 4. Resolve API key from provider auth manager.
	apiKey, baseURL, err := resolveAPIKey(deps.authManager, logger)
	if err != nil {
		return nil, fmt.Errorf("resolve API key: %w", err)
	}

	// Create LLM client (use resolved key, or fall back to pre-configured client).
	client := deps.llmClient
	if apiKey != "" {
		client = llm.NewClient(baseURL, apiKey, llm.WithLogger(logger))
	}
	if client == nil {
		return nil, fmt.Errorf("no LLM client available")
	}

	// 5. Build tool list from registry.
	var tools []llm.Tool
	if deps.tools != nil {
		names := deps.tools.Names()
		tools = make([]llm.Tool, len(names))
		for i, name := range names {
			tools[i] = llm.Tool{
				Name:        name,
				Description: "Tool: " + name,
				InputSchema: map[string]any{"type": "object"},
			}
		}
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

	// Broadcast completion.
	if broadcaster != nil {
		broadcaster.EmitComplete(result.Text, result.Usage)
	}

	// Update session lifecycle.
	deps.sessions.ApplyLifecycleEvent(params.SessionKey, session.LifecycleEvent{
		Phase: session.PhaseEnd,
		Ts:    now,
	})
	if deps.broadcast != nil {
		deps.broadcast("sessions.changed", map[string]any{
			"sessionKey": params.SessionKey,
			"reason":     "completed",
			"status":     "done",
		})
	}

	// Emit lifecycle end event for job tracker.
	if deps.jobTracker != nil {
		deps.jobTracker.OnLifecycleEvent(agent.LifecycleEvent{
			RunID: params.ClientRunID,
			Phase: "end",
			Ts:    now,
		})
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
		deps.sessions.ApplyLifecycleEvent(params.SessionKey, session.LifecycleEvent{
			Phase: session.PhaseEnd,
			Ts:    now,
		})
		if deps.broadcast != nil {
			deps.broadcast("sessions.changed", map[string]any{
				"sessionKey": params.SessionKey,
				"reason":     "aborted",
				"status":     "killed",
			})
		}
	} else {
		logger.Error("agent run failed", "error", err)
		if broadcaster != nil {
			broadcaster.EmitError(err.Error())
		}
		deps.sessions.ApplyLifecycleEvent(params.SessionKey, session.LifecycleEvent{
			Phase: session.PhaseError,
			Ts:    now,
		})
		if deps.broadcast != nil {
			deps.broadcast("sessions.changed", map[string]any{
				"sessionKey": params.SessionKey,
				"reason":     "error",
				"status":     "failed",
			})
		}
	}

	// Emit lifecycle event for job tracker.
	if deps.jobTracker != nil {
		phase := "error"
		if aborted {
			phase = "end"
		}
		deps.jobTracker.OnLifecycleEvent(agent.LifecycleEvent{
			RunID:   params.ClientRunID,
			Phase:   phase,
			Aborted: aborted,
			Error:   err.Error(),
			Ts:      now,
		})
	}
}

// resolveAPIKey retrieves the Anthropic API key from the provider auth manager.
func resolveAPIKey(authManager *provider.AuthManager, logger *slog.Logger) (apiKey, baseURL string, err error) {
	if authManager == nil {
		return "", "", nil // Will use pre-configured client.
	}

	cred := authManager.Resolve("anthropic", "")
	if cred == nil {
		logger.Warn("no Anthropic credential found in auth manager")
		return "", "", nil
	}

	if cred.IsExpired() {
		logger.Warn("Anthropic credential is expired")
		return "", "", nil
	}

	base := cred.BaseURL
	if base == "" {
		base = llm.DefaultAnthropicBaseURL
	}
	return cred.APIKey, base, nil
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
