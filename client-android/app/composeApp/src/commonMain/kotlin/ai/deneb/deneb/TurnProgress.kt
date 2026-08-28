package ai.deneb.deneb

import ai.deneb.ui.chat.History
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.Serializable
import kotlin.time.Duration.Companion.milliseconds
import kotlin.time.TimeSource
import kotlin.uuid.Uuid

/**
 * A gateway SSE frame marking a tool's lifecycle (started/completed) within a
 * streamed chat turn. Carried in the `tool` event; [TurnProgress] turns each
 * into a transient status row in the chat history.
 *
 * Internal (not private) so it can live next to [TurnProgress] in this file
 * while still being referenced from [DenebGatewayClient.sendStreaming].
 */
@Serializable
internal data class ToolEvent(
    val state: String = "",
    val tool: String = "",
    val toolUseId: String = "",
    val detail: String = "",
    val isError: Boolean = false,
    /** Gateway-authored one-line digest of what the call produced; completed
     * frames only. Rendered verbatim — the server owns the wording so every
     * client shows the same line. */
    val resultSummary: String = "",
)

/** ": <요약>" for a status row, or "" when the frame carried none. The gateway
 * already bounded the length, so this never re-truncates. */
internal fun ToolEvent.summarySuffix(): String = if (resultSummary.isNotEmpty()) ": $resultSummary" else ""

/** Deterministic gateway-owned turn phase; unlike thinking previews this never
 * contains model chain-of-thought. */
@Serializable
internal data class ProgressEvent(
    val phase: String = "",
    val label: String = "",
    val startedAtMs: Long = 0,
    val softDeadlineMs: Long = 0,
    val hardDeadlineMs: Long = 0,
)

/**
 * Turn-scoped live progress for [DenebGatewayClient.ask]: gateway `tool`/
 * `thinking` SSE frames become transient [History.Role.TOOL_EXECUTING] rows
 * (status-only, Korean labels via [ToolStatusLabels]) that the chat screen's
 * waiting chip picks up through its existing executing-tools derivation — the
 * same mechanism the local-provider pipeline uses.
 *
 * Coverage goal: never regress the chip to the generic spinner mid-turn.
 * Gateway progress frames narrate safe server-owned phases; legacy gateways
 * can still fall back to their thinking preview. When the last running tool
 * completes the row is repurposed as a
 * continuity status ("결과 검토 중…") that bridges the event-silent prefill
 * stretch until the next thinking/tool/delta event.
 *
 * Threading: all map/flag state is touched only from the SSE read coroutine
 * (callbacks run inline in [DenebGatewayClient.sendStreaming]); the delayed
 * removals/swaps launched on [scope] only perform id-keyed history edits, so no
 * synchronization is needed. [clear] runs in ask()'s finally, so a
 * mid-stream error or cancel can never leak a zombie chip row.
 *
 * Split out of DenebGatewayClient.kt — the class is self-contained (its own
 * maps/flags) and reaches the gateway's chat history + coroutine scope through
 * the constructor, so it carries no coupling to the client's other state.
 */
internal class TurnProgress(
    private val chatHistory: MutableStateFlow<List<History>>,
    private val scope: CoroutineScope,
) {
    private val phaseId = "progress-phase-${Uuid.random()}"
    private var phaseVisible = false
    private val thinkingId = "progress-thinking-${Uuid.random()}"
    private var thinkingVisible = false

    // toolUseId (or tool name when the gateway omits the id) → row id/start.
    private val rowIds = mutableMapOf<String, String>()
    private val startMarks = mutableMapOf<String, TimeSource.Monotonic.ValueTimeMark>()
    private val allRowIds = mutableSetOf<String>()

    // Row currently repurposed as the between-steps continuity status
    // ("결과 검토 중…"); null when no continuity chip is showing.
    private var continuityRowId: String? = null

    // Completed tools in execution order (tool name + error flag) — the
    // source of the post-turn footprint line under the answer.
    private val trail = mutableListOf<Pair<String, Boolean>>()

    /** Gateway-authored phase narration shown from request acceptance through
     * finalization. Later tool/thinking frames temporarily replace it with their
     * more concrete status; the next progress frame restores the phase row. */
    fun onProgress(ev: ProgressEvent) {
        val label = ev.label.trim()
        if (label.isEmpty()) return
        hideThinking()
        hideContinuity()
        if (!phaseVisible) {
            phaseVisible = true
            allRowIds += phaseId
            chatHistory.update { list ->
                list + History(
                    id = phaseId,
                    role = History.Role.TOOL_EXECUTING,
                    content = ev.phase.ifBlank { "progress" },
                    toolName = label,
                    isStatusMessage = true,
                )
            }
        } else {
            chatHistory.update { list ->
                list.map {
                    if (it.id == phaseId) it.copy(content = ev.phase.ifBlank { it.content }, toolName = label) else it
                }
            }
        }
    }

    /**
     * Reasoning liveness pulse → keep the gateway-authored phase when present,
     * otherwise retain the legacy thinking preview for compatibility with older
     * gateways. New gateways always emit the safe phase immediately first, so
     * raw reasoning is not used as their process narration.
     */
    fun onThinking(preview: String) {
        hideContinuity()
        if (phaseVisible) return
        val label = ToolStatusLabels.THINKING +
            if (preview.isNotEmpty()) ": $preview" else ""
        if (!thinkingVisible) {
            thinkingVisible = true
            allRowIds += thinkingId
            chatHistory.update { list ->
                list + History(
                    id = thinkingId,
                    role = History.Role.TOOL_EXECUTING,
                    content = "thinking",
                    toolName = label,
                    isStatusMessage = true,
                )
            }
        } else if (preview.isNotEmpty()) {
            chatHistory.update { list ->
                list.map { if (it.id == thinkingId) it.copy(toolName = label) else it }
            }
        }
    }

    /** Visible answer text is flowing — drop legacy transient rows while the
     * gateway-owned writing phase remains visible. */
    fun onDelta() {
        hideThinking()
        hideContinuity()
    }

    fun onTool(ev: ToolEvent) {
        val key = ev.toolUseId.ifEmpty { ev.tool }
        if (key.isBlank() || ev.tool.isBlank()) return
        when (ev.state) {
            "started" -> {
                hidePhase()
                hideThinking()
                hideContinuity()
                val label = ToolStatusLabels.label(ev.tool) +
                    if (ev.detail.isNotEmpty()) ": ${ev.detail}" else ""
                rowIds[key]?.let { existingId ->
                    // An SSE reconnect/replay can repeat a started frame. Refresh
                    // its detail in place instead of leaking a second progress row
                    // that can no longer be paired with the eventual completion.
                    chatHistory.update { list ->
                        list.map { if (it.id == existingId) it.copy(toolName = label) else it }
                    }
                    return
                }
                val rowId = "progress-tool-${Uuid.random()}"
                rowIds[key] = rowId
                startMarks[key] = TimeSource.Monotonic.markNow()
                allRowIds += rowId
                // "메일 확인 중: 아르고에너지" — the server-extracted hint
                // names the target, not just the tool.
                chatHistory.update { list ->
                    list + History(
                        id = rowId,
                        role = History.Role.TOOL_EXECUTING,
                        content = ev.tool,
                        toolName = label,
                        isStatusMessage = true,
                    )
                }
            }

            "completed" -> {
                val rowId = rowIds.remove(key) ?: return
                // Count only lifecycle pairs this client actually observed.
                // Duplicate or replayed completion frames must not inflate the
                // post-turn footprint.
                trail += ev.tool to ev.isError
                if (ev.isError) {
                    // Swap the row to its failure form ("메일 확인 실패")
                    // and hold it readable — the agent usually keeps going,
                    // so this explains why the turn is taking longer.
                    val failure = ToolStatusLabels.failureLabel(ev.tool) + ev.summarySuffix()
                    chatHistory.update { list ->
                        list.map { if (it.id == rowId) it.copy(toolName = failure) else it }
                    }
                    scope.launch {
                        delay(DenebGatewayClient.FAILURE_DISPLAY_MS.milliseconds)
                        removeRow(rowId)
                    }
                    startMarks.remove(key)
                    return
                }
                val elapsed = startMarks.remove(key)?.elapsedNow() ?: 0.milliseconds
                val remaining = DenebGatewayClient.MIN_PROGRESS_DISPLAY_MS.milliseconds - elapsed
                // The row lingers for `remaining`; give that moment the finished
                // label and what came back instead of a stale "…중".
                if (remaining.isPositive()) {
                    val done = ToolStatusLabels.doneLabel(ev.tool) + ev.summarySuffix()
                    chatHistory.update { list ->
                        list.map { if (it.id == rowId) it.copy(toolName = done) else it }
                    }
                }
                if (rowIds.isEmpty()) {
                    // Last running tool finished — the model is back in an
                    // LLM step reading the results, which on a cache-missed
                    // prefill can stay event-silent for tens of seconds.
                    // Repurpose the row as a continuity status instead of
                    // dropping the chip back to the generic spinner; the
                    // next thinking/tool/delta event (or clear) removes it.
                    continuityRowId = rowId
                    val swap = {
                        chatHistory.update { list ->
                            list.map {
                                if (it.id == rowId) {
                                    it.copy(content = "continuity", toolName = ToolStatusLabels.REVIEWING)
                                } else {
                                    it
                                }
                            }
                        }
                    }
                    if (remaining.isPositive()) {
                        // Keep the finished tool's label readable first. The
                        // delayed swap is an idempotent id-keyed map, so
                        // racing hideContinuity()/clear() (row already gone)
                        // is harmless.
                        scope.launch {
                            delay(remaining)
                            swap()
                        }
                    } else {
                        swap()
                    }
                    return
                }
                if (remaining.isPositive()) {
                    // Hold fast tools on screen long enough to read; the
                    // removal is an idempotent id filter, so racing clear()
                    // or a conversation switch is safe.
                    scope.launch {
                        delay(remaining)
                        removeRow(rowId)
                    }
                } else {
                    removeRow(rowId)
                }
            }
        }
    }

    /**
     * One-line trail of what this turn did — "메일 확인 ×2 · 웹 검색 ⚠" —
     * attached under the finished answer. Null when no tool completed.
     * Live-turn only by design: the gateway transcript does not carry it,
     * so reloading a conversation drops the line.
     */
    fun footprint(): String? {
        if (trail.isEmpty()) return null
        val counts = LinkedHashMap<String, IntArray>() // tool → [count, errored(0/1)]
        for ((tool, isError) in trail) {
            val agg = counts.getOrPut(tool) { intArrayOf(0, 0) }
            agg[0]++
            if (isError) agg[1] = 1
        }
        val parts = counts.entries.take(DenebGatewayClient.FOOTPRINT_MAX_TOOLS).map { (tool, agg) ->
            buildString {
                append(ToolStatusLabels.trailLabel(tool))
                if (agg[0] > 1) append(" ×${agg[0]}")
                if (agg[1] == 1) append(" ⚠")
            }
        }
        val more = counts.size - DenebGatewayClient.FOOTPRINT_MAX_TOOLS
        return parts.joinToString(" · ") + if (more > 0) " 외 $more" else ""
    }

    /** Remove every row this turn added (idempotent; runs in ask()'s finally). */
    fun clear() {
        if (allRowIds.isEmpty()) return
        thinkingVisible = false
        phaseVisible = false
        continuityRowId = null
        val ids = allRowIds.toSet()
        chatHistory.update { list -> list.filter { it.id !in ids } }
    }

    private fun hideThinking() {
        if (!thinkingVisible) return
        thinkingVisible = false
        removeRow(thinkingId)
    }

    private fun hidePhase() {
        if (!phaseVisible) return
        phaseVisible = false
        removeRow(phaseId)
    }

    private fun hideContinuity() {
        val id = continuityRowId ?: return
        continuityRowId = null
        removeRow(id)
    }

    private fun removeRow(id: String) {
        chatHistory.update { list -> list.filter { it.id != id } }
    }
}
