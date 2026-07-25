package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class BrowserNavigationTest {
    @Test
    fun httpAndFriendsStayInThePage() {
        for (url in listOf(
            "https://old.reddit.com/r/x/",
            "http://example.com",
            "about:blank",
            "data:text/html,<p>hi</p>",
            "blob:https://example.com/abc",
            "javascript:void(0)",
        )) {
            assertFalse(isExternalSchemeUrl(url), "must stay in-page: $url")
        }
    }

    @Test
    fun appSchemesGoToTheOs() {
        // The dead-tap set: Korean login/payment/cert flows and store links.
        for (url in listOf(
            "intent://scan/#Intent;scheme=zxing;end",
            "market://details?id=com.example",
            "kakaotalk://launch",
            "kakaolink://send?a=1",
            "tel:+82-2-1234-5678",
            "mailto:a@b.com",
            "sms:01012345678",
            "ispmobile://pay",
            "supertoss://send",
        )) {
            assertTrue(isExternalSchemeUrl(url), "must go external: $url")
        }
    }

    @Test
    fun schemelessAndBlankStayInPage() {
        for (url in listOf("", "   ", "/relative/path", "example.com/page", "#anchor")) {
            assertFalse(isExternalSchemeUrl(url), "must stay in-page: $url")
        }
    }

    @Test
    fun textThatMerelyContainsAColonIsNotAScheme() {
        // A stray colon in prose or a ratio must not be read as a scheme, or we
        // would fling random navigations at the OS.
        assertFalse(isExternalSchemeUrl("5:1 비율"))
        assertFalse(isExternalSchemeUrl("재무:검토"))
        assertEquals("", urlScheme("5:1"))
    }

    @Test
    fun extractsHttpFallbackFromIntentUrl() {
        val url =
            "intent://shop/item/1#Intent;scheme=myapp;package=com.example;" +
                "S.browser_fallback_url=https%3A%2F%2Fshop.example.com%2Fitem%2F1;end"
        assertEquals("https://shop.example.com/item/1", intentFallbackUrl(url))
    }

    @Test
    fun fallbackIsNullWhenAbsentOrNotHttp() {
        assertNull(intentFallbackUrl("intent://scan/#Intent;scheme=zxing;end"))
        // A non-http fallback would bounce straight back into the external path.
        assertNull(
            intentFallbackUrl("intent://x#Intent;S.browser_fallback_url=myapp%3A%2F%2Fy;end"),
        )
        // Only intent:// carries a fallback.
        assertNull(intentFallbackUrl("https://example.com?S.browser_fallback_url=https%3A%2F%2Fa.com"))
    }
}
