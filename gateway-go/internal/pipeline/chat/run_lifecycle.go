package chat

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
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
	}
	if params.Delivery != nil && result.Text != "" && deps.chatport.ParseReplyDirectives == nil {
		logger.Warn("parseReplyDirectives is nil, channel delivery skipped",
			"session", params.SessionKey,
			"channel", params.Delivery.Channel,
			"textLen", len(result.Text))
	}
	if params.Delivery != nil && result.Text != "" && deps.chatport.ParseReplyDirectives != nil {
		directives := deps.chatport.ParseReplyDirectives(result.Text, params.Delivery.MessageID, "")
		if directives.IsSilent {
			logger.Info("suppressing silent reply (NO_REPLY); streamed draft preserved")
			// Do not delete the draft: content already streamed to the user
			// stays visible. NO_REPLY after streaming means "stop editing",
			// not "retract what was shown".
		} else {
			replyText := jsonutil.StripThinkingTags(directives.Text)
			replyText = strings.TrimSpace(replyText)

			if replyText != "" {
				replyCtx, replyCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer replyCancel()
				if deps.callbacks.replyFunc == nil {
					logger.Warn("replyFunc is nil, response will not be delivered",
						"session", params.SessionKey,
						"channel", params.Delivery.Channel,
						"textLen", len(replyText))
				}
				if deps.callbacks.replyFunc != nil {
					// Primary path: channel-specific reply function (handles dedup,
					// formatting, chunking, etc.).
					if err := deps.callbacks.replyFunc(replyCtx, params.Delivery, replyText); err != nil {
						logger.Error("channel reply failed", "error", err, "channel", params.Delivery.Channel)
					}
				}
			}

			// Deliver MEDIA: tokens extracted by ParseReplyDirectives.
			// Each media URL is sent via mediaSendFn (photo/document/audio
			// auto-detected by the channel adapter). [[audio_as_voice]] tag
			// forces voice mode for audio files.
			if deps.callbacks.mediaSendFn != nil && len(directives.MediaURLs) > 0 {
				mediaCtx, mediaCancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer mediaCancel()
				for _, mediaURL := range directives.MediaURLs {
					mediaType := ""
					if directives.AudioAsVoice {
						mediaType = "voice"
					}
					if err := deps.callbacks.mediaSendFn(mediaCtx, params.Delivery, mediaURL, mediaType, "", false); err != nil {
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
