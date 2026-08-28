package ai.deneb.deneb

import ai.deneb.DenebLog
import ai.deneb.data.UiSubmission
import ai.deneb.ui.chat.History
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlin.uuid.ExperimentalUuidApi
import kotlin.uuid.Uuid

/**
 * Runs one optimistic, streaming chat turn while keeping UI pacing separate
 * from the gateway transport and the repository's long-lived state surface.
 */
@OptIn(ExperimentalUuidApi::class)
internal suspend fun DenebGatewayClient.askGateway(
    question: String?,
    uiSubmission: UiSubmission?,
): Boolean {
    // Our send owns this conversation now — a lingering foreign-turn watch
    // would drop a stale status row into the fresh turn (and waste polls).
    cancelForeignTurnWatch()
    val displayText = question?.trim().orEmpty()
    val sendText = if (uiSubmission != null) formatUiCallback(uiSubmission) else displayText
    if (sendText.isEmpty()) return true

    val assistantId = Uuid.random().toString()
    val sessionKeyAtSend = sessionKey
    askActive = true
    historyGate.withLock {
        historyEpoch++
        _chatHistory.update { list ->
            val withUser = if (displayText.isNotEmpty()) {
                // Carry the submission on the row. Without it the chat list's guard
                // (uiSubmission == null -> draw a bubble) never fires, so a card
                // button showed its internal English label — "Pressed: confirm" —
                // as if the user had typed it, and the card never froze into its
                // answered state.
                list + History(role = History.Role.USER, content = displayText, uiSubmission = uiSubmission)
            } else {
                list
            }
            withUser + History(id = assistantId, role = History.Role.ASSISTANT, content = "")
        }
    }
    val accumulated = StringBuilder()
    val replaceAssistant: (String, String?) -> Unit = { text, fallback ->
        _chatHistory.update { list ->
            list.map {
                if (it.id == assistantId) it.copy(content = text, fallbackServiceName = fallback) else it
            }
        }
    }

    // Network frames often arrive in bursts. Reveal the received text at a
    // steady cadence on Main so the StringBuilder and UI updates stay serialized.
    val revealTickMs = 33L
    val revealDrainDivisor = 4
    val revealMinChars = 2
    var revealed = 0
    val progress = TurnProgress(_chatHistory, scope)
    val reply = try {
        withContext(Dispatchers.Main) {
            val pacer = launch {
                while (isActive) {
                    if (revealed < accumulated.length) {
                        val backlog = accumulated.length - revealed
                        val step = maxOf(revealMinChars, backlog / revealDrainDivisor)
                        revealed = minOf(accumulated.length, revealed + step)
                        replaceAssistant(accumulated.toString().take(revealed), null)
                    }
                    delay(revealTickMs)
                }
            }
            try {
                streamGatewayChat(
                    http = http,
                    jsonCodec = jsonCodec,
                    gatewayUrl = gatewayUrl,
                    clientToken = clientToken,
                    sessionKey = sessionKey,
                    message = sendText,
                    onTool = progress::onTool,
                    onProgress = progress::onProgress,
                    onThinking = progress::onThinking,
                    onReasoning = { reasoning ->
                        // Live full reasoning-so-far → grow the answer's expandable
                        // block during streaming (the done frame settles the final).
                        _chatHistory.update { list ->
                            list.map { if (it.id == assistantId) it.copy(reasoningContent = reasoning) else it }
                        }
                    },
                ) { delta ->
                    progress.onDelta()
                    accumulated.append(delta)
                }
            } finally {
                pacer.cancel()
            }
        }
    } catch (cancel: CancellationException) {
        // The server turn may still be running; its answer will land only in
        // the transcript. Ask the NEXT completed turn to reconcile the view.
        reconcileAfterTurn = true
        settleChatPlaceholder(assistantId, accumulated.toString())
        askActive = false
        throw cancel
    } catch (refused: GatewayStreamRefusedException) {
        // The turn never started: the connect failed, or the gateway refused before
        // any event. There is no detached run to poll for, so recovering would spend
        // the whole 90s budget showing "답변 이어받는 중…" and then report the wrong
        // reason — the observed offline-send behaviour. Fail now, in Korean.
        DenebLog.warn("chat", "stream refused: $refused")
        GatewayReply(gatewayFailureText(refused), ok = false)
    } catch (streamError: GatewayStreamErrorException) {
        // The gateway itself said this turn failed. Its message IS the outcome; the
        // old path discarded it and reported a recovery failure instead.
        DenebLog.warn("chat", "stream error: ${streamError.gatewayMessage}")
        GatewayReply("⚠️ ${streamError.gatewayMessage}", ok = false)
    } catch (_: Exception) {
        // A half-open mobile socket can die after the gateway accepted the turn.
        // The detached server run keeps producing the answer; poll the canonical
        // transcript for it. Show a status row meanwhile (rendered as the waiting
        // chip) so a reconnect never looks like a frozen preamble — the reported
        // "stuck on the opening line" — even though recovery can now run minutes.
        val recoveryRowId = "recovery-$assistantId"
        _chatHistory.update { list ->
            list + History(
                id = recoveryRowId,
                role = History.Role.TOOL_EXECUTING,
                content = "recovery",
                toolName = ToolStatusLabels.RESUMING,
                isStatusMessage = true,
            )
        }
        val recovery = try {
            recoverTurnFromTranscript(sessionKeyAtSend, sendText)
        } catch (cancel: CancellationException) {
            // A re-send cancelled the recovery poll — the finished answer it
            // was about to install stays stranded in the transcript (production
            // 2026-08-01, "채팅 안 보임"). Reconcile after the next turn.
            reconcileAfterTurn = true
            settleChatPlaceholder(assistantId, accumulated.toString())
            askActive = false
            throw cancel
        } finally {
            // Drop the status row on every exit (a successful recovery installs the
            // server transcript, which already lacks it, so this is then a no-op).
            _chatHistory.update { list -> list.filter { it.id != recoveryRowId } }
        }
        when (recovery) {
            is TurnRecoveryResult.Recovered -> recovery.reply

            TurnRecoveryResult.NotArrived -> if (accumulated.isEmpty()) {
                try {
                    sendGatewayChat(http, gatewayUrl, clientToken, sessionKeyAtSend, sendText)
                } catch (cancel: CancellationException) {
                    reconcileAfterTurn = true
                    settleChatPlaceholder(assistantId, accumulated.toString())
                    askActive = false
                    throw cancel
                } catch (sendError: Exception) {
                    DenebLog.warn("chat", "blocking send failed: $sendError")
                    GatewayReply(gatewayFailureText(sendError), ok = false)
                }
            } else {
                GatewayReply(text = accumulated.toString(), ok = false)
            }

            TurnRecoveryResult.GiveUp -> if (accumulated.isEmpty()) {
                GatewayReply("⚠️ 답변을 이어받지 못했습니다", ok = false)
            } else {
                GatewayReply(text = accumulated.toString(), ok = false)
            }
        }
    } finally {
        progress.clear()
    }

    val streamed = accumulated.toString()
    val finalText = when {
        streamed.length > reply.text.length + 40 -> streamed
        reply.text.isNotBlank() -> reply.text
        else -> streamed
    }
    replaceAssistant(
        finalText.ifBlank { "⚠️ 빈 응답" },
        if (reply.fellBack && reply.model.isNotBlank()) reply.model else null,
    )
    // Attach the turn's reasoning (from the done frame) so the answer shows its
    // expandable reasoning block immediately, without waiting for a transcript
    // reload. A recovery reply carries none; that path installs the transcript,
    // whose rows already include reasoning.
    reply.reasoning?.takeIf { it.isNotBlank() }?.let { reasoning ->
        _chatHistory.update { list ->
            list.map { if (it.id == assistantId) it.copy(reasoningContent = reasoning) else it }
        }
    }
    progress.footprint()?.let { footprint ->
        _chatHistory.update { list ->
            list.map { if (it.id == assistantId) it.copy(toolFootprint = footprint) else it }
        }
    }
    askActive = false
    // A previously cancelled turn/recovery left an answer stranded in the
    // transcript: now that no turn is active, install the canonical transcript
    // (epoch-guarded; the just-persisted current answer is included, so this
    // merges rather than clobbers).
    if (reconcileAfterTurn) {
        reconcileAfterTurn = false
        reconcileOpenConversationAsync()
    }
    return reply.ok
}

private fun DenebGatewayClient.settleChatPlaceholder(assistantId: String, partial: String) {
    _chatHistory.update { list ->
        if (partial.isBlank()) {
            list.filter { it.id != assistantId }
        } else {
            list.map { if (it.id == assistantId) it.copy(content = partial) else it }
        }
    }
}

private fun formatUiCallback(submission: UiSubmission): String = buildString {
    append("[deneb-ui] event=").append(submission.pressedEvent)
    if (submission.values.isNotEmpty()) {
        append(" values={")
        append(submission.values.entries.joinToString(", ") { "${it.key}=${it.value}" })
        append("}")
    }
}
