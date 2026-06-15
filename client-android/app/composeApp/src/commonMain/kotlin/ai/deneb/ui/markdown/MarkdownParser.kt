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
    // Rewrite the messy table forms LLMs slip into chat into real markdown tables
    // before scanning: box-drawing (ASCII-art) tables → markdown, then bordered pipe
    // tables that are missing their `| --- |` delimiter row get one inserted. Box runs
    // first because its output already carries a delimiter (so the pipe pass skips it).
    val normalized = normalizePipeTables(normalizeBoxTables(text))
    return try {
        MarkdownDocument(BlockScanner.scan(normalized).toImmutableList())
    } catch (_: Throwable) {
        MarkdownDocument(persistentListOf(Paragraph(persistentListOf(Text(normalized)))))
    }
}
