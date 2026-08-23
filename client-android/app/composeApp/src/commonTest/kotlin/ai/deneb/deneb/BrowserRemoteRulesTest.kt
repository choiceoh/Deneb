package ai.deneb.deneb

import ai.deneb.deneb.generated.BrowserConfigOut
import ai.deneb.deneb.generated.BrowserQuirkOut
import kotlin.test.AfterTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

class BrowserRemoteRulesTest {
    @AfterTest
    fun resetRegistry() {
        BrowserRuleRegistry.reset()
    }

    @Test
    fun wireRulesAreTrimmedLowercasedAndDeduped() {
        val rules = browserRemoteRulesFromWire(
            BrowserConfigOut(
                version = 3,
                adHostSuffixes = listOf("Ads.Example.com", " ads.example.com ", "  "),
                adPathSegments = listOf("/banner/", "/banner/"),
                quirks = listOf(
                    BrowserQuirkOut(hosts = listOf("Site.Example.com", ""), css = " body{overflow:auto} "),
                    BrowserQuirkOut(hosts = listOf(""), css = "ignored"),
                    BrowserQuirkOut(hosts = listOf("ok.example.com"), css = "  "),
                ),
            ),
        )

        assertEquals(3L, rules.version)
        assertEquals(listOf("ads.example.com"), rules.adHostSuffixes)
        assertEquals(listOf("/banner/"), rules.adPathSegments)
        assertEquals(1, rules.quirks.size)
        assertEquals(setOf("site.example.com"), rules.quirks.single().hosts)
        assertEquals("body{overflow:auto}", rules.quirks.single().css)
    }

    @Test
    fun remoteRulesAddBlocksOnTopOfTheBuiltIns() {
        // Clean path/host/query: nothing in the compiled-in heuristics matches.
        assertFalse(shouldBlockBrowserAdRequest("https://tracker.example.net/lib.js"))
        BrowserRuleRegistry.install(
            BrowserRemoteRules(
                adHostSuffixes = listOf("tracker.example.net"),
                adPathSegments = listOf("/banners/"),
                adQueryMarkers = listOf("campaign_id="),
            ),
        )

        assertTrue(shouldBlockBrowserAdRequest("https://tracker.example.net/pixel.gif"))
        assertTrue(shouldBlockBrowserAdRequest("https://cdn.example.org/banners/foo.png"))
        assertTrue(shouldBlockBrowserAdRequest("https://shop.example/page?campaign_id=9"))
        // Built-ins keep working alongside remote entries.
        assertTrue(shouldBlockBrowserAdRequest("https://doubleclick.net/x.js"))
        // Main frames stay exempt.
        assertFalse(shouldBlockBrowserAdRequest("https://tracker.example.net/", isForMainFrame = true))
    }

    @Test
    fun emptyRegistryLeavesBlockingUnchanged() {
        BrowserRuleRegistry.install(BrowserRemoteRules())
        assertTrue(shouldBlockBrowserAdRequest("https://doubleclick.net/x.js"))
        assertFalse(shouldBlockBrowserAdRequest("https://unknown.example.net/x.js"))
    }

    @Test
    fun remoteQuirkScriptInjectsMatchingHostsOnly() {
        BrowserRuleRegistry.install(
            BrowserRemoteRules(
                quirks = listOf(
                    BrowserRemoteQuirk(hosts = setOf("example.com"), css = "body{overflow:auto !important}"),
                ),
            ),
        )

        val match = browserRemoteQuirkScript("https://www.example.com/article") // dot-suffix host match
        assertNotNull(match)
        assertTrue(match.contains("__deneb-remote-quirk"))
        assertTrue(match.contains("body{overflow:auto !important}"))
        assertNull(browserRemoteQuirkScript("https://other.example.org/"))
    }

    @Test
    fun siteQuirkScriptComposesBuiltInAndRemote() {
        BrowserRuleRegistry.install(
            BrowserRemoteRules(
                quirks = listOf(BrowserRemoteQuirk(hosts = setOf("reddit.com"), css = ".xpromo{display:none}")),
            ),
        )

        val script = browserSiteQuirkScript("https://www.reddit.com/r/x/")
        assertNotNull(script)
        assertTrue(script.contains("__deneb-scroll-unlock"))
        assertTrue(script.contains("__deneb-remote-quirk"))
        // A registry reset restores compiled-in-only behavior.
        BrowserRuleRegistry.reset()
        val builtinOnly = browserSiteQuirkScript("https://www.reddit.com/r/x/")
        assertTrue(builtinOnly!!.contains("__deneb-scroll-unlock"))
        assertFalse(builtinOnly.contains("__deneb-remote-quirk"))
        assertNull(browserSiteQuirkScript("https://plain.example.com/"))
    }

    @Test
    fun cssIsEscapedIntoTheJsStringLiteral() {
        BrowserRuleRegistry.install(
            BrowserRemoteRules(
                quirks = listOf(
                    BrowserRemoteQuirk(hosts = setOf("quote.example.com"), css = "a::after{content:'x'}\nb{}"),
                ),
            ),
        )
        val script = browserRemoteQuirkScript("https://quote.example.com/")!!
        assertTrue(script.contains("'a::after{content:\\'x\\'}\\nb{}'"))
    }

    @Test
    fun diskSeedDecodesAndInstalls() {
        val json = """
            {"version":5,"adHostSuffixes":["seed.example.com"],"quirks":[{"hosts":["q.example.com"],"css":"b{}"}]}
        """.trimIndent()
        assertTrue(seedBrowserRulesFromDisk(json))
        assertTrue(shouldBlockBrowserAdRequest("https://seed.example.com/x.js"))
        // An empty cache has nothing to install — false, but no crash.
        assertFalse(seedBrowserRulesFromDisk(""))
        assertFalse(seedBrowserRulesFromDisk("{broken"))
    }
}
