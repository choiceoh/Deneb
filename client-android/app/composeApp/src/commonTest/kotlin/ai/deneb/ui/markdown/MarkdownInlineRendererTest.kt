package ai.deneb.ui.markdown

import androidx.compose.material3.lightColorScheme
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.BaselineShift
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.unit.em
import kotlinx.collections.immutable.persistentListOf
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class MarkdownInlineRendererTest {

    private val colors = lightColorScheme()

    @Test
    fun plainTextImagesLineBreaksAndMathPreserveVisibleContent() {
        val rendered = listOf(
            Text("before "),
            Image("https://example.test/image.png", "diagram"),
            LineBreak,
            InlineMath("x^2 + y^2"),
        ).toAnnotatedString(colors, FontFamily.Monospace)

        assertEquals("before diagram\nx^2 + y^2", rendered.text)
    }

    @Test
    fun emptyInlineListProducesEmptyUnstyledString() {
        val rendered = emptyList<InlineNode>().toAnnotatedString(colors, FontFamily.Monospace)

        assertEquals("", rendered.text)
        assertTrue(rendered.spanStyles.isEmpty())
    }

    @Test
    fun strongAndEmphasisApplyNestedFontStylesToExactRanges() {
        val rendered = listOf(
            Strong(persistentListOf(Text("bold "), Emphasis(persistentListOf(Text("both"))))),
            Text(" plain"),
        ).toAnnotatedString(colors, FontFamily.Monospace)

        assertEquals("bold both plain", rendered.text)
        val bold = rendered.spanStyles.single { it.item.fontWeight == FontWeight.Bold }
        val italic = rendered.spanStyles.single { it.item.fontStyle == FontStyle.Italic }
        assertEquals(0, bold.start)
        assertEquals(9, bold.end)
        assertEquals(5, italic.start)
        assertEquals(9, italic.end)
    }

    @Test
    fun strikeUsesMutedColorAndUnderlineKeepsNormalColor() {
        val rendered = listOf(
            Strike(persistentListOf(Text("removed"))),
            Text(" "),
            Underline(persistentListOf(Text("kept"))),
        ).toAnnotatedString(colors, FontFamily.Monospace)

        val strike = rendered.spanStyles.single { it.item.textDecoration == TextDecoration.LineThrough }
        val underline = rendered.spanStyles.single { it.item.textDecoration == TextDecoration.Underline }
        assertEquals(colors.onSurfaceVariant, strike.item.color)
        assertEquals(0, strike.start)
        assertEquals(7, strike.end)
        assertEquals(8, underline.start)
        assertEquals(12, underline.end)
    }

    @Test
    fun highlightUsesThirtyPercentPrimaryBackground() {
        val rendered = listOf(Highlight(persistentListOf(Text("marked")))).toAnnotatedString(colors, FontFamily.Monospace)

        val style = rendered.spanStyles.single().item
        assertEquals(colors.primary.copy(alpha = 0.30f), style.background)
    }

    @Test
    fun superAndSubscriptApplyBaselineAndReducedFontSize() {
        val rendered = listOf(
            Text("x"),
            Superscript(persistentListOf(Text("2"))),
            Text(" H"),
            Subscript(persistentListOf(Text("2"))),
            Text("O"),
        ).toAnnotatedString(colors, FontFamily.Monospace)

        assertEquals("x2 H2O", rendered.text)
        val superStyle = rendered.spanStyles.single { it.item.baselineShift == BaselineShift.Superscript }
        val subStyle = rendered.spanStyles.single { it.item.baselineShift == BaselineShift.Subscript }
        assertEquals(0.75.em, superStyle.item.fontSize)
        assertEquals(0.75.em, subStyle.item.fontSize)
        assertEquals(1, superStyle.start)
        assertEquals(2, superStyle.end)
        assertEquals(4, subStyle.start)
        assertEquals(5, subStyle.end)
    }

    @Test
    fun inlineCodeIsMonospaceAndMutedWithNoBackgroundOrPadding() {
        val rendered = listOf(Text("run "), InlineCode("git status"), Text(" now")).toAnnotatedString(colors, FontFamily.Monospace)

        // The tinted chip is gone: Compose paints a span background per LINE
        // FRAGMENT, so a wrapping code span split into two detached pills
        // (`tailscale serve --https=443` plus a lone `off`). The hair-space
        // padding that widened that chip went with it — the code text is verbatim.
        assertEquals("run git status now", rendered.text)
        val code = rendered.spanStyles.single { it.item.fontFamily == FontFamily.Monospace }
        assertEquals(Color.Unspecified, code.item.background)
        // Muted, not primary — links own primary, and sharing it would make a
        // command and a URL read alike.
        assertEquals(colors.onSurfaceVariant, code.item.color)
        assertEquals("git status", rendered.text.substring(code.start, code.end))
    }

    @Test
    fun linkCarriesUrlAndVisibleChildrenWithoutBold() {
        val rendered = listOf(
            Text("see "),
            Link("https://example.test/path", persistentListOf(Text("example"))),
        ).toAnnotatedString(colors, FontFamily.Monospace)

        assertEquals("see example", rendered.text)
        val annotation = rendered.getLinkAnnotations(0, rendered.length).single()
        val url = annotation.item as LinkAnnotation.Url
        assertEquals("https://example.test/path", url.url)
        assertEquals(4, annotation.start)
        assertEquals(11, annotation.end)
        assertEquals(colors.primary, url.styles?.style?.color)
        assertEquals(TextDecoration.Underline, url.styles?.style?.textDecoration)
        assertTrue(rendered.spanStyles.none { it.item.fontWeight == FontWeight.Bold })
    }

    @Test
    fun multipleLinksKeepIndependentAnnotationsAndRanges() {
        val rendered = listOf(
            Link("https://one.test", persistentListOf(Text("one"))),
            Text(" and "),
            Link("mailto:two@example.test", persistentListOf(Text("two"))),
        ).toAnnotatedString(colors, FontFamily.Monospace)

        val links = rendered.getLinkAnnotations(0, rendered.length)

        assertEquals("one and two", rendered.text)
        assertEquals(2, links.size)
        assertEquals("https://one.test", (links[0].item as LinkAnnotation.Url).url)
        assertEquals(0, links[0].start)
        assertEquals(3, links[0].end)
        assertEquals("mailto:two@example.test", (links[1].item as LinkAnnotation.Url).url)
        assertEquals(8, links[1].start)
        assertEquals(11, links[1].end)
    }

    @Test
    fun emptyImageAltAndEmptyFormattingChildrenAddNoVisibleText() {
        val rendered = listOf(
            Image("https://example.test/image.png", ""),
            Strong(persistentListOf()),
            Emphasis(persistentListOf()),
            Text("visible"),
        ).toAnnotatedString(colors, FontFamily.Monospace)

        assertEquals("visible", rendered.text)
        assertTrue(rendered.spanStyles.all { it.start == it.end })
    }

    @Test
    fun nestedFormattingPreservesTextAndAllApplicableRanges() {
        val rendered = listOf(
            Strong(
                persistentListOf(
                    Text("bold"),
                    Underline(persistentListOf(Text(" under"))),
                    InlineCode("code"),
                ),
            ),
        ).toAnnotatedString(colors, FontFamily.Monospace)

        assertEquals("bold undercode", rendered.text)
        assertTrue(rendered.spanStyles.any { it.item.fontWeight == FontWeight.Bold && it.start == 0 && it.end == rendered.length })
        assertTrue(rendered.spanStyles.any { it.item.textDecoration == TextDecoration.Underline })
        assertTrue(rendered.spanStyles.any { it.item.fontFamily == FontFamily.Monospace })
    }
}
