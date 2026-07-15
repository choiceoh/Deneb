package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class ErpSnapshotTest {
    @Test
    fun parsesSummarySectionsAndRows() {
        val text = """
            현재고 요약 (구매/자재 · Amaranth · 품목 집계)
            기준연도: 2026 · 필터: 5
            원본행: 61 · 집계품목: 54

            재고 상위(창고 합산):
            1. LR7-72HGD-615Ma · 재고 42,284 · 가용 42,284 · 모듈 · M-LR0615-03
            2. JKM635N-78HL4-BDV-S · 재고 22,515 · 가용 22,515 · 모듈 · M-JK0635-01
        """.trimIndent()

        val blocks = parseErpSnapshot(text)
        val summary = blocks.filterIsInstance<ErpBlock.Summary>().first()
        assertTrue(summary.lines.first().startsWith("현재고 요약"))
        val section = blocks.filterIsInstance<ErpBlock.Section>().single()
        assertEquals("재고 상위(창고 합산)", section.label)
        val rows = blocks.filterIsInstance<ErpBlock.Row>()
        assertEquals(2, rows.size)
        assertEquals("LR7-72HGD-615Ma", rows[0].title)
        assertTrue(rows[0].meta.startsWith("재고 42,284"))
    }

    @Test
    fun dropsInternalIdSegments() {
        val blocks = parseErpSnapshot("게시판 최근 글 (5건)\n\n1. 하계휴가 안내 · 작성 국연정 · 일자 2026-07-02 · id=17106")
        val row = blocks.filterIsInstance<ErpBlock.Row>().single()
        assertEquals("하계휴가 안내", row.title)
        assertEquals("작성 국연정 · 일자 2026-07-02", row.meta)
    }

    @Test
    fun unknownShapeYieldsEmpty() {
        assertTrue(parseErpSnapshot("그냥 자유 텍스트\n줄 둘").isEmpty())
        assertTrue(parseErpSnapshot("").isEmpty())
    }
}
