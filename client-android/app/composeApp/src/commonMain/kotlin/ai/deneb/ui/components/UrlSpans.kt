package ai.deneb.ui.components

/**
 * Plain-text URL detection for non-markdown surfaces (the mail body view).
 *
 * The mail gateway deliberately renders HTML anchors as "label (https://…)" and
 * keeps bare links visible — for auth/CTA mails the link IS the content — so the
 * reading surface must make those URLs tappable. This is NOT markdown: `*`, `#`,
 * `-` in an email are prose, so the body never goes through the markdown parser;
 * only URLs get annotated. Email addresses intentionally stay plain text.
 *
 * The regex and trailing-punctuation trim mirror the markdown tokenizer's
 * autolink rules (InlineTokenizer.AUTOLINK_REGEX) so chat and mail agree on
 * what counts as a link.
 */
internal data class UrlSpan(
    val start: Int,
    /** Exclusive end index. */
    val end: Int,
    /** Destination with scheme (www. gains https://). */
    val url: String,
)

private val URL_REGEX = Regex("(?<![\\w/@.])(?:https?://|www\\.)[^\\s<>]+")

internal fun findUrlSpans(text: String): List<UrlSpan> {
    if ("http" !in text && "www." !in text) return emptyList()
    val spans = mutableListOf<UrlSpan>()
    for (m in URL_REGEX.findAll(text)) {
        // Trim trailing prose punctuation, and a ) only when unbalanced — so
        // "(see https://x.com)" drops the closer but wiki-style _(disambiguation)
        // keeps its paren. Same rule as the markdown autolink.
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
        val url = if (raw.startsWith("www.")) "https://$raw" else raw
        spans += UrlSpan(m.range.first, m.range.last + 1 - trimmed, url)
    }
    return spans
}
