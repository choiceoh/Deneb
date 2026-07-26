package ai.deneb.ui.markdown

import androidx.compose.material3.ColorScheme
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextLinkStyles
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.BaselineShift
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.withLink
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.em

// Not @Composable: takes the resolved ColorScheme so callers can cache the result
// with remember(inlines, colors). Building the AnnotatedString on every streaming
// token (it was rebuilt per recomposition) was a measurable hot-path cost.
/**
 * Annotation tag marking an inline-code range. [InlineContent] reads these back
 * off the laid-out text and draws one rounded chip per LINE the range covers —
 * the continuity a SpanStyle background cannot give, because Compose paints span
 * backgrounds per line fragment and a wrapping span becomes two detached pills.
 */
internal const val CODE_SPAN_TAG = "deneb.code"

internal fun List<InlineNode>.toAnnotatedString(colors: ColorScheme, monoFamily: FontFamily): AnnotatedString = buildAnnotatedString { appendInlines(this@toAnnotatedString, colors, monoFamily) }

private fun AnnotatedString.Builder.appendInlines(nodes: List<InlineNode>, colors: ColorScheme, monoFamily: FontFamily) {
    for (n in nodes) appendInline(n, colors, monoFamily)
}

private fun AnnotatedString.Builder.appendInline(node: InlineNode, colors: ColorScheme, monoFamily: FontFamily) {
    when (node) {
        is Text -> append(node.value)

        is Strong -> withStyle(SpanStyle(fontWeight = FontWeight.Bold)) {
            appendInlines(node.children, colors, monoFamily)
        }

        is Emphasis -> withStyle(SpanStyle(fontStyle = FontStyle.Italic)) {
            appendInlines(node.children, colors, monoFamily)
        }

        is Strike -> withStyle(
            // Struck text is "removed" — mute it so a correction's old value recedes.
            SpanStyle(textDecoration = TextDecoration.LineThrough, color = colors.onSurfaceVariant),
        ) {
            appendInlines(node.children, colors, monoFamily)
        }

        is Underline -> withStyle(SpanStyle(textDecoration = TextDecoration.Underline)) {
            appendInlines(node.children, colors, monoFamily)
        }

        is Highlight -> withStyle(
            // A soft cool wash behind the glyphs (the doctrine's primary "소프트 fill"); body
            // text color is left as-is so the highlight reads as emphasis, not a recolor.
            SpanStyle(background = colors.primary.copy(alpha = 0.30f)),
        ) {
            appendInlines(node.children, colors, monoFamily)
        }

        is Superscript -> withStyle(SpanStyle(baselineShift = BaselineShift.Superscript, fontSize = 0.75.em)) {
            appendInlines(node.children, colors, monoFamily)
        }

        is Subscript -> withStyle(SpanStyle(baselineShift = BaselineShift.Subscript, fontSize = 0.75.em)) {
            appendInlines(node.children, colors, monoFamily)
        }

        // The chip is NOT a SpanStyle background: Compose paints those per line
        // FRAGMENT, so a wrapping span split into two detached pills (a measured
        // case rendered `tailscale serve --https=443` on one line and a lone `off`
        // chip on the next, reading as two commands). Spans here are routinely
        // long, so wrapping is the normal case.
        //
        // Instead the range is TAGGED here and InlineContent draws a rounded chip
        // per line from the text layout — the cross-line continuity Compose has no
        // span style for.
        is InlineCode -> {
            pushStringAnnotation(tag = CODE_SPAN_TAG, annotation = "")
            withStyle(
                SpanStyle(
                    fontFamily = monoFamily,
                    color = colors.onSurfaceVariant,
                ),
            ) {
                append(node.code)
            }
            pop()
        }

        is Link -> withLink(
            LinkAnnotation.Url(
                url = node.href,
                styles = TextLinkStyles(
                    // Colour + underline is enough; bold made links shout against
                    // the body text.
                    style = SpanStyle(
                        color = colors.primary,
                        textDecoration = TextDecoration.Underline,
                    ),
                ),
            ),
        ) {
            appendInlines(node.children, colors, monoFamily)
        }

        is Image -> append(node.alt)

        LineBreak -> append('\n')

        is InlineMath -> withStyle(SpanStyle(fontFamily = monoFamily)) {
            // Fallback path: if math reaches the AnnotatedString builder it means the caller
            // didn't use [InlineContent]. Emit the raw LaTeX so nothing is lost.
            append(node.latex)
        }
    }
}
