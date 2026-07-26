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

        is InlineCode -> withStyle(
            SpanStyle(
                fontFamily = monoFamily,
                color = colors.onSurfaceVariant,
            ),
        ) {
            // NO tinted background. A SpanStyle background is painted PER LINE
            // FRAGMENT, so a span that wraps splits into two detached pills — a
            // measured case rendered `tailscale serve --https=443` on one line and
            // a lone `off` chip on the next, reading as two separate commands.
            // Spans here are routinely long (`tailscale serve --bg --https=443
            // http://127.0.0.1:8000`), so wrapping is the NORMAL case, and no
            // padding trick avoids it: Compose has no contiguous cross-line span
            // background. The hair-space padding that used to widen the chip is
            // gone with it.
            //
            // The mono face plus the accent colour already say "code", and both
            // survive a line break intact.
            append(node.code)
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
