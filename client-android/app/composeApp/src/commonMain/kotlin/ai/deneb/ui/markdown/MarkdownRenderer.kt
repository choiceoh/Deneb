package ai.deneb.ui.markdown

import ai.deneb.ui.DenebType
import ai.deneb.ui.components.LocalShowFullScreenImageModel
import ai.deneb.ui.dynamicui.DenebUiParser
import ai.deneb.ui.dynamicui.DenebUiRenderer
import ai.deneb.ui.dynamicui.FrozenSubmission
import ai.deneb.ui.handCursor
import ai.deneb.ui.markdown.math.MathFormula
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckBox
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material.icons.filled.ExpandMore
import androidx.compose.material.icons.outlined.CheckBoxOutlineBlank
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.VerticalDivider
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.isSpecified
import androidx.compose.ui.unit.sp
import coil3.compose.AsyncImage
import kotlinx.collections.immutable.persistentListOf
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlin.math.sqrt

/**
 * Render a parsed [MarkdownDocument] as a Compose layout. Each block becomes one child of the
 * outer [Column]; inline content is rendered as [androidx.compose.ui.text.AnnotatedString].
 *
 * Deneb-UI blocks dispatch to [DenebUiRenderer]; pass `isInteractive = false` to render them as
 * read-only (completed historical messages keep their layout but disable buttons/inputs).
 */
@Composable
fun MarkdownContent(
    document: MarkdownDocument,
    modifier: Modifier = Modifier,
    isInteractive: Boolean = false,
    onUiCallback: (event: String, data: Map<String, String>) -> Unit = { _, _ -> },
    frozen: FrozenSubmission? = null,
    textScale: Float = 1f,
    baseStyle: TextStyle? = null,
) {
    CompositionLocalProvider(
        LocalContentColor provides MaterialTheme.colorScheme.onSurface,
        LocalChatTextScale provides textScale,
        LocalMarkdownBaseStyle provides baseStyle,
    ) {
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
    textScale: Float = 1f,
    baseStyle: TextStyle? = null,
) {
    val doc = rememberContentDocument(content)
    MarkdownContent(doc, modifier, isInteractive, onUiCallback, frozen, textScale, baseStyle)
}

// Bodies up to this length parse synchronously (sub-frame, no async flash); longer ones go to
// a background core so opening a long page (a big wiki note, a mail body) doesn't jank the
// first frame.
private const val SYNC_PARSE_MAX_CHARS = 2000

// rememberContentDocument parses [content] into a document while keeping a large parse off the
// UI thread. Short bodies parse synchronously (instant). A long body parses on
// Dispatchers.Default via produceState — one extra frame on open, vs a dropped frame from
// parsing inline on the composition thread — then shows the formatted document. Malformed
// input falls back to the raw text as a single paragraph, as before. (Chat uses the
// MarkdownDocument overload with a pre-parsed doc, so it's unaffected.)
@Composable
private fun rememberContentDocument(content: String): MarkdownDocument {
    val sync = remember(content) {
        if (content.length <= SYNC_PARSE_MAX_CHARS) parseContentSafely(content) else null
    }
    if (sync != null) return sync
    val empty = remember { parseMarkdown("") }
    val doc by produceState(initialValue = empty, content) {
        value = withContext(Dispatchers.Default) { parseContentSafely(content) }
    }
    return doc
}

private fun parseContentSafely(content: String): MarkdownDocument = runCatching { parseMarkdown(content) }.getOrElse {
    MarkdownDocument(persistentListOf(Paragraph(persistentListOf(ai.deneb.ui.markdown.Text(content)))))
}

/**
 * True while the surrounding message is still streaming. [DenebUiPending] blocks
 * (deneb-ui fences whose closing ``` hasn't arrived) render as a quiet placeholder
 * when set, and fall back to the salvage decode when the message is final.
 */
val LocalDenebUiStreaming = compositionLocalOf { false }

// Per-message font-size/line-height multiplier for chat body + headings. 1f
// everywhere except the 챗봇 workspace, where ChatModeScreen provides a larger
// scale (see [ChatbotTextScale]) for a roomier, more readable casual conversation.
// Only MarkdownContent (chat) and the user bubble read it, so it never touches
// mail/wiki/etc. typography.
val LocalChatTextScale = compositionLocalOf { 1f }

// The 챗봇 workspace's enlarged chat text scale (업무 stays 1f).
const val ChatbotTextScale = 1.15f

// Optional base style for body / list / table / quote text. Null = the chat body
// style below. Non-chat surfaces (wiki, diary, skill, person, cron) provide their own
// (e.g. MaterialTheme.typography.bodyMedium) so MarkdownContent matches their existing
// typography while still rendering the full feature set (tables, footnotes, math, …).
val LocalMarkdownBaseStyle = compositionLocalOf<TextStyle?> { null }

// Scale a style's font size and line height by [scale] (no-op at 1f; leaves
// unspecified dimensions untouched).
internal fun TextStyle.scaledBy(scale: Float): TextStyle = if (scale == 1f) {
    this
} else {
    copy(
        fontSize = if (fontSize.isSpecified) fontSize * scale else fontSize,
        lineHeight = if (lineHeight.isSpecified) lineHeight * scale else lineHeight,
    )
}

// The base text style for AI-answer body content. One step down from bodyLarge
// (14sp vs 15sp) with messenger-tight line-height (18sp ≈ 1.29, inside the
// 1.2–1.35 band Telegram/KakaoTalk use). Chat reads denser than a document —
// the first pass's 1.71 felt like an article. Headings keep their own
// typography roles; only paragraphs, list items, and table cells share this.
// Scaled by [LocalChatTextScale] so the 챗봇 workspace reads larger.
private val markdownBodyStyle: TextStyle
    @Composable get() = (
        LocalMarkdownBaseStyle.current ?: MaterialTheme.typography.bodyLarge.copy(
            fontSize = 14.sp,
            lineHeight = 18.sp,
        )
        ).scaledBy(LocalChatTextScale.current)

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

        is Collapsible -> CollapsibleBlock(block, isInteractive, onUiCallback, frozen)

        is BulletList -> BulletListBlock(block, isInteractive, onUiCallback, frozen)

        is OrderedList -> OrderedListBlock(block, isInteractive, onUiCallback, frozen)

        is Table -> TableBlock(block)

        HorizontalRule -> HorizontalDivider(
            modifier = Modifier.padding(vertical = 10.dp),
            color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.6f),
        )

        is DisplayMath -> DisplayMathBlock(block)

        is DenebUiBlock -> DenebUiRenderer(
            node = block.node,
            isInteractive = isInteractive,
            onCallback = onUiCallback,
            frozen = frozen,
            modifier = Modifier.padding(vertical = 8.dp),
        )

        is DenebUiError -> CodeFenceBlock(
            language = "json",
            code = block.rawJson,
            modifier = Modifier.padding(vertical = 4.dp),
        )

        is DenebUiPending -> DenebUiPendingBlock(block, isInteractive, onUiCallback, frozen)
    }
}

@Composable
private fun DenebUiPendingBlock(
    block: DenebUiPending,
    isInteractive: Boolean,
    onUiCallback: (String, Map<String, String>) -> Unit,
    frozen: FrozenSubmission?,
) {
    if (LocalDenebUiStreaming.current) {
        // Mid-stream: hold a stable placeholder instead of re-rendering a half-built
        // form (or the truncation-salvage warning) on every token tick.
        Row(
            verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier.padding(vertical = 8.dp),
        ) {
            CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
            Text(
                "화면 구성 중…",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(start = 8.dp),
            )
        }
        return
    }
    // Final message with an unclosed fence — a genuinely truncated reply. Decode with
    // the usual salvage pipeline so whatever finished streaming is still shown.
    val result = remember(block.rawBody) { DenebUiParser.parseUiBlockBody(block.rawBody) }
    when (result) {
        is DenebUiParser.UiBlockResult.Ui -> DenebUiRenderer(
            node = result.node,
            isInteractive = isInteractive,
            onCallback = onUiCallback,
            frozen = frozen,
            modifier = Modifier.padding(vertical = 8.dp),
        )

        else -> CodeFenceBlock(
            language = "json",
            code = block.rawBody,
            modifier = Modifier.padding(vertical = 4.dp),
        )
    }
}

@Composable
private fun HeadingBlock(block: Heading) {
    // Heading ladder rides the DenebType scale:
    // # = subject (22), ## = cardTitle (18), ###+ = rowTitleStrong (15). Deeper
    // levels collapse onto the emphasis rung on purpose — hierarchy comes from
    // register jumps, not a continuous ladder (see DenebType.kt law 1).
    val style = when (block.level) {
        1 -> DenebType.subject
        2 -> DenebType.cardTitle
        else -> DenebType.rowTitleStrong
    }.scaledBy(LocalChatTextScale.current)
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
        val showFullScreen = LocalShowFullScreenImageModel.current
        AsyncImage(
            model = img.src,
            contentDescription = img.alt,
            contentScale = ContentScale.FillWidth,
            // Rounded + capped to match attachment images, instead of a raw
            // edge-to-edge bitmap that can dwarf the message on a wide image.
            // Tapping opens the fullscreen zoom/pan viewer (same overlay as attachments).
            modifier = Modifier
                .padding(vertical = 4.dp)
                .fillMaxWidth()
                .widthIn(max = 520.dp)
                .clip(RoundedCornerShape(8.dp))
                .handCursor()
                .clickable { showFullScreen(img.src) },
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

// HTML <details>: a tappable summary header that expands/collapses the body blocks.
@Composable
private fun CollapsibleBlock(
    block: Collapsible,
    isInteractive: Boolean,
    onUiCallback: (String, Map<String, String>) -> Unit,
    frozen: FrozenSubmission?,
) {
    var open by remember(block) { mutableStateOf(block.initiallyOpen) }
    Column(Modifier.padding(vertical = 4.dp)) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(6.dp))
                .clickable { open = !open }
                .padding(vertical = 6.dp, horizontal = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(
                imageVector = if (open) Icons.Filled.ExpandMore else Icons.Filled.ChevronRight,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(20.dp),
            )
            Spacer(Modifier.width(4.dp))
            InlineContent(
                inlines = block.summary,
                style = markdownBodyStyle.copy(fontWeight = FontWeight.Bold),
            )
        }
        if (open) {
            Column(Modifier.padding(start = 24.dp, top = 2.dp)) {
                block.children.forEach { BlockRenderer(it, isInteractive, onUiCallback, frozen) }
            }
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
        // Loose lists (blank lines between items in the source) read as separate thoughts —
        // give them more air than tight ones.
        verticalArrangement = Arrangement.spacedBy(if (block.tight) 4.dp else 8.dp),
    ) {
        for (item in block.items) {
            val checked = item.checked
            if (checked != null) {
                // A GFM task item ("- [ ] …" / "- [x] …"): the parser already stripped the
                // marker and recorded the state, so render a real checkbox.
                TaskItemRow(checked, item, isInteractive, onUiCallback, frozen)
            } else {
                // The bullet is decoration, not content — mute it so the eye lands
                // on the text, and "•" reads lighter than the body weight here.
                ListItemRow("•", 16.dp, MaterialTheme.colorScheme.onSurfaceVariant, item, isInteractive, onUiCallback, frozen)
            }
        }
    }
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
    // Size the marker column to the widest number in this list so "10."/"100." doesn't
    // wrap into the content column (the old fixed 24dp fit two digits at most).
    val lastMarkerLen = "${block.start + block.items.size - 1}.".length
    val markerWidth = when {
        lastMarkerLen <= 2 -> 24.dp
        lastMarkerLen == 3 -> 32.dp
        else -> 40.dp
    }
    Column(
        modifier = Modifier.padding(vertical = 4.dp),
        verticalArrangement = Arrangement.spacedBy(if (block.tight) 4.dp else 8.dp),
    ) {
        block.items.forEachIndexed { index, item ->
            ListItemRow("${block.start + index}.", markerWidth, Color.Unspecified, item, isInteractive, onUiCallback, frozen)
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
    // Content-derived natural width per column (CJK-aware: a full-width Hangul/Han
    // glyph is ~2 latin chars). The fit-vs-scroll choice is driven by whether that
    // natural width fits the viewport, NOT a fixed column count — so a 3-column table
    // with long Korean cells scrolls just like a 7-column one, instead of the weight
    // layout crushing each cell to a glyph per line.
    val colWidths = remember(block, numCols) { naturalColWidths(block, numCols) }
    BoxWithConstraints(Modifier.fillMaxWidth()) {
        if (colWidths.sum() <= maxWidth.value) {
            FittedTable(block, numCols)
        } else {
            WideTable(block, numCols, colWidths)
        }
    }
}

@Composable
private fun FittedTable(block: Table, numCols: Int) {
    // Weight each column by its widest cell so a short key column stops wasting
    // half the width on a key/value table (the common analysis shape). sqrt
    // compresses the extremes, so the long value column gets the room without
    // crushing the narrow key column to nothing.
    val weights = remember(block) {
        FloatArray(numCols) { i ->
            var maxLen = inlineDisplayUnits(block.headers.getOrNull(i) ?: emptyList())
            for (row in block.rows) {
                maxLen = maxOf(maxLen, inlineDisplayUnits(row.getOrNull(i) ?: emptyList()))
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

// Wide table: fixed content-derived column widths under one horizontal scroll, so the
// header and every row stay aligned and each cell remains readable. Long cells wrap
// within their clamped column width instead of stretching the table indefinitely.
// [colWidths] is the CJK-aware natural width per column computed by the caller.
@Composable
private fun WideTable(block: Table, numCols: Int, colWidths: IntArray) {
    val totalWidth = colWidths.sum().dp
    val rowDivider = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.35f)
    val scroll = rememberScrollState()
    Column(
        Modifier
            .padding(vertical = 4.dp)
            .horizontalScroll(scroll),
    ) {
        if (block.headers.any { it.isNotEmpty() }) {
            Row {
                block.headers.forEachIndexed { i, cell ->
                    InlineContent(
                        inlines = cell,
                        style = markdownBodyStyle.copy(fontWeight = FontWeight.Bold),
                        textAlign = alignTextFor(block.alignments.getOrNull(i)),
                        modifier = Modifier.width(colWidths.getOrElse(i) { 100 }.dp)
                            .padding(horizontal = 8.dp, vertical = 6.dp),
                    )
                }
            }
            // Dividers must span the content width, not the viewport, inside the scroller.
            HorizontalDivider(Modifier.width(totalWidth))
        }
        block.rows.forEachIndexed { rowIdx, row ->
            if (rowIdx > 0) {
                HorizontalDivider(Modifier.width(totalWidth), thickness = 1.dp, color = rowDivider)
            }
            Row {
                row.forEachIndexed { i, cell ->
                    InlineContent(
                        inlines = cell,
                        style = markdownBodyStyle,
                        textAlign = alignTextFor(block.alignments.getOrNull(i)),
                        modifier = Modifier.width(colWidths.getOrElse(i) { 100 }.dp)
                            .padding(horizontal = 8.dp, vertical = 6.dp),
                    )
                }
            }
        }
    }
}

// Per-column natural width in dp, sized by the widest cell's CJK-aware display width
// (~7dp per display unit at the body size; a full-width Hangul/Han glyph is 2 units ≈
// 14dp, latin ≈ 7dp), plus cell padding. Clamped so a short key column stays legible
// and one verbose cell doesn't explode its column. Shared by the fit-vs-scroll
// decision and WideTable so both agree on the table's natural width.
private fun naturalColWidths(block: Table, numCols: Int): IntArray = IntArray(numCols) { i ->
    var units = inlineDisplayUnits(block.headers.getOrNull(i) ?: emptyList())
    for (row in block.rows) {
        units = maxOf(units, inlineDisplayUnits(row.getOrNull(i) ?: emptyList()))
    }
    (units * 7 + 16).coerceIn(72, 240)
}

// inlineDisplayUnits is the cell's rendered width in "display units": every char is 1
// unit except East-Asian-wide ones (Hangul/Han/kana/full-width), which are 2 — so a
// Korean column is sized for its real on-screen width, not its character count.
private fun inlineDisplayUnits(inlines: List<InlineNode>): Int = inlines.sumOf { node ->
    when (node) {
        is Text -> displayUnits(node.value)
        is InlineCode -> displayUnits(node.code)
        is InlineMath -> displayUnits(node.latex)
        is Emphasis -> inlineDisplayUnits(node.children)
        is Strong -> inlineDisplayUnits(node.children)
        is Strike -> inlineDisplayUnits(node.children)
        is Underline -> inlineDisplayUnits(node.children)
        is Highlight -> inlineDisplayUnits(node.children)
        is Superscript -> inlineDisplayUnits(node.children)
        is Subscript -> inlineDisplayUnits(node.children)
        is Link -> inlineDisplayUnits(node.children)
        is Image -> displayUnits(node.alt)
        else -> 0
    }
}

private fun displayUnits(s: String): Int = s.sumOf { if (isEastAsianWide(it)) 2 else 1 }

// East-Asian "wide" approximation: Hangul (jamo + syllables), CJK ideographs and
// radicals through Yi (covers kana 3040–30FF and CJK symbols 3000–303F), CJK compat,
// and full-width forms. Good enough to size columns; not a full Unicode width table.
private fun isEastAsianWide(c: Char): Boolean {
    val u = c.code
    return u in 0x1100..0x115F ||
        u in 0x2E80..0xA4CF ||
        u in 0xAC00..0xD7A3 ||
        u in 0xF900..0xFAFF ||
        u in 0xFE30..0xFE4F ||
        u in 0xFF00..0xFF60 ||
        u in 0xFFE0..0xFFE6
}

private fun alignTextFor(align: ColumnAlign?): TextAlign = when (align) {
    ColumnAlign.LEFT -> TextAlign.Start
    ColumnAlign.CENTER -> TextAlign.Center
    ColumnAlign.RIGHT -> TextAlign.End
    else -> TextAlign.Unspecified
}
