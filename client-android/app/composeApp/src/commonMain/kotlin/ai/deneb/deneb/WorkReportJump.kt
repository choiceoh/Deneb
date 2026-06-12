package ai.deneb.deneb

import ai.deneb.data.SharedJson
import ai.deneb.ui.chat.History
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonPrimitive
import kotlin.math.abs

// Jump from a proactive 업무-feed card to the transcript message that mirrors it
// in client:main. The gateway relay appends the transcript mirror and the feed
// card within the same call, so their timestamps land milliseconds apart —
// nearest-timestamp matching is the link (the feed item carries no message id).
// Pure functions, kept out of DenebGatewayClient so they are unit-testable.

/**
 * How far apart the card's createdAtMs and the mirror's timestampMs may be and
 * still count as the same report. Real pairs differ by milliseconds; the
 * tolerance only rejects cards whose mirror is genuinely absent (older than the
 * fetched transcript window, or delivered before timestamps were recorded).
 */
internal const val MIRRORED_REPORT_TOLERANCE_MS = 120_000L

/**
 * Index of the assistant message mirroring a work-feed card stamped at
 * [createdAtMs], or -1 when no message lands within the tolerance.
 */
internal fun indexOfMirroredReport(history: List<History>, createdAtMs: Long): Int {
    if (createdAtMs <= 0) return -1
    var best = -1
    var bestDelta = Long.MAX_VALUE
    history.forEachIndexed { i, h ->
        if (h.role != History.Role.ASSISTANT || h.timestampMs <= 0) return@forEachIndexed
        val delta = abs(h.timestampMs - createdAtMs)
        if (delta < bestDelta) {
            best = i
            bestDelta = delta
        }
    }
    return if (best >= 0 && bestDelta <= MIRRORED_REPORT_TOLERANCE_MS) best else -1
}

/**
 * Rewrites a server-assembled collapsed-report fence (```deneb-ui accordion)
 * to open expanded, so a card jump shows the report without a second tap.
 * Anything that isn't the canonical server shape — plain prose mirrors like the
 * morning letter, or a non-accordion fence — passes through unchanged.
 */
internal fun expandCollapsedReportFence(content: String): String {
    val lines = content.split("\n")
    if (lines.size < 3 || lines.first().trim() != "```deneb-ui") return content
    var close = -1
    for (i in 1 until lines.size) {
        if (lines[i].trim() == "```") {
            close = i
            break
        }
    }
    if (close <= 0) return content
    val payload = lines.subList(1, close).joinToString("\n")
    val root = runCatching { SharedJson.parseToJsonElement(payload) }.getOrNull() as? JsonObject
        ?: return content
    if (root["type"]?.jsonPrimitive?.contentOrNull != "accordion") return content
    val expanded = JsonObject(root + ("expanded" to JsonPrimitive(true)))
    val rebuilt = buildString {
        append(lines.first())
        append('\n')
        append(SharedJson.encodeToString(JsonObject.serializer(), expanded))
        append('\n')
        append(lines.subList(close, lines.size).joinToString("\n"))
    }
    return rebuilt
}
