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
}
