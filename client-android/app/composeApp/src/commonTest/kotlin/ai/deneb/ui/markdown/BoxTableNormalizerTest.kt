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
    fun `merges multi-line cells in a real status board (reported case)`() {
        // The exact table a chat answer drew: 3 columns, multi-line Note cells with
        // blank Track/Status continuation rows, CJK + ◐◆→※ symbols, border rows
        // between logical rows. Each logical row's wrapped Note must merge into one.
        val box = """
            |┌──────────────────────────────┬─────────┬──────────────────┐
            |│ Track                         │ Status  │ Note             │
            |├──────────────────────────────┼─────────┼──────────────────┤
            |│ 물품 자체조달                   │ ◐ 진행  │ 규격서 송부完     │
            |│                               │         │ → 공단 확인後    │
            |│                               │         │   사전규격공고    │
            |├──────────────────────────────┼─────────┼──────────────────┤
            |│ 중앙조달 (전기공사)            │ ◐ 진행  │ 과업지시서+도면   │
            |│                               │         │ revise 완료       │
            |│                               │         │ → 조달청 의뢰待   │
            |├──────────────────────────────┼─────────┼──────────────────┤
            |│ 착수신고                       │ ◆ RISK  │ 6/2 "다음주까지"  │
            |│                               │         │ → 현재 미확인     │
            |│                               │         │ ※ 지연신청서 필요可能 │
            |└──────────────────────────────┴─────────┴──────────────────┘
        """.trimMargin().trim()
        val md = normalizeBoxTables(box)
        assertTrue(md.contains("| Track | Status | Note |"), md)
        assertTrue(md.contains("| --- | --- | --- |"), md)
        // Each logical row's wrapped Note cell merges into one.
        assertTrue(md.contains("| 물품 자체조달 | ◐ 진행 | 규격서 송부完 → 공단 확인後 사전규격공고 |"), md)
        assertTrue(md.contains("| 중앙조달 (전기공사) | ◐ 진행 | 과업지시서+도면 revise 완료 → 조달청 의뢰待 |"), md)
        assertTrue(md.contains("| 착수신고 | ◆ RISK | 6/2 \"다음주까지\" → 현재 미확인 ※ 지연신청서 필요可能 |"), md)
        // No box-drawing borders survive.
        assertTrue(md.none { it in "─│┌┐└┘├┤┬┴┼" }, md)
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
    fun `converts a box table the model wrapped in a bare fence`() {
        // dsv4 sometimes fences a "pretty" box table in bare ``` — it then renders as
        // monospace art (CJK misaligns). A bare fence whose body is ONLY a box table is
        // converted and the fence dropped.
        val src = """
            |```
            |┌────┬────┐
            |│ A  │ B  │
            |├────┼────┤
            |│ 1  │ 2  │
            |└────┴────┘
            |```
        """.trimMargin().trim()
        val md = normalizeBoxTables(src)
        assertTrue(md.contains("| A | B |"), md)
        assertTrue(md.contains("| 1 | 2 |"), md)
        assertTrue(!md.contains("```"), md) // fence dropped
        assertTrue(md.none { it in "─│┌┐└┘├┤┬┴┼" }, md)
    }

    @Test
    fun `leaves a language-fenced block untouched even if it looks like a box table`() {
        // A language info string (```text) means the author wants it verbatim.
        val src = """
            |```text
            |┌────┬────┐
            |│ A  │ B  │
            |└────┴────┘
            |```
        """.trimMargin().trim()
        assertEquals(src, normalizeBoxTables(src))
    }

    @Test
    fun `leaves a fenced tree diagram untouched`() {
        // Box-drawing that is NOT a │-delimited multi-column table (a tree) stays code.
        val src = "```\nsrc/\n│\n├── a.kt\n└── b.kt\n```"
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

    @Test
    fun `converts a heavy-style box table`() {
        val box = """
            |┏━━━━┳━━━━┓
            |┃ A  ┃ B  ┃
            |┣━━━━╋━━━━┫
            |┃ 1  ┃ 2  ┃
            |┗━━━━┻━━━━┛
        """.trimMargin().trim()
        val lines = normalizeBoxTables(box).lines()
        assertEquals("| A | B |", lines[0])
        assertEquals("| --- | --- |", lines[1])
        assertEquals("| 1 | 2 |", lines[2])
        assertEquals(3, lines.size) // heavy corners recognized → no stray borders
    }

    @Test
    fun `does not close a longer fence on a shorter inner fence`() {
        // Outer ```` (4) shows markdown that itself contains a ``` (3) + a box
        // table; the inner triple must not end the fence, so the table stays literal.
        val src = "````\n" +
            "```\n" +
            "┌────┬────┐\n" +
            "│ A  │ B  │\n" +
            "└────┴────┘\n" +
            "```\n" +
            "````"
        assertEquals(src, normalizeBoxTables(src))
    }

    @Test
    fun `converts a fenced box table inside a blockquote, keeping the prefix`() {
        val src = """
            |> ```
            |> ┌────┬────┐
            |> │ A  │ B  │
            |> ├────┼────┤
            |> │ 1  │ 2  │
            |> └────┴────┘
            |> ```
        """.trimMargin().trim()
        val md = normalizeBoxTables(src)
        assertTrue(md.contains("> | A | B |"), md)
        assertTrue(md.contains("> | 1 | 2 |"), md)
        assertTrue(!md.contains("```"), md) // fence dropped, prefix kept
    }

    @Test
    fun `converts a dashed-vertical box table`() {
        val box = """
            |┌┄┄┄┄┬┄┄┄┄┐
            |┆ A  ┆ B  ┆
            |├┄┄┄┄┼┄┄┄┄┤
            |┆ 1  ┆ 2  ┆
            |└┄┄┄┄┴┄┄┄┄┘
        """.trimMargin().trim()
        val lines = normalizeBoxTables(box).lines()
        assertEquals("| A | B |", lines[0])
        assertEquals("| --- | --- |", lines[1])
        assertEquals("| 1 | 2 |", lines[2])
    }
}
