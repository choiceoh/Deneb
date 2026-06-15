package ai.deneb.ui.markdown

// Block-level HTML → markdown normalizer (defense-in-depth).
//
// Almost all HTML is converted to markdown upstream (the gateway turns web/email content
// into markdown) and the system prompt tells the model to output markdown, so block HTML
// rarely reaches the renderer. But the model occasionally slips and emits a block tag;
// BlockScanner doesn't parse HTML blocks, so those would surface as literal tags. This
// pre-pass rewrites the common, mechanically-safe block forms into markdown so the
// existing parser draws them:
//   <h1>..</h1> … <h6>  → #..######      <hr> → ---        <p>x</p> → x
//   <ul>/<ol>/<li>      → - / N. (nested by depth)         <blockquote> → >
//
// Deliberately NOT handled (complexity/risk far above their frequency): <table>, <pre>,
// <code> blocks, <div>. Those stay literal — a rare, separate ask if it ever shows up.
//
// Fenced code blocks pass through untouched (same fence grammar as BlockScanner), so an
// HTML example inside ``` stays literal. Conversions are line-anchored (the tag owns the
// whole line) so inline `<p>`/`<li>` inside prose or code spans are never touched.

private val HB_FENCE_REGEX = Regex("""^(\s{0,3})(`{3,}|~{3,})\s*(.*?)\s*$""")
private val HB_HR_REGEX = Regex("""^\s*<hr\s*/?>\s*$""", RegexOption.IGNORE_CASE)
private val HB_HEADING_REGEX = Regex("""^\s*<h([1-6])(?:\s[^>]*)?>([\s\S]*?)</h\1>\s*$""", RegexOption.IGNORE_CASE)
private val HB_PARA_INLINE_REGEX = Regex("""^\s*<p(?:\s[^>]*)?>([\s\S]*?)</p>\s*$""", RegexOption.IGNORE_CASE)
private val HB_PARA_TAG_REGEX = Regex("""^\s*</?p(?:\s[^>]*)?>\s*$""", RegexOption.IGNORE_CASE)
private val HB_UL_OPEN_REGEX = Regex("""^\s*<ul(?:\s[^>]*)?>\s*$""", RegexOption.IGNORE_CASE)
private val HB_OL_OPEN_REGEX = Regex("""^\s*<ol(?:\s[^>]*)?>\s*$""", RegexOption.IGNORE_CASE)
private val HB_LIST_CLOSE_REGEX = Regex("""^\s*</(?:ul|ol)>\s*$""", RegexOption.IGNORE_CASE)
private val HB_LI_REGEX = Regex("""^\s*<li(?:\s[^>]*)?>([\s\S]*?)</li>\s*$""", RegexOption.IGNORE_CASE)
private val HB_OL_START_REGEX = Regex("""start\s*=\s*["']?(\d+)""", RegexOption.IGNORE_CASE)
private val HB_BQ_INLINE_REGEX = Regex("""^\s*<blockquote(?:\s[^>]*)?>([\s\S]*?)</blockquote>\s*$""", RegexOption.IGNORE_CASE)
private val HB_BQ_OPEN_REGEX = Regex("""^\s*<blockquote(?:\s[^>]*)?>\s*$""", RegexOption.IGNORE_CASE)
private val HB_BQ_CLOSE_REGEX = Regex("""^\s*</blockquote>\s*$""", RegexOption.IGNORE_CASE)

/**
 * Rewrite common block-level HTML in [text] as markdown. Returns [text] unchanged when
 * it has no `<` (the common case).
 */
fun normalizeHtmlBlocks(text: String): String {
    if ('<' !in text) return text
    val lines = text.split("\n")
    val out = ArrayList<String>(lines.size + 4)
    var inFence = false
    var fenceCh = ' '
    var fenceLen = 0
    // List nesting: each frame is the next number for an <ol>, or null for a <ul>.
    val listStack = ArrayDeque<Int?>()
    var bqDepth = 0 // open <blockquote> nesting

    for (line in lines) {
        val fence = HB_FENCE_REGEX.matchEntire(line)
        if (inFence) {
            out += line
            if (fence != null && fence.groupValues[2][0] == fenceCh &&
                fence.groupValues[2].length >= fenceLen && fence.groupValues[3].isBlank()
            ) {
                inFence = false
            }
            continue
        }
        if (fence != null) {
            inFence = true
            fenceCh = fence.groupValues[2][0]
            fenceLen = fence.groupValues[2].length
            out += line
            continue
        }

        val q = "> ".repeat(bqDepth) // blockquote prefix for emitted lines

        if (HB_BQ_OPEN_REGEX.matches(line)) {
            bqDepth++
            continue
        }
        if (HB_BQ_CLOSE_REGEX.matches(line)) {
            if (bqDepth > 0) bqDepth--
            continue
        }
        val bqInline = HB_BQ_INLINE_REGEX.matchEntire(line)
        if (bqInline != null) {
            out += "$q> ${bqInline.groupValues[1].trim()}"
            continue
        }

        if (HB_HR_REGEX.matches(line)) {
            out += "$q---"
            continue
        }

        val heading = HB_HEADING_REGEX.matchEntire(line)
        if (heading != null) {
            out += q + "#".repeat(heading.groupValues[1].toInt()) + " " + heading.groupValues[2].trim()
            continue
        }

        if (HB_UL_OPEN_REGEX.matches(line)) {
            listStack.addLast(null)
            continue
        }
        if (HB_OL_OPEN_REGEX.matches(line)) {
            listStack.addLast(HB_OL_START_REGEX.find(line)?.groupValues?.get(1)?.toIntOrNull() ?: 1)
            continue
        }
        if (HB_LIST_CLOSE_REGEX.matches(line)) {
            if (listStack.isNotEmpty()) listStack.removeLast()
            continue
        }
        val li = HB_LI_REGEX.matchEntire(line)
        if (li != null) {
            val indent = "  ".repeat(maxOf(0, listStack.size - 1))
            val frame = listStack.lastOrNull()
            val marker = if (frame == null) {
                "- "
            } else {
                listStack[listStack.size - 1] = frame + 1
                "$frame. "
            }
            out += "$q$indent$marker${li.groupValues[1].trim()}"
            continue
        }

        val para = HB_PARA_INLINE_REGEX.matchEntire(line)
        if (para != null) {
            out += q + para.groupValues[1].trim()
            if (bqDepth == 0) out += "" // separate stacked <p> paragraphs (not inside a quote)
            continue
        }
        if (HB_PARA_TAG_REGEX.matches(line)) continue // standalone <p> / </p> → drop

        out += line
    }
    return out.joinToString("\n")
}
