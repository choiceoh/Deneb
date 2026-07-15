package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
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
    fun `blocks korea and analytics hosts`() {
        assertTrue(shouldBlockBrowserAdRequest("https://ad.daum.net/bnc.js"))
        assertTrue(shouldBlockBrowserAdRequest("https://siape.veta.naver.com/ad.js"))
        assertTrue(shouldBlockBrowserAdRequest("https://realclick.co.kr/track"))
        assertTrue(shouldBlockBrowserAdRequest("https://www.google-analytics.com/analytics.js"))
        assertTrue(shouldBlockBrowserAdRequest("https://www.clarity.ms/tag/x"))
    }

    @Test
    fun `blocks topwar and topcor yandex rtb and adfox delivery hosts`() {
        // Homepage loads context.js; bids/creatives come from these hosts.
        assertTrue(shouldBlockBrowserAdRequest("https://yandex.ru/ads/system/context.js"))
        assertTrue(shouldBlockBrowserAdRequest("https://yandex.ru/ads/system/header-bidding.js"))
        assertTrue(shouldBlockBrowserAdRequest("https://an.yandex.ru/system/context.js"))
        assertTrue(shouldBlockBrowserAdRequest("https://ads.adfox.ru/123456/getCode?p1=x"))
        assertTrue(shouldBlockBrowserAdRequest("https://adfox.yandex.ru/api/get"))
        assertTrue(shouldBlockBrowserAdRequest("https://awaps.yandex.ru/1"))
        assertTrue(shouldBlockBrowserAdRequest("https://mc.yandex.ru/metrika/tag.js"))
        assertTrue(shouldBlockBrowserAdRequest("https://top-fwz1.mail.ru/js/code.js"))
        assertTrue(shouldBlockBrowserAdRequest("https://counter.yadro.ru/hit;vomedia?r=1"))
        assertTrue(shouldBlockBrowserAdRequest("https://content.adriver.ru/AdRiverFlash.js"))
        assertTrue(shouldBlockBrowserAdRequest("https://yastatic.net/partner-code-bundles/123.js"))
        assertTrue(shouldBlockBrowserAdRequest("https://www.googletagmanager.com/gtag/js?id=G-N03S8HWT5F"))
        // Keep Yandex site-search usable; first-party article HTML stays.
        assertFalse(shouldBlockBrowserAdRequest("https://yandex.ru/search/site/?text=танк"))
        assertFalse(shouldBlockBrowserAdRequest("https://topwar.ru/286165-article.html"))
        assertFalse(shouldBlockBrowserAdRequest("https://topcor.ru/72772-article.html"))
    }

    @Test
    fun `blocks russiadefence forumotion taboola prebid stack`() {
        assertTrue(shouldBlockBrowserAdRequest("https://cdn.taboola.com/libtrc/forumotion-en/loader.js"))
        assertTrue(shouldBlockBrowserAdRequest("https://cdn.viously.com/js/sdk/boot.js"))
        assertTrue(shouldBlockBrowserAdRequest("https://illiweb.com/rs3/18/frm/nm/prebid/build11_11/dist/prebid.js"))
        assertTrue(shouldBlockBrowserAdRequest("https://bidder.smilewanted.com/prebid"))
        assertTrue(shouldBlockBrowserAdRequest("https://cache.consentframework.com/js/pa/24697/c/IxWav/stub"))
        assertTrue(shouldBlockBrowserAdRequest("https://www.googletagmanager.com/gtag/js?id=UA-144337024-1"))
        // Forum chrome / static assets stay.
        assertFalse(shouldBlockBrowserAdRequest("https://russiadefence.net/f1-forum"))
        assertFalse(shouldBlockBrowserAdRequest("https://illipro.net/rs3/18/frm/jquery/cookie/jquery.cookie.js"))
    }

    @Test
    fun `blocks clear ad paths and query markers`() {
        assertTrue(shouldBlockBrowserAdRequest("https://news.example.com/pagead/js/ads.js"))
        assertTrue(shouldBlockBrowserAdRequest("https://cdn.example.com/ads/banner.js"))
        assertTrue(shouldBlockBrowserAdRequest("https://cdn.example.com/x.js?ad_slot=123"))
        assertTrue(shouldBlockBrowserAdRequest("https://cdn.example.com/pixel.gif"))
    }

    @Test
    fun `never blocks main frame or normal content`() {
        assertFalse(shouldBlockBrowserAdRequest("https://www.example.com/article", isForMainFrame = true))
        assertFalse(shouldBlockBrowserAdRequest("https://www.example.com/article"))
        assertFalse(shouldBlockBrowserAdRequest("https://www.example.com/admin/settings"))
        assertFalse(shouldBlockBrowserAdRequest("https://fonts.gstatic.com/s/roboto/v1/x.woff2"))
        assertFalse(shouldBlockBrowserAdRequest("https://www.google.com/search?q=ads"))
        assertFalse(shouldBlockBrowserAdRequest("https://www.example.com/blog/ads-policy"))
        assertFalse(shouldBlockBrowserAdRequest("data:text/plain,hello"))
        assertFalse(shouldBlockBrowserAdRequest(""))
    }

    @Test
    fun `browserRequestHost strips port and userinfo`() {
        assertEquals("ads.doubleclick.net", browserRequestHost("https://user:pass@ads.doubleclick.net:443/x"))
        assertEquals("example.com", browserRequestHost("http://example.com/path"))
    }

    @Test
    fun `browserBlockedResponseMime picks by extension`() {
        assertEquals("application/javascript", browserBlockedResponseMime("https://x.com/a.js?v=1"))
        assertEquals("text/css", browserBlockedResponseMime("https://x.com/a.css"))
        assertEquals("image/png", browserBlockedResponseMime("https://x.com/a.png"))
        assertEquals("text/plain", browserBlockedResponseMime("https://x.com/a"))
    }
}
