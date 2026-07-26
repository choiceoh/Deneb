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
    fun unlocksTheViewportViaAnImportantStylesheet() {
        val js = browserSiteQuirkScript("https://www.reddit.com/")!!
        // The measured lock is `body { overflow-y: hidden }`, which propagates to
        // the viewport because the root element's overflow is `visible`. Both
        // elements must be covered, and the rule must outrank the page's own.
        assertTrue(js.contains("html,body"), "must target both html and body: $js")
        assertTrue(js.contains("overflow-y:visible !important"), "needs !important: $js")
        // Inline styles lost this fight before; a stylesheet is what survives.
        assertTrue(js.contains("createElement('style')"), "must inject a stylesheet: $js")
    }

    @Test
    fun reinjectionIsIdempotentAndNonDestructive() {
        val js = browserSiteQuirkScript("https://www.reddit.com/")!!
        // Injected again on every SPA soft-nav; it must no-op rather than pile up
        // duplicate <style> nodes.
        assertTrue(js.contains("getElementById"), "needs an existing-node guard: $js")
        assertFalse(js.contains(".remove()"), "must not delete site markup")
        assertFalse(js.contains(".click()"), "must not synthesize clicks")
    }
}
