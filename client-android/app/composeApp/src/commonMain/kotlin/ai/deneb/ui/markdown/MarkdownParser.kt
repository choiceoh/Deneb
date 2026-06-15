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
    // Pre-passes that rewrite messy LLM output into plain markdown before scanning:
    //  1. footnotes — `[^1]` → superscript marker + a trailing notes section.
    //  2. block HTML (<h1-6>, <hr>, <p>, <ul>/<ol>/<li>, <blockquote>) → markdown.
    //  3. box-drawing (ASCII-art) tables → markdown tables.
    //  4. bordered pipe tables missing their `| --- |` delimiter row get one inserted.
    // Footnotes run first so its appended notes flow through the later passes; HTML runs
    // before the table passes so any markdown it emits is then table-normalized; box runs
    // before pipe because box output already carries a delimiter (so the pipe pass skips it).
    val normalized = normalizePipeTables(normalizeBoxTables(normalizeHtmlBlocks(normalizeFootnotes(text))))
    return try {
        MarkdownDocument(BlockScanner.scan(normalized).toImmutableList())
    } catch (_: Throwable) {
        MarkdownDocument(persistentListOf(Paragraph(persistentListOf(Text(normalized)))))
    }
}
