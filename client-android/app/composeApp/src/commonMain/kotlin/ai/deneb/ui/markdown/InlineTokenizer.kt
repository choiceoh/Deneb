package ai.deneb.ui.markdown

import kotlinx.collections.immutable.ImmutableList
import kotlinx.collections.immutable.persistentListOf
import kotlinx.collections.immutable.toImmutableList

/**
 * Inline markdown tokenizer. Produces a flat list of [InlineNode]s from a string.
 *
 * Strategy: two-phase.
 *  1. Extract "atomic" inlines whose contents are not themselves re-parsed for emphasis:
 *     inline code, images, links, hard line breaks.
 *  2. Scan for emphasis / strong / strike pairs over the full text with atomic ranges
 *     masked out, so a delimiter pair may span across an atomic (e.g. `**foo `code` bar**`)
 *     while delimiter characters inside an atomic are ignored. Unpaired delimiters degrade
 *     to literal text.
 *
 * Link text is recursively parsed (link text can contain emphasis).
 * Image alt text is treated as a literal string (no nested inline parsing).
 */
internal object InlineTokenizer {

    private val CODE_REGEX = Regex("(?<!\\\\)(`+)([\\s\\S]+?)\\1")
    private val IMAGE_REGEX = Regex("(?<!\\\\)!\\[([^\\]]*)\\]\\(([^)]*)\\)")

    // The inner alternation must not let `[^\[\]]` consume `\` — otherwise `\X` has two ways
    // to match (one `\\.` iteration vs. two `[^…]` iterations), producing exponential
    // backtracking on Android's ICU regex engine when the surrounding `](url)` doesn't close.
    private val LINK_REGEX = Regex("(?<!\\\\)\\[((?:\\\\.|[^\\\\\\[\\]])*)\\]\\(([^)]*)\\)")

    // GFM-style bare autolink: http(s):// or www. runs the LLM emits as plain text (not wrapped
    // in []()). Stops at whitespace/<>; the opener can't sit right after a word char so it won't
    // bite into "ahttps". Trailing sentence punctuation and unbalanced ) are trimmed at use site.
    private val AUTOLINK_REGEX = Regex("(?<![\\w/@.])(?:https?://|www\\.)[^\\s<>]+")

    // CommonMark angle autolinks: <https://…>, <mailto:…>, <user@host.tld>. The whole range
    // including the brackets becomes the link node, so the <> never reaches the screen.
    private val ANGLE_AUTOLINK_REGEX = Regex("<(https?://[^\\s<>]+|mailto:[^\\s<>]+|[\\w.+-]+@[\\w-]+(?:\\.[\\w-]+)+)>")

    // Bare email runs — this is a mail-first assistant, so addresses appear in prose constantly.
    // The final label must be an alphabetic TLD (≥2 letters) so "node@18.0.0"-style package
    // versions don't become mailto links, and trailing "a@b.com." prose dots stay out.
    private val EMAIL_REGEX = Regex("(?<![\\w.+-])[\\w.+-]+@[\\w-]+(?:\\.[\\w-]+)*\\.[A-Za-z]{2,}")
    private val HARD_BREAK_REGEX = Regex(" {2,}\\n|\\\\\\n")

    // LLM-emitted literal <br>/<br/> — common in table cells where a real newline would split the row.
    private val HTML_BREAK_REGEX = Regex("<br\\s*/?>", RegexOption.IGNORE_CASE)

    // LLM-emitted semantic inline HTML. Paired tags only: b/strong/i/em map to real style
    // nodes, code/kbd to inline code, sub/sup to Unicode scripts (H<sub>2</sub>O → H₂O),
    // u/mark keep their content with the tags dropped. Unknown tags stay literal — they may
    // be generics ("List<T>") or prose. Closing tag must match the opener (backreference).
    private val HTML_INLINE_REGEX = Regex(
        "<(b|strong|i|em|s|del|strike|u|mark|code|kbd|sub|sup)>([\\s\\S]+?)</\\1>",
        RegexOption.IGNORE_CASE,
    )

    // LLM-emitted HTML anchor: <a href="url">text</a> → a real Link (other attributes
    // ignored). The href quote can be " or '. Empty link text falls back to the href.
    private val HTML_ANCHOR_REGEX = Regex(
        "<a\\s+[^>]*?href\\s*=\\s*[\"']([^\"']+)[\"'][^>]*>([\\s\\S]*?)</a>",
        RegexOption.IGNORE_CASE,
    )
    private val EMOJI_SHORTCODE_REGEX = Regex(":([a-zA-Z0-9_+-]+):")

    // Math delimiters. `$$…$$` and `\[…\]` are display-flavored but still accepted inline
    // as a fallback; block-level versions are promoted to [DisplayMath] by [BlockScanner].
    // `$…$` follows the KaTeX rule: opener not followed by whitespace, closer not preceded by
    // whitespace, and closer not followed by a digit (avoids `$5 – $3` currency false positives).
    private val MATH_DOUBLE_DOLLAR_REGEX = Regex("(?<!\\\\)\\$\\$([\\s\\S]+?)\\$\\$")
    private val MATH_DOLLAR_REGEX = Regex("(?<!\\\\)(?<!\\$)\\$(?!\\s)((?:\\\\.|[^\\\\$\\n])+?)(?<!\\s)\\$(?!\\d)(?!\\$)")
    private val MATH_PAREN_REGEX = Regex("\\\\\\(([\\s\\S]+?)\\\\\\)")
    private val MATH_BRACKET_REGEX = Regex("\\\\\\[([\\s\\S]+?)\\\\\\]")

    // `*`/`**` need the flanking guard the `_` patterns already get from their word-boundary
    // lookarounds: a non-space right after the opener and right before the closer. Without it
    // "단가는 3 * 4 * 5" makes "* 4 *" emphasis (the stars vanish, "4" goes italic). This is a
    // trimmed CommonMark left/right-flanking rule — it kills space-flanked false positives while
    // leaving "*italic*", "foo*bar*baz", and "**a `code` b**" intact.
    private val STRONG_STAR_REGEX = Regex("(?<!\\\\)\\*\\*(?=\\S)([\\s\\S]+?)(?<=\\S)\\*\\*")
    private val STRONG_UNDER_REGEX = Regex("(?<![A-Za-z0-9_\\\\])__([\\s\\S]+?)__(?![A-Za-z0-9_])")
    private val EMPH_STAR_REGEX = Regex("(?<!\\\\)\\*(?=\\S)([\\s\\S]+?)(?<=\\S)\\*")
    private val EMPH_UNDER_REGEX = Regex("(?<![A-Za-z0-9_\\\\])_([\\s\\S]+?)_(?![A-Za-z0-9_])")
    private val STRIKE_REGEX = Regex("(?<!\\\\)~~([\\s\\S]+?)~~")

    // `***both***` must resolve as one bold-italic span. Without this, STRONG_STAR eats the
    // first two stars and the inner/trailing `*` leak into the text as literals. Listed first:
    // on a tie at the same start offset the earlier pattern wins.
    private val TRIPLE_STAR_REGEX = Regex("(?<!\\\\)\\*\\*\\*(?=\\S)([\\s\\S]+?)(?<=\\S)\\*\\*\\*")

    private val EMPHASIS_PATTERNS: List<Pair<Regex, (ImmutableList<InlineNode>) -> InlineNode>> = listOf(
        TRIPLE_STAR_REGEX to { children -> Emphasis(persistentListOf(Strong(children))) },
        STRONG_STAR_REGEX to { children -> Strong(children) },
        STRONG_UNDER_REGEX to { children -> Strong(children) },
        EMPH_STAR_REGEX to { children -> Emphasis(children) },
        EMPH_UNDER_REGEX to { children -> Emphasis(children) },
        STRIKE_REGEX to { children -> Strike(children) },
    )

    private const val ATOMIC_MASK = ''

    private const val MAX_INLINE_DEPTH = 16
    private const val MAX_INLINE_INPUT = 100_000
    private const val MAX_DELIMITER_RUN = 64

    private val ESCAPABLE = setOf(
        '*', '_', '`', '\\', '[', ']', '(', ')', '!', '~', '#', '-', '+',
        '.', '<', '>', '{', '}', '"', '\'', '|',
    )

    fun tokenize(text: String): ImmutableList<InlineNode> {
        if (text.isEmpty()) return persistentListOf()
        if (text.length > MAX_INLINE_INPUT) return persistentListOf(Text(text))
        if (hasPathologicalRun(text)) return persistentListOf(Text(text))
        return try {
            parse(text, 0)
        } catch (_: Throwable) {
            persistentListOf(Text(text))
        }
    }

    private fun hasPathologicalRun(text: String): Boolean {
        var run = 0
        var last = ' '
        for (c in text) {
            if ((c == '*' || c == '_' || c == '~' || c == '`' || c == '[') && c == last) {
                run++
                if (run >= MAX_DELIMITER_RUN) return true
            } else {
                run = 1
                last = c
            }
        }
        return false
    }

    private fun parse(text: String, depth: Int): ImmutableList<InlineNode> {
        if (depth >= MAX_INLINE_DEPTH) return persistentListOf(Text(text))
        val atomics = findAtomics(text, depth)
        val masked = if (atomics.isEmpty()) text else buildMasked(text, atomics)
        return mergeAdjacentText(parseRange(text, masked, atomics, 0, text.length, depth)).toImmutableList()
    }

    private fun buildMasked(text: String, atomics: List<Pair<IntRange, InlineNode>>): String {
        val sb = StringBuilder(text)
        for ((range, _) in atomics) {
            for (i in range) sb[i] = ATOMIC_MASK
        }
        return sb.toString()
    }

    private fun parseRange(
        text: String,
        masked: String,
        atomics: List<Pair<IntRange, InlineNode>>,
        start: Int,
        end: Int,
        depth: Int,
    ): List<InlineNode> {
        if (start >= end) return emptyList()
        if (depth >= MAX_INLINE_DEPTH) return emitTextAndAtomics(text, atomics, start, end)

        val result = mutableListOf<InlineNode>()
        var cursor = start
        while (cursor < end) {
            // Earliest emphasis delimiter at/after cursor. Scanning `masked` from a start index
            // (not substring(cursor, end)) avoids an O(n) copy per step — the old version was
            // O(n²) in allocation on long answers. A match running past `end` is rejected: that
            // opener had no in-range closer, exactly what substring(cursor, end) would yield.
            var bestMatch: MatchResult? = null
            var bestWrap: ((ImmutableList<InlineNode>) -> InlineNode)? = null
            for ((regex, wrapper) in EMPHASIS_PATTERNS) {
                val m = regex.find(masked, cursor) ?: continue
                if (m.range.last + 1 > end) continue
                if (bestMatch == null || m.range.first < bestMatch.range.first) {
                    bestMatch = m
                    bestWrap = wrapper
                }
            }
            val match = bestMatch
            if (match == null) {
                result += emitTextAndAtomics(text, atomics, cursor, end)
                break
            }
            val delimLen = (match.value.length - match.groupValues[1].length) / 2
            val matchStart = match.range.first
            val matchEnd = match.range.last + 1
            val innerStart = matchStart + delimLen
            val innerEnd = matchEnd - delimLen
            if (matchStart > cursor) {
                result += emitTextAndAtomics(text, atomics, cursor, matchStart)
            }
            result += bestWrap!!(
                mergeAdjacentText(parseRange(text, masked, atomics, innerStart, innerEnd, depth + 1)).toImmutableList(),
            )
            cursor = matchEnd
        }
        return result
    }

    private fun emitTextAndAtomics(
        text: String,
        atomics: List<Pair<IntRange, InlineNode>>,
        start: Int,
        end: Int,
    ): List<InlineNode> {
        val result = mutableListOf<InlineNode>()
        var pos = start
        for ((range, node) in atomics) {
            if (range.last < start) continue
            if (range.first >= end) break
            if (range.first > pos) {
                result += Text(unescape(text.substring(pos, range.first)))
            }
            result += node
            pos = range.last + 1
        }
        if (pos < end) {
            result += Text(unescape(text.substring(pos, end)))
        }
        return result
    }

    private fun findAtomics(text: String, depth: Int): List<Pair<IntRange, InlineNode>> {
        val all = mutableListOf<Pair<IntRange, InlineNode>>()
        for (m in CODE_REGEX.findAll(text)) {
            val content = m.groupValues[2]
            val cleaned = if (content.length >= 2 && content.startsWith(' ') && content.endsWith(' ')) {
                content.substring(1, content.length - 1)
            } else {
                content
            }
            all += m.range to InlineCode(cleaned)
        }
        for (m in IMAGE_REGEX.findAll(text)) {
            all += m.range to Image(cleanHref(m.groupValues[2]), m.groupValues[1])
        }
        for (m in LINK_REGEX.findAll(text)) {
            val inner = parse(m.groupValues[1], depth + 1)
            all += m.range to Link(cleanHref(m.groupValues[2]), inner)
        }
        for (m in AUTOLINK_REGEX.findAll(text)) {
            // Trim trailing prose punctuation, and a ) only when it has no matching ( inside the
            // URL — so wiki links like ..._(disambiguation) keep their paren but "(see https://x.com)"
            // drops the closer. (Atomics that fall inside a [..](..) link are dropped by the
            // overlap pass below, so this never double-links a URL already in markdown form.)
            var raw = m.value
            var trimmed = 0
            while (raw.length > "https://".length) {
                val last = raw.last()
                val isProse = last in ".,;:!?\"'»」』。、…"
                val unbalancedParen = last == ')' && raw.count { it == '(' } < raw.count { it == ')' }
                if (isProse || unbalancedParen) {
                    raw = raw.dropLast(1)
                    trimmed++
                } else {
                    break
                }
            }
            if (raw == "www." || raw.endsWith("://")) continue
            val range = m.range.first..(m.range.last - trimmed)
            val href = if (raw.startsWith("www.")) "https://$raw" else raw
            all += range to Link(href, persistentListOf(Text(raw)))
        }
        for (m in HARD_BREAK_REGEX.findAll(text)) {
            all += m.range to LineBreak
        }
        if ('<' in text) {
            for (m in HTML_BREAK_REGEX.findAll(text)) {
                all += m.range to LineBreak
            }
            for (m in ANGLE_AUTOLINK_REGEX.findAll(text)) {
                val inner = m.groupValues[1]
                val href = if (inner.startsWith("http") || inner.startsWith("mailto:")) inner else "mailto:$inner"
                all += m.range to Link(href, persistentListOf(Text(inner.removePrefix("mailto:"))))
            }
            // Cap the paired-tag scan: thousands of unclosed "<b>" would make the lazy
            // closer search quadratic. Real prose has a handful of tags per paragraph.
            if (text.count { it == '<' } <= 256) {
                for (m in HTML_ANCHOR_REGEX.findAll(text)) {
                    val href = cleanHref(m.groupValues[1])
                    val inner = parse(m.groupValues[2], depth + 1)
                    all += m.range to Link(href, if (inner.isEmpty()) persistentListOf(Text(href)) else inner)
                }
                for (m in HTML_INLINE_REGEX.findAll(text)) {
                    val inner = m.groupValues[2]
                    val node: InlineNode = when (m.groupValues[1].lowercase()) {
                        "b", "strong" -> Strong(parse(inner, depth + 1))
                        "i", "em" -> Emphasis(parse(inner, depth + 1))
                        "s", "del", "strike" -> Strike(parse(inner, depth + 1))
                        "u" -> Underline(parse(inner, depth + 1))
                        "mark" -> Highlight(parse(inner, depth + 1))
                        "code", "kbd" -> InlineCode(inner)
                        "sub" -> scriptNode(inner, SUBSCRIPT_CHARS, depth) { Subscript(it) }
                        "sup" -> scriptNode(inner, SUPERSCRIPT_CHARS, depth) { Superscript(it) }
                        else -> Text(inner) // unknown paired tags: keep content, drop tags
                    }
                    all += m.range to node
                }
            }
        }
        if ('@' in text) {
            // Bare addresses. Ones inside a longer atomic (a [..](..) link, an autolinked URL's
            // userinfo/query) start later than that atomic and are dropped by the overlap pass.
            for (m in EMAIL_REGEX.findAll(text)) {
                all += m.range to Link("mailto:${m.value}", persistentListOf(Text(m.value)))
            }
        }
        // Streaming hot path: skip the four math scans when the text has no math sentinels.
        val mayHaveMath = '$' in text || '\\' in text
        if (mayHaveMath) {
            for (m in MATH_DOUBLE_DOLLAR_REGEX.findAll(text)) {
                all += m.range to InlineMath(m.groupValues[1].trim())
            }
            for (m in MATH_BRACKET_REGEX.findAll(text)) {
                all += m.range to InlineMath(m.groupValues[1].trim())
            }
            for (m in MATH_DOLLAR_REGEX.findAll(text)) {
                all += m.range to InlineMath(m.groupValues[1].trim())
            }
            for (m in MATH_PAREN_REGEX.findAll(text)) {
                all += m.range to InlineMath(m.groupValues[1].trim())
            }
        }

        all.sortWith(compareBy({ it.first.first }, { -(it.first.last - it.first.first) }))

        val result = mutableListOf<Pair<IntRange, InlineNode>>()
        var lastEnd = -1
        for (item in all) {
            if (item.first.first > lastEnd) {
                result += item
                lastEnd = item.first.last
            }
        }
        return result
    }

    private val SUPERSCRIPT_CHARS = mapOf(
        '0' to '⁰', '1' to '¹', '2' to '²', '3' to '³', '4' to '⁴',
        '5' to '⁵', '6' to '⁶', '7' to '⁷', '8' to '⁸', '9' to '⁹',
        '+' to '⁺', '-' to '⁻', '=' to '⁼', '(' to '⁽', ')' to '⁾', 'n' to 'ⁿ', 'i' to 'ⁱ',
    )

    private val SUBSCRIPT_CHARS = mapOf(
        '0' to '₀', '1' to '₁', '2' to '₂', '3' to '₃', '4' to '₄',
        '5' to '₅', '6' to '₆', '7' to '₇', '8' to '₈', '9' to '₉',
        '+' to '₊', '-' to '₋', '=' to '₌', '(' to '₍', ')' to '₎',
    )

    // `<sub>`/`<sup>`: prefer clean, selectable Unicode scripts when every char has one
    // ("m<sup>2</sup>" → "m²"); otherwise fall back to a real baseline-shifted node
    // ("5<sup>th</sup>" → raised "th") instead of dropping the script to plain text.
    private fun scriptNode(
        inner: String,
        map: Map<Char, Char>,
        depth: Int,
        wrap: (ImmutableList<InlineNode>) -> InlineNode,
    ): InlineNode {
        val unicode = convertScriptOrNull(inner, map)
        return if (unicode != null) Text(unicode) else wrap(parse(inner, depth + 1))
    }

    // Map every char to its Unicode script form, or null if any char has none.
    private fun convertScriptOrNull(inner: String, map: Map<Char, Char>): String? {
        val sb = StringBuilder(inner.length)
        for (c in inner) {
            sb.append(map[c] ?: return null)
        }
        return sb.toString()
    }

    // Normalize a raw link destination: drop a CommonMark <…> wrapper and a trailing
    // "title" / 'title' so `[text](url "tooltip")` links to the url, not the whole blob.
    private fun cleanHref(raw: String): String {
        var href = raw.trim()
        val space = href.indexOfFirst { it == ' ' || it == '\t' }
        if (space > 0) {
            val rest = href.substring(space).trim()
            val quoted = rest.length >= 2 &&
                ((rest.first() == '"' && rest.last() == '"') || (rest.first() == '\'' && rest.last() == '\''))
            if (quoted) href = href.substring(0, space)
        }
        if (href.length >= 2 && href.first() == '<' && href.last() == '>') {
            href = href.substring(1, href.length - 1)
        }
        return href
    }

    private fun unescape(text: String): String {
        val withoutEscapes = if ('\\' !in text) {
            text
        } else {
            val out = StringBuilder()
            var i = 0
            while (i < text.length) {
                val c = text[i]
                if (c == '\\' && i + 1 < text.length && text[i + 1] in ESCAPABLE) {
                    out.append(text[i + 1])
                    i += 2
                } else {
                    out.append(c)
                    i++
                }
            }
            out.toString()
        }
        val decoded = decodeEntities(withoutEscapes)
        if (':' !in decoded) return decoded
        return EMOJI_SHORTCODE_REGEX.replace(decoded) { m ->
            EMOJI_SHORTCODES[m.groupValues[1]] ?: m.value
        }
    }

    // HTML entities LLMs emit in prose (`AT&amp;T`, `&lt;tag&gt;`, `5&nbsp;kg`) plus numeric
    // forms. Only Text segments pass through here — inline code and autolinked URLs are
    // atomics and keep their raw `&`.
    private val ENTITY_REGEX = Regex("&(#\\d{1,7}|#[xX][0-9a-fA-F]{1,6}|[a-zA-Z][a-zA-Z0-9]{1,31});")

    private val NAMED_ENTITIES = mapOf(
        "amp" to "&", "lt" to "<", "gt" to ">", "quot" to "\"", "apos" to "'",
        "nbsp" to "\u00A0", "mdash" to "—", "ndash" to "–", "hellip" to "…",
        "middot" to "·", "bull" to "•", "times" to "×", "deg" to "°", "plusmn" to "±",
        "larr" to "←", "rarr" to "→", "le" to "≤", "ge" to "≥", "ne" to "≠",
        // Smart quotes + punctuation — very common in web/email-extracted text.
        "lsquo" to "‘", "rsquo" to "’", "ldquo" to "“", "rdquo" to "”",
        "sbquo" to "‚", "bdquo" to "„", "laquo" to "«", "raquo" to "»",
        "lsaquo" to "‹", "rsaquo" to "›", "dagger" to "†", "Dagger" to "‡",
        "permil" to "‰", "prime" to "′", "Prime" to "″",
        // Legal / currency symbols.
        "copy" to "©", "reg" to "®", "trade" to "™", "sect" to "§", "para" to "¶",
        "cent" to "¢", "euro" to "€", "pound" to "£", "yen" to "¥", "curren" to "¤", "micro" to "µ",
        // Math / fractions / superscripts / arrows.
        "divide" to "÷", "minus" to "−", "frac12" to "½", "frac14" to "¼", "frac34" to "¾",
        "sup1" to "¹", "sup2" to "²", "sup3" to "³", "infin" to "∞", "equiv" to "≡", "asymp" to "≈",
        "uarr" to "↑", "darr" to "↓", "harr" to "↔",
        // Wide spaces (collapse to a normal space for layout).
        "ensp" to " ", "emsp" to " ", "thinsp" to " ",
    )

    private fun decodeEntities(text: String): String {
        if ('&' !in text) return text
        return ENTITY_REGEX.replace(text) { m ->
            val body = m.groupValues[1]
            if (body.startsWith("#")) {
                val code = if (body.length > 1 && (body[1] == 'x' || body[1] == 'X')) {
                    body.substring(2).toIntOrNull(16)
                } else {
                    body.substring(1).toIntOrNull()
                }
                if (code != null && code in 0x20..0x10FFFF && code !in 0xD800..0xDFFF) {
                    codePointToString(code)
                } else {
                    m.value // out-of-range / control — keep the literal
                }
            } else {
                NAMED_ENTITIES[body] ?: m.value // unknown name — keep the literal
            }
        }
    }

    // Kotlin common has no Character.toChars; build the surrogate pair by hand for astral planes.
    private fun codePointToString(code: Int): String = if (code <= 0xFFFF) {
        code.toChar().toString()
    } else {
        val c = code - 0x10000
        charArrayOf((0xD800 + (c shr 10)).toChar(), (0xDC00 + (c and 0x3FF)).toChar()).concatToString()
    }

    private fun mergeAdjacentText(nodes: List<InlineNode>): List<InlineNode> {
        if (nodes.size < 2) return nodes
        val result = mutableListOf<InlineNode>()
        for (n in nodes) {
            val last = result.lastOrNull()
            if (n is Text && last is Text) {
                result[result.lastIndex] = Text(last.value + n.value)
            } else {
                result += n
            }
        }
        return result
    }
}
