@file:OptIn(ExperimentalMaterial3Api::class)

package ai.deneb.ui.dynamicui

import ai.deneb.ui.DenebMotion
import ai.deneb.ui.DenebType
import ai.deneb.ui.JetBrainsMonoFamily
import ai.deneb.ui.denebOnSuccessContainer
import ai.deneb.ui.denebOnWarningContainer
import ai.deneb.ui.denebSuccessContainer
import ai.deneb.ui.denebWarningContainer
import ai.deneb.ui.icons.filled.Image
import ai.deneb.ui.icons.filled.Map
import ai.deneb.ui.markdown.InlineTokenizer
import ai.deneb.ui.markdown.MarkdownContent
import ai.deneb.ui.markdown.toAnnotatedString
import ai.deneb.ui.text.denebPhraseLineBreak
import ai.deneb.ui.text.displayUnits
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.wrapContentHeight
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Person
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.SnapshotStateMap
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import kotlin.math.roundToInt
import kotlin.time.Clock
import kotlin.time.Duration.Companion.seconds

/**
 * Read-only display components of the deneb-ui renderer: text / image / table /
 * progress / countdown / alert / quote / badge / stat / avatar.
 */

private const val DEFAULT_IMAGE_HEIGHT = 220
private const val DEFAULT_IMAGE_ASPECT_RATIO = 1.91f

@Composable
internal fun RenderText(node: TextNode) {
    val style = when (node.style) {
        // Hero voice: card headlines carry the letter masthead and
        // dashboard titles — 28sp Light tracked display (viewTitle), not
        // the 22sp subject rung, so a temperature or a report title lands
        // with editorial weight.
        TextNodeStyle.HEADLINE -> DenebType.viewTitle

        TextNodeStyle.TITLE -> DenebType.cardTitle

        TextNodeStyle.BODY -> MaterialTheme.typography.bodyLarge

        TextNodeStyle.CAPTION -> MaterialTheme.typography.bodySmall

        null -> MaterialTheme.typography.bodyLarge
    }
    val color = when (node.color) {
        "primary" -> MaterialTheme.colorScheme.primary

        "secondary" -> MaterialTheme.colorScheme.secondary

        "error" -> MaterialTheme.colorScheme.error

        // Status voice for prose ("전 현장 정상 가동"): the same on-container
        // pair RenderStat's trend text already uses on plain surface.
        "success" -> denebOnSuccessContainer()

        "warning" -> denebOnWarningContainer()

        else -> MaterialTheme.colorScheme.onSurface
    }
    Text(
        text = denebUiInlineText(node.value),
        style = style,
        color = color,
        fontWeight = if (node.bold == true) FontWeight.Bold else null,
        fontStyle = if (node.italic == true) FontStyle.Italic else null,
    )
}

/**
 * Inline-span rendering for deneb-ui text values: models naturally write
 * `**볼드**`/`*이탤릭*`/`` `코드` `` inside card text, and the old behavior —
 * strip the markers and bold the WHOLE line when it merely started with ** —
 * mangled mixed-emphasis lines. Reuses the chat markdown inline tokenizer so
 * emphasis renders identically in prose and cards. Plain values skip the
 * tokenizer entirely.
 */
@Composable
internal fun denebUiInlineText(value: String): AnnotatedString {
    // Fast-path gate covers every marker family the tokenizer understands
    // (emphasis, code, strikethrough, _italic_, links, inline HTML); the
    // result is memoized per value+scheme — tokenizing on every
    // recomposition would tax long cards (review catch on #3233).
    val colors = MaterialTheme.colorScheme
    if (value.none { it == '*' || it == '`' || it == '~' || it == '_' || it == '[' || it == '<' }) {
        return AnnotatedString(value)
    }
    val monoFamily = JetBrainsMonoFamily()
    return remember(value, colors, monoFamily) {
        InlineTokenizer.tokenize(value).toAnnotatedString(colors, monoFamily)
    }
}

/**
 * List-item text with the letter/briefing prefix convention: a short lead
 * ("09:00", "김부장") joined by " — " reads as the item's key and gets
 * SemiBold, so schedules and mail digests scan by time/sender. Applies only
 * when the lead is 14 chars or less — longer leads are sentences, not keys.
 */
@Composable
internal fun denebUiListItemText(value: String): AnnotatedString {
    val sep = " — "
    val idx = value.indexOf(sep)
    if (idx !in 1..14) return denebUiInlineText(value)
    // Resolve the composable inline rendering BEFORE the builder lambda —
    // composable calls inside builder lambdas are a fragile pattern even
    // where the compiler tolerates the inline case (review catch on #3233).
    // The lead runs through the tokenizer too: models often mark keys as
    // **볼드** themselves, and a raw append showed the literal asterisks
    // (2026-07-07 live letter). Tokenizer bold overrides the SemiBold base.
    val lead = denebUiInlineText(value.substring(0, idx))
    val rest = denebUiInlineText(value.substring(idx + sep.length))
    return buildAnnotatedString {
        withStyle(SpanStyle(fontWeight = FontWeight.SemiBold)) { append(lead) }
        append(sep)
        append(rest)
    }
}

/**
 * Full markdown body inside a deneb-ui tree, rendered through the chat markdown
 * pipeline (same renderer as a regular assistant message). Interactivity and UI
 * callbacks pass through so a nested deneb-ui fence inside the markdown — rare,
 * but possible — keeps working; recursion is naturally bounded by the content.
 */
@Composable
internal fun RenderMarkdown(
    node: MarkdownNode,
    isInteractive: Boolean,
    onCallback: (String, Map<String, String>) -> Unit,
) {
    MarkdownContent(
        content = node.value,
        modifier = Modifier.fillMaxWidth(),
        isInteractive = isInteractive,
        onUiCallback = onCallback,
    )
}

@Composable
internal fun RenderImage(node: ImageNode) {
    val height = (node.height ?: DEFAULT_IMAGE_HEIGHT).dp
    val aspectRatio = (node.aspectRatio ?: DEFAULT_IMAGE_ASPECT_RATIO)
    BoxWithConstraints(Modifier.fillMaxWidth()) {
        val width = minOf(maxWidth, height * aspectRatio)
        val modifier = Modifier.height(width / aspectRatio).width(width).clip(RoundedCornerShape(6.dp))
        val previewBitmap = LocalPreviewImages.current[node.url]
        if (previewBitmap != null) {
            Image(
                bitmap = previewBitmap,
                contentDescription = node.alt,
                modifier = modifier,
                contentScale = ContentScale.Crop,
            )
        } else {
            coil3.compose.AsyncImage(
                model = node.url,
                contentDescription = node.alt,
                modifier = modifier,
                contentScale = ContentScale.Crop,
            )
        }
    }
}

@Composable
internal fun RenderTable(node: TableNode) {
    val columnCount = maxOf(
        node.headers.size,
        node.rows.maxOfOrNull { it.size } ?: 0,
    )
    if (columnCount == 0) return
    // Numeric columns read best right-aligned: a column is numeric when every
    // non-empty cell starts with a digit (covers "12", "7/10", "68%", "2.4억").
    val numericColumn = BooleanArray(columnCount) { index ->
        val cells = node.rows.mapNotNull { it.getOrNull(index)?.trim()?.takeIf(String::isNotEmpty) }
        cells.isNotEmpty() && cells.all { cell ->
            // Test past inline markers so a model-bolded number ("**12**")
            // still classifies its column as numeric (the cell renders bold
            // via denebUiInlineText below, but alignment keys off the digit).
            val bare = cell.trimStart('*', '_', '~', '`', ' ')
            bare.isNotEmpty() &&
                // Negative values ("-12", "−3") are numeric too (review catch
                // on #3235 — the desktop port surfaced the shared gap).
                (bare.first().isDigit() || ((bare.first() == '-' || bare.first() == '−') && bare.length > 1 && bare[1].isDigit()))
        }
    }

    // Column width follows content: the AVERAGE cell length (in runes) sets
    // each column's share — an average reads truer than the max, where one
    // long outlier cell starved every other column. The header participates
    // as a floor so a short column never collapses under its own label.
    // sqrt squashes the spread so the ratio stays civil (~2.7:1 max). Real
    // briefing tables put the long prose in ANY column (비고, 결과) — the old
    // fixed first-column boost squeezed exactly those columns (2026-07-12
    // corpus audit over production transcript cards).
    val weights = FloatArray(columnCount) { index ->
        val cells = node.rows.mapNotNull { it.getOrNull(index)?.trim()?.takeIf(String::isNotEmpty) }
        val avgRunes = if (cells.isEmpty()) 0 else cells.sumOf { it.length } / cells.size
        val headerRunes = node.headers.getOrNull(index)?.trim()?.length ?: 0
        kotlin.math.sqrt(maxOf(avgRunes, headerRunes).coerceIn(4, 30).toFloat())
    }
    fun columnWeight(index: Int) = weights[index]
    // Numbering-style columns ("#", "1".."99") take their intrinsic width
    // instead of a weight share — the sqrt floor above otherwise hands a
    // single-digit column a fifth of the card width (2026-07-13 fix, shared
    // with the markdown table renderer). Width measures marker-stripped text
    // ("**1**" is one digit) in CJK-aware display units; 9dp per unit is
    // generous so a digit never wraps. All-tiny tables keep the weight layout
    // so they still span the card.
    // Header and cell widths measured separately (marker-stripped, CJK-aware units).
    // Detection keys off CELL data only: a numbering column whose cells are tiny
    // ("1".."5") is tiny even when its label is a short WORD ("단계", 순번). Width sizes
    // the wider of digits and header — but at DIFFERENT rates: digits a generous 9dp
    // (never wrap a figure), a word header the normal 6dp text rate. The old single
    // 9dp/unit over the header left "단계" (4 units → 36dp) still too wide even after it
    // became tiny (2026-07 operator screenshots: #-column narrowed, 단계-column didn't).
    val headerUnits = IntArray(columnCount) { index ->
        displayUnits(bareCellText(node.headers.getOrNull(index)))
    }
    val cellUnits = IntArray(columnCount) { index ->
        var units = 0
        for (row in node.rows) {
            units = maxOf(units, displayUnits(bareCellText(row.getOrNull(index))))
        }
        units
    }
    val tinyColumn = BooleanArray(columnCount) { cellUnits[it] in 1..3 }
    if (tinyColumn.all { it }) tinyColumn.fill(false)
    fun RowScope.cellWidth(index: Int): Modifier = if (tinyColumn[index]) {
        Modifier.width((maxOf(cellUnits[index] * 9, headerUnits[index] * 6) + 4).dp)
    } else {
        Modifier.weight(columnWeight(index))
    }
    val hairline = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.5f)

    // Dense tables (3+ columns) drop one type rung so a narrow column fits
    // whole phrases per line; 2-column label/value tables keep the body voice.
    val dense = columnCount >= 3
    // Phrase-aware wrapping keeps Korean units whole — "10월 단종 카운트다운"
    // breaks at the spaces instead of mid-word ("카/운트다운", production
    // corpus). Cells are short, so the phrase strategy's width cost is nil.
    val cellStyle = (if (dense) MaterialTheme.typography.bodySmall else MaterialTheme.typography.bodyMedium)
        .copy(lineBreak = denebPhraseLineBreak())
    // Tabular figures align digits vertically down a numeric column.
    val numericCellStyle = cellStyle.copy(fontFeatureSettings = "tnum")

    // A visible gutter between columns — weight-divided cells otherwise sit
    // flush and adjacent Korean cells read as one run ("수정계약기한 초과").
    val columnGap = Arrangement.spacedBy(10.dp)
    Column(Modifier.fillMaxWidth().wrapContentHeight()) {
        if (node.headers.isNotEmpty()) {
            Row(Modifier.fillMaxWidth().padding(vertical = 6.dp), horizontalArrangement = columnGap) {
                for (index in 0 until columnCount) {
                    Text(
                        text = denebUiInlineText(node.headers.getOrElse(index) { "" }),
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        textAlign = if (numericColumn[index]) TextAlign.End else TextAlign.Start,
                        modifier = cellWidth(index),
                    )
                }
            }
            HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
        }
        node.rows.forEachIndexed { rowIndex, row ->
            Row(
                Modifier.fillMaxWidth().padding(vertical = 6.dp),
                horizontalArrangement = columnGap,
                // Top-align multi-line cells (the HTML-table reading
                // convention): center-aligning wrapped cells made every tall
                // briefing row look jumbled in the production corpus.
                verticalAlignment = androidx.compose.ui.Alignment.Top,
            ) {
                for (index in 0 until columnCount) {
                    Text(
                        text = denebUiInlineText(row.getOrElse(index) { "" }),
                        style = if (numericColumn[index]) numericCellStyle else cellStyle,
                        textAlign = if (numericColumn[index]) TextAlign.End else TextAlign.Start,
                        modifier = cellWidth(index),
                    )
                }
            }
            if (rowIndex < node.rows.lastIndex) {
                HorizontalDivider(color = hairline)
            }
        }
    }
}

// Marker-stripped cell text for column-width measurement — inline emphasis/code
// markers ("**1**") render invisibly, so they must not count toward width.
private fun bareCellText(cell: String?): String = cell.orEmpty().trim().filterNot { it == '*' || it == '_' || it == '~' || it == '`' }

@Composable
internal fun RenderProgress(node: ProgressNode) {
    Column(Modifier.fillMaxWidth()) {
        // Label row carries the readable number — a bare bar makes the reader
        // eyeball-estimate the one value the node exists to communicate.
        val pct = node.value?.let { "${(it.coerceIn(0f, 1f) * 100).roundToInt()}%" }
            // The authoring contract long showed label="50%" examples — when
            // the label already carries the percent anywhere ("진행률 50%"),
            // a computed twin on the right would duplicate it (review
            // catches on #3228 and #3233).
            ?.takeIf { p -> node.label?.contains(p) != true }
        if (node.label != null || pct != null) {
            Row(
                Modifier.fillMaxWidth().padding(bottom = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = node.label ?: "",
                    style = MaterialTheme.typography.bodyMedium,
                    modifier = Modifier.weight(1f),
                )
                if (pct != null) {
                    Text(
                        text = pct,
                        style = MaterialTheme.typography.labelLarge,
                        fontWeight = FontWeight.Bold,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                }
            }
        }
        if (node.value != null) {
            LinearProgressIndicator(
                progress = { node.value.coerceIn(0f, 1f) },
                modifier = Modifier.fillMaxWidth().height(6.dp).clip(RoundedCornerShape(3.dp)),
                drawStopIndicator = {},
                gapSize = 0.dp,
            )
        } else {
            LinearProgressIndicator(
                modifier = Modifier.fillMaxWidth().height(6.dp).clip(RoundedCornerShape(3.dp)),
                gapSize = 0.dp,
            )
        }
    }
}

@Composable
internal fun RenderCountdown(
    node: CountdownNode,
    isInteractive: Boolean,
    formState: SnapshotStateMap<String, String>,
    toggleState: SnapshotStateMap<String, Boolean>,
    onCallback: (String, Map<String, String>) -> Unit,
) {
    val targetMs = remember { Clock.System.now().toEpochMilliseconds() + node.seconds.toLong() * 1000L }
    var remainingSeconds by remember { mutableStateOf<Long>(node.seconds.toLong()) }
    var expired by remember { mutableStateOf(false) }
    val currentOnCallback by rememberUpdatedState(onCallback)
    val currentIsInteractive by rememberUpdatedState(isInteractive)
    val uriHandler = LocalUriHandler.current
    val clipboardManager = LocalClipboardManager.current

    LaunchedEffect(targetMs) {
        while (true) {
            val diff = (targetMs - Clock.System.now().toEpochMilliseconds()) / 1000L
            remainingSeconds = diff.coerceAtLeast(0L)
            if (diff <= 0L) {
                if (!expired) {
                    expired = true
                    node.id?.let { formState[it] = "0" }
                    if (shouldRunCountdownExpiryAction(currentIsInteractive, node.action)) {
                        try {
                            when (val action = node.action) {
                                is CallbackAction -> {
                                    val data = collectFormData(action, formState)
                                    currentOnCallback(action.event, data)
                                }

                                is ToggleAction -> {
                                    toggleState[action.targetId] = !(toggleState[action.targetId] ?: true)
                                }

                                is OpenUrlAction -> {
                                    uriHandler.openUri(action.url)
                                }

                                is CopyToClipboardAction -> {
                                    clipboardManager.setText(AnnotatedString(action.text))
                                }

                                null -> {}
                            }
                        } catch (_: Exception) {}
                    }
                }
                break
            }
            node.id?.let { formState[it] = diff.toString() }
            delay(1.seconds)
        }
    }

    Column(Modifier.fillMaxWidth()) {
        if (node.label != null) {
            Text(
                text = node.label,
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.padding(bottom = 4.dp),
            )
        }
        val h = remainingSeconds / 3600
        val m = (remainingSeconds % 3600) / 60
        val s = remainingSeconds % 60
        val formatted = if (h > 0) {
            "$h:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}"
        } else {
            "${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}"
        }
        Text(
            text = formatted,
            // Big display number rides the subject rung (22) of the DenebType scale.
            style = DenebType.subject,
            color = if (expired) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface,
        )
    }
}

internal fun shouldRunCountdownExpiryAction(isInteractive: Boolean, action: UiAction?): Boolean = isInteractive && action != null

@Composable
internal fun RenderAlert(node: AlertNode) {
    // Success/warning use the shared status-container pairs from ui/Theme.kt
    // (the roles M3's scheme lacks); error/info stay on their M3 roles.
    val containerColor = when (node.severity) {
        AlertSeverity.SUCCESS -> denebSuccessContainer()
        AlertSeverity.WARNING -> denebWarningContainer()
        AlertSeverity.ERROR -> MaterialTheme.colorScheme.errorContainer
        AlertSeverity.INFO, null -> MaterialTheme.colorScheme.primaryContainer
    }
    val contentColor = when (node.severity) {
        AlertSeverity.SUCCESS -> denebOnSuccessContainer()
        AlertSeverity.WARNING -> denebOnWarningContainer()
        AlertSeverity.ERROR -> MaterialTheme.colorScheme.onErrorContainer
        AlertSeverity.INFO, null -> MaterialTheme.colorScheme.onPrimaryContainer
    }
    Surface(
        color = containerColor,
        contentColor = contentColor,
        shape = RoundedCornerShape(8.dp),
        modifier = Modifier.fillMaxWidth(),
    ) {
        Row(
            modifier = Modifier.padding(12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            AlertIcon(node.severity, contentColor, containerColor)
            Spacer(Modifier.width(12.dp))
            Column {
                if (node.title != null) {
                    Text(
                        text = denebUiInlineText(node.title),
                        style = MaterialTheme.typography.titleSmall,
                        fontWeight = FontWeight.Bold,
                    )
                    Spacer(Modifier.height(2.dp))
                }
                // Alert bodies are prose — models emphasize inline ("**중요**:
                // …"), so route through the tokenizer like text nodes.
                Text(
                    text = denebUiInlineText(node.message),
                    style = MaterialTheme.typography.bodyMedium,
                )
            }
        }
    }
}

@Composable
private fun AlertIcon(severity: AlertSeverity?, contentColor: Color, containerColor: Color) {
    Box(
        contentAlignment = Alignment.Center,
        modifier = Modifier
            .size(20.dp)
            .background(contentColor, androidx.compose.foundation.shape.CircleShape),
    ) {
        when (severity) {
            AlertSeverity.SUCCESS -> Icon(Icons.Default.Check, null, Modifier.size(14.dp), tint = containerColor)
            AlertSeverity.ERROR -> Icon(Icons.Default.Close, null, Modifier.size(14.dp), tint = containerColor)
            AlertSeverity.WARNING -> Text("!", style = MaterialTheme.typography.labelSmall, fontWeight = FontWeight.Bold, color = containerColor)
            AlertSeverity.INFO, null -> Text("i", style = MaterialTheme.typography.labelSmall, fontWeight = FontWeight.Bold, color = containerColor)
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun RenderQuote(node: QuoteNode) {
    Row(
        modifier = Modifier.fillMaxWidth().height(IntrinsicSize.Min),
    ) {
        Box(
            modifier = Modifier
                .width(3.dp)
                .fillMaxHeight()
                .background(MaterialTheme.colorScheme.primary, RoundedCornerShape(1.5.dp)),
        )
        Spacer(Modifier.width(12.dp))
        Column {
            Text(
                text = denebUiInlineText(node.text),
                style = MaterialTheme.typography.bodyLarge,
                fontStyle = FontStyle.Italic,
                color = MaterialTheme.colorScheme.onSurface,
            )
            if (node.source != null) {
                Spacer(Modifier.height(2.dp))
                Text(
                    text = "— ${node.source}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
internal fun RenderBadge(node: BadgeNode) {
    // Soft container tints (2-accent doctrine: small marks, soft fills — never
    // full-saturation chips). success/warning were silently falling through to
    // primary, erasing exactly the status distinction a badge exists for.
    val (backgroundColor, contentColor) = when (node.color) {
        "primary" -> MaterialTheme.colorScheme.primaryContainer to MaterialTheme.colorScheme.onPrimaryContainer
        "secondary" -> MaterialTheme.colorScheme.secondaryContainer to MaterialTheme.colorScheme.onSecondaryContainer
        "error" -> MaterialTheme.colorScheme.errorContainer to MaterialTheme.colorScheme.onErrorContainer
        "success" -> denebSuccessContainer() to denebOnSuccessContainer()
        "warning" -> denebWarningContainer() to denebOnWarningContainer()
        else -> MaterialTheme.colorScheme.surfaceVariant to MaterialTheme.colorScheme.onSurfaceVariant
    }
    Surface(
        color = backgroundColor,
        contentColor = contentColor,
        shape = RoundedCornerShape(12.dp),
    ) {
        Text(
            text = node.value,
            style = MaterialTheme.typography.labelSmall,
            fontWeight = FontWeight.Medium,
            modifier = Modifier.padding(horizontal = 8.dp, vertical = 3.dp),
        )
    }
}

@Composable
internal fun RenderStat(node: StatNode) {
    // No tile box: inside a card, a bordered/filled container reads as an awkward
    // box-in-a-box (2026-07 operator feedback). A KPI is a clean number + unit +
    // label; the row's gap separates siblings. Typography carries it, not a border.
    Column(
        horizontalAlignment = Alignment.CenterHorizontally,
        modifier = Modifier.widthIn(min = 72.dp).padding(vertical = 2.dp),
    ) {
        // Count-up entrance: the numeric run inside the value rolls from 0 to
        // its target (600ms) while prefix/suffix ("$", "톤", "/t") stay fixed;
        // tabular figures keep the digits from jittering as they roll. Static
        // contexts render the final value immediately.
        val motion = LocalDenebUiMotion.current
        // Unit-suffix typography: "381톤"/"68%"/"2.4억" put the number on the subject
        // rung (22, Bold) with a quieter baseline-aligned unit; a ratio ("7/10") or a
        // bare number stays whole. Count-up rolls the number only.
        val unitMatch = Regex("^(-?[\\d,]+(?:\\.\\d+)?)([^\\d\\s]{1,2})$").find(node.value.trim())
        val numPart = unitMatch?.groupValues?.get(1) ?: node.value
        val unitPart = unitMatch?.groupValues?.getOrNull(2)?.takeIf { it.isNotEmpty() }
        val numDisplay = if (motion) statCountUpValue(numPart) else numPart
        Row {
            Text(
                text = numDisplay,
                // Stat value on the subject rung (22); Bold override keeps the metric
                // reading as a number, not a content title (law 3: weight = function).
                style = DenebType.subject.copy(fontFeatureSettings = "tnum"),
                fontWeight = FontWeight.Bold,
                color = MaterialTheme.colorScheme.onSurface,
                modifier = Modifier.alignByBaseline(),
            )
            if (unitPart != null) {
                Text(
                    text = unitPart,
                    style = MaterialTheme.typography.titleSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.alignByBaseline().padding(start = 2.dp),
                )
            }
        }
        Text(
            text = node.label,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        if (node.description != null) {
            // Trend voice: a signed description ("+2.1%", "-14톤", "▲3") is a
            // direction claim — tint it and normalize the arrow glyph so every
            // stat block reads like a proper KPI tile.
            val desc = node.description.trim()
            val positive = desc.startsWith("+") || desc.startsWith("▲")
            val negative = desc.startsWith("-") || desc.startsWith("−") || desc.startsWith("▼")
            val trendColor = when {
                positive -> denebOnSuccessContainer()
                negative -> MaterialTheme.colorScheme.onErrorContainer
                else -> MaterialTheme.colorScheme.onSurfaceVariant
            }
            val chipColor = when {
                positive -> denebSuccessContainer()
                negative -> MaterialTheme.colorScheme.errorContainer
                else -> MaterialTheme.colorScheme.surfaceContainerHigh
            }
            val display = when {
                desc.startsWith("+") -> "▲ " + desc.removePrefix("+")

                desc.startsWith("-") || desc.startsWith("−") ->
                    "▼ " + desc.removePrefix("-").removePrefix("−")

                else -> desc
            }
            // Trend as a tinted pill so the direction reads at a glance, not as a
            // bare colored word (desktop .dui-stat-desc chip parity).
            Surface(
                shape = RoundedCornerShape(999.dp),
                color = chipColor,
                modifier = Modifier.padding(top = 5.dp),
            ) {
                Text(
                    text = display,
                    style = MaterialTheme.typography.labelMedium,
                    fontWeight = if (positive || negative) FontWeight.SemiBold else null,
                    color = trendColor,
                    modifier = Modifier.padding(horizontal = 7.dp, vertical = 2.dp),
                )
            }
        }
    }
}

// Matches the first numeric run (with commas/decimal) inside a stat value.
private val statNumberRe = Regex("""\d[\d,]*(?:\.\d+)?""")

/** Animates the numeric run of a stat value from 0 to its target once. */
@Composable
internal fun statCountUpValue(value: String): String {
    val match = statNumberRe.find(value) ?: return value
    val target = match.value.replace(",", "").toFloatOrNull() ?: return value
    val anim = remember(value) { Animatable(0f) }
    var settled by remember(value) { mutableStateOf(false) }
    LaunchedEffect(value) {
        anim.animateTo(
            target,
            animationSpec = tween(durationMillis = DenebMotion.DurationStatCount, easing = FastOutSlowInEasing),
        )
        settled = true
    }
    // The animation is a transition, not the source of truth: once settled,
    // render the ORIGINAL string so exact metrics ("12.45%", 2-decimal FX)
    // keep their full precision (review catch on #3234). Mid-flight frames
    // format with the target's own decimal width.
    if (settled) return value
    val decimals = match.value.substringAfter('.', "").length
    val grouped = formatStatNumber(anim.value, decimals, match.value.contains(','))
    return value.replaceRange(match.range, grouped)
}

private fun formatStatNumber(v: Float, decimals: Int, grouped: Boolean): String {
    val raw = if (decimals > 0) {
        var scale = 1f
        repeat(decimals) { scale *= 10f }
        val scaled = kotlin.math.round(v * scale) / scale
        val text = scaled.toString()
        val frac = text.substringAfter('.', "")
        // Float.toString may emit fewer digits ("12.4") — pad to the target width.
        if (frac.length >= decimals) text else text + "0".repeat(decimals - frac.length)
    } else {
        kotlin.math.round(v).toLong().toString()
    }
    if (!grouped) return raw
    val parts = raw.split(".")
    val whole = parts[0].reversed().chunked(3).joinToString(",").reversed()
    return if (parts.size > 1) "$whole.${parts[1]}" else whole
}

@Composable
internal fun RenderAvatar(node: AvatarNode) {
    val sizeDp = (node.size ?: 40).coerceIn(24, 80).dp
    if (node.imageUrl != null) {
        Surface(
            shape = androidx.compose.foundation.shape.CircleShape,
            color = MaterialTheme.colorScheme.surfaceContainer,
            modifier = Modifier.size(sizeDp),
        ) {
            coil3.compose.AsyncImage(
                model = node.imageUrl,
                contentDescription = node.name,
                modifier = Modifier.size(sizeDp),
            )
        }
    } else if (node.name != null) {
        val initials = node.name.split(" ")
            .filter { it.isNotEmpty() }
            .take(2)
            .joinToString("") { it.first().uppercase() }
        Surface(
            color = MaterialTheme.colorScheme.primaryContainer,
            contentColor = MaterialTheme.colorScheme.onPrimaryContainer,
            shape = androidx.compose.foundation.shape.CircleShape,
            modifier = Modifier.size(sizeDp),
        ) {
            Box(contentAlignment = Alignment.Center, modifier = Modifier.size(sizeDp)) {
                Text(
                    text = initials,
                    style = MaterialTheme.typography.titleSmall,
                    fontWeight = FontWeight.Bold,
                )
            }
        }
    } else {
        Surface(
            color = MaterialTheme.colorScheme.primaryContainer,
            contentColor = MaterialTheme.colorScheme.onPrimaryContainer,
            shape = androidx.compose.foundation.shape.CircleShape,
            modifier = Modifier.size(sizeDp),
        ) {
            Box(contentAlignment = Alignment.Center, modifier = Modifier.size(sizeDp)) {
                Icon(
                    imageVector = Icons.Default.Person,
                    contentDescription = null,
                    modifier = Modifier.size(sizeDp * 0.6f),
                )
            }
        }
    }
}
