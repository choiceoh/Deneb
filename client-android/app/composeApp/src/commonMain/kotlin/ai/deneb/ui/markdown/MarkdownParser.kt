package ai.deneb.ui.markdown

import kotlinx.collections.immutable.persistentListOf
import kotlinx.collections.immutable.toImmutableList

/**
 * Parse markdown text into a [MarkdownDocument].
 *
 * The parser targets the subset of CommonMark / GFM that LLM chat output actually uses; see
 * [BlockScanner] for the block-level scope and [InlineTokenizer] for the inline scope.
 *
 * Robust to streaming input: unclosed code fences, unterminated emphasis, or partial links
 * degrade to their nearest sensible rendering instead of throwing. The returned document is
 * always well-formed and renderable.
 */
fun parseMarkdown(text: String): MarkdownDocument {
    if (text.isEmpty()) return MarkdownDocument(persistentListOf())
    // Rewrite box-drawing (ASCII-art) tables into markdown tables first, so the
    // block scanner renders them as real tables instead of broken monospace.
    val normalized = normalizeBoxTables(text)
    return try {
        MarkdownDocument(BlockScanner.scan(normalized).toImmutableList())
    } catch (_: Throwable) {
        MarkdownDocument(persistentListOf(Paragraph(persistentListOf(Text(normalized)))))
    }
}
