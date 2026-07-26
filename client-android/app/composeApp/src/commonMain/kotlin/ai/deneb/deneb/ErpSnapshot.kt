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

/** "레이블: 값" — the shape every ERP summary line uses (기간·건수·원본행 …). */
private val erpPairRegex = Regex("""^[^:]{1,20}:\s+\S.*$""")

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
    return if (looksLikeSnapshot(blocks)) blocks else emptyList()
}

/**
 * Requiring a Row threw away perfectly good parses: 매출 is a title plus three
 * "레이블: 값" lines and NOTHING else, so it fell to the markdown fallback —
 * which joins consecutive lines into one paragraph, costing the screen its
 * summary card entirely (measured on the live app 2026-07-26: 재고 rendered a
 * card, 매출 rendered four bare lines).
 *
 * A snapshot is recognized when anything ERP-shaped came out: a row, a section
 * header, or summary lines in "레이블: 값" form. Genuinely unfamiliar text still
 * yields nothing and still falls back, which is what the guard was for.
 */
private fun looksLikeSnapshot(blocks: List<ErpBlock>): Boolean {
    if (blocks.any { it is ErpBlock.Row || it is ErpBlock.Section }) return true
    return blocks.filterIsInstance<ErpBlock.Summary>().any { summary ->
        // Skip the title: it is a heading, not a labelled figure.
        summary.lines.drop(1).any(erpPairRegex::matches)
    }
}
