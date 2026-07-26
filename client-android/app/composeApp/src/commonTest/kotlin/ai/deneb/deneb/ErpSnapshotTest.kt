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
    fun liftsIdSegmentIntoRefId() {
        val blocks = parseErpSnapshot("게시판 최근 글 (5건)\n\n1. 하계휴가 안내 · 작성 국연정 · 일자 2026-07-02 · id=17106")
        val row = blocks.filterIsInstance<ErpBlock.Row>().single()
        assertEquals("하계휴가 안내", row.title)
        assertEquals("작성 국연정 · 일자 2026-07-02", row.meta)
        assertEquals("17106", row.refId)
    }

    @Test
    fun unknownShapeYieldsEmpty() {
        assertTrue(parseErpSnapshot("그냥 자유 텍스트\n줄 둘").isEmpty())
        assertTrue(parseErpSnapshot("").isEmpty())
    }

    /**
     * 매출 is a title plus "레이블: 값" lines and no numbered rows at all. The
     * parser used to require a Row and threw the whole parse away, dropping the
     * screen to the markdown fallback — which joins consecutive lines into one
     * paragraph, so the summary card vanished (live app, 2026-07-26).
     */
    @Test
    fun keepsARowlessSnapshotInsteadOfFallingBackToMarkdown() {
        val sales = """
            매출
            기간: 2026 YTD (2026-01-01 ~ 2026-07-26)
            건수: 2,192
            매출: 3,334억 5,028만 8,281원
        """.trimIndent()

        val blocks = parseErpSnapshot(sales)
        val summary = blocks.filterIsInstance<ErpBlock.Summary>().single()
        assertEquals("매출", summary.lines.first())
        assertEquals(4, summary.lines.size)
        assertTrue(summary.lines.last().startsWith("매출: 3,334억"))
    }

    /** Text with no ERP shape must STILL fall back — that is what the guard is for. */
    @Test
    fun unrecognizedTextStillFallsBack() {
        val prose = """
            그냥 문장입니다
            두 번째 줄도 그렇습니다
        """.trimIndent()
        assertTrue(parseErpSnapshot(prose).isEmpty())
        assertTrue(parseErpSnapshot("").isEmpty())
        // A colon deep inside a sentence is not a short label, so it must not
        // masquerade as a labelled figure.
        val colonProse = """
            제목
            이것은 아주 길고 라벨이 아닌 문장인데 콜론이: 있습니다
        """.trimIndent()
        assertTrue(parseErpSnapshot(colonProse).isEmpty())
    }
}
