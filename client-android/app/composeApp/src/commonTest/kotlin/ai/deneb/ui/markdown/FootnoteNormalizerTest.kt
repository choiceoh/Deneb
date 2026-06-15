package ai.deneb.ui.markdown

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class FootnoteNormalizerTest {

    @Test
    fun `converts a reference to a superscript and appends the note`() {
        val src = "본문 사실입니다[^1].\n\n[^1]: 출처는 내부 문서."
        val out = normalizeFootnotes(src)
        assertTrue(out.contains("본문 사실입니다¹."), out)
        assertFalse(out.contains("[^1]"), out) // reference + definition both gone
        assertTrue(out.contains("---"), out) // a footnotes divider was appended
        assertTrue(out.contains("¹ 출처는 내부 문서."), out)
    }

    @Test
    fun `parseMarkdown renders the note as a separate block after a rule`() {
        val src = "주장[^1].\n\n[^1]: 근거."
        val blocks = parseMarkdown(src).blocks
        assertTrue(blocks.any { it is HorizontalRule }, "expected a rule: $blocks")
        // The note text survives as a rendered paragraph.
        assertTrue(blocks.filterIsInstance<Paragraph>().isNotEmpty())
    }

    @Test
    fun `numbers multiple footnotes by order of first reference`() {
        val src = "가[^a] 나[^b].\n\n[^a]: 첫째\n[^b]: 둘째"
        val out = normalizeFootnotes(src)
        assertTrue(out.contains("가¹ 나²."), out)
        assertTrue(out.contains("¹ 첫째"), out)
        assertTrue(out.contains("² 둘째"), out)
    }

    @Test
    fun `reuses one number for repeated references to the same id`() {
        val src = "처음[^x] 그리고 다시[^x].\n\n[^x]: 같은 노트"
        val out = normalizeFootnotes(src)
        assertTrue(out.contains("처음¹ 그리고 다시¹."), out)
        // The note appears exactly once in the section.
        assertEquals(1, Regex("¹ 같은 노트").findAll(out).count(), out)
    }

    @Test
    fun `numbers two-digit footnotes with multi-glyph superscripts`() {
        val refs = (1..12).joinToString(" ") { "표시[^$it]" }
        val defsBlock = (1..12).joinToString("\n") { "[^$it]: 노트$it" }
        val out = normalizeFootnotes("$refs\n\n$defsBlock")
        assertTrue(out.contains("¹⁰ 노트10"), out)
        assertTrue(out.contains("¹² 노트12"), out)
    }

    @Test
    fun `leaves an undefined reference literal`() {
        val src = "정의됨[^ok] 미정의[^missing].\n\n[^ok]: 노트"
        val out = normalizeFootnotes(src)
        assertTrue(out.contains("정의됨¹"), out)
        assertTrue(out.contains("[^missing]"), out) // undefined → kept literal
    }

    @Test
    fun `returns input untouched when no reference matches a definition`() {
        // A definition with no matching reference must not silently drop content.
        val src = "본문만 있습니다. [^1] 같은 표시는 없음."
        assertEquals(src, normalizeFootnotes(src))
    }

    @Test
    fun `does not touch text without any footnote syntax`() {
        val t = "그냥 평범한 문장.\n둘째 줄."
        assertEquals(t, normalizeFootnotes(t))
    }

    @Test
    fun `does not convert a reference inside an inline code span`() {
        val src = "코드 `[^1]` 는 그대로, 본문[^1] 은 변환.\n\n[^1]: 노트"
        val out = normalizeFootnotes(src)
        assertTrue(out.contains("`[^1]`"), out) // inside code → literal
        assertTrue(out.contains("본문¹ 은 변환."), out) // outside code → superscript
    }

    @Test
    fun `does not touch footnotes inside a fenced code block`() {
        val src = "```\n참조[^1]\n[^1]: 노트\n```"
        assertEquals(src, normalizeFootnotes(src))
    }
}
