package chat

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/llmerr"
)

const externalDeliveryFailureNotice = "외부 채널 전송이 실패했습니다. 전달이 확인되지 않았습니다. 현재 채팅에 보인다고 가정하지 말고 채널 연결을 확인한 뒤 다시 시도해 주세요."

// persistReplyDeliveryFailure records a synthetic system note in the
// transcript when the primary channel reply callback fails permanently.
// Without this, the next turn's agent only sees its own (successful-looking)
// assistant output and will hallucinate reasons to the user ("channel was
// disconnected", "I already replied") when asked why they didn't see it.
// The note gives the agent ground truth to work with.
func persistReplyDeliveryFailure(deps runDeps, sessionKey, channel string, deliverErr error, logger *slog.Logger) {
	if deps.transcript == nil {
		return
	}
	text := "[SYSTEM: 직전 어시스턴트 응답은 " + channel + " 채널로 **전송이 확인되지 않았습니다**. " +
		"유저가 그 응답을 봤다고 가정하지 마세요. 유저가 이유를 물으면 상황을 모른다고 말하고, " +
		"채널 연결 같은 추측성 설명을 지어내지 마세요."
	if deliverErr != nil {
		text += " (원인 힌트: " + deliverErr.Error() + ")"
	}
	text += "]"
	msg := NewTextChatMessage("user", text, time.Now().UnixMilli())
	if err := deps.transcript.Append(sessionKey, msg); err != nil {
		logger.Warn("failed to persist delivery-failure note", "error", err)
	}
}

// persistMediaDeliveryFailure records a synthetic system note when one or
// more media attachments the agent sent failed to reach the channel.
// Without this, the agent believes the image/audio was delivered and may
// reference it in subsequent turns ("did you see the image I just sent?")
// even though the user only saw the text.
func persistMediaDeliveryFailure(deps runDeps, sessionKey, channel string, failedURLs []string, logger *slog.Logger) {
	if deps.transcript == nil || len(failedURLs) == 0 {
		return
	}
	text := "[SYSTEM: 직전 턴에서 시도한 미디어 첨부 " +
		strconv.Itoa(len(failedURLs)) + "건이 " + channel + " 채널로 **전달되지 않았습니다**. " +
		"유저는 그 미디어를 보지 못했습니다. 다음 턴에서 '아까 보낸 이미지/오디오'라고 언급하지 마세요."
	text += " 실패한 URL: " + strings.Join(failedURLs, ", ") + "]"
	msg := NewTextChatMessage("user", text, time.Now().UnixMilli())
	if err := deps.transcript.Append(sessionKey, msg); err != nil {
		logger.Warn("failed to persist media-failure note", "error", err)
	}
}

// fallbackForStopReason returns a user-visible Korean message for abnormal
// terminations where the agent produced no text output. Empty string means
// no fallback needed (e.g., end_turn is a normal termination — tool-only
// turns legitimately produce no text and the caller already logged it).
func fallbackForStopReason(stopReason string) string {
	switch stopReason {
	case "max_turns":
		return "응답 생성이 반복 한도에 도달해 마무리되지 않았어요. 다시 한 번 말해 주세요 — 더 짧게 끊어서 요청하면 잘 끝납니다."
	case "max_turns_graceful":
		// The grace call iteration normally produces a wrap-up text, so this
		// fallback is only reached when even that turn yielded no output.
		return "응답이 턴 예산 한도에 도달했지만 마무리 답변을 받지 못했어요. 다시 요청해 주세요."
	case "timeout":
		return "응답 생성이 시간 초과로 중단됐어요. 잠시 후에 다시 시도해 주세요."
	case "aborted":
		return "작업이 중단됐어요. 이어서 진행하려면 다시 요청해 주세요."
	case "error":
		return "응답 생성 중 오류가 발생했어요. 다시 시도해 주세요."
	case stopReasonCompressionStuck:
		// Mid-loop compaction can no longer reduce context (either the
		// protected head+tail already exceed budget, or two consecutive
		// retries produced byte-identical inputs). The session is
		// unrecoverable without starting fresh.
		return compactionKoreanError
	default:
		// end_turn, empty, or unknown — empty text is legitimate (tool-only turn).
		return ""
	}
}

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

func hasFailedExternalDeliveryTool(toolActivities []agent.ToolActivity) bool {
	for _, ta := range toolActivities {
		if !ta.IsError {
			continue
		}
		if ta.Name == "message" || ta.Name == "send_file" {
			return true
		}
	}
	return false
}

func shouldForceExternalDeliveryFailureNotice(delivery *DeliveryContext, toolActivities []agent.ToolActivity, text string, isSilent bool) bool {
	if delivery == nil || !hasFailedExternalDeliveryTool(toolActivities) {
		return false
	}
	if isSilent {
		return true
	}
	return strings.TrimSpace(text) == ""
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
	if shouldForceExternalDeliveryFailureNotice(params.Delivery, result.ToolActivities, result.Text, isSilent) {
		result.Text = externalDeliveryFailureNotice
		if strings.TrimSpace(result.AllText) == "" {
			result.AllText = result.Text
		}
		isSilent = false
		logger.Warn("forcing explicit reply after external delivery failure",
			"session", params.SessionKey,
			"channel", params.Delivery.Channel)
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
			if deps.callbacks.emitTranscriptFn != nil {
				deps.callbacks.emitTranscriptFn(params.SessionKey, assistantMsg, "")
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

		// Abnormal stop with empty output — tell the user something went
		// wrong instead of leaving them staring at silence. end_turn with
		// empty text can happen legitimately (tool-only turns) so we only
		// surface for limit/error-like terminations.
		if fallbackMsg := fallbackForStopReason(result.StopReason); fallbackMsg != "" && deps.callbacks.replyFunc != nil {
			replyCtx, replyCancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := deps.callbacks.replyFunc(replyCtx, params.Delivery, fallbackMsg); err != nil {
				logger.Error("fallback delivery failed",
					"error", err, "stopReason", result.StopReason,
					"session", params.SessionKey)
				if deps.broadcast != nil {
					deps.broadcast("chat.delivery_failed", map[string]any{
						"session": params.SessionKey,
						"channel": params.Delivery.Channel,
						"reason":  "stop_fallback_error",
						"error":   err.Error(),
					})
				}
			}
			replyCancel()
		}
	}
	if params.Delivery != nil && result.Text != "" && deps.chatport.ParseReplyDirectives == nil {
		logger.Warn("parseReplyDirectives is nil, channel delivery skipped",
			"session", params.SessionKey,
			"channel", params.Delivery.Channel,
			"textLen", len(result.Text))
	}
	if params.Delivery != nil && result.Text != "" && deps.chatport.ParseReplyDirectives != nil {
		directives := deps.chatport.ParseReplyDirectives(result.Text, params.Delivery.MessageID, "")
		// IsSilent suppresses TEXT delivery only. Media tokens represent
		// explicit agent intent ("send this image/audio") and are delivered
		// regardless — otherwise an agent reply of "NO_REPLY [[media:url]]"
		// would silently drop the media the agent clearly wanted to send.
		if !directives.IsSilent {
			replyText := jsonutil.StripThinkingTags(directives.Text)
			replyText = strings.TrimSpace(replyText)

			if replyText != "" {
				replyCtx, replyCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer replyCancel()
				if deps.callbacks.replyFunc == nil {
					// Wiring bug: reply callback not registered. Escalate so
					// operators notice — silent Warn was hiding broken deploys.
					logger.Error("replyFunc is nil — response not delivered (wiring bug)",
						"session", params.SessionKey,
						"channel", params.Delivery.Channel,
						"textLen", len(replyText))
					if deps.broadcast != nil {
						deps.broadcast("chat.delivery_failed", map[string]any{
							"session": params.SessionKey,
							"channel": params.Delivery.Channel,
							"reason":  "reply_func_nil",
						})
					}
					persistReplyDeliveryFailure(deps, params.SessionKey, params.Delivery.Channel, nil, logger)
				} else {
					// Primary path: channel-specific reply function (handles dedup,
					// formatting, chunking, etc.). Retry once on transient errors
					// so flaky networks don't silently drop the reply.
					err := deps.callbacks.replyFunc(replyCtx, params.Delivery, replyText)
					if err != nil {
						logger.Warn("channel reply failed, retrying once",
							"error", err, "channel", params.Delivery.Channel)
						time.Sleep(500 * time.Millisecond)
						err = deps.callbacks.replyFunc(replyCtx, params.Delivery, replyText)
					}
					if err != nil {
						logger.Error("channel reply failed after retry",
							"error", err, "channel", params.Delivery.Channel,
							"session", params.SessionKey)
						if deps.broadcast != nil {
							deps.broadcast("chat.delivery_failed", map[string]any{
								"session": params.SessionKey,
								"channel": params.Delivery.Channel,
								"reason":  "reply_func_error",
								"error":   err.Error(),
							})
						}
						// Record the failure in the transcript so the next turn's
						// agent has ground truth instead of inventing reasons.
						persistReplyDeliveryFailure(deps, params.SessionKey, params.Delivery.Channel, err, logger)
					}
				}
			}

		} else {
			logger.Info("suppressing silent reply (NO_REPLY); streamed draft preserved",
				"hasMedia", len(directives.MediaURLs) > 0)
			// Do not delete the draft: content already streamed to the user
			// stays visible. NO_REPLY after streaming means "stop editing",
			// not "retract what was shown".
		}

		// Deliver MEDIA: tokens extracted by ParseReplyDirectives. Always run
		// whenever media tokens are present — including when IsSilent is true —
		// because the agent explicitly included them as output intent.
		// [[audio_as_voice]] tag forces voice mode for audio files.
		if deps.callbacks.mediaSendFn != nil && len(directives.MediaURLs) > 0 {
			mediaCtx, mediaCancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer mediaCancel()
			var failedURLs []string
			for _, mediaURL := range directives.MediaURLs {
				mediaType := ""
				if directives.AudioAsVoice {
					mediaType = "voice"
				}
				if err := deps.callbacks.mediaSendFn(mediaCtx, params.Delivery, mediaURL, mediaType, "", false); err != nil {
					logger.Warn("media delivery failed", "url", mediaURL, "error", err)
					failedURLs = append(failedURLs, mediaURL)
				}
			}
			if len(failedURLs) > 0 {
				if deps.broadcast != nil {
					deps.broadcast("chat.media_delivery_failed", map[string]any{
						"session": params.SessionKey,
						"channel": params.Delivery.Channel,
						"count":   len(failedURLs),
						"total":   len(directives.MediaURLs),
						"urls":    failedURLs,
					})
				}
				persistMediaDeliveryFailure(deps, params.SessionKey, params.Delivery.Channel, failedURLs, logger)
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

	// Diary recording: append raw conversation turn to today's diary.
	// Wiki page curation is handled by the main LLM via system prompt.
	if deps.wikiStore != nil && params.Message != "" {
		toolNames := make([]string, 0, len(result.ToolActivities))
		for _, ta := range result.ToolActivities {
			toolNames = append(toolNames, ta.Name)
		}
		go recordDiary(deps.wikiStore, logger, params.Message, toolNames)
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
	broadcaster *streaming.Broadcaster,
	logger *slog.Logger,
	err error,
	now int64,
	runLog *agentlog.RunLogger,
) {
	// Do not delete the draft streaming message on error: any content already
	// streamed to the user should remain visible instead of vanishing. The
	// partial draft is preferable to a blank chat with no explanation.

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
		finishRun(deps, params, session.PhaseError, "error", "failed", classifyRunFailureReason(err), now)
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
//
// Classification delegates to pkg/llmerr so the surfaced Korean label shares
// one taxonomy with isContextOverflow, isTransientLLMError, and the autoreply
// runner. classifyLLMError lifts *httpretry.APIError status/body into the
// structured pipeline so a wrapped "API error 429: ..." is matched by status,
// not a bare digit substring.
//
// Behaviour deltas vs. the prior substring classifier:
//   - Adds coverage for HTTP 402 (billing) and 413 (payload too large →
//     context overflow family) via structured status classification.
//   - Adds coverage for structured provider codes (insufficient_quota,
//     context_length_exceeded, invalid_api_key, resource_exhausted, …).
//   - Legacy "529" → 서버 일시 장애 is preserved via ReasonOverloaded.
//   - Legacy "521" (Cloudflare web-server-down) has no direct llmerr status
//     bucket, so the bare-digit fallback below keeps it mapped to
//     서버 일시 장애.
//   - A free-form "unauthorized" string with no HTTP status now still maps
//     to 인증 실패 via the message-pattern pipeline (authPatterns).
//
// The final bare-digit fallback preserves behavior for raw strings that
// mention "429"/"401"/"502"/"503"/"521"/"529" without any structured status,
// exactly as the pre-migration implementation did.
func classifyRunFailureReason(err error) string {
	if err == nil {
		return ""
	}
	label := llmerrToFailureReason(classifyLLMError(err))
	if label != "" {
		return label
	}
	// Preserve the legacy bare-digit + keyword fallback so plain-string
	// errors with an embedded HTTP status or the loose "billing"/"payment"
	// keywords still produce a user-facing label. llmerr deliberately
	// avoids matching these because bare digits and unqualified "billing"
	// can produce false positives on structured inputs; here the risk is
	// bounded because any structured input has already been consumed by
	// the llmerr pipeline above.
	errMsg := err.Error()
	lower := strings.ToLower(errMsg)
	switch {
	case strings.Contains(errMsg, "429"):
		return "API 요청 한도 초과 (429)"
	case strings.Contains(errMsg, "401"):
		return "API 인증 실패 (401)"
	case strings.Contains(lower, "billing"),
		strings.Contains(lower, "payment"):
		return "결제 오류"
	case strings.Contains(errMsg, "502"),
		strings.Contains(errMsg, "503"),
		strings.Contains(errMsg, "521"),
		strings.Contains(errMsg, "529"):
		return "서버 일시 장애"
	}
	return ""
}

// llmerrToFailureReason maps a Classified result to the legacy Korean label
// set used by Session.FailureReason. Returns "" for reasons that the prior
// implementation would not have labelled (keeps surface area identical for
// reasons the caller never displayed, avoiding accidental new messages in
// the UI for unmigrated edge cases).
func llmerrToFailureReason(c llmerr.Classified) string {
	switch c.Reason {
	case llmerr.ReasonRateLimit, llmerr.ReasonLongContextTier:
		return "API 요청 한도 초과 (429)"
	case llmerr.ReasonAuth, llmerr.ReasonAuthPermanent:
		return "API 인증 실패 (401)"
	case llmerr.ReasonBilling:
		return "결제 오류"
	case llmerr.ReasonServerError, llmerr.ReasonOverloaded:
		return "서버 일시 장애"
	case llmerr.ReasonContextOverflow, llmerr.ReasonPayloadTooLarge:
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
