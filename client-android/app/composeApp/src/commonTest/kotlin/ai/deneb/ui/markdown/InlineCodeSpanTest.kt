package ai.deneb.ui.markdown

import androidx.compose.material3.lightColorScheme
import androidx.compose.ui.text.font.FontFamily
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * Inline code must NOT carry a SpanStyle background.
 *
 * Compose paints a span background per LINE FRAGMENT, so a code span that wraps
 * splits into two detached pills — a measured chat message rendered
 * `tailscale serve --https=443` on one line and a lone `off` chip on the next,
 * reading as two separate commands. Spans here are routinely long
 * (`tailscale serve --bg --https=443 http://127.0.0.1:8000`), so wrapping is the
 * normal case and no padding trick avoids it.
 *
 * The mono face plus a muted colour carry "this is code" and both survive a line
 * break intact.
 */
class InlineCodeSpanTest {
    private val colors = lightColorScheme()
    private val mono = FontFamily.Monospace

    private fun codeSpans(text: String) = listOf<InlineNode>(InlineCode(text)).toAnnotatedString(colors, mono).spanStyles

    @Test
    fun inlineCodeHasNoSpanBackground() {
        val spans = codeSpans("tailscale serve --https=443 off")
        assertTrue(spans.isNotEmpty(), "inline code should carry a style")
        for (span in spans) {
            assertNull(
                span.item.background.takeIf { it != androidx.compose.ui.graphics.Color.Unspecified },
                "a span background splits across line wraps into detached pills",
            )
        }
    }

    @Test
    fun inlineCodeUsesTheMonoFaceAndAMutedColour() {
        val span = codeSpans("max_model_len").single()
        assertEquals(mono, span.item.fontFamily)
        // Muted, not `primary`: links already own primary, so sharing it would make
        // a command and a URL read alike.
        assertEquals(colors.onSurfaceVariant, span.item.color)
    }

    @Test
    fun theCodeTextSurvivesVerbatim() {
        // The hair-space padding that used to widen the chip is gone with the chip;
        // nothing may pad or alter the code text itself.
        val code = "tailscale serve --bg --https=443 http://127.0.0.1:8000"
        val rendered = listOf<InlineNode>(InlineCode(code)).toAnnotatedString(colors, mono)
        assertEquals(code, rendered.text)
    }
}
