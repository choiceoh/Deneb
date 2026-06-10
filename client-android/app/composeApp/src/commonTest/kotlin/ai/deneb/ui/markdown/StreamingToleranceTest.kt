package ai.deneb.ui.markdown

import kotlinx.collections.immutable.persistentListOf
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class StreamingToleranceTest {

    @Test
    fun `unclosed code fence yields open fence`() {
        val doc = parseMarkdown("```kotlin\nval x =")
        val fence = doc.blocks.single() as CodeFence
        assertEquals(false, fence.closed)
        assertEquals("kotlin", fence.language)
        assertEquals("val x =", fence.code)
    }

    @Test
    fun `unclosed emphasis yields literal text`() {
        val doc = parseMarkdown("this is *partial")
        val para = doc.blocks.single() as Paragraph
        assertEquals(persistentListOf(Text("this is *partial")), para.inlines)
    }

    @Test
    fun `partial link yields literal text`() {
        val doc = parseMarkdown("before [foo")
        val para = doc.blocks.single() as Paragraph
        assertEquals(persistentListOf(Text("before [foo")), para.inlines)
    }

    @Test
    fun `partial image yields literal text`() {
        val doc = parseMarkdown("before ![alt")
        val para = doc.blocks.single() as Paragraph
        assertEquals(persistentListOf(Text("before ![alt")), para.inlines)
    }

    @Test
    fun `unclosed deneb-ui fence becomes a pending block carrying the body`() {
        // While the fence is open the decode is deferred to render time: a quiet
        // placeholder during streaming, the salvage pipeline on a final reply.
        val md = """
            ```deneb-ui
            {"type":"column","children":[{"type":"text","value":"a"
        """.trimIndent()
        val doc = parseMarkdown(md)
        assertEquals(1, doc.blocks.size)
        val pending = doc.blocks[0]
        assertTrue(pending is DenebUiPending)
        assertTrue("\"type\":\"column\"" in pending.rawBody)
    }

    @Test
    fun `closed deneb-ui fence still decodes immediately`() {
        val md = "```deneb-ui\n{\"type\":\"text\",\"value\":\"a\"}\n```"
        val doc = parseMarkdown(md)
        assertTrue(doc.blocks.single() is DenebUiBlock)
    }

    @Test
    fun `trailing incomplete paragraph still renders`() {
        val doc = parseMarkdown("Full paragraph.\n\nPartial **bold")
        assertEquals(2, doc.blocks.size)
        assertTrue(doc.blocks[1] is Paragraph)
    }

    @Test
    fun `document always renders - even after malformed table`() {
        val doc = parseMarkdown("| a | b |\n| - |")
        assertTrue(doc.blocks.isNotEmpty())
    }

    @Test
    fun `extremely deep blockquote does not crash`() {
        val md = "> ".repeat(10_000) + "leaf"
        val doc = parseMarkdown(md)
        assertTrue(doc.blocks.isNotEmpty())
    }

    @Test
    fun `deeply nested list does not crash`() {
        val md = buildString {
            for (i in 0 until 500) {
                append(" ".repeat(i * 2))
                append("- item\n")
            }
        }
        val doc = parseMarkdown(md)
        assertTrue(doc.blocks.isNotEmpty())
    }

    @Test
    fun `long run of asterisks does not crash`() {
        val doc = parseMarkdown("*".repeat(5_000))
        assertTrue(doc.blocks.isNotEmpty())
    }

    @Test
    fun `long run of bracket openers does not crash`() {
        val doc = parseMarkdown("[".repeat(5_000))
        assertTrue(doc.blocks.isNotEmpty())
    }

    @Test
    fun `huge single paragraph falls back to plain text`() {
        val big = "x".repeat(200_000)
        val doc = parseMarkdown(big)
        val para = doc.blocks.single() as Paragraph
        assertEquals(persistentListOf(Text(big)), para.inlines)
    }

    @Test
    fun `deeply nested emphasis does not crash`() {
        val md = "*".repeat(1_000) + "text" + "*".repeat(1_000)
        val doc = parseMarkdown(md)
        assertTrue(doc.blocks.isNotEmpty())
    }
}
