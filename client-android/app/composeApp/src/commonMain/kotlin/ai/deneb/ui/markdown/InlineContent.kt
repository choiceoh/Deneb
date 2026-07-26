package ai.deneb.ui.markdown

import ai.deneb.ui.JetBrainsMonoFamily
import ai.deneb.ui.markdown.math.MathFormula
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.TextLayoutResult
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import kotlinx.collections.immutable.ImmutableList

/**
 * Render a list of [InlineNode]s. When no [InlineMath] is present this delegates to a plain
 * [Text] — preserving native text selection, word wrapping, and alignment. When math is
 * present, the inlines are split around each formula and laid out as a [FlowRow] of text
 * and [MathFormula] composables. Formulas stay atomic at wrap boundaries; text segments
 * wrap normally within their own Text.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun InlineContent(
    inlines: ImmutableList<InlineNode>,
    style: TextStyle,
    modifier: Modifier = Modifier,
    textAlign: TextAlign = TextAlign.Unspecified,
) {
    val colors = MaterialTheme.colorScheme
    val monoFamily = JetBrainsMonoFamily()
    if (!containsMath(inlines)) {
        // Cache the AnnotatedString. The remember() avoids a rebuild per streaming token;
        // cachedAnnotatedString additionally survives LazyColumn item disposal, so scrolling
        // the message back into view reuses it instead of rebuilding the spans (keyed by the
        // cached inline-list + colors references, so a theme change still refreshes it).
        val annotated = remember(inlines, colors, monoFamily) {
            cachedAnnotatedString(inlines, colors) { inlines.toAnnotatedString(colors, monoFamily) }
        }
        InlineText(
            annotated = annotated,
            style = style,
            textAlign = textAlign,
            modifier = modifier,
        )
        return
    }

    val segments = remember(inlines) { splitAroundMath(inlines) }
    FlowRow(
        modifier = modifier,
        horizontalArrangement = Arrangement.Start,
        verticalArrangement = Arrangement.Center,
        // Top-align keeps adjacent text anchored when a math child (e.g. a fraction) is tall,
        // which in turn keeps list-bullets aligned with their first line of content.
        itemVerticalAlignment = Alignment.Top,
    ) {
        for (seg in segments) {
            when (seg) {
                is InlineSegment.TextRun -> InlineText(
                    annotated = remember(seg, colors, monoFamily) {
                        seg.nodes.toAnnotatedString(colors, monoFamily).flattenNewlines()
                    },
                    style = style,
                    textAlign = textAlign,
                )

                is InlineSegment.Math -> MathFormula(latex = seg.latex, display = false)
            }
        }
    }
}

/**
 * A [Text] that draws inline-code chips itself.
 *
 * A SpanStyle background is painted per LINE FRAGMENT, so a code span that wraps
 * became two detached pills — `tailscale serve --https=443` on one line and a
 * lone `off` chip on the next, reading as two commands. Spans here are routinely
 * long (`tailscale serve --bg --https=443 http://127.0.0.1:8000`), so wrapping is
 * the normal case, not an edge case.
 *
 * So the chip is drawn from the LAYOUT instead: one rounded rect per line the
 * range covers, each clipped to that line's own start/end. A wrapped span reads
 * as one command continued, and every inline surface — paragraph, list item,
 * heading, table cell, quote — gets it from this single funnel.
 */
@Composable
private fun InlineText(
    annotated: AnnotatedString,
    style: TextStyle,
    textAlign: TextAlign,
    modifier: Modifier = Modifier,
) {
    val codeRanges = remember(annotated) {
        annotated.getStringAnnotations(CODE_SPAN_TAG, 0, annotated.length)
            .map { it.start to it.end }
    }
    if (codeRanges.isEmpty()) {
        Text(text = annotated, style = style, textAlign = textAlign, modifier = modifier)
        return
    }

    var layout by remember { mutableStateOf<TextLayoutResult?>(null) }
    val chip = MaterialTheme.colorScheme.surfaceVariant
    Text(
        text = annotated,
        style = style,
        textAlign = textAlign,
        onTextLayout = { layout = it },
        modifier = modifier.drawBehind {
            val result = layout ?: return@drawBehind
            for ((start, end) in codeRanges) {
                if (start >= end || end > result.layoutInput.text.length) continue
                val firstLine = result.getLineForOffset(start)
                val lastLine = result.getLineForOffset(end - 1)
                for (line in firstLine..lastLine) {
                    // Clip each line's chip to the span, not the whole line: the
                    // first line starts mid-sentence and the last one ends there.
                    val left = if (line == firstLine) result.getHorizontalPosition(start, usePrimaryDirection = true) else result.getLineLeft(line)
                    val right = if (line == lastLine) result.getHorizontalPosition(end, usePrimaryDirection = true) else result.getLineRight(line)
                    if (right <= left) continue
                    val top = result.getLineTop(line)
                    val bottom = result.getLineBottom(line)
                    // Inset vertically so consecutive lines' chips don't fuse into
                    // one slab, and bleed horizontally for the padding a SpanStyle
                    // could never carry.
                    val padX = 3.dp.toPx()
                    val insetY = (bottom - top) * 0.12f
                    drawRoundRect(
                        color = chip,
                        topLeft = Offset(left - padX, top + insetY),
                        size = Size(right - left + padX * 2, bottom - top - insetY * 2),
                        cornerRadius = CornerRadius(4.dp.toPx()),
                    )
                }
            }
        },
    )
}

/** `\n` inside a FlowRow TextRun forces a hard break that breaks flow around math; flatten to spaces. */
private fun AnnotatedString.flattenNewlines(): AnnotatedString = if ('\n' !in text) {
    this
} else {
    AnnotatedString(text.replace('\n', ' '), spanStyles, paragraphStyles)
}

private sealed interface InlineSegment {
    data class TextRun(val nodes: List<InlineNode>) : InlineSegment
    data class Math(val latex: String) : InlineSegment
}

private fun containsMath(nodes: List<InlineNode>): Boolean {
    for (n in nodes) {
        when (n) {
            is InlineMath -> return true
            is Emphasis -> if (containsMath(n.children)) return true
            is Strong -> if (containsMath(n.children)) return true
            is Strike -> if (containsMath(n.children)) return true
            is Underline -> if (containsMath(n.children)) return true
            is Highlight -> if (containsMath(n.children)) return true
            is Superscript -> if (containsMath(n.children)) return true
            is Subscript -> if (containsMath(n.children)) return true
            is Link -> if (containsMath(n.children)) return true
            else -> Unit
        }
    }
    return false
}

private fun splitAroundMath(nodes: List<InlineNode>): List<InlineSegment> {
    val out = mutableListOf<InlineSegment>()
    val current = mutableListOf<InlineNode>()
    for (n in nodes) {
        if (n is InlineMath) {
            if (current.isNotEmpty()) {
                out += InlineSegment.TextRun(current.toList())
                current.clear()
            }
            out += InlineSegment.Math(n.latex)
        } else {
            current += n
        }
    }
    if (current.isNotEmpty()) out += InlineSegment.TextRun(current.toList())
    return out
}
