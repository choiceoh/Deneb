package ai.deneb.ui.chat.composables

import kotlin.test.Test
import kotlin.test.assertEquals

/**
 * appendStreamCaret의 마크다운 안전 계약: 캐럿은 흐르는 텍스트 끝에 인라인으로
 * 붙되, 미완결 코드펜스 안에서는 생략되고, 펜스 라인 바로 뒤에서는 제 줄을 얻는다
 * ("```▍"는 info-string "▍"의 새 오프너로 스캔되어 방금 닫힌 블록을 다시 연다).
 */
class StreamCaretTest {

    @Test
    fun appends_inline_to_plain_text() {
        assertEquals("안녕하세요▍", appendStreamCaret("안녕하세요"))
        assertEquals("1. 항목 하나▍", appendStreamCaret("1. 항목 하나"))
    }

    @Test
    fun skips_inside_an_unclosed_fence() {
        assertEquals("```kotlin\nval x = 1", appendStreamCaret("```kotlin\nval x = 1"))
        assertEquals("~~~\ncode", appendStreamCaret("~~~\ncode"))
        // Longer fences (````) still count as fences.
        assertEquals("````\nraw", appendStreamCaret("````\nraw"))
    }

    @Test
    fun own_line_after_a_just_closed_fence() {
        assertEquals("```\ncode\n```\n▍", appendStreamCaret("```\ncode\n```"))
        assertEquals("~~~\ncode\n~~~\n▍", appendStreamCaret("~~~\ncode\n~~~"))
    }

    @Test
    fun inline_after_text_following_a_closed_fence() {
        assertEquals("```\ncode\n```\n정리하면▍", appendStreamCaret("```\ncode\n```\n정리하면"))
    }

    @Test
    fun appends_after_indented_fence_lines_toggle_correctly() {
        // 들여쓴 펜스(리스트 안 코드블록)도 토글로 잡힌다.
        val text = "- 항목\n  ```\n  code\n  ```\n다음 문장"
        assertEquals(text + "▍", appendStreamCaret(text))
    }
}
