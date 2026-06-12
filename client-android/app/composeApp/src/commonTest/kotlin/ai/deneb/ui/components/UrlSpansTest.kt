package ai.deneb.ui.components

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class UrlSpansTest {

    private fun urls(text: String): List<String> = findUrlSpans(text).map { it.url }

    private fun spanText(text: String): List<String> = findUrlSpans(text).map { text.substring(it.start, it.end) }

    @Test
    fun `bare url is detected`() {
        assertEquals(listOf("https://example.com/path"), urls("자세히는 https://example.com/path 참고"))
    }

    @Test
    fun `www url gains https scheme but span keeps original text`() {
        assertEquals(listOf("https://www.example.com"), urls("www.example.com 방문"))
        assertEquals(listOf("www.example.com"), spanText("www.example.com 방문"))
    }

    @Test
    fun `trailing punctuation stays out of the span`() {
        assertEquals(listOf("https://example.com"), urls("(링크: https://example.com)."))
        assertEquals(listOf("https://example.com"), spanText("(링크: https://example.com)."))
    }

    @Test
    fun `gateway anchor form label paren url`() {
        // The gateway renders HTML anchors as "label (https://…)" — the closing
        // paren must not be swallowed into the link.
        assertEquals(listOf("https://pay.example.com/x"), urls("결제 확인 (https://pay.example.com/x) 부탁드립니다"))
    }

    @Test
    fun `balanced paren inside url is kept`() {
        assertEquals(
            listOf("https://ko.wikipedia.org/wiki/서울_(도시)"),
            urls("https://ko.wikipedia.org/wiki/서울_(도시)"),
        )
    }

    @Test
    fun `email address is not a link`() {
        assertTrue(findUrlSpans("문의: kim@topsolar.kr 으로 회신").isEmpty())
    }

    @Test
    fun `plain text has no spans`() {
        assertTrue(findUrlSpans("링크 없는 일반 본문입니다").isEmpty())
    }

    @Test
    fun `multiple urls keep order and offsets`() {
        val text = "A https://a.com 그리고 B www.b.com 끝"
        val spans = findUrlSpans(text)
        assertEquals(2, spans.size)
        assertEquals("https://a.com", text.substring(spans[0].start, spans[0].end))
        assertEquals("www.b.com", text.substring(spans[1].start, spans[1].end))
        assertEquals("https://www.b.com", spans[1].url)
    }
}
