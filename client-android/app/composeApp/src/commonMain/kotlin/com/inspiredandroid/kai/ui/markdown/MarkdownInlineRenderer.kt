package com.inspiredandroid.kai.ui.markdown

import androidx.compose.material3.ColorScheme
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextLinkStyles
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.withLink
import androidx.compose.ui.text.withStyle

// Not @Composable: takes the resolved ColorScheme so callers can cache the result
// with remember(inlines, colors). Building the AnnotatedString on every streaming
// token (it was rebuilt per recomposition) was a measurable hot-path cost.
internal fun List<InlineNode>.toAnnotatedString(colors: ColorScheme): AnnotatedString =
    buildAnnotatedString { appendInlines(this@toAnnotatedString, colors) }

private fun AnnotatedString.Builder.appendInlines(nodes: List<InlineNode>, colors: ColorScheme) {
    for (n in nodes) appendInline(n, colors)
}

private fun AnnotatedString.Builder.appendInline(node: InlineNode, colors: ColorScheme) {
    when (node) {
        is Text -> append(node.value)

        is Strong -> withStyle(SpanStyle(fontWeight = FontWeight.Bold)) {
            appendInlines(node.children, colors)
        }

        is Emphasis -> withStyle(SpanStyle(fontStyle = FontStyle.Italic)) {
            appendInlines(node.children, colors)
        }

        is Strike -> withStyle(
            // Struck text is "removed" — mute it so a correction's old value recedes.
            SpanStyle(textDecoration = TextDecoration.LineThrough, color = colors.onSurfaceVariant),
        ) {
            appendInlines(node.children, colors)
        }

        is InlineCode -> withStyle(
            SpanStyle(
                fontFamily = FontFamily.Monospace,
                background = colors.surfaceVariant,
            ),
        ) {
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
            appendInlines(node.children, colors)
        }

        is Image -> append(node.alt)

        LineBreak -> append('\n')

        is InlineMath -> withStyle(SpanStyle(fontFamily = FontFamily.Monospace)) {
            // Fallback path: if math reaches the AnnotatedString builder it means the caller
            // didn't use [InlineContent]. Emit the raw LaTeX so nothing is lost.
            append(node.latex)
        }
    }
}
