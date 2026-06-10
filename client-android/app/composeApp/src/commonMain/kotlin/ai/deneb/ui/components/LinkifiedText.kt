package ai.deneb.ui.components

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextLinkStyles
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.withLink

/**
 * Plain text with tappable URLs — for content that is NOT markdown (the mail
 * body): everything renders verbatim except http(s)/www runs, which become
 * links styled like markdown links (primary color + underline). Wrap in a
 * SelectionContainer at the call site when the text should also be copyable.
 */
@Composable
internal fun LinkifiedText(
    text: String,
    style: TextStyle,
    modifier: Modifier = Modifier,
    color: Color = Color.Unspecified,
) {
    val linkColor = MaterialTheme.colorScheme.primary
    val annotated = remember(text, linkColor) { linkifyUrls(text, linkColor) }
    Text(text = annotated, style = style, color = color, modifier = modifier)
}

private fun linkifyUrls(
    text: String,
    linkColor: Color,
): AnnotatedString {
    val spans = findUrlSpans(text)
    if (spans.isEmpty()) return AnnotatedString(text)
    return buildAnnotatedString {
        var pos = 0
        val linkStyles = TextLinkStyles(
            style = SpanStyle(color = linkColor, textDecoration = TextDecoration.Underline),
        )
        for (span in spans) {
            if (span.start > pos) append(text.substring(pos, span.start))
            withLink(LinkAnnotation.Url(url = span.url, styles = linkStyles)) {
                append(text.substring(span.start, span.end))
            }
            pos = span.end
        }
        if (pos < text.length) append(text.substring(pos))
    }
}
