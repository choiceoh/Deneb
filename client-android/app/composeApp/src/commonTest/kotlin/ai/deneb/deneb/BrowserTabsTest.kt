package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class BrowserTabsTest {

    @Test
    fun `empty store opens one fallback tab`() {
        val store = resolveBrowserTabStore(
            stored = BrowserTabStore(),
            explicitUrl = "",
            fallbackUrl = "https://home.example/start",
            translateDefault = true,
            nowMs = 7,
        )

        assertEquals("tab-7", store.activeId)
        assertEquals(listOf("https://home.example/start"), store.tabs.map { it.url })
        assertTrue(store.tabs.single().translateEnabled)
    }

    @Test
    fun `routed links reuse matches then preserve the opener at the cap`() {
        var store = resolveBrowserTabStore(BrowserTabStore(), "https://one.example", "", false, nowMs = 1)
        store = resolveBrowserTabStore(store, "https://two.example", "", false, nowMs = 2)
        val reused = resolveBrowserTabStore(store, "https://one.example", "", false, nowMs = 3)

        assertEquals(2, reused.tabs.size)
        assertEquals("https://one.example", reused.tabs.first { it.id == reused.activeId }.url)
        assertEquals(3, reused.tabs.first { it.id == reused.activeId }.lastUsedAtMs)

        for (i in 3..5) {
            store = resolveBrowserTabStore(store, "https://$i.example", "", false, nowMs = i.toLong())
        }
        assertEquals(BROWSER_TAB_LIMIT, store.tabs.size)
        val opener = store.tabs.first { it.id == store.activeId }
        val capped = resolveBrowserTabStore(store, "https://replacement.example", "", false, nowMs = 9)
        assertEquals(store, capped)
        assertEquals(opener, capped.tabs.first { it.id == capped.activeId })
        assertTrue(browserTabLimitBlocks(store, "https://replacement.example"))
        assertFalse(browserTabLimitBlocks(store, opener.url))
    }

    @Test
    fun `new select and close preserve a usable active tab`() {
        val first = resolveBrowserTabStore(BrowserTabStore(), "https://one.example", "", false, nowMs = 1)
        val withBlank = addBrowserTab(first, translateDefault = true, nowMs = 2)
        val blank = withBlank.tabs.last()
        assertEquals(blank.id, withBlank.activeId)
        assertTrue(blank.translateEnabled)

        val selected = selectBrowserTab(withBlank, first.tabs.single().id, nowMs = 3)
        assertEquals(first.tabs.single().id, selected.activeId)

        val oneLeft = closeBrowserTab(selected, first.tabs.single().id, translateDefault = false, nowMs = 4)
        assertEquals(blank.id, oneLeft.activeId)
        val reset = closeBrowserTab(oneLeft, blank.id, translateDefault = false, nowMs = 5)
        assertEquals(1, reset.tabs.size)
        assertEquals("", reset.tabs.single().url)
        assertFalse(reset.tabs.single().translateEnabled)
    }

    @Test
    fun `round trip drops malformed duplicate and excess tabs`() {
        val raw = buildString {
            append("{\"activeId\":\"same\",\"tabs\":[")
            append("{\"id\":\"bad id\",\"url\":\"https://drop.example\"},")
            repeat(7) { index ->
                if (index > 0) append(',')
                append("{\"id\":\"${if (index == 1 || index == 2) "same" else "t$index"}\",\"url\":\"https://$index.example\"}")
            }
            append("]}")
        }
        val decoded = decodeBrowserTabStore(raw)
        val roundTrip = decodeBrowserTabStore(encodeBrowserTabStore(decoded))

        assertEquals(BROWSER_TAB_LIMIT, roundTrip.tabs.size)
        assertEquals(roundTrip.tabs.map { it.id }.distinct(), roundTrip.tabs.map { it.id })
        assertTrue(roundTrip.tabs.any { it.id == roundTrip.activeId })
        assertTrue(decodeBrowserTabStore("{broken").tabs.isEmpty())
    }

    @Test
    fun `stored non web url is cleared without dropping its tab`() {
        val decoded = decodeBrowserTabStore(
            """{"activeId":"tab-1","tabs":[{"id":"tab-1","url":"javascript:alert(1)"}]}""",
        )

        assertEquals("tab-1", decoded.activeId)
        assertEquals(1, decoded.tabs.size)
        assertEquals("", decoded.tabs.single().url)
    }

    @Test
    fun `tab updates sanitize titles and reject non web urls`() {
        val store = resolveBrowserTabStore(BrowserTabStore(), "https://one.example", "", false, nowMs = 1)
        val id = store.activeId
        val updated = updateBrowserTab(store, id, "https://two.example/path", "  Two   page  ", true, nowMs = 2)
        val tab = updated.tabs.single()

        assertEquals("https://two.example/path", tab.url)
        assertEquals("Two page", tab.title)
        assertEquals("Two page", browserTabDisplayTitle(tab))
        assertTrue(tab.translateEnabled)
        assertEquals(updated, updateBrowserTab(updated, id, "javascript:alert(1)", "bad", false, nowMs = 3))
    }

    @Test
    fun `transient renderer urls keep the last durable web url`() {
        assertEquals(
            "https://saved.example/article",
            stableBrowserTabUrl(
                currentUrl = "about:blank",
                requestedUrl = "blob:https://saved.example/local",
                previousUrl = "https://saved.example/article",
            ),
        )
        assertEquals(
            "https://committed.example/article",
            stableBrowserTabUrl(
                currentUrl = "blob:https://committed.example/local",
                requestedUrl = "https://stale-request.example/start",
                previousUrl = "https://committed.example/article",
            ),
        )
        assertEquals("", stableBrowserTabUrl("about:blank", "data:text/plain,hello", ""))
        assertEquals(
            "Saved title",
            stableBrowserPageTitle("", "https://saved.example/article", "https://saved.example/article", "Saved title"),
        )
        assertEquals(
            "Saved title",
            stableBrowserPageTitle("about:blank", "about:blank", "https://saved.example/article", "Saved title"),
        )
        assertEquals(
            "Saved title",
            stableBrowserPageTitle("Popup title", "blob:https://saved.example/local", "https://saved.example/article", "Saved title"),
        )
        assertEquals(
            "Loaded title",
            stableBrowserPageTitle(" Loaded title ", "https://saved.example/article", "https://saved.example/article", "Saved title"),
        )
        assertEquals(
            "Saved title",
            stableBrowserTabTitle("data:text/plain,popup", "https://saved.example/article", "Popup title", "Saved title"),
        )
        assertEquals(
            "Loaded title",
            stableBrowserTabTitle("https://saved.example/article", "https://saved.example/article", " Loaded title ", "Saved title"),
        )
    }
}
