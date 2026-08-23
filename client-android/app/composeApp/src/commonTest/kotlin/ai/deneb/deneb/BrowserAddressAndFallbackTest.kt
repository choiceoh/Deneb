package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * What may appear in the address bar, and how an intent:// fallback is decoded.
 *
 * Both feed things the user acts on — the omnibox text is what 'URL 복사' and
 * '외부 브라우저로 열기' operate on, and the fallback URL is loaded directly.
 */
class BrowserAddressAndFallbackTest {

    @Test
    fun onlyRealPagesBecomeTheShownAddress() {
        assertTrue(canShowAsAddress("https://example.com/a"))
        assertTrue(canShowAsAddress("http://example.com"))
        assertTrue(canShowAsAddress("blob:https://example.com/uuid"))
        assertTrue(canShowAsAddress("data:text/html,hi"))
    }

    @Test
    fun abortedAppSchemeNavigationsNeverBecomeTheAddress() {
        // Chromium reports these back through onPageFinished(failingUrl) after
        // shouldOverrideUrlLoading refuses them; committing one left an
        // un-openable address in the omnibox that copy/open-externally then used.
        assertFalse(canShowAsAddress("tel:+82212345678"))
        assertFalse(canShowAsAddress("kakaotalk://open"))
        assertFalse(canShowAsAddress("intent://scan/#Intent;scheme=zxing;end"))
        assertFalse(canShowAsAddress("market://details?id=com.kakao.talk"))
        assertFalse(canShowAsAddress("about:blank"))
    }

    @Test
    fun koreanFallbackUrlsSurviveDecoding() {
        // %XX is a UTF-8 BYTE. Decoding each one to a Char read it as Latin-1 and
        // produced mojibake, which loadUrl then re-encoded — a 404 instead of the page.
        val encoded = "%ED%95%9C%EA%B8%80"
        assertEquals("한글", percentDecode(encoded))
        assertEquals(
            "https://example.com/검색?q=한글",
            percentDecode("https://example.com/%EA%B2%80%EC%83%89?q=%ED%95%9C%EA%B8%80"),
        )
    }

    @Test
    fun plainTextPassesThroughDecodingUnchanged() {
        assertEquals("https://example.com/a?b=c", percentDecode("https://example.com/a?b=c"))
        // A stray % that is not a valid escape stays literal rather than eating bytes.
        assertEquals("100%", percentDecode("100%"))
    }

    @Test
    fun onlyWebFallbacksAreHonoured() {
        assertEquals(
            "https://example.com/한글",
            intentFallbackUrl("intent://x/#Intent;S.browser_fallback_url=https%3A%2F%2Fexample.com%2F%ED%95%9C%EA%B8%80;end"),
        )
        // A fallback that is itself a custom scheme would bounce straight back.
        assertNull(intentFallbackUrl("intent://x/#Intent;S.browser_fallback_url=kakaotalk%3A%2F%2Fopen;end"))
        assertNull(intentFallbackUrl("https://example.com"))
    }
}
