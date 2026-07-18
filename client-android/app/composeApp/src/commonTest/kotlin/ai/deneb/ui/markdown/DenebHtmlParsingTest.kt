package ai.deneb.ui.markdown

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertIs
import kotlin.test.assertTrue

// ```deneb-html — webpage-style HTML answer routing: closed fence → sandboxed
// block, unclosed fence → pending (scripts must not run mid-stream), and the
// plain-text projection keeps the source readable.
class DenebHtmlParsingTest {

    @Test
    fun `closed deneb-html fence becomes a DenebHtmlBlock between prose`() {
        val doc = parseMarkdown("앞 설명\n```deneb-html\n<!doctype html>\n<div>페이지</div>\n```\n뒤 설명")
        assertEquals(3, doc.blocks.size)
        val html = assertIs<DenebHtmlBlock>(doc.blocks[1])
        assertEquals("<!doctype html>\n<div>페이지</div>", html.html)
    }

    @Test
    fun `unclosed deneb-html fence stays pending`() {
        val doc = parseMarkdown("```deneb-html\n<div>아직")
        assertIs<DenebHtmlPending>(doc.blocks.single())
    }

    @Test
    fun `deneb-html does not shadow deneb-ui routing`() {
        val doc = parseMarkdown("```deneb-ui\n<column><text>카드</text></column>\n```")
        assertIs<DenebUiBlock>(doc.blocks.single())
    }

    @Test
    fun `plain projection carries the document source, speakable skips it`() {
        val doc = parseMarkdown("```deneb-html\n<div>본문</div>\n```")
        assertTrue(doc.toPlainText().contains("<div>본문</div>"))
        assertEquals("", doc.toSpeakableText())
    }
}
