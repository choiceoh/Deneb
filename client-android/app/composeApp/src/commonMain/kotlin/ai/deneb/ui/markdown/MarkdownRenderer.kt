package ai.deneb.ui.markdown

import ai.deneb.ui.DenebOutlinedTextField
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebChip
import ai.deneb.ui.components.LocalShowFullScreenImageModel
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebBreathing
import ai.deneb.ui.dynamicui.DenebHtmlAnswerBlock
import ai.deneb.ui.dynamicui.DenebUiHtml
import ai.deneb.ui.dynamicui.DenebUiParser
import ai.deneb.ui.dynamicui.DenebUiRenderer
import ai.deneb.ui.dynamicui.FrozenSubmission
import ai.deneb.ui.dynamicui.hasInteractiveNode
import ai.deneb.ui.handCursor
import ai.deneb.ui.icons.filled.CheckBox
import ai.deneb.ui.icons.filled.ChevronRight
import ai.deneb.ui.icons.filled.ExpandMore
import ai.deneb.ui.icons.outlined.BrokenImage
import ai.deneb.ui.icons.outlined.CheckBoxOutlineBlank
import ai.deneb.ui.markdown.math.MathFormula
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
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
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
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
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil3.compose.SubcomposeAsyncImage
import kotlinx.collections.immutable.persistentListOf
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

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
    baseStyle: TextStyle? = null,
) {
    CompositionLocalProvider(
        LocalContentColor provides MaterialTheme.colorScheme.onSurface,
        LocalMarkdownBaseStyle provides baseStyle,
    ) {
        Column(modifier) {
            // Document-level rhythm context: the FIRST block sheds its own top
            // air (the message container already insets, and a heading's 16dp
            // opener stacked to a 36dp start on heading-led replies); a
            // heading right after a rule keeps only a hairline (the rule
            // already separates — the stacked pair measured 46dp); and a
            // paragraph following another PARAGRAPH takes extra air so prose
            // breaks read unmistakably while structured blocks keep their
            // density (density stays — only consecutive-prose boundaries grow).
            var prevWasRule = false
            var prevWasParagraph = false
            document.blocks.forEachIndexed { index, block ->
                BlockRenderer(
                    block,
                    isInteractive,
                    onUiCallback,
                    frozen,
                    isFirst = index == 0,
                    afterRule = prevWasRule,
                    afterParagraph = prevWasParagraph,
                )
                prevWasRule = block is HorizontalRule
                prevWasParagraph = block is Paragraph
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
    baseStyle: TextStyle? = null,
) {
    val doc = rememberContentDocument(content)
    MarkdownContent(doc, modifier, isInteractive, onUiCallback, frozen, baseStyle)
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

// True while rendering a list item's children: the item's paragraph must not
// stack its own block air on top of the list's item spacing. The doubled
// padding made TIGHT bullet groups looser than plain paragraphs (14dp vs
// 10dp — inverted rhythm, visible across the 2026-07-12 reply corpus).
internal val LocalInsideListItem = compositionLocalOf { false }

// Optional base style for body / list / table / quote text. Null = the chat body
// style below. Non-chat surfaces (wiki, diary, skill, person, cron) provide their own
// (e.g. MaterialTheme.typography.bodyMedium) so MarkdownContent matches their existing
// typography while still rendering the full feature set (tables, footnotes, math, …).
val LocalMarkdownBaseStyle = compositionLocalOf<TextStyle?> { null }

// The base text style for AI-answer body content. One step down from bodyLarge
// (14sp vs 15sp) with messenger-tight line-height (18sp ≈ 1.29, inside the
// 1.2–1.35 band Telegram/KakaoTalk use). Chat reads denser than a document —
// the first pass's 1.71 felt like an article. Headings keep their own
// typography roles; only paragraphs, list items, and table cells share this.
internal val markdownBodyStyle: TextStyle
    @Composable get() = LocalMarkdownBaseStyle.current ?: MaterialTheme.typography.bodyLarge.copy(
        fontSize = 14.sp,
        lineHeight = 18.sp,
    )

@Composable
private fun BlockRenderer(
    block: BlockNode,
    isInteractive: Boolean,
    onUiCallback: (String, Map<String, String>) -> Unit,
    frozen: FrozenSubmission?,
    // Document-root rhythm context (defaults keep nested call sites — list
    // items, quotes — unaffected): see MarkdownContent's block loop.
    isFirst: Boolean = false,
    afterRule: Boolean = false,
    afterParagraph: Boolean = false,
) {
    when (block) {
        is Heading -> HeadingBlock(block, isFirst, afterRule)

        is Paragraph -> ParagraphBlock(block, isFirst, afterParagraph)

        is CodeFence -> {
            if (block.language?.trim()?.lowercase() == "choices") {
                ChoiceChipsBlock(
                    raw = block.code,
                    isInteractive = isInteractive,
                    onChoose = { onUiCallback("choice", mapOf("text" to it)) },
                    modifier = Modifier.padding(vertical = 4.dp),
                )
            } else if (block.code.isNotBlank() || !block.language.isNullOrBlank()) {
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
            modifier = Modifier.padding(vertical = 8.dp),
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

        is DenebHtmlBlock -> DenebHtmlAnswerBlock(
            html = block.html,
            // Page → chat rides the existing "choice" callback (a user message);
            // stale/read-only rows pass null so page sends are ignored.
            onSendPrompt = if (isInteractive) {
                { text -> onUiCallback("choice", mapOf("text" to text)) }
            } else {
                null
            },
        )

        is DenebHtmlPending -> DenebHtmlPendingBlock(block, isInteractive, onUiCallback)
    }
}

@Composable
private fun DenebHtmlPendingBlock(
    block: DenebHtmlPending,
    isInteractive: Boolean,
    onUiCallback: (String, Map<String, String>) -> Unit,
) {
    if (LocalDenebUiStreaming.current) {
        // Never run scripts in a half-streamed document. A page takes tens of
        // seconds to generate, so hold a page-shaped breathing skeleton
        // instead of a bare spinner line.
        Column(
            verticalArrangement = Arrangement.spacedBy(10.dp),
            modifier = Modifier
                .fillMaxWidth()
                .padding(vertical = 8.dp)
                .clip(RoundedCornerShape(12.dp))
                .border(
                    width = 1.dp,
                    color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.6f),
                    shape = RoundedCornerShape(12.dp),
                )
                .padding(14.dp),
        ) {
            val barColor = MaterialTheme.colorScheme.surfaceVariant

            @Composable
            fun bar(widthFraction: Float, height: Int) = Box(
                Modifier
                    .fillMaxWidth(widthFraction)
                    .height(height.dp)
                    .clip(RoundedCornerShape(6.dp))
                    .background(barColor)
                    .denebBreathing(minScale = 1f, maxScale = 1f, minAlpha = 0.35f),
            )
            bar(0.45f, 18)
            bar(0.92f, 12)
            bar(0.78f, 12)
            bar(0.6f, 34)
            Text(
                "웹 응답 생성 중…",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        return
    }
    // Final but unclosed (truncated reply): render if it is markup, else keep
    // the source readable as a code fence.
    val trimmed = block.rawBody.trim()
    if (trimmed.startsWith("<")) {
        BlockRenderer(DenebHtmlBlock(trimmed), isInteractive, onUiCallback, frozen = null)
    } else {
        CodeFenceBlock(language = "html", code = block.rawBody, modifier = Modifier.padding(vertical = 4.dp))
    }
}

// Renders a ```choices fence as tappable chips: each non-empty line is one option,
// and tapping sends it as a user message (onUiCallback "choice"). An always-present
// "직접 입력" chip reveals an inline field for a free-text answer (Claude's "Other"
// pattern), so a structured skill (kb-interview) can offer one-tap answers without
// locking the user out of typing. Read-only history/preview renders the chips disabled.
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ChoiceChipsBlock(
    raw: String,
    isInteractive: Boolean,
    onChoose: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    val options = remember(raw) {
        raw.lines()
            .map { it.trim().removePrefix("-").removePrefix("*").trim() }
            .filter { it.isNotEmpty() }
            .distinct()
    }
    if (options.isEmpty()) return
    val haptics = rememberHaptics()
    var customOpen by remember(raw) { mutableStateOf(false) }
    var customText by remember(raw) { mutableStateOf("") }
    val submitCustom = {
        val answer = customText.trim()
        if (answer.isNotEmpty()) {
            customText = ""
            customOpen = false
            onChoose(answer)
        }
    }
    Column(modifier.fillMaxWidth()) {
        FlowRow(
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            for (label in options) {
                DenebChip(
                    onClick = if (isInteractive) {
                        {
                            haptics.tap()
                            onChoose(label)
                        }
                    } else {
                        null
                    },
                    enabled = isInteractive,
                ) {
                    Text(label, style = DenebType.button)
                }
            }
            if (isInteractive) {
                DenebChip(
                    selected = customOpen,
                    onClick = {
                        haptics.tap()
                        customOpen = !customOpen
                    },
                ) {
                    Text("✏️ 직접 입력", style = DenebType.button)
                }
            }
        }
        if (isInteractive && customOpen) {
            DenebOutlinedTextField(
                value = customText,
                onValueChange = { customText = it },
                singleLine = true,
                placeholder = { Text("직접 입력…", style = DenebType.button) },
                trailingIcon = {
                    if (customText.isNotBlank()) {
                        IconButton(onClick = submitCustom) {
                            Icon(Icons.AutoMirrored.Filled.Send, contentDescription = "전송")
                        }
                    }
                },
                modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
            )
        }
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
        // Mid-stream, HTML v2 body: partial trees parse cleanly (EOF auto-close), so
        // paint the card live as it streams — the upgrade the JSON format could never
        // support. Interactive trees stay on the placeholder below: a half-built form
        // must not accept taps mid-stream.
        val trimmed = block.rawBody.trim()
        if (trimmed.startsWith("<")) {
            val node = remember(block.rawBody) { DenebUiHtml.parse(trimmed) }
            if (node != null && !node.hasInteractiveNode()) {
                DenebUiRenderer(
                    node = node,
                    isInteractive = false,
                    onCallback = onUiCallback,
                    frozen = frozen,
                    modifier = Modifier.padding(vertical = 8.dp),
                )
                return
            }
        }
        // Legacy JSON stream or an interactive tree: hold a stable placeholder instead
        // of re-rendering a half-built form on every token tick.
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
private fun HeadingBlock(block: Heading, isFirst: Boolean = false, afterRule: Boolean = false) {
    // Heading ladder rides the DenebType scale:
    // # = subject (22), ## = cardTitle (18), ###+ = rowTitleStrong (15). Deeper
    // levels collapse onto the emphasis rung on purpose — hierarchy comes from
    // register jumps, not a continuous ladder (see DenebType.kt law 1).
    val style = when (block.level) {
        1 -> DenebType.subject
        2 -> DenebType.cardTitle
        else -> DenebType.rowTitleStrong
    }
    // A heading opens a section: clear air above (more for higher levels) and a
    // tight gap below, so the title visibly groups with the content it leads.
    // Uniform 4dp let sections blur together in long analyses. Two stacking
    // cases shed that air (2026-07-12 rhythm measurements): the document's
    // first heading (the container inset already opens the message — the
    // stack measured 36dp) and a heading right after a rule (the rule already
    // separates — the pair measured 46dp).
    val topPad = when {
        isFirst -> 0.dp

        afterRule -> 2.dp

        else -> when (block.level) {
            1 -> 16.dp
            2 -> 14.dp
            3 -> 10.dp
            else -> 6.dp
        }
    }
    InlineContent(
        inlines = block.inlines,
        style = style,
        modifier = Modifier.padding(top = topPad, bottom = 3.dp),
    )
}

// Placeholder shown while a markdown image loads, or when it can't be fetched: a
// compact tinted banner with a spinner / broken-image glyph and the alt text, instead
// of the blank gap plain AsyncImage left on error.
@Composable
private fun ImageStatusBox(alt: String, loading: Boolean) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.5f))
            .padding(horizontal = 12.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (loading) {
            CircularProgressIndicator(
                modifier = Modifier.size(16.dp),
                strokeWidth = 2.dp,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        } else {
            Icon(
                imageVector = Icons.Outlined.BrokenImage,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(18.dp),
            )
        }
        Spacer(Modifier.width(8.dp))
        Text(
            text = if (loading) alt.ifBlank { "이미지 불러오는 중…" } else alt.ifBlank { "이미지를 불러올 수 없습니다" },
            style = markdownBodyStyle,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            maxLines = 2,
        )
    }
}

@Composable
private fun ParagraphBlock(block: Paragraph, isFirst: Boolean = false, afterParagraph: Boolean = false) {
    if (block.inlines.size == 1 && block.inlines[0] is Image) {
        val img = block.inlines[0] as Image
        val showFullScreen = LocalShowFullScreenImageModel.current
        // SubcomposeAsyncImage (not AsyncImage) so a slow/broken/unreachable URL shows a
        // graceful placeholder + the alt text instead of a blank gap — plain AsyncImage
        // drew nothing on error. Rounded + capped to match attachment images; tapping
        // opens the fullscreen zoom/pan viewer (same overlay as attachments).
        SubcomposeAsyncImage(
            model = img.src,
            contentDescription = img.alt,
            contentScale = ContentScale.FillWidth,
            modifier = Modifier
                .padding(vertical = 4.dp)
                .fillMaxWidth()
                .widthIn(max = 520.dp)
                .clip(RoundedCornerShape(8.dp))
                .handCursor()
                .clickable { showFullScreen(img.src) },
            loading = { ImageStatusBox(img.alt, loading = true) },
            error = { ImageStatusBox(img.alt, loading = false) },
        )
        return
    }
    // A paragraph carries more air above/below than the body line-height, so
    // consecutive paragraphs read as distinct blocks rather than one wall of
    // text. A paragraph FOLLOWING another paragraph takes extra top so prose
    // breaks read unmistakably (~20dp measured) — while transitions into
    // structured blocks (lists, tables, headings) keep the denser 14dp bond,
    // so overall density holds. Inside a list item the list's spacedBy owns
    // the rhythm — the paragraph keeps only a hairline of its own. The
    // document's first paragraph sheds its top so text-led and heading-led
    // replies open at the same height.
    val vPad = if (LocalInsideListItem.current) 1.dp else 5.dp
    val topPad = when {
        isFirst -> 0.dp
        afterParagraph -> 9.dp
        else -> vPad
    }
    InlineContent(
        inlines = block.inlines,
        style = markdownBodyStyle,
        modifier = Modifier.padding(top = topPad, bottom = vPad),
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
        modifier = Modifier.padding(vertical = 2.dp),
        // Loose lists (blank lines between items in the source) read as separate thoughts —
        // give them more air than tight ones. Tight-list spacing sits well under
        // the paragraph gap so a bullet group reads as ONE unit (measured: the
        // old 4dp landed within 2dp of the paragraph rhythm).
        verticalArrangement = Arrangement.spacedBy(if (block.tight) 3.dp else 8.dp),
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
            CompositionLocalProvider(LocalInsideListItem provides true) {
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
        modifier = Modifier.padding(vertical = 2.dp),
        verticalArrangement = Arrangement.spacedBy(if (block.tight) 3.dp else 8.dp),
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
            CompositionLocalProvider(LocalInsideListItem provides true) {
                item.children.forEach { BlockRenderer(it, isInteractive, onUiCallback, frozen) }
            }
        }
    }
}
