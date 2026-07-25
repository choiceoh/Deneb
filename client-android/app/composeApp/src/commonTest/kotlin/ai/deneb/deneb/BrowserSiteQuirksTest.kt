package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

class BrowserSiteQuirksTest {
    @Test
    fun readsHostIgnoringPathQueryPortAndUserinfo() {
        assertEquals("www.reddit.com", urlHost("https://www.reddit.com/r/x/?a=1#f"))
        assertEquals("www.reddit.com", urlHost("https://www.reddit.com:443/r/x"))
        assertEquals("www.reddit.com", urlHost("https://user@www.reddit.com/r/x"))
        assertEquals("example.com", urlHost("http://example.com"))
    }

    @Test
    fun nonHttpUrlsHaveNoHost() {
        // A quirk must never be selected for an app-scheme navigation.
        assertEquals("", urlHost("intent://reddit.com#Intent;end"))
        assertEquals("", urlHost("about:blank"))
        assertEquals("", urlHost(""))
    }

    @Test
    fun quirkAppliesToModernRedditOnly() {
        for (u in listOf(
            "https://www.reddit.com/r/LocalLLaMA/comments/x/",
            "https://reddit.com/",
            "https://sh.reddit.com/r/x",
        )) {
            assertNotNull(browserSiteQuirkScript(u), "reddit must get the quirk: $u")
        }
        // old.reddit already scrolls (operator-confirmed) — leave it alone.
        assertNull(browserSiteQuirkScript("https://old.reddit.com/r/LocalLLaMA/"))
        assertNull(browserSiteQuirkScript("https://ko.wikipedia.org/wiki/x"))
    }

    @Test
    fun lookalikeHostsDoNotGetTheQuirk() {
        // Suffix matching would hand the quirk to an attacker-controlled host.
        assertNull(browserSiteQuirkScript("https://reddit.com.evil.example/"))
        assertNull(browserSiteQuirkScript("https://notreddit.com/"))
        assertFalse(isRedditHost("https://myreddit.com/"))
    }

    @Test
    fun scriptIsIdempotentAndScopedToDocumentElements() {
        val js = browserSiteQuirkScript("https://www.reddit.com/")!!
        // Re-injection on SPA soft-nav must not stack observers.
        assertTrue(js.contains("__denebScrollUnlock"), "needs a re-entry guard")
        // It unlocks the document, it does not remove page elements.
        assertTrue(js.contains("documentElement"))
        assertTrue(js.contains("document.body"))
        assertFalse(js.contains(".remove()"), "must not delete site markup")
        assertFalse(js.contains(".click()"), "must not synthesize clicks")
    }
}
