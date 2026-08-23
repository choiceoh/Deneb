package chat

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	chatrecall "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/recall"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/llmerr"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
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
	isSilent := applySilentReplyPolicy(params, result, logger)

	substituteRunMarketTokens(result)
	normalizeRunCardReplies(result, params, deps, logger)

	persistAggregateAssistantText(params, deps, result, now, logger)

	if broadcaster != nil {
		// Read Sino-Korean Hanja as Hangul for the done frame the client settles
		// to (報告書 → 보고서). The persisted transcript keeps the raw text; the
		// display read transliterates it (toolport.TransliterateAssistantTextForDisplay).
		// stripReasoningLeak mirrors the sync path (buildSyncResult): without it
		// the async done frame was the one surface that could still show leaked
		// "[thinking]" delimiters.
		broadcaster.EmitComplete(streaming.Transliterate(strings.TrimSpace(stripReasoningLeak(result.Text))), result.Usage)
	}

	// broadcaster != nil means EmitComplete (above) delivered the reply to the
	// native app over SSE; deliverRunReply uses this to tell a client session
	// that already got its answer from a genuine reply-delivery wiring gap.
	deliverRunReply(params, deps, result, isSilent, broadcaster != nil, logger)

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

	finishTurnSideEffects(deps, params, result, logger)

	logger.Info(
		"agent run completed",
		"stopReason", result.StopReason,
		"turns", result.Turns,
		"inputTokens", result.Usage.InputTokens,
		"outputTokens", result.Usage.OutputTokens,
	)
}

// finishTurnSideEffects runs the post-run side effects shared by EVERY entry
// path — the async lifecycle (handleRunSuccess) and the synchronous
// SendSync/SendSyncStream builders:
//   - coding turn end: checkpoint + verify the worktree (Mode==code sessions)
//   - auto-diary + dream-turn trigger
//
// One call site per entry path, one implementation here. Both hooks were
// historically wired on the async path only, which left them dead on the
// native client's sync surface after PR #1922 — adding any future entry path
// must call this, not re-wire the hooks individually.
func finishTurnSideEffects(deps runDeps, params RunParams, result *agent.AgentResult, logger *slog.Logger) {
	if result == nil || deps.briefcaseMode {
		return
	}
	// Diary recording: append raw conversation turn to today's diary.
	// Wiki page curation is handled by the main LLM via system prompt.
	maybeRecordRunDiary(deps, params, result, logger)
	maybeRunMemoryInduction(deps, params, result, logger)
	// deneb-ui card health observes the already-normalized final response.
	if deps.reportCardHealth != nil {
		deps.reportCardHealth(result.Text, params.SessionKey, logger)
	}
	// 효용 접지 (cite): of the wiki pages the recall preflight injected this
	// turn, record which ones the answer actually referenced. AllText covers
	// multi-turn runs whose final turn is tool-only; consume-once semantics
	// live in RecordAnswerCitations. Best-effort — never affects delivery.
	answer := strings.TrimSpace(result.AllText)
	if answer == "" {
		answer = strings.TrimSpace(result.Text)
	}
	chatrecall.RecordAnswerCitations(deps.memory.Wiki, params.SessionKey, answer, logger)
	// Deliverable → 작업 피드 auto safety net: a document-analysis turn whose result
	// the model did not publish itself gets filed as a doc_analysis card. Anchored
	// on a hard signal (a document was ingested this turn), so ordinary chat never
	// trips it.
	maybeAutoPublishDeliverable(deps, params, result, logger)
}

// maybeAutoPublishDeliverable files the turn's final response as a doc_analysis
// work-feed card when the turn was a document analysis the model did not publish
// itself. The gates are layered so ordinary chat can never produce a card:
//   - hard anchor: a document was actually attached + extracted THIS turn
//     (hasDocumentAttachment) — no document, no card, ever;
//   - mutual exclusion: skip if the model already used the workfeed tool this turn
//     (it likely published via the guidance path — don't double-card).
//
// The substance floor (thin/narration-only answers) lives in publishDeliverable.
// No-op unless the server wired deps.deliverablePublisher.
func maybeAutoPublishDeliverable(deps runDeps, params RunParams, result *agent.AgentResult, logger *slog.Logger) {
	if deps.deliverablePublisher == nil || result == nil {
		return
	}
	// Hard anchor: a document was ingested this turn. This is the false-positive
	// floor — response shape is never used to guess "this was a deliverable".
	if !hasDocumentAttachment(params.Attachments) {
		return
	}
	// The model already touched the feed this turn (likely published via the
	// guidance path) — don't file a second card.
	if result.ToolCounts["workfeed"] > 0 {
		return
	}
	text := strings.TrimSpace(result.Text)
	if text == "" {
		return
	}
	published, err := deps.deliverablePublisher(text)
	switch {
	case err != nil:
		logger.Warn("auto-publish deliverable card failed", "session", params.SessionKey, "error", err)
	case published:
		logger.Info("auto-published deliverable card", "session", params.SessionKey)
	}
}

// applySilentReplyPolicy strips the silent-reply token (NO_REPLY) from the
// response text before persisting, broadcasting, or delivering — the internal
// token must never reach any client (RPC, WebSocket, native) or the transcript
// — and forces an explicit failure notice when an external delivery tool
// failed and the reply would otherwise be empty/silent. Returns isSilent.
func applySilentReplyPolicy(params RunParams, result *agent.AgentResult, logger *slog.Logger) bool {
	// Strip silent reply token (NO_REPLY) from the response text before
	// persisting, broadcasting, or delivering. This ensures the internal
	// token is never exposed to any client (RPC, SSE, native client)
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
	return isSilent
}

// substituteRunMarketTokens replaces market letter tokens ("{{market:usd_krw}}")
// in the finishing run's text fields with their recorded display values, before
// any consumer reads them: the aggregate transcript write, the SSE done frame
// the native card settles to, the channel reply, session LastOutput, and the
// work-feed publisher. A streamed/async turn that mimics the morning-letter
// skeleton (2026-07-11 production transcript, client:main) would otherwise
// surface raw template syntax as "{{market:usd_krw}}원". Sibling substitutions:
// SyncResult.BestText (sync RPC response), sanitizeAssistantForTranscript
// (per-turn persist), proactive relay (prepareProactiveDelivery).
//
// Mutating the current turn's result at finalize follows the
// applySilentReplyPolicy precedent — prompt-cache Rule A governs messages
// already sent to the LLM, not the finishing turn's outputs. Async-only path
// (handleRunSuccess), so Briefcase determinism is unaffected.
func substituteRunMarketTokens(result *agent.AgentResult) {
	result.Text = toolwire.SubstituteMarketLetterTokens(result.Text)
	result.AllText = toolwire.SubstituteMarketLetterTokens(result.AllText)
	result.DeliverableText = toolwire.SubstituteMarketLetterTokens(result.DeliverableText)
}

// persistAggregateAssistantText persists the run's accumulated text as one
// assistant message (legacy path) when per-turn persistence was NOT active.
func persistAggregateAssistantText(params RunParams, deps runDeps, result *agent.AgentResult, now int64, logger *slog.Logger) {
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
				deps.callbacks.emitTranscriptFn(params.SessionKey, mustRawJSON(assistantMsg), "")
			}
		}
		// Sync Aurora summaries for channel replies when available.
	}
}

// deliverRunReply routes the final reply to the originating channel:
// empty-response fallbacks, directive parsing (silent/media/threading), the
// channel reply with one retry, and media token delivery — with every
// user-observable failure escalated per logging.md (Error + broadcast +
// transcript note).
func deliverRunReply(params RunParams, deps runDeps, result *agent.AgentResult, isSilent, sseDelivered bool, logger *slog.Logger) {
	// Deliver response back to the originating channel (e.g., the native client).
	// Use parseReplyDirectives (chatport boundary) for unified processing: silent token
	// detection, leaked tool-call stripping, MEDIA: extraction, and threading.
	if params.Delivery != nil && result.Text == "" && !isSilent {
		deliverEmptyRunReply(params, deps, result, logger)
	}
	if params.Delivery != nil && result.Text != "" && deps.chatport.ParseReplyDirectives == nil {
		reportMissingReplyDirectiveParser(params, deps, result, logger)
	}
	if params.Delivery != nil && result.Text != "" && deps.chatport.ParseReplyDirectives != nil {
		directives := deps.chatport.ParseReplyDirectives(result.Text, params.Delivery.MessageID, "")
		replyCancel := deliverDirectiveReplyText(params, deps, result, directives, sseDelivered, logger)
		defer replyCancel()

		deliverDirectiveMedia(params, deps, directives, logger)
	}
}

// deliverEmptyRunReply preserves the empty-output decision tree: abnormal
// stops receive a bounded fallback reply, while legitimate tool-only empties
// remain observable through chat.empty_response.
func deliverEmptyRunReply(params RunParams, deps runDeps, result *agent.AgentResult, logger *slog.Logger) {
	logger.Warn("agent produced empty response, nothing to deliver",
		"session", params.SessionKey,
		"channel", params.Delivery.Channel,
		"turns", result.Turns,
		"stopReason", result.StopReason,
		"inputTokens", result.Usage.InputTokens,
		"outputTokens", result.Usage.OutputTokens)

	fallbackMsg := enrichStopFallback(fallbackForStopReason(result.StopReason), result.ToolActivities)
	if fallbackMsg == "" && isEmptyFinalResult(result) {
		fallbackMsg = fallbackForEmptyFinalReply()
	}
	if fallbackMsg != "" {
		persistTimeoutRemnant(deps, params.SessionKey, result, fallbackMsg, logger)
	}
	if fallbackMsg != "" && deps.callbacks.replyFunc != nil {
		replyCtx, replyCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := deps.callbacks.replyFunc(replyCtx, params.Delivery, fallbackMsg); err != nil {
			logger.Error("fallback delivery failed",
				"error", err, "stopReason", result.StopReason,
				"session", params.SessionKey)
			if deps.broadcast != nil {
				broadcastPayload(deps.broadcast, "chat.delivery_failed", ChatDeliveryFailedEvent{
					Session: params.SessionKey,
					Channel: params.Delivery.Channel,
					Reason:  "stop_fallback_error",
					Error:   err.Error(),
				})
			}
		}
		replyCancel()
		return
	}
	if deps.broadcast != nil {
		broadcastPayload(deps.broadcast, "chat.empty_response", ChatEmptyResponseEvent{
			Session:    params.SessionKey,
			Channel:    params.Delivery.Channel,
			StopReason: result.StopReason,
			Turns:      result.Turns,
		})
	}
}

func reportMissingReplyDirectiveParser(params RunParams, deps runDeps, result *agent.AgentResult, logger *slog.Logger) {
	logger.Error("parseReplyDirectives is nil — response not delivered (wiring bug)",
		"session", params.SessionKey,
		"channel", params.Delivery.Channel,
		"textLen", len(result.Text))
	if deps.broadcast != nil {
		broadcastPayload(deps.broadcast, "chat.delivery_failed", ChatDeliveryFailedEvent{
			Session: params.SessionKey,
			Channel: params.Delivery.Channel,
			Reason:  "parse_directives_nil",
		})
	}
	persistReplyDeliveryFailure(deps, params.SessionKey, params.Delivery.Channel, nil, logger)
}

// deliverDirectiveReplyText applies the directive-level silent decision before
// any channel side effect. Silent replies intentionally leave streamed drafts
// untouched; media delivery remains a later independent stage.
func deliverDirectiveReplyText(
	params RunParams,
	deps runDeps,
	result *agent.AgentResult,
	directives chatport.ReplyDirectives,
	sseDelivered bool,
	logger *slog.Logger,
) context.CancelFunc {
	if directives.IsSilent {
		logger.Info("suppressing silent reply (NO_REPLY); streamed draft preserved",
			"hasMedia", len(directives.MediaURLs) > 0)
		return func() {}
	}
	replyText := formatRunReplyText(params, deps, result, directives.Text)
	if replyText == "" {
		return func() {}
	}
	return deliverChannelReply(params, deps, replyText, sseDelivered, logger)
}

func formatRunReplyText(params RunParams, deps runDeps, result *agent.AgentResult, text string) string {
	replyText := strings.TrimSpace(stripReasoningLeak(jsonutil.StripThinkingTags(text)))
	if !showThinkingInChat(deps, params.SessionKey) || result.Thinking == "" {
		return replyText
	}
	formatted := formatThinkingForChannel(params.Delivery.Channel,
		translateThinkingForDisplay(deps, result.Thinking))
	if formatted == "" {
		return replyText
	}
	if replyText == "" {
		return formatted
	}
	return formatted + "\n\n" + replyText
}

func deliverChannelReply(params RunParams, deps runDeps, replyText string, sseDelivered bool, logger *slog.Logger) context.CancelFunc {
	replyCtx, replyCancel := context.WithTimeout(context.Background(), 30*time.Second)
	if deps.callbacks.replyFunc == nil {
		handleMissingChannelReply(params, deps, replyText, sseDelivered, logger)
		return replyCancel
	}
	sendChannelReply(replyCtx, params, deps, replyText, logger)
	return replyCancel
}

// channelReplyAlreadyHandled identifies run shapes with no pending channel
// push: sub-agents, channel-less runs, and native replies already sent by SSE.
func channelReplyAlreadyHandled(params RunParams, deps runDeps, sseDelivered bool) bool {
	return isSubagentSession(deps, params.SessionKey) ||
		params.Delivery.Channel == "" ||
		(params.Delivery.Channel == "client" && sseDelivered)
}

func handleMissingChannelReply(params RunParams, deps runDeps, replyText string, sseDelivered bool, logger *slog.Logger) {
	if channelReplyAlreadyHandled(params, deps, sseDelivered) {
		logger.Debug("run produced reply text but has no channel replyFunc (expected: sub-agent or channel-less session; output read via LastOutput)",
			"session", params.SessionKey,
			"channel", params.Delivery.Channel,
			"textLen", len(replyText))
		return
	}
	logger.Error("replyFunc is nil — response not delivered (wiring bug)",
		"session", params.SessionKey,
		"channel", params.Delivery.Channel,
		"textLen", len(replyText))
	if deps.broadcast != nil {
		broadcastPayload(deps.broadcast, "chat.delivery_failed", ChatDeliveryFailedEvent{
			Session: params.SessionKey,
			Channel: params.Delivery.Channel,
			Reason:  "reply_func_nil",
		})
	}
	persistReplyDeliveryFailure(deps, params.SessionKey, params.Delivery.Channel, nil, logger)
}

// channelReplyRetryDelay is the pause before one channel-reply retry.
// Tests shrink this via TestMain.
var channelReplyRetryDelay = 500 * time.Millisecond

func sendChannelReply(ctx context.Context, params RunParams, deps runDeps, replyText string, logger *slog.Logger) {
	err := deps.callbacks.replyFunc(ctx, params.Delivery, replyText)
	if err != nil {
		logger.Warn("channel reply failed, retrying once",
			"error", err, "channel", params.Delivery.Channel)
		time.Sleep(channelReplyRetryDelay)
		err = deps.callbacks.replyFunc(ctx, params.Delivery, replyText)
	}
	if err == nil {
		return
	}
	logger.Error("channel reply failed after retry",
		"error", err, "channel", params.Delivery.Channel,
		"session", params.SessionKey)
	if deps.broadcast != nil {
		broadcastPayload(deps.broadcast, "chat.delivery_failed", ChatDeliveryFailedEvent{
			Session: params.SessionKey,
			Channel: params.Delivery.Channel,
			Reason:  "reply_func_error",
			Error:   err.Error(),
		})
	}
	persistReplyDeliveryFailure(deps, params.SessionKey, params.Delivery.Channel, err, logger)
}

// deliverDirectiveMedia is deliberately last and independent of text silence:
// explicit media tokens still express delivery intent after NO_REPLY.
func deliverDirectiveMedia(
	params RunParams,
	deps runDeps,
	directives chatport.ReplyDirectives,
	logger *slog.Logger,
) {
	if deps.callbacks.mediaSendFn == nil || len(directives.MediaURLs) == 0 {
		return
	}
	mediaCtx, mediaCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer mediaCancel()

	mediaType := ""
	if directives.AudioAsVoice {
		mediaType = "voice"
	}
	failedURLs := sendDirectiveMedia(mediaCtx, params, deps, directives.MediaURLs, mediaType, logger)
	if len(failedURLs) == 0 {
		return
	}
	logger.Error("media delivery failed",
		"session", params.SessionKey,
		"channel", params.Delivery.Channel,
		"failed", len(failedURLs),
		"total", len(directives.MediaURLs))
	if deps.broadcast != nil {
		broadcastPayload(deps.broadcast, "chat.media_delivery_failed", ChatMediaDeliveryFailedEvent{
			Session: params.SessionKey,
			Channel: params.Delivery.Channel,
			Count:   len(failedURLs),
			Total:   len(directives.MediaURLs),
			URLs:    failedURLs,
		})
	}
	persistMediaDeliveryFailure(deps, params.SessionKey, params.Delivery.Channel, failedURLs, logger)
}

func sendDirectiveMedia(
	ctx context.Context,
	params RunParams,
	deps runDeps,
	mediaURLs []string,
	mediaType string,
	logger *slog.Logger,
) []string {
	var failedURLs []string
	for _, mediaURL := range mediaURLs {
		if err := deps.callbacks.mediaSendFn(ctx, params.Delivery, mediaURL, mediaType, "", false); err != nil {
			logger.Warn("media url send failed", "url", mediaURL, "error", err)
			failedURLs = append(failedURLs, mediaURL)
		}
	}
	return failedURLs
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
		broadcastPayload(deps.broadcast, "sessions.changed", SessionsChangedEvent{
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

// maybeRecordRunDiary appends a successful run to today's diary and, when an
// entry was actually recorded, feeds the dream-turn trigger. Shared by the
// async lifecycle (handleRunSuccess) and the synchronous SendSync/
// SendSyncStream paths: the native client's chat path is SendSync, so hooking
// only the async path left auto-diary AND turn-triggered dreaming dead on the
// real interactive surface — the prompt doctrine "서버가 성공한 대화 턴을
// 자동으로 일지에 기록한다" silently held for no interactive turn since the
// native client became the sole surface (PR #1922), leaving the diary fed
// only by mail polling/captures and dreaming fired only by the 30-min timer.
func maybeRecordRunDiary(deps runDeps, params RunParams, result *agent.AgentResult, logger *slog.Logger) {
	if deps.memory.Wiki == nil || result == nil || !shouldRecordRunDiary(params) {
		return
	}
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
	// stripReasoningLeak on top of the tag strip: leaked "[thinking]"/bare
	// "<think>" delimiters would otherwise persist into the diary and later
	// seed dreaming with reasoning noise.
	assistantText = strings.TrimSpace(stripReasoningLeak(StripSilentToken(jsonutil.StripThinkingTags(assistantText))))
	dreamTurnFn := deps.dreamTurnFn
	shouldIncrementDream := dreamTurnFn != nil
	prefSignalFn := deps.preferenceSignalFn
	// Background work rides the server lifecycle ctx (canceled on shutdown,
	// not on request completion).
	bgCtx := deps.callbacks.shutdownCtx
	if bgCtx == nil {
		bgCtx = context.Background()
	}
	safego.GoWithSlog(logger, "run-diary", func() {
		recorded, signal := recordDiary(deps.memory.Wiki, logger, params.Message, toolNames, assistantText, result.StopReason, result.Turns)
		if !recorded {
			return
		}
		// Note the 선호 capsule before the dream-turn increment: the increment's
		// ShouldDream check must already see the pending preference signal.
		if signal.preference() && prefSignalFn != nil {
			prefSignalFn()
		}
		if shouldIncrementDream {
			dreamTurnFn(bgCtx)
		}
	})
}
