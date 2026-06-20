package ai.deneb.ui.launcher

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * The Korean-reading transliteration that lets the launcher index/monograms file a
 * Latin-branded app under its 초성 ([appInitial]) instead of a Latin letter.
 */
class AppNameReadingTest {

    @Test
    fun `curated apps file under their Korean reading initial`() {
        assertEquals("ㅇ", appInitial("YouTube")) // 유튜브
        assertEquals("ㅈ", appInitial("Gmail")) // 지메일 — G, NOT ㄱ
        assertEquals("ㅋ", appInitial("Chrome")) // 크롬
        assertEquals("ㅍ", appInitial("Facebook")) // 페이스북
        assertEquals("ㅇ", appInitial("X")) // 엑스 — X, NOT ㅅ
    }

    @Test
    fun `un-curated Latin apps fall back to the letter's initial consonant`() {
        assertEquals("ㅅ", appInitial("Strava")) // 스트라바
        assertEquals("ㄴ", appInitial("Nike")) // 나이키
        assertEquals("ㅈ", appInitial("James")) // 제임스
        assertEquals("ㅂ", appInitial("Booking")) // 부킹
    }

    @Test
    fun `Korean labels keep their own initial`() {
        assertEquals("ㅋ", appInitial("카카오톡"))
        assertEquals("ㅌ", appInitial("탑솔라 ERP"))
        assertEquals("ㅈ", appInitial("전화"))
    }

    @Test
    fun `digits and symbols stay under hash`() {
        assertEquals("#", appInitial("365"))
        assertEquals("#", appInitial("1Password"))
    }

    @Test
    fun `sort key preserves Latin order within a Hangul bucket`() {
        // Two un-curated C apps both land in ㅋ, ordered by their Latin name.
        val clova = koreanAppSortKey("Clova")
        val coupang = koreanAppSortKey("Coupang")
        assertEquals("ㅋ", appInitial("Clova"))
        assertEquals("ㅋ", appInitial("Coupang"))
        assertTrue(clova < coupang)
    }

    @Test
    fun `reading lookup powers search by Korean spelling`() {
        assertEquals("유튜브", appKoreanReading("YouTube"))
        assertEquals("지메일", appKoreanReading("gmail")) // case-insensitive
        assertNull(appKoreanReading("탑솔라")) // Korean label has no override
    }
}
