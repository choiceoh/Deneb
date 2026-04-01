package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/reply"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// memoryExtractSem limits concurrent memory extraction goroutines to prevent
// overloading the local sglang model during rapid successive messages (e.g.,
// Telegram message bursts). At most 2 extractions run concurrently.
var memoryExtractSem = make(chan struct{}, 2)

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
	sb.WriteString("[System: the previous assistant turn was interrupted by the user while executing tools: ")
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
	sb.WriteString(" Continue or adjust based on the user's new message.]")

	msg := ChatMessage{
		Role:      "user",
		Content:   sb.String(),
		Timestamp: time.Now().UnixMilli(),
	}
	if err := deps.transcript.Append(sessionKey, msg); err != nil {
		logger.Warn("failed to persist interrupted context", "error", err)
	} else {
		logger.Info("persisted interrupted context",
			"tools", result.InterruptedToolNames,
			"turns", result.Turns)
	}

	// Sync to Aurora store for compaction awareness.
	if deps.auroraStore != nil {
		tokenCount := uint64(estimateTokens(sb.String()))
		if _, err := deps.auroraStore.SyncMessage(1, "user", sb.String(), tokenCount); err != nil {
			logger.Warn("aurora: failed to sync interrupted context", "error", err)
		}
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
		if deps.emitTranscriptFn != nil {
			deps.emitTranscriptFn(params.SessionKey, assistantMsg, "")
		}
	}
	// Sync Aurora summaries for channel replies when available.
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
	// Use reply.ParseReplyDirectives for unified processing: silent token
	// detection, leaked tool-call stripping, MEDIA: extraction, and threading.
	if params.Delivery != nil && result.Text != "" {
		directives := reply.ParseReplyDirectives(result.Text, params.Delivery.MessageID, "")
		if directives.IsSilent {
			logger.Info("suppressing silent reply (NO_REPLY)")
			// Clean up draft streaming message when reply is suppressed.
			cleanupDraftMessage(ctx, params.Delivery, deps, logger)
		} else {
			replyText := jsonutil.StripThinkingTags(directives.Text)
			replyText = strings.TrimSpace(replyText)

			// Use reply-to ID from directives ([[reply_to_current]],
			// [[reply_to:<id>]]) when available; fall back to the
			// triggering message ID for thread continuity.
			replyToID := directives.ReplyToID
			if replyToID == "" {
				replyToID = params.Delivery.MessageID
			}

			if replyText != "" {
				// Plugin hook: allow plugins to mutate or cancel the outbound message.
				if deps.pluginHookRunner != nil {
					msResult := deps.pluginHookRunner.RunMessageSending(ctx, map[string]any{
						"to":         params.Delivery.To,
						"content":    replyText,
						"channel":    params.Delivery.Channel,
						"sessionKey": params.SessionKey,
					})
					if msResult != nil {
						if msResult.Cancel {
							logger.Info("message delivery cancelled by plugin hook")
							replyText = ""
						} else if msResult.ModifiedText != "" {
							replyText = msResult.ModifiedText
						}
					}
				}
			}
			if replyText != "" {
				replyCtx, replyCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer replyCancel()
				if deps.replyFunc != nil {
					// Primary path: channel-specific reply function (handles dedup,
					// formatting, chunking, etc.).
					if err := deps.replyFunc(replyCtx, params.Delivery, replyText); err != nil {
						logger.Error("channel reply failed", "error", err, "channel", params.Delivery.Channel)
					} else if deps.hookRegistry != nil {
						// Fire message.send hook after successful delivery.
						go deps.hookRegistry.Fire(deps.shutdownCtx, hooks.EventMessageSend, map[string]string{
							"DENEB_CHANNEL":     params.Delivery.Channel,
							"DENEB_TO":          params.Delivery.To,
							"DENEB_SESSION_KEY": params.SessionKey,
						})
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

	// Store last output on the session so cron and other consumers can read it.
	if result.Text != "" {
		if sess := deps.sessions.Get(params.SessionKey); sess != nil {
			sess.LastOutput = result.Text
		}
	}

	finishRun(deps, params, session.PhaseEnd, "completed", "done", "", now)
	emitJobEvent(deps, params.ClientRunID, "end", false, "", now)

	// Auto-memory: extract key learnings asynchronously via local sglang.
	// When structured memory store is available, use Honcho-style importance extraction.
	// Falls back to legacy MEMORY.md append otherwise.
	//
	// Dream turn is incremented on every successful run with a user message,
	// regardless of whether the response is empty or memory extraction succeeds.
	// This ensures dreaming triggers reliably even for tool-only or silent runs.
	//
	// Execute auto-memory extraction/dreaming for successful runs with user input.
	if params.Message != "" {
		go func() {
			// Limit concurrent extractions to avoid overloading sglang.
			select {
			case memoryExtractSem <- struct{}{}:
				defer func() { <-memoryExtractSem }()
			default:
				logger.Debug("memory extraction skipped: semaphore full")
				return
			}

			// Bound by the server shutdown context (if set) so the goroutine
			// exits when the process is shutting down rather than leaking until
			// autoMemoryTimeout fires against a dead process.
			base := deps.shutdownCtx
			if base == nil {
				base = context.Background()
			}
			memCtx, memCancel := context.WithTimeout(base, autoMemoryTimeout)
			defer memCancel()

			if deps.memoryStore != nil {
				// Structured extraction: extract facts with importance scoring.
				// Skip tool-only responses (file contents relay, command output)
				// that rarely contain user-model-worthy information.
				if result.Text != "" && !isToolOnlyResponse(result.Text) {
					if !checkSglangHealth() {
						logger.Debug("structured memory extraction skipped: sglang unhealthy")
					} else {
						lwClient := getLightweightClient()
						facts, err := memory.ExtractFacts(memCtx, lwClient, getLightweightModel(), params.Message, result.Text, logger)
						if err != nil {
							if shouldLogStructuredMemoryExtractionError(err) {
								logger.Debug("structured memory extraction failed, falling back", "error", err)
							}
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
					}
				}

				// Increment dream turn on every run (not just when response is non-empty).
				if deps.dreamTurnFn != nil {
					deps.dreamTurnFn(memCtx)
				}
			} else if result.Text != "" {
				// Legacy: append bullet points to MEMORY.md.
				notes := extractAutoMemory(memCtx, params.Message, result.Text, logger)
				if notes != "" {
					workspaceDir := resolveWorkspaceDirForPrompt()
					appendToMemoryFile(workspaceDir, notes, logger)
				}
			}

			// Session memory: update structured session state.
			// Runs alongside fact extraction, sharing the same semaphore.
			if deps.sessionMemory != nil {
				UpdateSessionMemory(memCtx, deps.sessionMemory, params.SessionKey,
					params.Message, result.Text, result.Turns, result.StopReason, logger)
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
// Returns the client and API type ("anthropic" or "openai").
func resolveClient(deps runDeps, providerID string, logger *slog.Logger) (*llm.Client, string) {
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
				apiType := cfg.API
				if apiType == "" {
					apiType = inferAPIType(providerID)
				}
				client := llm.NewClient(baseURL, apiKey, llm.WithLogger(logger))
				logger.Info("using provider from config", "provider", providerID, "apiType", apiType)
				return client, apiType
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
			apiType := inferAPIType(target)
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

			return llm.NewClient(base, apiKey, llm.WithLogger(logger)), apiType
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
) (*agent.AgentResult, error) {
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
	case "anthropic":
		return llm.DefaultAnthropicBaseURL
	case "zai":
		return defaultZaiBaseURL
	case "google":
		return "https://generativelanguage.googleapis.com/v1beta/openai"
	case "sglang":
		return modelrole.DefaultSglangBaseURL
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

// memoryContextOpts returns LoadContextOptions based on whether the structured
// memory store is active. When active, MEMORY.md is skipped from context files
// because PrefetchKnowledge already provides the same information with
// importance-weighted scoring via the structured memory store.
func memoryContextOpts(deps runDeps) []prompt.LoadContextOption {
	if deps.memoryStore != nil {
		return []prompt.LoadContextOption{prompt.WithSkipMemory()}
	}
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

// toPromptToolDefs converts chat.ToolDef slice to the minimal prompt.ToolDef
// slice needed for system prompt assembly. Only the Name field is required
// by the prompt package; the full ToolDef (with Fn, Schema, etc.) stays in chat/.
func toPromptToolDefs(defs []ToolDef) []prompt.ToolDef {
	out := make([]prompt.ToolDef, len(defs))
	for i, d := range defs {
		out[i] = prompt.ToolDef{Name: d.Name}
	}
	return out
}

