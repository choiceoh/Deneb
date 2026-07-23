package ai.deneb.deneb

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
                list + History(role = History.Role.USER, content = displayText)
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
                    onThinking = progress::onThinking,
                ) { delta ->
                    progress.onDelta()
                    accumulated.append(delta)
                }
            } finally {
                pacer.cancel()
            }
        }
    } catch (cancel: CancellationException) {
        settleChatPlaceholder(assistantId, accumulated.toString())
        askActive = false
        throw cancel
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
        val recovered = try {
            recoverTurnFromTranscript(sessionKeyAtSend, sendText)
        } catch (cancel: CancellationException) {
            settleChatPlaceholder(assistantId, accumulated.toString())
            askActive = false
            throw cancel
        } finally {
            // Drop the status row on every exit (a successful recovery installs the
            // server transcript, which already lacks it, so this is then a no-op).
            _chatHistory.update { list -> list.filter { it.id != recoveryRowId } }
        }
        recovered ?: if (accumulated.isEmpty()) {
            try {
                sendGatewayChat(http, gatewayUrl, clientToken, sessionKey, sendText)
            } catch (cancel: CancellationException) {
                settleChatPlaceholder(assistantId, accumulated.toString())
                askActive = false
                throw cancel
            } catch (sendError: Exception) {
                GatewayReply("⚠️ ${sendError.message ?: "gateway request failed"}", ok = false)
            }
        } else {
            GatewayReply(text = accumulated.toString(), ok = false)
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
