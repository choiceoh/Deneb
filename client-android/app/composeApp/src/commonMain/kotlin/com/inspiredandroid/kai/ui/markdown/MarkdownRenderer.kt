package com.inspiredandroid.kai.ui.markdown

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckBox
import androidx.compose.material.icons.outlined.CheckBoxOutlineBlank
import androidx.compose.material3.Icon
import kotlinx.collections.immutable.toImmutableList
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.layout.widthIn
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.graphics.Color
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.VerticalDivider
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.remember
import kotlin.math.sqrt
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil3.compose.AsyncImage
import com.inspiredandroid.kai.ui.dynamicui.FrozenSubmission
import com.inspiredandroid.kai.ui.dynamicui.KaiUiRenderer
import com.inspiredandroid.kai.ui.markdown.math.MathFormula
import kotlinx.collections.immutable.persistentListOf

/**
 * Render a parsed [MarkdownDocument] as a Compose layout. Each block becomes one child of the
 * outer [Column]; inline content is rendered as [androidx.compose.ui.text.AnnotatedString].
 *
 * Kai-UI blocks dispatch to [KaiUiRenderer]; pass `isInteractive = false` to render them as
 * read-only (completed historical messages keep their layout but disable buttons/inputs).
 */
@Composable
fun MarkdownContent(
    document: MarkdownDocument,
    modifier: Modifier = Modifier,
    isInteractive: Boolean = false,
    onUiCallback: (event: String, data: Map<String, String>) -> Unit = { _, _ -> },
    frozen: FrozenSubmission? = null,
) {
    CompositionLocalProvider(LocalContentColor provides MaterialTheme.colorScheme.onSurface) {
        Column(modifier) {
            for (block in document.blocks) {
                BlockRenderer(block, isInteractive, onUiCallback, frozen)
            }
        }
    }
}

@Composable
fun MarkdownContent(
    content: String,
    modifier: Modifier = Modifier,
    isInteractive: Boolean = false,
    onUiCallback: (event: String, data: Map<String, String>) -> Unit = { _, _ -> },
    frozen: FrozenSubmission? = null,
) {
    val doc = remember(content) {
        runCatching { parseMarkdown(content) }.getOrElse {
            MarkdownDocument(persistentListOf(Paragraph(persistentListOf(com.inspiredandroid.kai.ui.markdown.Text(content)))))
        }
    }
    MarkdownContent(doc, modifier, isInteractive, onUiCallback, frozen)
}

// The base text style for AI-answer body content. One step down from bodyLarge
// (14sp vs 15sp) with messenger-tight line-height (18sp ≈ 1.29, inside the
// 1.2–1.35 band Telegram/KakaoTalk use). Chat reads denser than a document —
// the first pass's 1.71 felt like an article. Headings keep their own
// typography roles; only paragraphs, list items, and table cells share this.
private val markdownBodyStyle: TextStyle
    @Composable get() = MaterialTheme.typography.bodyLarge.copy(
        fontSize = 14.sp,
        lineHeight = 18.sp,
    )

@Composable
private fun BlockRenderer(
    block: BlockNode,
    isInteractive: Boolean,
    onUiCallback: (String, Map<String, String>) -> Unit,
    frozen: FrozenSubmission?,
) {
    when (block) {
        is Heading -> HeadingBlock(block)

        is Paragraph -> ParagraphBlock(block)

        is CodeFence -> {
            if (block.code.isNotBlank() || !block.language.isNullOrBlank()) {
                CodeFenceBlock(
                    language = block.language,
                    code = block.code,
                    modifier = Modifier.padding(vertical = 4.dp),
                )
            }
        }

        is Blockquote -> BlockquoteBlock(block, isInteractive, onUiCallback, frozen)

        is BulletList -> BulletListBlock(block, isInteractive, onUiCallback, frozen)

        is OrderedList -> OrderedListBlock(block, isInteractive, onUiCallback, frozen)

        is Table -> TableBlock(block)

        HorizontalRule -> HorizontalDivider(
            modifier = Modifier.padding(vertical = 10.dp),
            color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.6f),
        )

        is DisplayMath -> DisplayMathBlock(block)

        is KaiUiBlock -> KaiUiRenderer(
            node = block.node,
            isInteractive = isInteractive,
            onCallback = onUiCallback,
            frozen = frozen,
            modifier = Modifier.padding(vertical = 8.dp),
        )

        is KaiUiError -> CodeFenceBlock(
            language = "json",
            code = block.rawJson,
            modifier = Modifier.padding(vertical = 4.dp),
        )
    }
}

@Composable
private fun HeadingBlock(block: Heading) {
    val typography = MaterialTheme.typography
    val style = when (block.level) {
        1 -> typography.headlineSmall
        2 -> typography.titleLarge
        3 -> typography.titleMedium
        4 -> typography.titleSmall
        5 -> typography.bodyLarge.copy(fontWeight = FontWeight.Bold)
        else -> typography.bodyMedium.copy(fontWeight = FontWeight.Bold)
    }
    // A heading opens a section: clear air above (more for higher levels) and a
    // tight gap below, so the title visibly groups with the content it leads.
    // Uniform 4dp let sections blur together in long analyses.
    val topPad = when (block.level) {
        1 -> 16.dp
        2 -> 14.dp
        3 -> 10.dp
        else -> 6.dp
    }
    InlineContent(
        inlines = block.inlines,
        style = style,
        modifier = Modifier.padding(top = topPad, bottom = 3.dp),
    )
}

@Composable
private fun ParagraphBlock(block: Paragraph) {
    if (block.inlines.size == 1 && block.inlines[0] is Image) {
        val img = block.inlines[0] as Image
        AsyncImage(
            model = img.src,
            contentDescription = img.alt,
            contentScale = ContentScale.FillWidth,
            // Rounded + capped to match attachment images, instead of a raw
            // edge-to-edge bitmap that can dwarf the message on a wide image.
            modifier = Modifier
                .padding(vertical = 4.dp)
                .fillMaxWidth()
                .widthIn(max = 520.dp)
                .clip(RoundedCornerShape(8.dp)),
        )
        return
    }
    // A paragraph carries more air above/below than the body line-height, so
    // consecutive paragraphs read as distinct blocks rather than one wall of
    // text (the old 2dp made the paragraph gap smaller than the line gap once
    // the line-height loosened).
    InlineContent(
        inlines = block.inlines,
        style = markdownBodyStyle,
        modifier = Modifier.padding(vertical = 5.dp),
    )
}

@Composable
private fun DisplayMathBlock(block: DisplayMath) {
    // Wrap in horizontal scroll so wide formulas overflow cleanly instead of squishing
    // their children into a narrow column (KaTeX/MathJax use the same pattern).
    val scroll = rememberScrollState()
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 8.dp)
            .horizontalScroll(scroll),
        contentAlignment = Alignment.Center,
    ) {
        MathFormula(latex = block.latex, display = true)
    }
}

@Composable
private fun BlockquoteBlock(
    block: Blockquote,
    isInteractive: Boolean,
    onUiCallback: (String, Map<String, String>) -> Unit,
    frozen: FrozenSubmission?,
) {
    // A soft callout: a thin accent bar plus a faint tinted panel reads calmer
    // than a heavy 3dp outline rule, and groups the quote as one block.
    Row(
        modifier = Modifier
            .padding(vertical = 4.dp)
            .fillMaxWidth()
            .clip(RoundedCornerShape(6.dp))
            .background(MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.4f))
            .height(IntrinsicSize.Min),
    ) {
        VerticalDivider(
            thickness = 2.dp,
            color = MaterialTheme.colorScheme.primary.copy(alpha = 0.6f),
            modifier = Modifier.fillMaxHeight(),
        )
        Column(Modifier.padding(start = 12.dp, end = 10.dp, top = 4.dp, bottom = 4.dp)) {
            block.children.forEach { BlockRenderer(it, isInteractive, onUiCallback, frozen) }
        }
    }
}

@Composable
private fun BulletListBlock(
    block: BulletList,
    isInteractive: Boolean,
    onUiCallback: (String, Map<String, String>) -> Unit,
    frozen: FrozenSubmission?,
) {
    Column(
        modifier = Modifier.padding(vertical = 4.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        for (item in block.items) {
            val task = remember(item) { detectTask(item) }
            if (task != null) {
                // A GFM task item ("- [ ] …" / "- [x] …") — render a real checkbox.
                TaskItemRow(task.first, task.second, isInteractive, onUiCallback, frozen)
            } else {
                // The bullet is decoration, not content — mute it so the eye lands
                // on the text, and "•" reads lighter than the body weight here.
                ListItemRow("•", 16.dp, MaterialTheme.colorScheme.onSurfaceVariant, item, isInteractive, onUiCallback, frozen)
            }
        }
    }
}

private val taskMarker = Regex("^\\[([ xX])]\\s+")

// detectTask returns (checked, item-with-marker-stripped) when a bullet item is
// a GFM task ("[ ] …" / "[x] …"), else null. Done at render time so the parser
// stays a plain CommonMark subset.
private fun detectTask(item: ListItem): Pair<Boolean, ListItem>? {
    val firstPara = item.children.firstOrNull() as? Paragraph ?: return null
    val firstText = firstPara.inlines.firstOrNull() as? com.inspiredandroid.kai.ui.markdown.Text ?: return null
    val match = taskMarker.find(firstText.value) ?: return null
    val checked = firstText.value[match.range.first + 1].lowercaseChar() == 'x'
    val rest = firstText.value.substring(match.range.last + 1)
    val newInlines = buildList {
        if (rest.isNotEmpty()) add(com.inspiredandroid.kai.ui.markdown.Text(rest))
        addAll(firstPara.inlines.drop(1))
    }.toImmutableList()
    val newChildren = buildList {
        add(Paragraph(newInlines))
        addAll(item.children.drop(1))
    }.toImmutableList()
    return checked to ListItem(newChildren)
}

@Composable
private fun TaskItemRow(
    checked: Boolean,
    item: ListItem,
    isInteractive: Boolean,
    onUiCallback: (String, Map<String, String>) -> Unit,
    frozen: FrozenSubmission?,
) {
    Row {
        Icon(
            imageVector = if (checked) Icons.Filled.CheckBox else Icons.Outlined.CheckBoxOutlineBlank,
            contentDescription = if (checked) "완료" else "미완료",
            tint = if (checked) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.size(18.dp).padding(end = 6.dp, top = 2.dp),
        )
        Column(Modifier.fillMaxWidth()) {
            // Done items read muted so the eye skips to what's still open.
            if (checked) {
                CompositionLocalProvider(LocalContentColor provides MaterialTheme.colorScheme.onSurfaceVariant) {
                    item.children.forEach { BlockRenderer(it, isInteractive, onUiCallback, frozen) }
                }
            } else {
                item.children.forEach { BlockRenderer(it, isInteractive, onUiCallback, frozen) }
            }
        }
    }
}

@Composable
private fun OrderedListBlock(
    block: OrderedList,
    isInteractive: Boolean,
    onUiCallback: (String, Map<String, String>) -> Unit,
    frozen: FrozenSubmission?,
) {
    Column(
        modifier = Modifier.padding(vertical = 4.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        block.items.forEachIndexed { index, item ->
            ListItemRow("${block.start + index}.", 24.dp, Color.Unspecified, item, isInteractive, onUiCallback, frozen)
        }
    }
}

@Composable
private fun ListItemRow(
    marker: String,
    markerWidth: androidx.compose.ui.unit.Dp,
    markerColor: Color,
    item: ListItem,
    isInteractive: Boolean,
    onUiCallback: (String, Map<String, String>) -> Unit,
    frozen: FrozenSubmission?,
) {
    Row {
        Text(
            text = marker,
            style = markdownBodyStyle,
            color = markerColor,
            modifier = Modifier.width(markerWidth).padding(end = 4.dp),
        )
        Column(Modifier.fillMaxWidth()) {
            item.children.forEach { BlockRenderer(it, isInteractive, onUiCallback, frozen) }
        }
    }
}

@Composable
private fun TableBlock(block: Table) {
    val numCols = maxOf(block.headers.size, block.rows.maxOfOrNull { it.size } ?: 0)
    if (numCols == 0) return
    // Weight each column by its widest cell so a short key column stops wasting
    // half the width on a key/value table (the common analysis shape). sqrt
    // compresses the extremes, so the long value column gets the room without
    // crushing the narrow key column to nothing.
    val weights = remember(block) {
        FloatArray(numCols) { i ->
            var maxLen = inlineTextLength(block.headers.getOrNull(i) ?: emptyList())
            for (row in block.rows) {
                maxLen = maxOf(maxLen, inlineTextLength(row.getOrNull(i) ?: emptyList()))
            }
            sqrt(maxLen.coerceAtLeast(1).toFloat()).coerceAtLeast(1f)
        }
    }
    val rowDivider = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.35f)
    Column(Modifier.padding(vertical = 4.dp)) {
        if (block.headers.any { it.isNotEmpty() }) {
            Row {
                block.headers.forEachIndexed { i, cell ->
                    InlineContent(
                        inlines = cell,
                        style = markdownBodyStyle.copy(fontWeight = FontWeight.Bold),
                        textAlign = alignTextFor(block.alignments.getOrNull(i)),
                        modifier = Modifier.weight(weights.getOrElse(i) { 1f })
                            .padding(horizontal = 8.dp, vertical = 6.dp),
                    )
                }
            }
            HorizontalDivider()
        }
        block.rows.forEachIndexed { rowIdx, row ->
            if (rowIdx > 0) HorizontalDivider(thickness = 1.dp, color = rowDivider)
            Row {
                row.forEachIndexed { i, cell ->
                    InlineContent(
                        inlines = cell,
                        style = markdownBodyStyle,
                        textAlign = alignTextFor(block.alignments.getOrNull(i)),
                        modifier = Modifier.weight(weights.getOrElse(i) { 1f })
                            .padding(horizontal = 8.dp, vertical = 6.dp),
                    )
                }
            }
        }
    }
}

// inlineTextLength is the plain-text character count of a cell's inline nodes,
// used to size table columns by content.
private fun inlineTextLength(inlines: List<InlineNode>): Int =
    inlines.sumOf { node ->
        when (node) {
            is Text -> node.value.length
            is InlineCode -> node.code.length
            is InlineMath -> node.latex.length
            is Emphasis -> inlineTextLength(node.children)
            is Strong -> inlineTextLength(node.children)
            is Strike -> inlineTextLength(node.children)
            is Link -> inlineTextLength(node.children)
            is Image -> node.alt.length
            else -> 0
        }
    }

private fun alignTextFor(align: ColumnAlign?): TextAlign = when (align) {
    ColumnAlign.LEFT -> TextAlign.Start
    ColumnAlign.CENTER -> TextAlign.Center
    ColumnAlign.RIGHT -> TextAlign.End
    else -> TextAlign.Unspecified
}
