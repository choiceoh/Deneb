package ai.deneb.ui.markdown

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class BoxTableNormalizerTest {

    @Test
    fun `converts a box-drawing table to a markdown table`() {
        val box = """
            |┌────────┬────────┬────────┐
            |│ Track  │ Status │ Note   │
            |├────────┼────────┼────────┤
            |│ 물품조달 │ 진행   │ 규격서  │
            |│ 전기공사 │ 진행   │ 도면완료 │
            |└────────┴────────┴────────┘
        """.trimMargin().trim()
        val lines = normalizeBoxTables(box).lines()
        assertEquals("| Track | Status | Note |", lines[0])
        assertEquals("| --- | --- | --- |", lines[1])
        assertEquals("| 물품조달 | 진행 | 규격서 |", lines[2])
        assertEquals("| 전기공사 | 진행 | 도면완료 |", lines[3])
    }

    @Test
    fun `merges continuation lines into the row above`() {
        val box = """
            |┌────────┬────────┐
            |│ 항목    │ 비고   │
            |├────────┼────────┤
            |│ 착수신고 │ 지연위험 │
            |│        │ 미확인  │
            |└────────┴────────┘
        """.trimMargin().trim()
        val md = normalizeBoxTables(box)
        assertTrue(md.contains("| 착수신고 | 지연위험 미확인 |"), md)
    }

    @Test
    fun `parseMarkdown renders a box table as a Table block`() {
        val box = """
            |┌────┬────┐
            |│ A  │ B  │
            |├────┼────┤
            |│ 1  │ 2  │
            |└────┴────┘
        """.trimMargin().trim()
        val table = parseMarkdown(box).blocks.filterIsInstance<Table>().single()
        assertEquals(2, table.headers.size)
        assertEquals(1, table.rows.size)
    }

    @Test
    fun `leaves a genuine markdown table untouched`() {
        val md = "| a | b |\n| --- | --- |\n| 1 | 2 |"
        assertEquals(md, normalizeBoxTables(md))
    }

    @Test
    fun `leaves prose without box verticals untouched`() {
        val t = "그냥 문장입니다. 표 없음.\n둘째 줄."
        assertEquals(t, normalizeBoxTables(t))
    }

    @Test
    fun `leaves a box table inside a fenced code block untouched`() {
        val src = """
            |```
            |┌────┬────┐
            |│ A  │ B  │
            |└────┴────┘
            |```
        """.trimMargin().trim()
        assertEquals(src, normalizeBoxTables(src))
    }

    @Test
    fun `does not convert a single-cell box (callout)`() {
        val box = """
            |┌──────────────┐
            |│ 중요 공지입니다 │
            |└──────────────┘
        """.trimMargin().trim()
        assertEquals(box, normalizeBoxTables(box))
    }

    @Test
    fun `recognizes rounded corners`() {
        val box = """
            |╭────┬────╮
            |│ A  │ B  │
            |├────┼────┤
            |│ 1  │ 2  │
            |╰────┴────╯
        """.trimMargin().trim()
        val lines = normalizeBoxTables(box).lines()
        assertEquals("| A | B |", lines[0])
        assertEquals("| --- | --- |", lines[1])
        assertEquals("| 1 | 2 |", lines[2])
        assertEquals(3, lines.size) // no stray border lines left over
    }

    @Test
    fun `preserves a border-separated row with a blank first cell`() {
        val box = """
            |┌────┬────┐
            |│ A  │ B  │
            |├────┼────┤
            |│ 1  │ x  │
            |├────┼────┤
            |│    │ y  │
            |└────┴────┘
        """.trimMargin().trim()
        val md = normalizeBoxTables(box)
        // The blank-first-cell row is separated by a border → kept as its own row,
        // not merged into the one above.
        assertTrue(md.contains("| 1 | x |"), md)
        assertTrue(md.contains("|  | y |"), md)
    }

    @Test
    fun `keeps the blockquote prefix on a quoted box table`() {
        val box = """
            |> ┌────┬────┐
            |> │ A  │ B  │
            |> ├────┼────┤
            |> │ 1  │ 2  │
            |> └────┴────┘
        """.trimMargin().trim()
        val lines = normalizeBoxTables(box).lines()
        assertEquals("> | A | B |", lines[0])
        assertEquals("> | --- | --- |", lines[1])
        assertEquals("> | 1 | 2 |", lines[2])
    }

    @Test
    fun `keeps indentation on a box table nested under a list`() {
        // Explicit string: trimMargin/trim would strip the leading indent we test.
        val box = "    ┌────┬────┐\n" +
            "    │ A  │ B  │\n" +
            "    ├────┼────┤\n" +
            "    │ 1  │ 2  │\n" +
            "    └────┴────┘"
        val lines = normalizeBoxTables(box).lines()
        assertEquals("    | A | B |", lines[0])
        assertEquals("    | 1 | 2 |", lines[2])
    }
}
