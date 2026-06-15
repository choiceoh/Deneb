package ai.deneb.ui.markdown

import ai.deneb.ui.markdown.math.MathFormula
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.style.TextAlign
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
    if (!containsMath(inlines)) {
        // Cache the AnnotatedString. The remember() avoids a rebuild per streaming token;
        // cachedAnnotatedString additionally survives LazyColumn item disposal, so scrolling
        // the message back into view reuses it instead of rebuilding the spans (keyed by the
        // cached inline-list + colors references, so a theme change still refreshes it).
        val annotated = remember(inlines, colors) {
            cachedAnnotatedString(inlines, colors) { inlines.toAnnotatedString(colors) }
        }
        Text(
            text = annotated,
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
                is InlineSegment.TextRun -> Text(
                    text = remember(seg, colors) {
                        seg.nodes.toAnnotatedString(colors).flattenNewlines()
                    },
                    style = style,
                    textAlign = textAlign,
                )

                is InlineSegment.Math -> MathFormula(latex = seg.latex, display = false)
            }
        }
    }
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
