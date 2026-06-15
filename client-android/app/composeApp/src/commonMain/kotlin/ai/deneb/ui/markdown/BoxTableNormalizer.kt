package ai.deneb.ui.markdown

// Box-drawing (ASCII-art) table → GitHub-markdown table normalizer.
//
// LLMs and pasted content sometimes draw tables with box-drawing characters
// (┌─┐ │ ├┼┤ └─┘, also rounded ╭╮╰╯). The markdown renderer can't lay those
// out, and with full-width CJK text the source columns are already misaligned,
// so even a monospace fallback looks broken. We rewrite such blocks into real
// markdown tables — cells taken from the │-delimited rows, border lines dropped,
// continuation lines merged into the row above — so the existing table renderer
// draws them cleanly. The model is also steered away from box tables in the
// system prompt; this is the defense-in-depth for pasted/legacy/slip cases.
//
// Markdown tables use the ASCII pipe `|` (0x7C); box tables use `│` (U+2502) and
// friends, so this never touches a genuine markdown table.
//
// Safety constraints (so it never corrupts non-table content):
//  - Fenced code blocks (``` / ~~~) are passed through untouched — a box table
//    in pasted terminal output stays literal.
//  - Only multi-column boxes (≥2 columns) with a border convert; a single-cell
//    box (a callout/diagram) is left as written.
//  - A consistent leading prefix (indentation and/or blockquote `>` markers) is
//    stripped before parsing and re-applied to the emitted rows, so an indented
//    (in a list) or quoted box table renders inside its container.

private const val VERTICALS = "│┃║" // cell delimiters in box tables
private val VERTICAL_SPLIT = Regex("[│┃║]")

// Border lines are made only of these horizontals/corners/junctions (+ verticals + spaces).
private const val BORDER_CHARS =
    "─━═┄┅┈┉╌╍" +
        "┌┐└┘├┤┬┴┼" +
        "╭╮╰╯" + // rounded corners
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
    var fenceOpen: Char? = null
    while (i < lines.size) {
        val line = lines[i]
        // Pass fenced code blocks through untouched (literal content).
        val fence = fenceChar(line)
        if (fenceOpen != null) {
            result += line
            if (fence == fenceOpen) fenceOpen = null
            i++
            continue
        }
        if (fence != null) {
            fenceOpen = fence
            result += line
            i++
            continue
        }

        // A box-table block: consecutive lines sharing the same leading prefix
        // (indent / blockquote markers) that, after the prefix, are data or
        // border lines.
        val prefix = blockPrefix(line)
        val content = line.substring(prefix.length)
        val startsBlock = isDataLine(content) ||
            (isBorderLine(content) && i + 1 < lines.size && isDataAfter(lines[i + 1], prefix))
        if (startsBlock) {
            var j = i
            var dataCount = 0
            var borderCount = 0
            var maxCols = 0
            val blockContents = mutableListOf<String>()
            while (j < lines.size && blockPrefix(lines[j]) == prefix) {
                val c = lines[j].substring(prefix.length)
                when {
                    isDataLine(c) -> {
                        dataCount++
                        maxCols = maxOf(maxCols, splitDataCells(c).size)
                    }

                    isBorderLine(c) -> borderCount++

                    else -> break
                }
                blockContents += c
                j++
            }
            // Convert only a real multi-column table — a border plus ≥2 columns.
            // A single-cell box is a callout/diagram, left as written.
            if (dataCount >= 1 && borderCount >= 1 && maxCols >= 2) {
                for (md in convertBlock(blockContents)) result += prefix + md
                i = j
                continue
            }
        }
        result += line
        i++
    }
    return result.joinToString("\n")
}

/** The fence marker char (`` ` `` or `~`) if [line] opens/closes a code fence, else null. */
private fun fenceChar(line: String): Char? {
    val t = line.trimStart()
    return when {
        t.startsWith("```") -> '`'
        t.startsWith("~~~") -> '~'
        else -> null
    }
}

/** Leading run of spaces, tabs, and blockquote `>` markers — the container
 *  prefix to strip before parsing and re-apply to emitted rows. */
private fun blockPrefix(line: String): String {
    var n = 0
    while (n < line.length && (line[n] == ' ' || line[n] == '\t' || line[n] == '>')) n++
    return line.substring(0, n)
}

private fun isDataAfter(line: String, prefix: String): Boolean = blockPrefix(line) == prefix && isDataLine(line.substring(prefix.length))

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
    // A blank-first-cell row continues the row above ONLY when no border line
    // separated them (wrapped cell text). A border means a new logical row, so a
    // genuine row with an intentionally blank first cell is preserved.
    var borderSinceRow = false
    for (line in block) {
        if (isBorderLine(line)) {
            borderSinceRow = true
            continue
        }
        val cells = splitDataCells(line)
        if (cells.isEmpty()) continue
        val continuation = rows.isNotEmpty() && !borderSinceRow && cells.first().isEmpty()
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
        borderSinceRow = false
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
