package ai.deneb.deneb

/**
 * Parse the groupware ERP reader's text snapshot (summary lines, `라벨:` section
 * headers, `N. 제목 · 메타 · …` rows) into blocks the screen renders natively —
 * a text dump is unreadable on a phone. Unknown shapes yield no rows and the
 * caller falls back to markdown.
 */
sealed interface ErpBlock {
    data class Summary(val lines: List<String>) : ErpBlock

    data class Section(val label: String) : ErpBlock

    /** [refId] carries a hidden `id=` segment (board post id) for open-on-tap. */
    data class Row(val index: Int, val title: String, val meta: String, val refId: String = "") : ErpBlock
}

private val erpRowRegex = Regex("""^\s*(\d+)\.\s+(.+)$""")

fun parseErpSnapshot(raw: String): List<ErpBlock> {
    val text = raw.replace("\r\n", "\n").trim()
    if (text.isEmpty()) return emptyList()

    val blocks = ArrayList<ErpBlock>()
    val summary = ArrayList<String>()

    fun flushSummary() {
        if (summary.isNotEmpty()) {
            blocks += ErpBlock.Summary(summary.toList())
            summary.clear()
        }
    }

    for (line in text.lineSequence()) {
        val t = line.trim()
        if (t.isEmpty()) continue
        val row = erpRowRegex.matchEntire(t)
        if (row != null) {
            flushSummary()
            val segs = row.groupValues[2].split(" · ")
            val title = segs.first().trim()
            // Internal ids stay out of the display but ride along as refId so a
            // board row can open its post.
            val rest = segs.drop(1).map(String::trim)
            val refId = rest.firstOrNull { it.startsWith("id=") }?.removePrefix("id=").orEmpty()
            val meta = rest.filterNot { it.startsWith("id=") }
            blocks += ErpBlock.Row(row.groupValues[1].toInt(), title, meta.joinToString(" · "), refId)
            continue
        }
        if (t.endsWith(":")) {
            flushSummary()
            blocks += ErpBlock.Section(t.removeSuffix(":").trim())
            continue
        }
        summary += t
    }
    flushSummary()
    return if (blocks.any { it is ErpBlock.Row }) blocks else emptyList()
}
