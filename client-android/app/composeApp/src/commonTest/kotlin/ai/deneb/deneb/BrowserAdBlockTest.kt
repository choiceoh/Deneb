package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class BrowserAdBlockTest {
    @Test
    fun `blocks known ad hosts and subdomains`() {
        assertTrue(shouldBlockBrowserAdRequest("https://pagead2.googlesyndication.com/pagead/js/adsbygoogle.js"))
        assertTrue(shouldBlockBrowserAdRequest("https://securepubads.g.doubleclick.net/gampad/ads"))
        assertTrue(shouldBlockBrowserAdRequest("https://www.googletagservices.com/tag/js/gpt.js"))
        assertTrue(shouldBlockBrowserAdRequest("https://adservice.google.com/adsid/integrator.js"))
        assertTrue(shouldBlockBrowserAdRequest("https://a.amazon-adsystem.com/e/dtb/ad"))
        assertTrue(shouldBlockBrowserAdRequest("https://cdn.taboola.com/libtrc/x.js"))
    }

    @Test
    fun `blocks clear ad paths`() {
        assertTrue(shouldBlockBrowserAdRequest("https://news.example.com/pagead/js/ads.js"))
        assertTrue(shouldBlockBrowserAdRequest("https://cdn.example.com/ads/banner.js"))
    }

    @Test
    fun `never blocks main frame or normal content`() {
        assertFalse(shouldBlockBrowserAdRequest("https://www.example.com/article", isForMainFrame = true))
        assertFalse(shouldBlockBrowserAdRequest("https://www.example.com/article"))
        assertFalse(shouldBlockBrowserAdRequest("https://fonts.gstatic.com/s/roboto/v1/x.woff2"))
        assertFalse(shouldBlockBrowserAdRequest("https://www.google.com/search?q=ads"))
        assertFalse(shouldBlockBrowserAdRequest("data:text/plain,hello"))
        assertFalse(shouldBlockBrowserAdRequest(""))
    }

    @Test
    fun `browserRequestHost strips port and userinfo`() {
        assertTrue(browserRequestHost("https://user:pass@ads.doubleclick.net:443/x") == "ads.doubleclick.net")
        assertTrue(browserRequestHost("http://example.com/path") == "example.com")
    }
}
