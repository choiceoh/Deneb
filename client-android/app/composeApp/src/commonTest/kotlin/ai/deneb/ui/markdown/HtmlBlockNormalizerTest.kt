package ai.deneb.ui.markdown

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class HtmlBlockNormalizerTest {

    @Test
    fun `heading tags become ATX headings`() {
        assertEquals("## 제목", normalizeHtmlBlocks("<h2>제목</h2>"))
        assertEquals("###### 작게", normalizeHtmlBlocks("<h6>작게</h6>"))
        val h = parseMarkdown("<h3>섹션</h3>").blocks.single() as Heading
        assertEquals(3, h.level)
        assertEquals("섹션", (h.inlines.single() as Text).value)
    }

    @Test
    fun `hr tag becomes a horizontal rule`() {
        assertEquals("---", normalizeHtmlBlocks("<hr>"))
        assertEquals("---", normalizeHtmlBlocks("<hr/>"))
        assertEquals("---", normalizeHtmlBlocks("<hr />"))
        assertEquals(HorizontalRule, parseMarkdown("<hr>").blocks.single())
    }

    @Test
    fun `paragraph tags are stripped to their text`() {
        assertTrue(normalizeHtmlBlocks("<p>본문 내용</p>").contains("본문 내용"))
        assertTrue(!normalizeHtmlBlocks("<p>본문 내용</p>").contains("<p>"))
        val p = parseMarkdown("<p>안녕하세요</p>").blocks.single() as Paragraph
        assertEquals("안녕하세요", (p.inlines.single() as Text).value)
    }

    @Test
    fun `unordered list tags become a bullet list`() {
        val md = normalizeHtmlBlocks("<ul>\n<li>첫째</li>\n<li>둘째</li>\n</ul>")
        assertEquals("- 첫째\n- 둘째", md)
        val list = parseMarkdown("<ul>\n<li>a</li>\n<li>b</li>\n</ul>").blocks.single() as BulletList
        assertEquals(2, list.items.size)
    }

    @Test
    fun `ordered list honors the start attribute and numbers items`() {
        val md = normalizeHtmlBlocks("<ol start=\"3\">\n<li>x</li>\n<li>y</li>\n</ol>")
        assertEquals("3. x\n4. y", md)
        val list = parseMarkdown("<ol>\n<li>a</li>\n<li>b</li>\n</ol>").blocks.single() as OrderedList
        assertEquals(1, list.start)
        assertEquals(2, list.items.size)
    }

    @Test
    fun `nested list indents the inner level`() {
        val md = normalizeHtmlBlocks("<ul>\n<li>겉</li>\n<ul>\n<li>속</li>\n</ul>\n</ul>")
        assertEquals("- 겉\n  - 속", md)
    }

    @Test
    fun `blockquote tag becomes a markdown quote`() {
        assertEquals("> 인용문", normalizeHtmlBlocks("<blockquote>인용문</blockquote>"))
        val bq = parseMarkdown("<blockquote>인용문</blockquote>").blocks.single() as Blockquote
        assertEquals("인용문", ((bq.children.single() as Paragraph).inlines.single() as Text).value)
    }

    @Test
    fun `html inside a fenced code block is left literal`() {
        val src = "```\n<h1>예시</h1>\n<hr>\n```"
        assertEquals(src, normalizeHtmlBlocks(src))
    }

    @Test
    fun `inline html mid-line is not converted`() {
        // Conversions are whole-line only; a tag embedded in prose stays untouched here.
        val src = "보세요 <p>중요</p> 입니다"
        assertEquals(src, normalizeHtmlBlocks(src))
    }

    @Test
    fun `text without angle brackets is returned unchanged`() {
        val src = "그냥 문장.\n둘째 줄."
        assertEquals(src, normalizeHtmlBlocks(src))
    }
}
