package ai.deneb.ui.markdown

import ai.deneb.ui.dynamicui.DenebUiNode
import androidx.compose.runtime.Immutable
import kotlinx.collections.immutable.ImmutableList

@Immutable
data class MarkdownDocument(val blocks: ImmutableList<BlockNode>)

@Immutable
sealed interface BlockNode

@Immutable
data class Heading(val level: Int, val inlines: ImmutableList<InlineNode>) : BlockNode

@Immutable
data class Paragraph(val inlines: ImmutableList<InlineNode>) : BlockNode

@Immutable
data class CodeFence(
    val language: String?,
    val code: String,
    val closed: Boolean,
) : BlockNode

@Immutable
data class Blockquote(val children: ImmutableList<BlockNode>) : BlockNode

@Immutable
data class BulletList(val items: ImmutableList<ListItem>, val tight: Boolean) : BlockNode

@Immutable
data class OrderedList(
    val start: Int,
    val items: ImmutableList<ListItem>,
    val tight: Boolean,
) : BlockNode

@Immutable
data class ListItem(
    val children: ImmutableList<BlockNode>,
    // GFM task marker: null = ordinary item, true/false = checked/unchecked checkbox. The
    // `[ ]`/`[x]` prefix is stripped from [children] by the parser, so renderers just read this.
    val checked: Boolean? = null,
)

@Immutable
data class Table(
    val headers: ImmutableList<ImmutableList<InlineNode>>,
    val alignments: ImmutableList<ColumnAlign>,
    val rows: ImmutableList<ImmutableList<ImmutableList<InlineNode>>>,
) : BlockNode

enum class ColumnAlign { LEFT, CENTER, RIGHT, NONE }

@Immutable
data object HorizontalRule : BlockNode

@Immutable
data class DisplayMath(val latex: String) : BlockNode

@Immutable
data class DenebUiBlock(val node: DenebUiNode, val rawJson: String) : BlockNode

@Immutable
data class DenebUiError(val rawJson: String) : BlockNode

// A deneb-ui fence whose closing ``` hasn't arrived yet. While the message is
// still streaming the renderer shows a quiet "구성 중" placeholder instead of a
// half-built form (or the truncation-salvage warning) flickering as JSON grows;
// once streaming ends (a genuinely truncated reply) it decodes the body with the
// usual salvage pipeline.
@Immutable
data class DenebUiPending(val rawBody: String) : BlockNode

@Immutable
sealed interface InlineNode

@Immutable
data class Text(val value: String) : InlineNode

@Immutable
data class Emphasis(val children: ImmutableList<InlineNode>) : InlineNode

@Immutable
data class Strong(val children: ImmutableList<InlineNode>) : InlineNode

@Immutable
data class Strike(val children: ImmutableList<InlineNode>) : InlineNode

// HTML `<u>`: a real underline (distinct from a Link, which also underlines but is tappable).
@Immutable
data class Underline(val children: ImmutableList<InlineNode>) : InlineNode

// HTML `<mark>`: highlighted text (tinted background behind the glyphs).
@Immutable
data class Highlight(val children: ImmutableList<InlineNode>) : InlineNode

// HTML `<sup>` whose content has no Unicode script form (e.g. `5<sup>th</sup>`); rendered
// with a raised baseline + smaller size. Pure-digit scripts (m²) stay literal Unicode text.
@Immutable
data class Superscript(val children: ImmutableList<InlineNode>) : InlineNode

// HTML `<sub>` counterpart to [Superscript] (e.g. `x<sub>i</sub>`).
@Immutable
data class Subscript(val children: ImmutableList<InlineNode>) : InlineNode

@Immutable
data class InlineCode(val code: String) : InlineNode

@Immutable
data class Link(val href: String, val children: ImmutableList<InlineNode>) : InlineNode

@Immutable
data class Image(val src: String, val alt: String) : InlineNode

@Immutable
data object LineBreak : InlineNode

@Immutable
data class InlineMath(val latex: String) : InlineNode
