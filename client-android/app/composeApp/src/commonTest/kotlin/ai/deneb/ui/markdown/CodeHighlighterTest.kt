package ai.deneb.ui.markdown

import androidx.compose.ui.graphics.Color
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * Syntax highlighter tokenizing. Colors are sentinel values so a span's role
 * (keyword/string/number/comment) is identifiable; the key invariants are that
 * the rendered text always equals the input and overlapping spans resolve by
 * "earliest, then longest" so a keyword inside a comment/string doesn't double-paint.
 */
class CodeHighlighterTest {

    private val kw = Color(0xFF111111)
    private val str = Color(0xFF222222)
    private val num = Color(0xFF333333)
    private val com = Color(0xFF444444)
    private val colors = HighlightColors(keyword = kw, string = str, number = num, comment = com)

    /** Colors applied over each character index, or null where unstyled. */
    private fun colorAt(code: String, lang: String?): List<Color?> {
        val ann = highlightCode(code, lang, colors)
        assertEquals(code, ann.text, "highlighting must never alter the text")
        val out = arrayOfNulls<Color>(code.length)
        for (s in ann.spanStyles) {
            for (i in s.start until s.end) out[i] = s.item.color
        }
        return out.toList()
    }

    private fun colorsUsed(code: String, lang: String?): Set<Color?> = colorAt(code, lang).toSet()

    @Test
    fun `unknown or plain languages render as untouched text`() {
        assertTrue(highlightCode("fun x", null, colors).spanStyles.isEmpty())
        assertTrue(highlightCode("fun x", "text", colors).spanStyles.isEmpty())
        assertTrue(highlightCode("fun x", "plain", colors).spanStyles.isEmpty())
    }

    @Test
    fun `keywords, strings, and numbers each get their color`() {
        val code = "val n = 42"
        val at = colorAt(code, "kotlin")
        assertEquals(kw, at[0]) // "val"
        assertEquals(num, at[8]) // "42"
        assertTrue(str !in colorsUsed(code, "kotlin")) // no string literal here
        assertEquals(str, colorAt("val s = \"hi\"", "kotlin")[8]) // the quote
    }

    @Test
    fun `language aliases share a keyword table`() {
        // ts/js/tsx all map to the JS keyword set.
        for (lang in listOf("js", "ts", "tsx", "javascript")) {
            assertEquals(kw, colorAt("function f", lang)[0], "$lang should highlight 'function'")
        }
    }

    @Test
    fun `a keyword inside a comment is painted as comment, not keyword`() {
        // "// fun" — the comment span wins the overlap; 'fun' must not re-color.
        val at = colorAt("// fun", "kotlin")
        assertTrue(at.all { it == com }, "the whole line is one comment span")
    }

    @Test
    fun `a keyword inside a string literal stays string-colored`() {
        val at = colorAt("\"val\"", "kotlin")
        assertTrue(at.all { it == str }, "the quoted keyword is one string span")
    }

    @Test
    fun `hash-comment languages use # and slash-comment languages use double-slash`() {
        assertEquals(com, colorAt("x = 1  # note", "python").last())
        assertTrue(com !in colorsUsed("x = 1  // note", "python")) // // is not a python comment
        assertEquals(com, colorAt("x := 1 // note", "go").last())
    }

    @Test
    fun `block comments span multiple lines`() {
        val at = colorAt("/* a\nb */ x", "kotlin")
        assertEquals(com, at[0])
        assertEquals(com, at[6]) // still inside the block comment on line 2
    }
}
