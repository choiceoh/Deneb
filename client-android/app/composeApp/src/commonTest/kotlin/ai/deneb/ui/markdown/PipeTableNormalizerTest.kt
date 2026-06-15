package ai.deneb.ui.markdown

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class PipeTableNormalizerTest {

    @Test
    fun `inserts a missing delimiter row into a bordered pipe table`() {
        val src = """
            || Track | Status |
            || 물품조달 | 진행 |
            || 전기공사 | 도면완료 |
        """.trimMargin().trim()
        val lines = normalizePipeTables(src).lines()
        assertEquals("| Track | Status |", lines[0])
        assertEquals("| --- | --- |", lines[1])
        assertEquals("| 물품조달 | 진행 |", lines[2])
        assertEquals("| 전기공사 | 도면완료 |", lines[3])
    }

    @Test
    fun `parseMarkdown renders a separator-less pipe table as a Table block`() {
        val src = """
            || A | B |
            || 1 | 2 |
        """.trimMargin().trim()
        val table = parseMarkdown(src).blocks.filterIsInstance<Table>().single()
        assertEquals(2, table.headers.size)
        assertEquals(1, table.rows.size)
    }

    @Test
    fun `leaves a genuine markdown table untouched`() {
        val md = "| a | b |\n| --- | --- |\n| 1 | 2 |"
        assertEquals(md, normalizePipeTables(md))
    }

    @Test
    fun `leaves a borderless table with a delimiter untouched`() {
        // No leading/trailing pipes, but it already has a delimiter — BlockScanner owns it.
        val md = "a | b\n--- | ---\n1 | 2"
        assertEquals(md, normalizePipeTables(md))
    }

    @Test
    fun `does not convert borderless prose pipes`() {
        // No leading/trailing pipes and no delimiter — indistinguishable from prose.
        val t = "사과 | 오렌지\n포도 | 바나나"
        assertEquals(t, normalizePipeTables(t))
    }

    @Test
    fun `does not convert a single bordered line`() {
        val t = "| 한 줄 표처럼 보이지만 한 행뿐 |"
        assertEquals(t, normalizePipeTables(t))
    }

    @Test
    fun `does not convert a single-column bordered block`() {
        val t = "| 항목 |\n| 가 |\n| 나 |"
        assertEquals(t, normalizePipeTables(t))
    }

    @Test
    fun `does not convert when column counts are inconsistent`() {
        val t = "| a | b |\n| 1 | 2 | 3 |"
        assertEquals(t, normalizePipeTables(t))
    }

    @Test
    fun `leaves a pipe table inside a fenced code block untouched`() {
        val src = """
            |```
            || A | B |
            || 1 | 2 |
            |```
        """.trimMargin().trim()
        assertEquals(src, normalizePipeTables(src))
    }

    @Test
    fun `keeps the blockquote prefix on a quoted pipe table`() {
        val src = """
            |> | A | B |
            |> | 1 | 2 |
        """.trimMargin().trim()
        val lines = normalizePipeTables(src).lines()
        assertEquals("> | A | B |", lines[0])
        assertEquals("> | --- | --- |", lines[1])
        assertEquals("> | 1 | 2 |", lines[2])
    }

    @Test
    fun `keeps indentation on a pipe table nested under a list`() {
        // Explicit string: trimMargin/trim would strip the indent we test.
        val src = "    | A | B |\n" +
            "    | 1 | 2 |"
        val lines = normalizePipeTables(src).lines()
        assertEquals("    | A | B |", lines[0])
        assertEquals("    | --- | --- |", lines[1])
        assertEquals("    | 1 | 2 |", lines[2])
    }

    @Test
    fun `respects escaped pipes when counting cells`() {
        // `a \| b` is one cell containing a literal pipe, so this is a 2-column table.
        val src = "| 식 | 값 |\n| a \\| b | 3 |"
        val lines = normalizePipeTables(src).lines()
        assertEquals("| --- | --- |", lines[1])
    }

    @Test
    fun `converts a three-column table`() {
        val src = "| 이름 | 역할 | 비고 |\n| 김 | 팀장 | 신규 |\n| 이 | 팀원 | - |"
        val table = parseMarkdown(src).blocks.filterIsInstance<Table>().single()
        assertEquals(3, table.headers.size)
        assertEquals(2, table.rows.size)
    }

    @Test
    fun `does not disturb prose around a recovered table`() {
        val src = "아래 표를 보세요:\n\n| A | B |\n| 1 | 2 |\n\n끝."
        val md = normalizePipeTables(src)
        assertTrue(md.contains("| --- | --- |"), md)
        assertTrue(md.startsWith("아래 표를 보세요:"), md)
        assertTrue(md.trimEnd().endsWith("끝."), md)
    }

    @Test
    fun `unwraps a markdown table the model wrapped in a bare fence`() {
        // dsv4 sometimes fences a real markdown table — it then renders as monospace
        // code (CJK misaligns). A bare fence whose body is ONLY a delimiter-bearing
        // pipe table is unwrapped so the table parser draws it.
        val src = "```\n| Track | Status |\n| --- | --- |\n| 자체조달 | 진행 |\n```"
        val md = normalizePipeTables(src)
        assertTrue(!md.contains("```"), md) // fence dropped
        val table = parseMarkdown(md).blocks.filterIsInstance<Table>().single()
        assertEquals(2, table.headers.size)
        assertEquals(1, table.rows.size)
    }

    @Test
    fun `unwraps a fenced table inside a blockquote, keeping the prefix`() {
        val src = "> ```\n> | A | B |\n> | --- | --- |\n> | 1 | 2 |\n> ```"
        val md = normalizePipeTables(src)
        assertTrue(!md.contains("```"), md)
        assertTrue(md.lines().all { it.startsWith(">") }, md) // quote prefix preserved
    }

    @Test
    fun `leaves fenced code with pipes but no delimiter untouched`() {
        // Shell/code containing `|` is NOT a table (no delimiter row) — stays code.
        val src = "```\ncat a | grep b\n| also | pipes |\n```"
        assertEquals(src, normalizePipeTables(src))
    }

    @Test
    fun `leaves a language-fenced table untouched`() {
        val src = "```text\n| A | B |\n| --- | --- |\n| 1 | 2 |\n```"
        assertEquals(src, normalizePipeTables(src))
    }

    @Test
    fun `does not unwrap a longer fence holding an inner fence and a table`() {
        val src = "````\n```\n| A | B |\n| --- | --- |\n| 1 | 2 |\n```\n````"
        assertEquals(src, normalizePipeTables(src))
    }
}
