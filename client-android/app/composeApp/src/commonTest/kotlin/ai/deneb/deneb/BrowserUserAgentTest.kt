package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class BrowserUserAgentTest {
    // A real Galaxy WebView UA. The `; wv` token is the app-gate trigger; every
    // other field must survive so the UA still names this device's real build.
    private val webViewUa =
        "Mozilla/5.0 (Linux; Android 15; SM-S938N Build/AP3A.240905.015; wv) " +
            "AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/131.0.6778.135 Mobile Safari/537.36"

    @Test
    fun dropsTheEmbeddedWebViewToken() {
        val out = browserUserAgent(webViewUa)
        assertFalse(out.contains("wv"), "wv token must be gone: $out")
        assertTrue(out.contains("Android 15"), "device platform must survive: $out")
        assertTrue(out.contains("SM-S938N"), "device model must survive: $out")
        assertTrue(out.contains("Chrome/131.0.6778.135"), "chrome version must survive: $out")
        assertTrue(out.contains("Mobile Safari/537.36"), "mobile marker must survive: $out")
        assertTrue(out.contains("Build/AP3A.240905.015)"), "parenthetical must stay closed: $out")
    }

    @Test
    fun handlesVendorFieldsAfterTheToken() {
        val out = browserUserAgent("Mozilla/5.0 (Linux; Android 14; Pixel 8; wv; SomeVendor/2) AppleWebKit/537.36")
        assertFalse(out.contains("wv"), out)
        assertTrue(out.contains("SomeVendor/2)"), "trailing vendor fields must survive: $out")
    }

    @Test
    fun leavesARealBrowserUserAgentUntouched() {
        // Chrome-on-Android has no wv token — must pass through byte-identical so
        // we never mangle a UA the platform already reports correctly.
        val chrome =
            "Mozilla/5.0 (Linux; Android 15; SM-S938N) AppleWebKit/537.36 " +
                "(KHTML, like Gecko) Chrome/131.0.6778.135 Mobile Safari/537.36"
        assertEquals(chrome, browserUserAgent(chrome))
    }

    @Test
    fun doesNotStripAWordMerelyContainingWv() {
        // "wv" inside a model name is not the token — only the "; wv" field is.
        val ua = "Mozilla/5.0 (Linux; Android 15; WVOverlay-7) AppleWebKit/537.36"
        assertEquals(ua, browserUserAgent(ua))
    }

    @Test
    fun emptyOrBlankIsPassedThrough() {
        assertEquals("", browserUserAgent(""))
        assertEquals("", browserUserAgent("   "))
    }
}
