package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/rlm"
	"github.com/choiceoh/deneb/gateway-go/internal/rlm/repl"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// persistInterruptedContext saves a context note to the transcript when a run
// is aborted while tools were executing. This ensures the next run has context
// about what was being done, preventing the "amnesia" bug where the assistant
// forgets its in-progress work when the user sends a message mid-execution.
func persistInterruptedContext(deps runDeps, sessionKey string, result *agent.AgentResult, logger *slog.Logger) {
	if deps.transcript == nil || len(result.InterruptedToolNames) == 0 {
		return
	}

	// Build a concise note listing the tools that were running and any
	// partial text the assistant had produced before interruption.
	var sb strings.Builder
	sb.WriteString("**System:** the previous assistant turn was interrupted by the user while executing tools: ")
	for i, name := range result.InterruptedToolNames {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(name)
	}
	sb.WriteString(".")
	if result.Text != "" {
		sb.WriteString(" Partial response before interruption: ")
		// Truncate to avoid bloating the transcript.
		partial := result.Text
		if len(partial) > 500 {
			partial = partial[:500] + "..."
		}
		sb.WriteString(partial)
	}
	sb.WriteString(" Continue or adjust based on the user's new message.")

	msg := NewTextChatMessage("user", sb.String(), time.Now().UnixMilli())
	if err := deps.transcript.Append(sessionKey, msg); err != nil {
		logger.Warn("failed to persist interrupted context", "error", err)
	} else {
		logger.Info("persisted interrupted context",
			"tools", result.InterruptedToolNames,
			"turns", result.Turns)
	}

}

// cleanupDraftMessage deletes the draft streaming message from Telegram when
// the reply is suppressed (silent), empty, or on error. This prevents orphaned
// draft messages from lingering in the chat.
func cleanupDraftMessage(ctx context.Context, delivery *DeliveryContext, deps runDeps, logger *slog.Logger) {
	if delivery == nil || delivery.DraftMsgID == "" || deps.draftDeleteFn == nil {
		return
	}
	msgID := delivery.DraftMsgID
	delivery.DraftMsgID = "" // consumed
	delCtx, delCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer delCancel()
	if err := deps.draftDeleteFn(delCtx, delivery, msgID); err != nil {
		logger.Warn("draft stream cleanup failed", "msgId", msgID, "error", err)
	}
}

// handleRunSuccess processes a successful agent run completion.
func handleRunSuccess(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	broadcaster *streaming.Broadcaster,
	logger *slog.Logger,
	result *agent.AgentResult,
	now int64,
	runLog *agentlog.RunLogger,
) {
	// Log run completion to agent detail log.
	runLog.LogEnd(agentlog.RunEndData{
		StopReason:   result.StopReason,
		Turns:        result.Turns,
		InputTokens:  result.Usage.InputTokens,
		OutputTokens: result.Usage.OutputTokens,
		TextLen:      len(result.Text),
	})
	// Strip silent reply token (NO_REPLY) from the response text before
	// persisting, broadcasting, or delivering. This ensures the internal
	// token is never exposed to any client (RPC, WebSocket, Telegram) and
	// is not stored in transcript history.
	isSilent := IsSilentReply(result.Text)
	if !isSilent {
		stripped := StripSilentToken(result.Text)
		if stripped == "" && result.Text != "" {
			isSilent = true
		} else {
			result.Text = stripped
		}
	}
	// Channel-silent tools: when the agent used a management tool (e.g. cron)
	// on a channel that marks it as silent, suppress chat delivery. The tool
	// executed normally — only the chat output is suppressed.
	if !isSilent && params.Delivery != nil {
		if shouldSilenceForChannel(params.Delivery.Channel, result.ToolActivities) {
			isSilent = true
			logger.Info("suppressing delivery for channel-silent tool",
				"channel", params.Delivery.Channel)
		}
	}

	if isSilent {
		result.Text = ""
		logger.Info("suppressing silent reply (NO_REPLY)")
	}

	// Persist assistant message to transcript + Aurora store.
	// When tool activities were recorded, prepend a compact summary so the
	// next context assembly includes what the agent actually did — not just
	// what it said. This fixes the "amnesia" bug where the agent forgets
	// its own tool work after a few turns.
	// When per-turn persistence was active (TurnsPersisted > 0), each
	// assistant and tool_result message was already written to transcript
	// during the agent loop. Skip the aggregate write to avoid duplicates.
	if result.TurnsPersisted == 0 {
		// Legacy path: persist accumulated text as a single assistant message.
		persistText := result.AllText
		if persistText == "" {
			persistText = result.Text
		}
		toolSummary := formatToolActivitySummary(result.ToolActivities)
		if toolSummary != "" && persistText != "" {
			persistText = toolSummary + "\n\n" + persistText
		}

		if deps.transcript != nil && persistText != "" {
			assistantMsg := NewTextChatMessage("assistant", persistText, now)
			if err := deps.transcript.Append(params.SessionKey, assistantMsg); err != nil {
				logger.Error("failed to persist assistant message", "error", err)
			}
			if deps.emitTranscriptFn != nil {
				deps.emitTranscriptFn(params.SessionKey, assistantMsg, "")
			}
		}
		// Sync Aurora summaries for channel replies when available.
	}

	if broadcaster != nil {
		broadcaster.EmitComplete(result.Text, result.Usage)
	}

	// Deliver response back to the originating channel (e.g., Telegram).
	// Use parseReplyDirectives (chatport boundary) for unified processing: silent token
	// detection, leaked tool-call stripping, MEDIA: extraction, and threading.
	if params.Delivery != nil && result.Text == "" && !isSilent {
		logger.Warn("agent produced empty response, nothing to deliver",
			"session", params.SessionKey,
			"channel", params.Delivery.Channel,
			"turns", result.Turns,
			"stopReason", result.StopReason,
			"inputTokens", result.Usage.InputTokens,
			"outputTokens", result.Usage.OutputTokens)
	}
	if params.Delivery != nil && result.Text != "" && deps.parseReplyDirectives == nil {
		logger.Warn("parseReplyDirectives is nil, channel delivery skipped",
			"session", params.SessionKey,
			"channel", params.Delivery.Channel,
			"textLen", len(result.Text))
	}
	if params.Delivery != nil && result.Text != "" && deps.parseReplyDirectives != nil {
		directives := deps.parseReplyDirectives(result.Text, params.Delivery.MessageID, "")
		if directives.IsSilent {
			logger.Info("suppressing silent reply (NO_REPLY)")
			// Clean up draft streaming message when reply is suppressed.
			cleanupDraftMessage(ctx, params.Delivery, deps, logger)
		} else {
			replyText := jsonutil.StripThinkingTags(directives.Text)
			replyText = strings.TrimSpace(replyText)

			if replyText != "" {
				replyCtx, replyCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer replyCancel()
				if deps.replyFunc == nil {
					logger.Warn("replyFunc is nil, response will not be delivered",
						"session", params.SessionKey,
						"channel", params.Delivery.Channel,
						"textLen", len(replyText))
				}
				if deps.replyFunc != nil {
					// Primary path: channel-specific reply function (handles dedup,
					// formatting, chunking, etc.).
					if err := deps.replyFunc(replyCtx, params.Delivery, replyText); err != nil {
						logger.Error("channel reply failed", "error", err, "channel", params.Delivery.Channel)
					} else {
						// Fire message.send internal hook after successful delivery.
						if deps.internalHookRegistry != nil {
							env := map[string]string{
								"DENEB_CHANNEL":     params.Delivery.Channel,
								"DENEB_TO":          params.Delivery.To,
								"DENEB_SESSION_KEY": params.SessionKey,
							}
							go deps.internalHookRegistry.TriggerFromEvent(deps.shutdownCtx, hooks.EventMessageSend, params.SessionKey, env)
						}
					}
				}
			}

			// Deliver MEDIA: tokens extracted by ParseReplyDirectives.
			// Each media URL is sent via mediaSendFn (photo/document/audio
			// auto-detected by the channel adapter). [[audio_as_voice]] tag
			// forces voice mode for audio files.
			if deps.mediaSendFn != nil && len(directives.MediaURLs) > 0 {
				mediaCtx, mediaCancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer mediaCancel()
				for _, mediaURL := range directives.MediaURLs {
					mediaType := ""
					if directives.AudioAsVoice {
						mediaType = "voice"
					}
					if err := deps.mediaSendFn(mediaCtx, params.Delivery, mediaURL, mediaType, "", false); err != nil {
						logger.Warn("media delivery failed", "url", mediaURL, "error", err)
					}
				}
			}
		}
	}

	// Store last output on the session so cron, subagent notifications, and
	// other consumers can read it. Prefer AllText (accumulated across all turns)
	// over Text (last turn only) — sub-agents often produce output in early turns
	// and finish with a tool-only turn, leaving Text empty.
	lastOutput := result.AllText
	if lastOutput == "" {
		lastOutput = result.Text
	}
	if lastOutput != "" {
		if sess := deps.sessions.Get(params.SessionKey); sess != nil {
			sess.LastOutput = lastOutput
		}
	}

	finishRun(deps, params, session.PhaseEnd, "completed", "done", "", now)
	emitJobEvent(deps, params.ClientRunID, "end", false, "", now)

	// Dream turn increment for successful runs with user input.
	// Auto-memory and session memory removed — RLM wiki handles long-term
	// memory, RLM compaction handles session context preservation.

	logger.Info("agent run completed",
		"stopReason", result.StopReason,
		"turns", result.Turns,
		"inputTokens", result.Usage.InputTokens,
		"outputTokens", result.Usage.OutputTokens,
	)
}

func shouldLogStructuredMemoryExtractionError(err error) bool {
	if err == nil {
		return false
	}

	// Context timeouts/cancelation in best-effort auto-memory are expected under load
	// or shutdown and should not spam logs.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}

	return true
}

// handleRunError processes a failed or aborted agent run.
func handleRunError(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	broadcaster *streaming.Broadcaster,
	logger *slog.Logger,
	err error,
	now int64,
	runLog *agentlog.RunLogger,
) {
	// Clean up draft streaming message on error so it doesn't linger.
	cleanupDraftMessage(ctx, params.Delivery, deps, logger)

	aborted := ctx.Err() != nil

	// Log run error to agent detail log.
	runLog.LogError(agentlog.RunErrorData{
		Error:   err.Error(),
		Aborted: aborted,
	})

	if aborted {
		logger.Info("agent run aborted", "error", err)
		if broadcaster != nil {
			broadcaster.EmitAborted("")
		}
		finishRun(deps, params, session.PhaseEnd, "aborted", "killed", "", now)
		emitJobEvent(deps, params.ClientRunID, "end", true, err.Error(), now)
	} else {
		logger.Error("agent run failed", "error", err)
		if broadcaster != nil {
			broadcaster.EmitError(err.Error())
		}
		finishRun(deps, params, session.PhaseError, "error", "failed", classifyRunFailureReason(err.Error()), now)
		emitJobEvent(deps, params.ClientRunID, "error", false, err.Error(), now)
	}
}

// finishRun transitions the session out of running and broadcasts the change.
// failureReason is a human-readable Korean description of why the run failed;
// pass "" for non-error completions.
func finishRun(deps runDeps, params RunParams, phase session.LifecyclePhase, reason, status, failureReason string, ts int64) {
	deps.sessions.ApplyLifecycleEvent(params.SessionKey, session.LifecycleEvent{
		Phase:         phase,
		Ts:            ts,
		FailureReason: failureReason,
	})
	if deps.broadcast != nil {
		deps.broadcast("sessions.changed", map[string]any{
			"sessionKey": params.SessionKey,
			"reason":     reason,
			"status":     status,
		})
	}
	// Clean up spillover files for completed/failed sessions.
	if deps.tools != nil {
		if ss := deps.tools.SpilloverStore(); ss != nil {
			ss.CleanSession(params.SessionKey)
		}
	}
}

// classifyRunFailureReason returns a Korean-language description of a run error
// for storage in Session.FailureReason. Returns "" for unrecognized errors.
func classifyRunFailureReason(errMsg string) string {
	lower := strings.ToLower(errMsg)
	switch {
	case strings.Contains(errMsg, "429"):
		return "API 요청 한도 초과 (429)"
	case strings.Contains(errMsg, "401") ||
		strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "invalid_api_key") ||
		strings.Contains(lower, "authentication_error"):
		return "API 인증 실패 (401)"
	case strings.Contains(lower, "billing") ||
		strings.Contains(lower, "payment") ||
		strings.Contains(lower, "insufficient_quota"):
		return "결제 오류"
	case strings.Contains(errMsg, "502") ||
		strings.Contains(errMsg, "503") ||
		strings.Contains(errMsg, "521") ||
		strings.Contains(errMsg, "529"):
		return "서버 일시 장애"
	case strings.Contains(lower, "context") &&
		(strings.Contains(lower, "overflow") || strings.Contains(lower, "too large") || strings.Contains(lower, "exceeded")):
		return "컨텍스트 초과"
	default:
		return ""
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
// provider runtime resolver, or falls back to the pre-configured client.
func resolveClient(deps runDeps, providerID string, logger *slog.Logger) *llm.Client {
	// 1. Try provider config from deneb.json.
	if deps.providerConfigs != nil && providerID != "" {
		if cfg, ok := deps.providerConfigs[providerID]; ok {
			baseURL := strings.TrimSpace(provider.ExpandEnvVars(cfg.BaseURL))
			if baseURL == "" {
				baseURL = resolveDefaultBaseURL(providerID)
			}
			apiKey := strings.TrimSpace(provider.ExpandEnvVars(cfg.APIKey))

			// Apply provider runtime auth override (e.g., token exchange).
			if deps.providerRuntime != nil && providerID != "" {
				authResult, err := deps.providerRuntime.PrepareRuntimeAuth(
					context.Background(), providerID,
					provider.RuntimeAuthContext{
						Provider: providerID,
						APIKey:   apiKey,
					},
				)
				if err != nil {
					logger.Warn("provider runtime auth failed", "provider", providerID, "error", err)
				} else if authResult != nil {
					if authResult.APIKey != "" {
						apiKey = authResult.APIKey
					}
					if authResult.BaseURL != "" {
						baseURL = authResult.BaseURL
					}
				}
			}

			if baseURL == "" {
				logger.Warn("provider config missing base URL", "provider", providerID)
			} else {
				client := llm.NewClient(baseURL, apiKey, llm.WithLogger(logger))
				logger.Info("using provider from config", "provider", providerID)
				return client
			}
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
			apiKey := cred.APIKey
			if base == "" {
				base = resolveDefaultBaseURL(target)
			}

			// Apply provider runtime auth override on auth-manager credentials.
			if deps.providerRuntime != nil {
				authResult, err := deps.providerRuntime.PrepareRuntimeAuth(
					context.Background(), target,
					provider.RuntimeAuthContext{
						Provider: target,
						APIKey:   apiKey,
					},
				)
				if err != nil {
					logger.Warn("provider runtime auth failed", "provider", target, "error", err)
				} else if authResult != nil {
					if authResult.APIKey != "" {
						apiKey = authResult.APIKey
					}
					if authResult.BaseURL != "" {
						base = authResult.BaseURL
					}
				}
			}

			return llm.NewClient(base, apiKey, llm.WithLogger(logger))
		}
	}

	// 3. Try registry: the modelrole.Registry has cached clients for known
	// provider/role mappings (vllm, google, localai, etc.) with correct base
	// URLs and API keys. This covers model-switch scenarios (e.g., /model
	// vllm/gemma4) where providerConfigs and authManager have no entry.
	if deps.registry != nil && providerID != "" {
		for _, role := range []modelrole.Role{modelrole.RoleMain, modelrole.RoleLightweight, modelrole.RoleFallback} {
			cfg := deps.registry.Config(role)
			if cfg.ProviderID == providerID {
				if client := deps.registry.Client(role); client != nil {
					logger.Info("using provider from registry", "provider", providerID, "role", string(role))
					return client
				}
			}
		}
	}

	// 4. Fall back to pre-configured client.
	return deps.llmClient
}

// Default base URLs for known providers (used when config doesn't specify one).
const (
	// Z.ai Coding Plan global endpoint (OpenAI-compatible).
	// Matches ZAI_CODING_GLOBAL_BASE_URL in src/plugins/provider-model-definitions.ts.
	defaultZaiBaseURL = "https://api.z.ai/api/coding/paas/v4"
)

// executeAgentRunWithDelta is a variant of executeAgentRun that accepts a direct
// onDelta callback for streaming text to HTTP clients.
func executeAgentRunWithDelta(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	onDelta func(string),
	logger *slog.Logger,
) (*chatRunResult, error) {
	deltaRaw := streaming.BroadcastRawFunc(func(event string, data []byte) int {
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
	broadcaster := streaming.NewBroadcaster(deltaRaw, params.SessionKey, params.ClientRunID)
	runLog := agentlog.NewRunLogger(deps.agentLog, params.SessionKey, params.ClientRunID)
	return executeAgentRun(ctx, params, deps, broadcaster, nil, nil, logger, runLog)
}

// resolveDefaultBaseURL returns the default API base URL for a known provider
// when no explicit base URL is configured.
func resolveDefaultBaseURL(providerID string) string {
	switch providerID {
	case "zai", "zai-subagent":
		return defaultZaiBaseURL
	case "google":
		return "https://generativelanguage.googleapis.com/v1beta/openai"
	case "localai":
		return modelrole.DefaultLocalAIBaseURL
	case "vllm":
		return modelrole.DefaultVllmBaseURL
	default:
		return ""
	}
}

// hasImageAttachment returns true if any attachment is an image.
func hasImageAttachment(attachments []ChatAttachment) bool {
	for _, att := range attachments {
		if att.Type == "image" {
			return true
		}
	}
	return false
}

// buildAttachmentBlocks creates a multimodal content block array from text and
// attachments. Images with base64 Data get inline ImageSource blocks;
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

// memoryContextOpts returns LoadContextOptions for context file loading.
func memoryContextOpts(_ runDeps) []prompt.LoadContextOption {
	return nil
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

// formatToolActivitySummary builds a compact, context-friendly summary of tool
// invocations from an agent run. Returns "" when there are no activities.
//
// The output is a plain metadata line (no brackets) that lists each unique tool
// with its call count, e.g.:
//
//	Tools used: read_file ×3, edit ×2, exec ×1
//
// IMPORTANT: Do NOT use bracket syntax here — models (especially GLM) mimic
// bracketed patterns as text output instead of making structured tool calls.
//
// This is prepended to the assistant's text before persisting to the transcript
// and Aurora store, so subsequent context assemblies include what the agent
// actually did — not just what it said.
func formatToolActivitySummary(activities []agent.ToolActivity) string {
	if len(activities) == 0 {
		return ""
	}

	// Count occurrences preserving first-seen order.
	type entry struct {
		name  string
		count int
	}
	seen := make(map[string]int) // name -> index in ordered
	var ordered []entry
	for _, a := range activities {
		if idx, ok := seen[a.Name]; ok {
			ordered[idx].count++
		} else {
			seen[a.Name] = len(ordered)
			ordered = append(ordered, entry{name: a.Name, count: 1})
		}
	}

	var sb strings.Builder
	sb.WriteString("Tools used: ")
	for i, e := range ordered {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(e.name)
		if e.count > 1 {
			fmt.Fprintf(&sb, " ×%d", e.count)
		}
	}
	return sb.String()
}

// toPromptToolDefs converts chat.ToolDef slice to the minimal prompt.ToolDef
// slice needed for system prompt assembly. Deferred tools are excluded — they
// are listed separately via DeferredSummaries in SystemPromptParams.
func toPromptToolDefs(defs []ToolDef) []prompt.ToolDef {
	out := make([]prompt.ToolDef, 0, len(defs))
	for _, d := range defs {
		if d.Deferred {
			continue
		}
		out = append(out, prompt.ToolDef{Name: d.Name})
	}
	return out
}

// buildREPLEnv creates a Starlark REPL environment for the repl tool.
// Conversation history is injected as `context`, and llm_query() / llm_query_batch() /
// rlm_query() calls go through the sub-agent path.
func buildREPLEnv(
	ctx context.Context,
	messages []llm.Message,
	client agent.LLMStreamer,
	model string,
	deps runDeps,
	params RunParams,
) *repl.Env {
	// Convert LLM messages to REPL MessageEntry format.
	entries := messagesToREPLEntries(messages)
	system := rlm.BuildSubAgentSystem("")

	// llm_query: single sub-agent completion.
	queryFn := func(ctx context.Context, prompt string) (string, error) {
		return client.Complete(ctx, llm.ChatRequest{
			Model:     model,
			System:    system,
			Messages:  []llm.Message{llm.NewTextMessage("user", prompt)},
			MaxTokens: 4096,
		})
	}

	// llm_query_batch: parallel sub-agent completions.
	batchFn := func(ctx context.Context, prompts []string) ([]string, error) {
		results := make([]string, len(prompts))
		errs := make([]error, len(prompts))
		var wg sync.WaitGroup
		// Match rlm.maxBatchConcurrency (12) to avoid overloading inference.
		sem := make(chan struct{}, 12)
		for i, p := range prompts {
			wg.Add(1)
			go func(idx int, prompt string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				text, err := client.Complete(ctx, llm.ChatRequest{
					Model:     model,
					System:    system,
					Messages:  []llm.Message{llm.NewTextMessage("user", prompt)},
					MaxTokens: 4096,
				})
				results[idx] = text
				errs[idx] = err
			}(i, p)
		}
		wg.Wait()
		// Return first error encountered.
		for _, err := range errs {
			if err != nil {
				return nil, err
			}
		}
		return results, nil
	}

	// Wiki funcs: built once, shared by root and recursive sub-REPLs.
	var wikiFuncs *repl.WikiFuncs
	if deps.wikiStore != nil {
		wikiFuncs = buildWikiFuncs(deps.wikiStore)
	}

	// rlm_query: recursive RLM loop with its own REPL.
	rlmCfg := rlm.ConfigFromEnv()
	rlmQueryFn := func(ctx context.Context, prompt string, subContext []repl.MessageEntry) (string, error) {
		subCfg := repl.EnvConfig{
			Messages:   subContext,
			LLMQueryFn: queryFn,
			LLMBatchFn: batchFn,
			Wiki:       wikiFuncs,
		}
		subEnv := repl.NewEnv(ctx, subCfg)
		loopResult, err := rlm.RunLoop(ctx, rlm.LoopConfig{
			Client:          client,
			Model:           model,
			System:          system,
			MaxTokens:       4096,
			MaxIter:         rlmCfg.MaxIterations,
			MaxConsecErrors: rlmCfg.MaxConsecutiveErrors,
			FallbackEnabled: rlmCfg.FallbackEnabled,
			REPLEnv:         subEnv,
			Logger:          deps.logger,
		}, prompt)
		if err != nil {
			return "", err
		}
		return loopResult.FinalAnswer, nil
	}

	cfg := repl.EnvConfig{
		Messages:   entries,
		LLMQueryFn: queryFn,
		LLMBatchFn: batchFn,
		RLMQueryFn: rlmQueryFn,
		Wiki:       wikiFuncs,
	}

	return repl.NewEnv(ctx, cfg)
}

// messagesToREPLEntries converts LLM messages to REPL MessageEntry format.
// Extracts text content from content blocks for Starlark access.
func messagesToREPLEntries(messages []llm.Message) []repl.MessageEntry {
	entries := make([]repl.MessageEntry, 0, len(messages))
	for i, msg := range messages {
		content := extractTextContent(msg.Content)
		entries = append(entries, repl.MessageEntry{
			Seq:     i,
			Role:    msg.Role,
			Content: content,
		})
	}
	return entries
}

// extractTextContent pulls plain text from a message's content field.
// Handles both plain string and content block array formats.
func extractTextContent(raw json.RawMessage) string {
	// Try plain string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	// Try content blocks.
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}

	return string(raw)
}
