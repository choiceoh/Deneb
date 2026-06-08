package com.inspiredandroid.kai.ui.markdown

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
    private val HARD_BREAK_REGEX = Regex(" {2,}\\n|\\\\\\n")
    // LLM-emitted literal <br>/<br/> — common in table cells where a real newline would split the row.
    private val HTML_BREAK_REGEX = Regex("<br\\s*/?>", RegexOption.IGNORE_CASE)
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

    private val EMPHASIS_PATTERNS: List<Pair<Regex, (ImmutableList<InlineNode>) -> InlineNode>> = listOf(
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
            all += m.range to Image(m.groupValues[2].trim(), m.groupValues[1])
        }
        for (m in LINK_REGEX.findAll(text)) {
            val inner = parse(m.groupValues[1], depth + 1)
            all += m.range to Link(m.groupValues[2].trim(), inner)
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
        if (':' !in withoutEscapes) return withoutEscapes
        return EMOJI_SHORTCODE_REGEX.replace(withoutEscapes) { m ->
            EMOJI_SHORTCODES[m.groupValues[1]] ?: m.value
        }
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
