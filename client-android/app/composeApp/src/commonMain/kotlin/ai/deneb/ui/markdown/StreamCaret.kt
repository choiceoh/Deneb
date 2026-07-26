package ai.deneb.ui.markdown

import androidx.compose.runtime.compositionLocalOf
import kotlinx.collections.immutable.ImmutableList

/**
 * The inline run that carries the streaming caret, or null when none should.
 *
 * The caret used to be a `▍` APPENDED TO THE PARSE SOURCE — a glyph of ours flowing
 * through the markdown parser, so it had to be withheld inside an unclosed fence (it
 * would read as code content) and pushed onto its own line right after a fence closed
 * (```` ```▍ ```` scans as a NEW opener with info-string `▍`, re-opening the block that
 * just closed) by line-toggle logic whose own comment called it "an approximation of
 * CommonMark".
 *
 * Now the caret is DRAWN from the text layout. Nothing is injected into the parse
 * source, so there is nothing left to corrupt and nothing to defend against.
 * [InlineContent] identity-compares its own inlines against this value — which is why
 * every inline surface (paragraph, list item, heading, quote) gets the caret without a
 * flag threaded through each block renderer.
 */
internal val LocalStreamCaretTail = compositionLocalOf<ImmutableList<InlineNode>?> { null }

/**
 * The inline run at the document's frontier: the tail of the LAST block, recursing into
 * containers (quote, list item).
 *
 * Only the last block is considered, deliberately. When the frontier is a block that
 * cannot host an inline caret — a code fence mid-stream, a table, display math, a
 * deneb-ui form — the answer is null and that sample tick shows no caret. Falling back
 * to an earlier paragraph would park the cursor in the MIDDLE of the message, which
 * reads as a stalled stream rather than a flowing one.
 */
internal fun MarkdownDocument.streamCaretTail(): ImmutableList<InlineNode>? = tailInlines(blocks)

private fun tailInlines(blocks: List<BlockNode>): ImmutableList<InlineNode>? = when (val last = blocks.lastOrNull()) {
    is Paragraph -> last.inlines.takeIf { it.isNotEmpty() }

    is Heading -> last.inlines.takeIf { it.isNotEmpty() }

    is Blockquote -> tailInlines(last.children)

    // A collapsed <details> simply never composes its children, so no caret is drawn;
    // no need to read the open state here.
    is Collapsible -> tailInlines(last.children)

    is BulletList -> last.items.lastOrNull()?.let { tailInlines(it.children) }

    is OrderedList -> last.items.lastOrNull()?.let { tailInlines(it.children) }

    else -> null
}
