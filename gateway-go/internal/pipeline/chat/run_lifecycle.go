package chat

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/llmerr"
)

// handleRunSuccess processes a successful agent run completion.
// Agent-detail logging (run.end) happens inside executeAgentRun — shared with
// the sync entry paths — so this handler owns only delivery and persistence.
func handleRunSuccess(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	broadcaster *streaming.Broadcaster,
	logger *slog.Logger,
	result *agent.AgentResult,
	now int64,
) {
	// Strip silent reply token (NO_REPLY) from the response text before
	// persisting, broadcasting, or delivering. This ensures the internal
	// token is never exposed to any client (RPC, WebSocket, native client)
	// and is not stored in transcript history.
	isSilent := IsSilentReply(result.Text)
	if !isSilent {
		stripped := StripSilentToken(result.Text)
		if stripped == "" && result.Text != "" {
			isSilent = true
		} else {
			result.Text = stripped
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

	// Deliver response back to the originating channel (e.g., the native client).
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
					deps.broadcast("chat.delivery_failed", ChatDeliveryFailedEvent{
						Session: params.SessionKey,
						Channel: params.Delivery.Channel,
						Reason:  "stop_fallback_error",
						Error:   err.Error(),
					})
				}
			}
			replyCancel()
		} else if deps.broadcast != nil {
			// No user-facing fallback (e.g. end_turn with empty text, which can
			// be a legitimate tool-only turn). Still surface to monitoring so a
			// silent no-reply is observable instead of being buried in a Warn.
			deps.broadcast("chat.empty_response", ChatEmptyResponseEvent{
				Session:    params.SessionKey,
				Channel:    params.Delivery.Channel,
				StopReason: result.StopReason,
				Turns:      result.Turns,
			})
		}
	}
	if params.Delivery != nil && result.Text != "" && deps.chatport.ParseReplyDirectives == nil {
		// Wiring bug: with no directive parser the channel reply is silently
		// dropped — the same broken-deploy class as replyFunc==nil below, so it
		// escalates the same way (Error + broadcast + transcript note) instead of
		// a Warn operators never read.
		logger.Error("parseReplyDirectives is nil — response not delivered (wiring bug)",
			"session", params.SessionKey,
			"channel", params.Delivery.Channel,
			"textLen", len(result.Text))
		if deps.broadcast != nil {
			deps.broadcast("chat.delivery_failed", ChatDeliveryFailedEvent{
				Session: params.SessionKey,
				Channel: params.Delivery.Channel,
				Reason:  "parse_directives_nil",
			})
		}
		persistReplyDeliveryFailure(deps, params.SessionKey, params.Delivery.Channel, nil, logger)
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

			// Optionally surface extended-thinking text to the channel reply.
			// Gated by the session flag so the noise stays opt-in. The reply
			// carries a collapsible thinking block that stays hidden by default.
			if showThinkingInChat(deps, params.SessionKey) && result.Thinking != "" {
				if formatted := formatThinkingForChannel(params.Delivery.Channel, result.Thinking); formatted != "" {
					if replyText != "" {
						replyText = formatted + "\n\n" + replyText
					} else {
						replyText = formatted
					}
				}
			}

			if replyText != "" {
				replyCtx, replyCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer replyCancel()
				// A nil replyFunc is only a wiring bug when there is a real user
				// channel to deliver to. Two run shapes legitimately have none, so
				// a missing replyFunc is the expected state, not an operator alarm:
				//   - sub-agent (child) sessions — the parent reads their result via
				//     session.LastOutput, not a channel push (SpawnedBy is set when
				//     the spawn captured a non-empty parent key);
				//   - channel-less runs — a sub-agent spawned with an empty parent
				//     key gets a session key like ":label:ts" (observed live), which
				//     deliveryFromSessionKey turns into an empty Delivery.Channel:
				//     there is literally no channel a reply could reach. (The empty
				//     parent key is a separate lineage bug — SendSync does not inject
				//     the session key into the tool context the way runAgentAsync
				//     does — but this guard must hold regardless of that fix.)
				noUserChannel := isSubagentSession(deps, params.SessionKey) || params.Delivery.Channel == ""
				if deps.callbacks.replyFunc == nil {
					if noUserChannel {
						// Expected: no channel to deliver to. Log quietly and skip the
						// operator escalation (Error + chat.delivery_failed broadcast +
						// transcript note) that would otherwise fire as a false alarm
						// on every sub-agent / channel-less completion.
						logger.Debug("run produced reply text but has no channel replyFunc (expected: sub-agent or channel-less session; output read via LastOutput)",
							"session", params.SessionKey,
							"channel", params.Delivery.Channel,
							"textLen", len(replyText))
					} else {
						// Wiring bug: reply callback not registered. Escalate so
						// operators notice — silent Warn was hiding broken deploys.
						logger.Error("replyFunc is nil — response not delivered (wiring bug)",
							"session", params.SessionKey,
							"channel", params.Delivery.Channel,
							"textLen", len(replyText))
						if deps.broadcast != nil {
							deps.broadcast("chat.delivery_failed", ChatDeliveryFailedEvent{
								Session: params.SessionKey,
								Channel: params.Delivery.Channel,
								Reason:  "reply_func_nil",
							})
						}
						persistReplyDeliveryFailure(deps, params.SessionKey, params.Delivery.Channel, nil, logger)
					}
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
							deps.broadcast("chat.delivery_failed", ChatDeliveryFailedEvent{
								Session: params.SessionKey,
								Channel: params.Delivery.Channel,
								Reason:  "reply_func_error",
								Error:   err.Error(),
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
					logger.Warn("media url send failed", "url", mediaURL, "error", err)
					failedURLs = append(failedURLs, mediaURL)
				}
			}
			if len(failedURLs) > 0 {
				// The agent intended to send this media and it did not arrive —
				// a user-visible delivery failure → Error, alongside the existing
				// broadcast + transcript note (per-URL detail stays at Warn above).
				logger.Error("media delivery failed",
					"session", params.SessionKey,
					"channel", params.Delivery.Channel,
					"failed", len(failedURLs),
					"total", len(directives.MediaURLs))
				if deps.broadcast != nil {
					deps.broadcast("chat.media_delivery_failed", ChatMediaDeliveryFailedEvent{
						Session: params.SessionKey,
						Channel: params.Delivery.Channel,
						Count:   len(failedURLs),
						Total:   len(directives.MediaURLs),
						URLs:    failedURLs,
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
	if deps.wikiStore != nil && shouldRecordRunDiary(params) {
		toolNames := make([]string, 0, len(result.ToolActivities))
		for _, ta := range result.ToolActivities {
			if ta.Name == "" {
				continue
			}
			name := ta.Name
			if ta.IsError {
				name += "!"
			}
			toolNames = append(toolNames, name)
			if len(toolNames) >= 16 {
				toolNames = append(toolNames, "...")
				break
			}
		}
		assistantText := result.AllText
		if assistantText == "" {
			assistantText = result.Text
		}
		assistantText = strings.TrimSpace(StripSilentToken(jsonutil.StripThinkingTags(assistantText)))
		dreamTurnFn := deps.dreamTurnFn
		shouldIncrementDream := dreamTurnFn != nil
		go func() {
			recorded := recordDiary(deps.wikiStore, logger, params.Message, toolNames, assistantText, result.StopReason, result.Turns)
			if recorded && shouldIncrementDream {
				dreamTurnFn(context.Background())
			}
		}()
	}

	// Hindsight memory: retain the completed turn into the self-hosted memory
	// bank so future sessions can recall it. Fire-and-forget; no-op unless the
	// operator configured Hindsight with the write path enabled.
	if deps.hindsightClient.RetainEnabled() && shouldRecordRunDiary(params) {
		assistantText := result.AllText
		if assistantText == "" {
			assistantText = result.Text
		}
		assistantText = strings.TrimSpace(StripSilentToken(jsonutil.StripThinkingTags(assistantText)))
		retainTurnToHindsight(deps.hindsightClient, params, assistantText, logger)
	}

	logger.Info("agent run completed",
		"stopReason", result.StopReason,
		"turns", result.Turns,
		"inputTokens", result.Usage.InputTokens,
		"outputTokens", result.Usage.OutputTokens,
	)
}

// handleRunError processes a failed or aborted agent run.
// Agent-detail logging (run.error) happens inside executeAgentRun — shared with
// the sync entry paths — so this handler owns only lifecycle and broadcast.
func handleRunError(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	broadcaster *streaming.Broadcaster,
	logger *slog.Logger,
	err error,
	now int64,
) {
	// Do not delete the draft streaming message on error: any content already
	// streamed to the user should remain visible instead of vanishing. The
	// partial draft is preferable to a blank chat with no explanation.

	aborted := ctx.Err() != nil

	if aborted {
		logger.Info("agent run aborted", "error", err)
		if broadcaster != nil {
			broadcaster.EmitAborted("")
		}
		finishRun(deps, params, session.PhaseEnd, "aborted", "killed", "", now)
		emitJobEvent(deps, params.ClientRunID, "end", true, err.Error(), now)
	} else {
		logger.Error("agent run failed", "error", err)
		// Surface a Korean reason to the user instead of the raw (often English)
		// error string — classifyRunFailureReason already computes the in-persona
		// label, with a generic fallback when the cause is unrecognized. (The raw
		// err is preserved in the log above and the job event below.)
		if broadcaster != nil {
			reason := classifyRunFailureReason(err)
			if reason == "" {
				reason = "응답을 생성하지 못했습니다. 잠시 후 다시 시도해 주세요."
			}
			broadcaster.EmitError(reason)
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
		deps.broadcast("sessions.changed", SessionsChangedEvent{
			SessionKey: params.SessionKey,
			Reason:     reason,
			Status:     status,
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
