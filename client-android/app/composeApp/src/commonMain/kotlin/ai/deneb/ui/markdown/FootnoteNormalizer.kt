package ai.deneb.ui.markdown

// Footnote support — GFM `[^id]` references + `[^id]: definition` lines.
//
// LLMs occasionally emit footnotes in research-style answers. Without handling, the
// reference leaks as literal `[^1]` text and the `[^1]: …` definition lines clutter
// the body as stray paragraphs. We rewrite them in a pre-pass (the same family as the
// table normalizers): collect the definitions, number each footnote by order of first
// reference, replace each reference with a superscript marker (¹ ² …), and append a
// footnotes section (a `---` rule + one paragraph per note) at the end. No new AST
// node or renderer change is needed — the output is plain markdown the existing parser
// already renders, and the digit superscripts reuse the glyphs [InlineTokenizer] uses
// for `<sup>`.
//
// Scope (kept deliberately small for chat):
//  - Definitions are single-line: `[^id]: text` at the start of a line. Indented
//    multi-line continuation is not parsed (rare in chat) — only the definition line.
//  - References are rewritten outside fenced code blocks and inline-code spans, so a
//    `[^1]` shown literally in code stays literal.
//  - An undefined reference is left literal. If NO reference matches a definition the
//    input is returned untouched (never strip content on a non-match).

private val FOOTNOTE_DEF_REGEX = Regex("""^\s{0,3}\[\^([^\]\s]+)]:\s+(.+)$""")
private val FOOTNOTE_REF_REGEX = Regex("""\[\^([^\]\s]+)]""")

// Same fence grammar as BlockScanner.FENCE_REGEX (file-private name, no clash).
private val FOOTNOTE_FENCE_REGEX = Regex("""^(\s{0,3})(`{3,}|~{3,})\s*(.*?)\s*$""")

// Approximate inline code span: an opener backtick run, non-backtick content, then a
// closer run of the same length (the backreference). Good enough to skip `[^1]` shown
// literally inside `code`.
private val FOOTNOTE_CODE_SPAN = Regex("(`+)[^`]*?\\1")

private val FOOTNOTE_SUPERSCRIPTS = mapOf(
    '0' to '⁰', '1' to '¹', '2' to '²', '3' to '³', '4' to '⁴',
    '5' to '⁵', '6' to '⁶', '7' to '⁷', '8' to '⁸', '9' to '⁹',
)

/**
 * Rewrite GFM footnotes in [text] into a superscript marker plus a trailing notes
 * section. Returns [text] unchanged when it has no `[^` sequence (the common case) or
 * when no reference matches a definition.
 */
fun normalizeFootnotes(text: String): String {
    if ("[^" !in text) return text
    val lines = text.split("\n")

    // Phase 1: collect definitions outside fenced code; blank their lines (removing
    // them outright would merge the paragraphs around a definition).
    val defs = LinkedHashMap<String, String>()
    val body = ArrayList<String>(lines.size)
    var inFence = false
    var fenceCh = ' '
    var fenceLen = 0
    for (line in lines) {
        val fence = FOOTNOTE_FENCE_REGEX.matchEntire(line)
        if (inFence) {
            body += line
            if (fence != null && fenceClosesRun(fence, fenceCh, fenceLen)) inFence = false
            continue
        }
        if (fence != null) {
            inFence = true
            fenceCh = fence.groupValues[2][0]
            fenceLen = fence.groupValues[2].length
            body += line
            continue
        }
        val def = FOOTNOTE_DEF_REGEX.matchEntire(line)
        if (def != null) {
            defs[def.groupValues[1]] = def.groupValues[2].trim()
            body += ""
            continue
        }
        body += line
    }
    if (defs.isEmpty()) return text

    // Phase 2: replace references with superscript ordinals, assigning each id its
    // 1-based number on first reference (left-to-right, top-to-bottom).
    val order = LinkedHashMap<String, Int>()
    inFence = false
    fenceCh = ' '
    fenceLen = 0
    for (idx in body.indices) {
        val line = body[idx]
        val fence = FOOTNOTE_FENCE_REGEX.matchEntire(line)
        if (inFence) {
            if (fence != null && fenceClosesRun(fence, fenceCh, fenceLen)) inFence = false
            continue
        }
        if (fence != null) {
            inFence = true
            fenceCh = fence.groupValues[2][0]
            fenceLen = fence.groupValues[2].length
            continue
        }
        if ("[^" in line) body[idx] = replaceRefsOutsideCode(line, defs, order)
    }
    // No matched reference → leave the original input untouched (never strip content).
    if (order.isEmpty()) return text

    // Phase 3: append the footnotes section in ordinal order, one paragraph per note.
    val out = StringBuilder(body.joinToString("\n").trimEnd())
    out.append("\n\n---\n")
    for ((id, ord) in order.entries.sortedBy { it.value }) {
        out.append("\n").append(superscriptOrdinal(ord)).append(' ').append(defs[id]).append("\n")
    }
    return out.toString()
}

private fun fenceClosesRun(fence: MatchResult, openCh: Char, openLen: Int): Boolean {
    val run = fence.groupValues[2]
    return run[0] == openCh && run.length >= openLen && fence.groupValues[3].isBlank()
}

private fun replaceRefsOutsideCode(
    line: String,
    defs: Map<String, String>,
    order: LinkedHashMap<String, Int>,
): String {
    if ('`' !in line) return replaceRefs(line, defs, order)
    // Transform only the gaps between inline-code spans; code spans pass through.
    val sb = StringBuilder()
    var last = 0
    for (m in FOOTNOTE_CODE_SPAN.findAll(line)) {
        sb.append(replaceRefs(line.substring(last, m.range.first), defs, order))
        sb.append(m.value)
        last = m.range.last + 1
    }
    sb.append(replaceRefs(line.substring(last), defs, order))
    return sb.toString()
}

private fun replaceRefs(
    segment: String,
    defs: Map<String, String>,
    order: LinkedHashMap<String, Int>,
): String {
    if ("[^" !in segment) return segment
    return FOOTNOTE_REF_REGEX.replace(segment) { m ->
        val id = m.groupValues[1]
        if (id !in defs) {
            m.value // undefined reference → keep literal
        } else {
            superscriptOrdinal(order.getOrPut(id) { order.size + 1 })
        }
    }
}

private fun superscriptOrdinal(n: Int): String = n.toString().map { FOOTNOTE_SUPERSCRIPTS[it] ?: it }.joinToString("")
