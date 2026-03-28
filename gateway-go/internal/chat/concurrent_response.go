package chat

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// concurrentResponseTimeout is the max wall time for a concurrent response run.
// Kept short since it's a single LLM call with no tools.
const concurrentResponseTimeout = 90 * time.Second

// runConcurrentResponse executes a parallel LLM call that shares the same
// identity, conversation history, and knowledge as the task core — but without
// tools. The task core continues running uninterrupted in the background.
//
// This is the same AI doing multitasking: same system prompt, same transcript,
// same memory — plus awareness of the ongoing background task via the progress
// tracker injected into the system prompt.
func runConcurrentResponse(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	progress *TaskProgress,
) {
	logger := deps.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With(
		"session", params.SessionKey,
		"runId", params.ClientRunID,
		"mode", "concurrent_response",
	)

	ctx, cancel := context.WithTimeout(ctx, concurrentResponseTimeout)
	defer cancel()

	// 1. Persist user message to transcript (shared with task core).
	if deps.transcript != nil && params.Message != "" {
		userMsg := ChatMessage{
			Role:      "user",
			Content:   params.Message,
			Timestamp: time.Now().UnixMilli(),
		}
		if err := deps.transcript.Append(params.SessionKey, userMsg); err != nil {
			logger.Error("concurrent response: failed to persist user message", "error", err)
		}
	}

	// 2. Assemble context from shared transcript (same as task core).
	var messages []llm.Message
	if deps.transcript != nil {
		result, err := assembleContext(deps.transcript, params.SessionKey, deps.contextCfg, logger)
		if err != nil {
			logger.Warn("concurrent response: context assembly failed", "error", err)
		} else {
			messages = result.Messages
		}
	}
	if len(messages) == 0 && params.Message != "" {
		messages = []llm.Message{llm.NewTextMessage("user", params.Message)}
	}

	// 3. Build the same system prompt as the task core.
	workspaceDir := resolveWorkspaceDirForPrompt()
	var systemPrompt json.RawMessage
	if deps.defaultSystem != "" {
		systemPrompt = llm.SystemString(deps.defaultSystem)
	}
	// Build full system prompt with tools listed (for identity consistency)
	// but no actual tools will be passed to the LLM call.
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
		systemPrompt = llm.SystemString(BuildSystemPrompt(spp))
	}

	// 4. Knowledge prefetch (same as task core — uses Vega/Memory).
	if params.Message != "" {
		kDeps := KnowledgeDeps{
			VegaBackend:    deps.vegaBackend,
			WorkspaceDir:   workspaceDir,
			MemoryStore:    deps.memoryStore,
			MemoryEmbedder: deps.memoryEmbedder,
		}
		if addition := PrefetchKnowledge(ctx, params.Message, kDeps); addition != "" {
			systemPrompt = llm.AppendSystemText(systemPrompt, addition)
		}
	}

	// 5. Inject background task progress — this is what makes it "aware"
	// of the ongoing work, so it can say "I'm currently working on X..."
	if progress != nil {
		systemPrompt = llm.AppendSystemText(systemPrompt, "\n"+progress.FormatContextBlock())
	}

	// 6. Resolve model and LLM client (same as task core).
	model := params.Model
	if model == "" {
		model = deps.defaultModel
	}
	if model == "" {
		model = defaultModel
	}
	providerID, modelName := parseModelID(model)
	model = modelName

	client, apiType := resolveClient(deps, providerID, logger)
	if client == nil {
		logger.Error("concurrent response: no LLM client available")
		return
	}

	// For Anthropic API: rebuild as ContentBlock array with cache hints.
	if apiType == "anthropic" && deps.tools != nil {
		tz, _ := loadCachedTimezone()
		spp := SystemPromptParams{
			WorkspaceDir: workspaceDir,
			ToolDefs:     deps.tools.Definitions(),
			UserTimezone: tz,
			ContextFiles: LoadContextFiles(workspaceDir),
			RuntimeInfo:  BuildDefaultRuntimeInfo(params.Model, deps.defaultModel),
			Channel:      deliveryChannel(params.Delivery),
		}
		systemPrompt = llm.SystemBlocks(BuildSystemPromptBlocks(spp))
		// Re-inject knowledge and task progress after block rebuild.
		if params.Message != "" {
			kDeps := KnowledgeDeps{
				VegaBackend:    deps.vegaBackend,
				WorkspaceDir:   workspaceDir,
				MemoryStore:    deps.memoryStore,
				MemoryEmbedder: deps.memoryEmbedder,
			}
			if addition := PrefetchKnowledge(ctx, params.Message, kDeps); addition != "" {
				systemPrompt = llm.AppendSystemText(systemPrompt, addition)
			}
		}
		if progress != nil {
			systemPrompt = llm.AppendSystemText(systemPrompt, "\n"+progress.FormatContextBlock())
		}
	}

	maxTokens := deps.maxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	// 7. Single LLM call — no tools, no agent loop.
	cfg := AgentConfig{
		MaxTurns:  1,
		Timeout:   concurrentResponseTimeout,
		Model:     model,
		System:    systemPrompt,
		Tools:     nil, // no tools — conversation only
		MaxTokens: maxTokens,
		APIType:   apiType,
	}

	result, err := RunAgent(ctx, cfg, messages, client, nil, StreamHooks{}, logger, nil)
	if err != nil {
		logger.Error("concurrent response: LLM call failed", "error", err)
		return
	}

	// 8. Persist assistant response to shared transcript.
	now := time.Now().UnixMilli()
	if deps.transcript != nil && result.Text != "" {
		assistantMsg := ChatMessage{
			Role:      "assistant",
			Content:   result.Text,
			Timestamp: now,
		}
		if err := deps.transcript.Append(params.SessionKey, assistantMsg); err != nil {
			logger.Error("concurrent response: failed to persist assistant message", "error", err)
		}
	}

	// 9. Deliver response to Telegram (same replyFunc as task core).
	if deps.replyFunc != nil && params.Delivery != nil && result.Text != "" {
		if !IsSilentReply(result.Text) {
			replyText := StripSilentToken(result.Text)
			if replyText != "" {
				replyCtx, replyCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer replyCancel()
				if err := deps.replyFunc(replyCtx, params.Delivery, replyText); err != nil {
					logger.Error("concurrent response: channel reply failed", "error", err)
				}
			}
		}
	}

	logger.Info("concurrent response completed",
		"inputTokens", result.Usage.InputTokens,
		"outputTokens", result.Usage.OutputTokens,
	)
}

