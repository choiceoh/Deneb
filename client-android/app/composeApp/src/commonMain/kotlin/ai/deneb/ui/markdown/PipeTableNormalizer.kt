package ai.deneb.ui.markdown

// Separator-less pipe-table recovery.
//
// GFM only renders a pipe table when a delimiter row (`| --- | --- |`) follows the
// header. LLMs frequently emit a "table" as a header plus data rows with the pipes
// but WITHOUT that delimiter row, so [BlockScanner] reads the block as a paragraph
// and the user sees raw `| a | b |` pipes instead of a table. We detect such a
// block — consecutive bordered pipe rows, same column count, no delimiter present —
// and insert the missing delimiter so the existing table parser draws it. This is
// the same defense-in-depth as [normalizeBoxTables]: clean up the messy table forms
// LLMs slip into chat.
//
// Safety constraints (so it never corrupts prose):
//  - Only the canonical BORDERED form converts: every row must start and end with an
//    unescaped `|`. A borderless `apples | oranges` line is indistinguishable from
//    prose with no delimiter to confirm it, so it is left alone.
//  - The block needs ≥2 rows and ≥2 columns with a consistent column count.
//  - A block that already contains a delimiter row is left untouched — it is a real
//    table (or a malformed one) that BlockScanner already owns.
//  - Fenced code blocks pass through, tracked with the same fence grammar as
//    BlockScanner; a leading prefix (indent and/or blockquote `>`) is stripped before
//    detection and re-applied to the inserted delimiter so quoted/indented tables work.

// Same fence grammar as BlockScanner.FENCE_REGEX (file-private, so the identical name
// in BoxTableNormalizer.kt does not clash).
private val PIPE_FENCE_REGEX = Regex("""^(\s{0,3})(`{3,}|~{3,})\s*(.*?)\s*$""")

// Same delimiter grammar as BlockScanner.TABLE_SEPARATOR_REGEX.
private val PIPE_SEPARATOR_REGEX = Regex("""^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)+\|?\s*$""")

/**
 * Insert the missing delimiter row into any bordered pipe table in [text] that lacks
 * one. Returns [text] unchanged when it contains no `|` (the common case).
 */
fun normalizePipeTables(text: String): String {
    if ('|' !in text) return text
    val lines = text.split("\n")
    val result = ArrayList<String>(lines.size + 4)
    var i = 0
    var inFence = false
    var fenceCh = ' '
    var fenceLen = 0
    while (i < lines.size) {
        val line = lines[i]
        val prefix = pipePrefix(line)
        val content = line.substring(prefix.length)
        val fence = PIPE_FENCE_REGEX.matchEntire(content)

        // Pass fenced code through untouched (close only on a same-char run at least as
        // long as the opener with a blank info string — the CommonMark rule).
        if (inFence) {
            result += line
            if (fence != null) {
                val run = fence.groupValues[2]
                if (run[0] == fenceCh && run.length >= fenceLen && fence.groupValues[3].isBlank()) {
                    inFence = false
                }
            }
            i++
            continue
        }
        if (fence != null) {
            inFence = true
            fenceCh = fence.groupValues[2][0]
            fenceLen = fence.groupValues[2].length
            result += line
            i++
            continue
        }

        // A run of same-prefix bordered pipe rows is a table candidate.
        if (isBorderedPipeRow(content)) {
            val cols = cellCount(content)
            var j = i
            var hasSeparator = false
            var consistent = true
            val run = ArrayList<String>()
            while (j < lines.size && pipePrefix(lines[j]) == prefix) {
                val c = lines[j].substring(prefix.length)
                if (!isBorderedPipeRow(c)) break
                if (PIPE_SEPARATOR_REGEX.matchEntire(c) != null) hasSeparator = true
                if (cellCount(c) != cols) consistent = false
                run += lines[j]
                j++
            }
            // Convert only a real table the parser would otherwise miss: ≥2 rows, ≥2
            // columns, a consistent width, and no delimiter already present (a present
            // delimiter means BlockScanner already handles it).
            if (run.size >= 2 && cols >= 2 && consistent && !hasSeparator) {
                result += run[0]
                result += prefix + "| " + List(cols) { "---" }.joinToString(" | ") + " |"
                for (k in 1 until run.size) result += run[k]
                i = j
                continue
            }
            // Not a table — emit the scanned run verbatim and resume after it, so the
            // pipe rows are not re-scanned line by line.
            for (l in run) result += l
            i = j
            continue
        }

        result += line
        i++
    }
    return result.joinToString("\n")
}

/** Leading run of spaces, tabs, and blockquote `>` markers to strip before detection
 *  and re-apply to the inserted delimiter. */
private fun pipePrefix(line: String): String {
    var n = 0
    while (n < line.length && (line[n] == ' ' || line[n] == '\t' || line[n] == '>')) n++
    return line.substring(0, n)
}

/** A canonical bordered pipe row: trimmed, starts and ends with an unescaped `|`, and
 *  yields ≥2 cells. The bordered form is the safe signal — a borderless `a | b` is
 *  indistinguishable from prose with no delimiter to confirm it. */
private fun isBorderedPipeRow(content: String): Boolean {
    val t = content.trim()
    if (t.length < 3 || t.first() != '|' || t.last() != '|' || t.endsWith("\\|")) return false
    return cellCount(content) >= 2
}

/** Cell count of a row, mirroring BlockScanner.splitRow: drop the border pipes, then
 *  count unescaped `|` separators (cells = separators + 1). */
private fun cellCount(content: String): Int {
    var s = content.trim()
    if (s.startsWith("|")) s = s.substring(1)
    if (s.endsWith("|") && !s.endsWith("\\|")) s = s.substring(0, s.length - 1)
    var cells = 1
    var i = 0
    while (i < s.length) {
        val c = s[i]
        if (c == '\\' && i + 1 < s.length && s[i + 1] == '|') {
            i += 2
            continue
        }
        if (c == '|') cells++
        i++
    }
    return cells
}
