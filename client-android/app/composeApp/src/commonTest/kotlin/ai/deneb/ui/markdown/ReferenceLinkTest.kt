package ai.deneb.ui.markdown

import kotlinx.collections.immutable.persistentListOf
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class ReferenceLinkTest {

    @Test
    fun `full reference link resolves and definition is dropped`() {
        val doc = parseMarkdown("[공식 문서][1] 참고\n\n[1]: https://example.com")
        assertEquals(1, doc.blocks.size)
        val para = doc.blocks[0] as Paragraph
        val link = para.inlines.filterIsInstance<Link>().single()
        assertEquals("https://example.com", link.href)
        assertEquals(persistentListOf(Text("공식 문서")), link.children)
    }

    @Test
    fun `collapsed reference link uses its text as the label`() {
        val doc = parseMarkdown("[example][] 보기\n\n[example]: https://example.com")
        val link = (doc.blocks[0] as Paragraph).inlines.filterIsInstance<Link>().single()
        assertEquals("https://example.com", link.href)
        assertEquals(persistentListOf(Text("example")), link.children)
    }

    @Test
    fun `unresolved reference stays literal`() {
        val doc = parseMarkdown("[없음][9] 그대로")
        val para = doc.blocks.single() as Paragraph
        assertEquals(persistentListOf(Text("[없음][9] 그대로")), para.inlines)
    }

    @Test
    fun `definition title and angle brackets are stripped`() {
        val doc = parseMarkdown("[a][1]\n\n[1]: <https://example.com/x> \"제목\"")
        val link = (doc.blocks[0] as Paragraph).inlines.filterIsInstance<Link>().single()
        assertEquals("https://example.com/x", link.href)
    }

    @Test
    fun `definition inside a code fence is not collected`() {
        val doc = parseMarkdown("```\n[1]: https://example.com\n```\n\n[a][1]")
        val fence = doc.blocks[0] as CodeFence
        assertEquals("[1]: https://example.com", fence.code)
        val para = doc.blocks[1] as Paragraph
        assertTrue(para.inlines.filterIsInstance<Link>().isEmpty())
    }

    @Test
    fun `korean bracket label line is not a definition`() {
        // "[중요]: 내일 회의 준비" is prose — the destination must look like a URL.
        val doc = parseMarkdown("[중요]: 내일 회의 준비")
        val para = doc.blocks.single() as Paragraph
        assertEquals(persistentListOf(Text("[중요]: 내일 회의 준비")), para.inlines)
    }

    @Test
    fun `label match is case-insensitive`() {
        val doc = parseMarkdown("[Docs][Ref]\n\n[ref]: https://example.com")
        val link = (doc.blocks[0] as Paragraph).inlines.filterIsInstance<Link>().single()
        assertEquals("https://example.com", link.href)
    }

    @Test
    fun `reference link inside a table cell resolves`() {
        val doc = parseMarkdown("| a | b |\n|---|---|\n| [c][1] | d |\n\n[1]: https://example.com")
        val table = doc.blocks[0] as Table
        assertTrue(table.rows[0][0].any { it is Link })
    }

    @Test
    fun `reference link inside a list item resolves`() {
        val doc = parseMarkdown("- [문서][1] 확인\n\n[1]: https://example.com")
        val list = doc.blocks[0] as BulletList
        val para = list.items[0].children[0] as Paragraph
        assertTrue(para.inlines.any { it is Link })
    }

    @Test
    fun `footnote definition is not a link definition`() {
        val doc = parseMarkdown("본문[^1]\n\n[^1]: https://example.com")
        // Footnote def line keeps whatever rendering it had before; the usage stays literal.
        val para = doc.blocks[0] as Paragraph
        assertTrue(para.inlines.filterIsInstance<Link>().isEmpty())
    }
}
