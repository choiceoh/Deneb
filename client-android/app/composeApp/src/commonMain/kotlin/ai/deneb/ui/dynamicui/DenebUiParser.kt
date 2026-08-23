package ai.deneb.ui.dynamicui

import ai.deneb.DenebLog
import ai.deneb.data.SharedJson
import kotlinx.collections.immutable.toImmutableList

/**
 * Decodes the body of a `deneb-ui` fenced block into a [DenebUiNode].
 *
 * Two wire formats share the fence:
 *  - **Labeled HTML (v2 — the authoring format since 2026-07)**: bodies whose
 *    first character is `<`, parsed by [DenebUiHtml]. Models have pre-trained
 *    HTML fluency, and EOF auto-close absorbs stream truncation, so this path
 *    needs no repair heuristics. Grammar: docs/research/deneb-ui-html.md.
 *  - **Legacy JSON (display-only)**: `{`/`[` bodies from pre-switch transcripts,
 *    parsed strictly — a single object, or NDJSON (one object per line, wrapped
 *    in a column). The old multi-stage syntax-repair pipeline (broken-key fix,
 *    brace sanitizing, truncation salvage) was deleted with the format switch;
 *    invalid legacy JSON now degrades to a code block instead of heuristic
 *    reconstruction.
 */
object DenebUiParser {

    /** Result of decoding a deneb-ui fence body; consumed by the markdown parser. */
    sealed interface UiBlockResult {
        /** [rawJson] carries the raw fence body of either wire format (field name is historical). */
        data class Ui(val node: DenebUiNode, val rawJson: String) : UiBlockResult
        data class Error(val rawJson: String) : UiBlockResult
    }

    /**
     * Interactive UI mode requires the entire reply to be one deneb-ui block, but models
     * sometimes omit the deneb-ui fence and emit the markup bare. If [content] is fenceless
     * HTML (or legacy JSON), wrap it in a deneb-ui fence so the markdown scanner recognizes
     * it as a UI block. No-op when the content is already fenced or isn't markup. Shared by
     * the renderer and the retry/no-valid-ui check so both agree on what counts as a valid UI.
     */
    fun wrapBareDenebUiContent(content: String): String {
        val trimmed = content.trim()
        if (trimmed.startsWith("```")) return content
        if (trimmed.startsWith("<") || trimmed.startsWith("{") || trimmed.startsWith("[")) {
            return "```deneb-ui\n$trimmed\n```"
        }
        return content
    }

    /**
     * Decode the raw body of a deneb-ui fence (everything between the opening and closing
     * triple backticks). Returns either a decoded [DenebUiNode] or an [UiBlockResult.Error]
     * carrying the raw body so callers can display it as a code block.
     */
    fun parseUiBlockBody(rawBlock: String): UiBlockResult? {
        val body = rawBlock.trim()
        if (body.isEmpty()) return null

        if (body.startsWith("<")) {
            val node = DenebUiHtml.parse(body)
            return if (node != null) {
                UiBlockResult.Ui(node, body)
            } else {
                DenebLog.warn("DenebUi", "html parse produced no nodes | ${bodyDigest(body)}")
                UiBlockResult.Error(body)
            }
        }

        // Legacy JSON (strict; display compatibility for old transcripts).
        val lines = body.lines().map { it.trim() }.filter { it.isNotEmpty() }
        if (lines.size > 1 && lines.all { it.startsWith("{") }) {
            val children = lines.mapNotNull { tryParseLine(it) }
            return if (children.isNotEmpty()) {
                UiBlockResult.Ui(ColumnNode(children = children.toImmutableList()), body)
            } else {
                UiBlockResult.Error(body)
            }
        }
        return try {
            parseSingleNode(body)?.let { UiBlockResult.Ui(it, body) } ?: UiBlockResult.Error(body)
        } catch (e: Exception) {
            DenebLog.warn("DenebUi", "legacy json parse error: ${e.message} | ${bodyDigest(body)}")
            UiBlockResult.Error(body)
        }
    }

    /**
     * What a failed card looked like, without printing what it said.
     *
     * These warnings fire in release too, and the body is assistant output about the
     * user's own mail, deals and people — 500 characters of it went to Logcat, where
     * any app with log access on an older device could read it. Shape and size are
     * what actually diagnose a parse failure.
     */
    private fun bodyDigest(body: String): String = "len=${body.length} head=${body.take(24).replace('\n', ' ')}"

    /** Try to parse a single NDJSON line, dropping lines that don't parse. */
    private fun tryParseLine(line: String): DenebUiNode? = runCatching { parseSingleNode(line) }.getOrNull()
        ?: run {
            DenebLog.warn("DenebUi", "legacy ndjson line failed to parse | ${bodyDigest(line)}")
            null
        }

    /** Parse a strict JSON string into a [DenebUiNode] via the direct builder pipeline. */
    private fun parseSingleNode(json: String): DenebUiNode? = parseNode(SharedJson.parseToJsonElement(json))
}
