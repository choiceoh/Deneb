package ai.deneb.ui.markdown

import kotlinx.collections.immutable.persistentListOf
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotNull
import kotlin.test.assertTrue

class BlockParsingTest {

    @Test
    fun `atx headings h1 through h6`() {
        for (level in 1..6) {
            val hashes = "#".repeat(level)
            val doc = parseMarkdown("$hashes Title")
            val heading = doc.blocks.single() as Heading
            assertEquals(level, heading.level)
            assertEquals(persistentListOf(Text("Title")), heading.inlines)
        }
    }

    @Test
    fun `atx heading allows trailing hashes`() {
        val doc = parseMarkdown("## Title ##")
        val heading = doc.blocks.single() as Heading
        assertEquals(2, heading.level)
        assertEquals("Title", (heading.inlines.single() as Text).value)
    }

    @Test
    fun `setext h1 and h2`() {
        val h1 = parseMarkdown("Title\n===")
        assertEquals(Heading(1, persistentListOf(Text("Title"))), h1.blocks.single())
        val h2 = parseMarkdown("Title\n---")
        assertEquals(Heading(2, persistentListOf(Text("Title"))), h2.blocks.single())
    }

    @Test
    fun `horizontal rules`() {
        assertEquals(HorizontalRule, parseMarkdown("---").blocks.single())
        assertEquals(HorizontalRule, parseMarkdown("***").blocks.single())
        assertEquals(HorizontalRule, parseMarkdown("___").blocks.single())
        assertEquals(HorizontalRule, parseMarkdown("- - -").blocks.single())
    }

    @Test
    fun `fenced code block with language`() {
        val doc = parseMarkdown("```kotlin\nval x = 1\n```")
        val fence = doc.blocks.single() as CodeFence
        assertEquals("kotlin", fence.language)
        assertEquals("val x = 1", fence.code)
        assertTrue(fence.closed)
    }

    @Test
    fun `fenced code block with no language`() {
        val doc = parseMarkdown("```\ncode\n```")
        val fence = doc.blocks.single() as CodeFence
        assertEquals(null, fence.language)
        assertEquals("code", fence.code)
        assertTrue(fence.closed)
    }

    @Test
    fun `unclosed fenced code is rendered with closed=false`() {
        val doc = parseMarkdown("```python\nprint('hi')")
        val fence = doc.blocks.single() as CodeFence
        assertEquals("python", fence.language)
        assertEquals("print('hi')", fence.code)
        assertEquals(false, fence.closed)
    }

    @Test
    fun `tilde fence is supported`() {
        val doc = parseMarkdown("~~~js\nlet x = 1\n~~~")
        val fence = doc.blocks.single() as CodeFence
        assertEquals("js", fence.language)
        assertEquals("let x = 1", fence.code)
    }

    @Test
    fun `blockquote single line`() {
        val doc = parseMarkdown("> quoted")
        val bq = doc.blocks.single() as Blockquote
        val inner = bq.children.single() as Paragraph
        assertEquals("quoted", (inner.inlines.single() as Text).value)
    }

    @Test
    fun `blockquote multiple lines`() {
        val doc = parseMarkdown("> line 1\n> line 2")
        val bq = doc.blocks.single() as Blockquote
        val inner = bq.children.single() as Paragraph
        assertEquals("line 1\nline 2", (inner.inlines.single() as Text).value)
    }

    @Test
    fun `bullet list with dash`() {
        val doc = parseMarkdown("- a\n- b\n- c")
        val list = doc.blocks.single() as BulletList
        assertEquals(3, list.items.size)
        assertTrue(list.tight)
        assertEquals("a", (list.items[0].children.single() as Paragraph).inlines.joinToString("") { (it as Text).value })
    }

    @Test
    fun `ordered list starting at 5`() {
        val doc = parseMarkdown("5. first\n6. second")
        val list = doc.blocks.single() as OrderedList
        assertEquals(5, list.start)
        assertEquals(2, list.items.size)
    }

    @Test
    fun `loose list via blank line between items`() {
        val doc = parseMarkdown("- a\n\n- b")
        val list = doc.blocks.single() as BulletList
        assertEquals(2, list.items.size)
        assertEquals(false, list.tight)
    }

    @Test
    fun `nested bullet list`() {
        val doc = parseMarkdown("- outer\n  - inner1\n  - inner2\n- second")
        val outer = doc.blocks.single() as BulletList
        assertEquals(2, outer.items.size)
        val first = outer.items[0]
        val nested = first.children.firstOrNull { it is BulletList } as? BulletList
        assertNotNull(nested)
        assertEquals(2, nested.items.size)
    }

    @Test
    fun `simple table with alignment`() {
        val doc = parseMarkdown("| a | b | c |\n| :- | :-: | -: |\n| 1 | 2 | 3 |")
        val table = doc.blocks.single() as Table
        assertEquals(listOf(ColumnAlign.LEFT, ColumnAlign.CENTER, ColumnAlign.RIGHT), table.alignments)
        assertEquals(3, table.headers.size)
        assertEquals(1, table.rows.size)
        assertEquals("1", (table.rows[0][0].single() as Text).value)
    }

    @Test
    fun `table without outer pipes`() {
        val doc = parseMarkdown("a | b\n---|---\n1 | 2")
        val table = doc.blocks.single() as Table
        assertEquals(2, table.headers.size)
        assertEquals(1, table.rows.size)
    }

    @Test
    fun `details with summary becomes a collapsible`() {
        val doc = parseMarkdown("<details>\n<summary>요약 제목</summary>\n\n본문 내용\n</details>")
        val c = doc.blocks.single() as Collapsible
        assertEquals("요약 제목", (c.summary.single() as Text).value)
        assertEquals(false, c.initiallyOpen)
        assertEquals("본문 내용", ((c.children.single() as Paragraph).inlines.single() as Text).value)
    }

    @Test
    fun `details open attribute starts expanded`() {
        val doc = parseMarkdown("<details open>\n<summary>제목</summary>\n본문\n</details>")
        assertTrue((doc.blocks.single() as Collapsible).initiallyOpen)
    }

    @Test
    fun `details without summary uses a default header`() {
        val c = parseMarkdown("<details>\n본문만\n</details>").blocks.single() as Collapsible
        assertEquals("자세히", (c.summary.single() as Text).value)
        assertEquals("본문만", ((c.children.single() as Paragraph).inlines.single() as Text).value)
    }

    @Test
    fun `br in a table cell becomes a line break`() {
        // Markdown tables can't hold a real newline (it ends the row), so the model
        // uses <br> for an in-cell line break. The cell tokenizes it to a LineBreak so
        // it renders on two lines instead of showing a literal "<br>".
        val doc = parseMarkdown("| 항목 | 비고 |\n| --- | --- |\n| 착수신고 | 6/2 지연<br>미확인 |")
        val table = doc.blocks.single() as Table
        val noteCell = table.rows[0][1]
        assertTrue(noteCell.any { it is LineBreak }, noteCell.toString())
        assertTrue(noteCell.none { it is Text && it.value.contains("<br>") }, noteCell.toString())
    }

    @Test
    fun `multiple paragraphs separated by blank line`() {
        val doc = parseMarkdown("first\n\nsecond")
        assertEquals(2, doc.blocks.size)
        assertTrue(doc.blocks[0] is Paragraph)
        assertTrue(doc.blocks[1] is Paragraph)
    }

    @Test
    fun `paragraph ends at heading opener`() {
        val doc = parseMarkdown("para\n# heading")
        assertEquals(2, doc.blocks.size)
        assertTrue(doc.blocks[0] is Paragraph)
        assertTrue(doc.blocks[1] is Heading)
    }

    @Test
    fun `empty input produces empty document`() {
        assertEquals(emptyList(), parseMarkdown("").blocks)
        assertEquals(emptyList(), parseMarkdown("   \n  ").blocks)
    }

    @Test
    fun `unchecked task list item carries checked=false with marker stripped`() {
        val item = (parseMarkdown("- [ ] todo").blocks.single() as BulletList).items.single()
        assertEquals(false, item.checked)
        assertEquals("todo", ((item.children.single() as Paragraph).inlines.single() as Text).value)
    }

    @Test
    fun `checked task list item carries checked=true`() {
        val item = (parseMarkdown("- [x] done").blocks.single() as BulletList).items.single()
        assertEquals(true, item.checked)
        assertEquals("done", ((item.children.single() as Paragraph).inlines.single() as Text).value)
    }

    @Test
    fun `ordinary bullet item has null checked`() {
        val item = (parseMarkdown("- regular").blocks.single() as BulletList).items.single()
        assertEquals(null, item.checked)
    }

    @Test
    fun `ordered item keeps task marker as text`() {
        // Ordered "1. [x]" is not lifted (would drop the number), so it stays literal.
        val item = (parseMarkdown("1. [x] keep").blocks.single() as OrderedList).items.single()
        assertEquals(null, item.checked)
    }

    @Test
    fun `unicode bullet is a bullet list`() {
        val list = parseMarkdown("• 첫째\n• 둘째").blocks.single() as BulletList
        assertEquals(2, list.items.size)
        assertEquals("첫째", ((list.items[0].children.single() as Paragraph).inlines.single() as Text).value)
    }

    @Test
    fun `triangle bullet is a bullet list`() {
        val list = parseMarkdown("▸ a\n▸ b").blocks.single() as BulletList
        assertEquals(2, list.items.size)
    }

    @Test
    fun `box-drawing runs are horizontal rules`() {
        assertEquals(HorizontalRule, parseMarkdown("━━━━━━").blocks.single())
        assertEquals(HorizontalRule, parseMarkdown("──────").blocks.single())
        assertEquals(HorizontalRule, parseMarkdown("══════").blocks.single())
    }

    @Test
    fun `mixed box-drawing tree art is not a horizontal rule`() {
        // "├─ foo" is tree art, not a separator — must stay text, not collapse to an HR.
        assertTrue(parseMarkdown("├─ foo").blocks.single() is Paragraph)
    }

    @Test
    fun `circled numbers are an ordered list`() {
        val list = parseMarkdown("① 첫째\n② 둘째\n③ 셋째").blocks.single() as OrderedList
        assertEquals(1, list.start)
        assertEquals(3, list.items.size)
        assertEquals("첫째", ((list.items[0].children.single() as Paragraph).inlines.single() as Text).value)
    }

    @Test
    fun `circled number with separator starts at its value`() {
        val list = parseMarkdown("③. 셋\n④. 넷").blocks.single() as OrderedList
        assertEquals(3, list.start)
        assertEquals(2, list.items.size)
    }

    @Test
    fun `table directly under a paragraph line is still a table`() {
        // LLMs often skip the blank line between a lead-in sentence and the table.
        val doc = parseMarkdown("요약:\n| 항목 | 값 |\n|---|---|\n| 매출 | 1억 |")
        assertEquals(2, doc.blocks.size)
        assertTrue(doc.blocks[0] is Paragraph)
        val table = doc.blocks[1] as Table
        assertEquals(2, table.headers.size)
        assertEquals(1, table.rows.size)
    }

    @Test
    fun `pipe line without separator stays in the paragraph`() {
        val doc = parseMarkdown("코드는 a | b 형태로\n그냥 본문이다")
        assertTrue(doc.blocks.single() is Paragraph)
    }

    @Test
    fun `korean date line is not an ordered list`() {
        // "2026. 6. 9." is the standard Korean date format — must stay prose, not item #2026.
        val doc = parseMarkdown("2026. 6. 9. 전체 회의")
        assertTrue(doc.blocks.single() is Paragraph)
    }

    @Test
    fun `three digit ordered marker still lists`() {
        val list = parseMarkdown("100. 백\n101. 백일").blocks.single() as OrderedList
        assertEquals(100, list.start)
        assertEquals(2, list.items.size)
    }
}
