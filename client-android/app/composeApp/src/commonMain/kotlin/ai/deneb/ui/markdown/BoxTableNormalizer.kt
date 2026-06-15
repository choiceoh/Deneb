package ai.deneb.ui.markdown

// Box-drawing (ASCII-art) table → GitHub-markdown table normalizer.
//
// LLMs and pasted content sometimes draw tables with box-drawing characters
// (┌─┐ │ ├┼┤ └─┘ …). The markdown renderer can't lay those out, and with
// full-width CJK text the source columns are already misaligned, so even a
// monospace fallback looks broken. We rewrite such blocks into real markdown
// tables — cells taken from the │-delimited rows, border lines dropped,
// continuation lines merged into the row above — so the existing table renderer
// draws them cleanly. The model is also steered away from box tables in the
// system prompt; this is the defense-in-depth for pasted/legacy/slip cases.
//
// Markdown tables use the ASCII pipe `|` (0x7C); box tables use `│` (U+2502) and
// friends, so this never touches a genuine markdown table.

private const val VERTICALS = "│┃║" // cell delimiters in box tables
private val VERTICAL_SPLIT = Regex("[│┃║]")

// Border lines are made only of these horizontals/corners/junctions (+ verticals + spaces).
private const val BORDER_CHARS =
    "─━═┄┅┈┉╌╍" +
        "┌┐└┘├┤┬┴┼" +
        "╒╓╔╕╖╗╘╙╚╛╜╝╞╟╠╡╢╣╤╥╦╧╨╩╪╫╬" +
        "╴╵╶╷"

/**
 * Rewrite any box-drawing tables in [text] as markdown tables. Returns [text]
 * unchanged when it contains no box-table verticals (the common case).
 */
fun normalizeBoxTables(text: String): String {
    if (text.none { it in VERTICALS }) return text
    val lines = text.split("\n")
    val result = mutableListOf<String>()
    var i = 0
    while (i < lines.size) {
        val startsBlock = isDataLine(lines[i]) ||
            (isBorderLine(lines[i]) && i + 1 < lines.size && isDataLine(lines[i + 1]))
        if (startsBlock) {
            var j = i
            var dataCount = 0
            var borderCount = 0
            while (j < lines.size && (isDataLine(lines[j]) || isBorderLine(lines[j]))) {
                if (isDataLine(lines[j])) dataCount++ else borderCount++
                j++
            }
            // Require at least one bordered row so a lone │-bearing prose line
            // isn't mistaken for a table.
            if (dataCount >= 1 && borderCount >= 1) {
                result += convertBlock(lines.subList(i, j))
                i = j
                continue
            }
        }
        result += lines[i]
        i++
    }
    return result.joinToString("\n")
}

private fun isBorderLine(line: String): Boolean {
    val t = line.trim()
    if (t.isEmpty()) return false
    var hasBorder = false
    for (c in t) {
        if (c in BORDER_CHARS) {
            hasBorder = true
        } else if (c !in VERTICALS && c != ' ') {
            return false // a real character → not a pure border line
        }
    }
    return hasBorder
}

private fun isDataLine(line: String): Boolean {
    val t = line.trim()
    if (t.isEmpty() || t[0] !in VERTICALS) return false
    return t.count { it in VERTICALS } >= 2
}

private fun splitDataCells(line: String): List<String> {
    val cells = line.trim().split(VERTICAL_SPLIT).map { it.trim() }.toMutableList()
    // The leading/trailing empty come from the row starting/ending with a vertical.
    if (cells.isNotEmpty() && cells.first().isEmpty()) cells.removeAt(0)
    if (cells.isNotEmpty() && cells.last().isEmpty()) cells.removeAt(cells.size - 1)
    return cells
}

private fun convertBlock(block: List<String>): List<String> {
    val rows = mutableListOf<MutableList<String>>()
    for (line in block) {
        if (isBorderLine(line)) continue
        val cells = splitDataCells(line)
        if (cells.isEmpty()) continue
        // A row whose first cell is blank continues the row above (wrapped cell
        // text spread across physical lines) — append each non-blank cell there.
        val continuation = rows.isNotEmpty() && cells.first().isEmpty()
        if (continuation) {
            val prev = rows.last()
            for (k in cells.indices) {
                val v = cells[k]
                if (v.isEmpty()) continue
                while (prev.size <= k) prev.add("")
                prev[k] = if (prev[k].isEmpty()) v else prev[k] + " " + v
            }
        } else {
            rows += cells.toMutableList()
        }
    }
    if (rows.isEmpty()) return block // couldn't parse → leave the original lines
    val numCols = rows.maxOf { it.size }
    rows.forEach { while (it.size < numCols) it.add("") }

    fun esc(c: String) = c.replace("|", "\\|")
    val md = ArrayList<String>(rows.size + 1)
    md += "| " + rows[0].joinToString(" | ") { esc(it) } + " |"
    md += "| " + List(numCols) { "---" }.joinToString(" | ") + " |"
    for (r in 1 until rows.size) {
        md += "| " + rows[r].joinToString(" | ") { esc(it) } + " |"
    }
    return md
}
