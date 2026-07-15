package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class ErpTextMarkdownTest {
    @Test
    fun promotesFirstLineToHeading() {
        val md = erpTextToMarkdown("현재고 요약\n\n1. 품목 A · 재고 10")
        assertTrue(md.startsWith("## 현재고 요약"))
        assertTrue(md.contains("1. 품목 A"))
    }

    @Test
    fun blankReturnsEmpty() {
        assertEquals("", erpTextToMarkdown("  \n "))
    }
}
