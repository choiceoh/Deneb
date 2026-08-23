package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class BrowserBookmarksTest {
    @Test
    fun `toggle adds current page at front and removes duplicate`() {
        val existing = listOf(BrowserBookmark(url = "https://example.com/old", title = "Old"))

        val added = toggleBrowserBookmark(existing, " https://deneb.local/page ", "  Deneb   Page  ", nowMs = 100)

        assertEquals(listOf("https://deneb.local/page", "https://example.com/old"), added.map { it.url })
        assertEquals("Deneb Page", added.first().title)
        assertTrue(isBrowserBookmarked(added, "https://deneb.local/page"))

        val removed = toggleBrowserBookmark(added, "https://deneb.local/page", "ignored", nowMs = 200)
        assertEquals(existing, removed)
    }

    @Test
    fun `decode drops invalid rows and keeps first duplicate`() {
        val raw = encodeBrowserBookmarks(
            listOf(
                BrowserBookmark(url = "https://example.com/a", title = "A"),
                BrowserBookmark(url = "mailto:user@example.com", title = "Mail"),
                BrowserBookmark(url = "https://example.com/a", title = "Duplicate"),
            ),
        )

        val decoded = decodeBrowserBookmarks(raw)

        assertEquals(1, decoded.size)
        assertEquals("A", decoded.first().title)
    }

    @Test
    fun `invalid json decodes to empty list`() {
        assertTrue(decodeBrowserBookmarks("{not-json").isEmpty())
    }

    @Test
    fun `update renames title and can retarget the url`() {
        val existing = listOf(
            BrowserBookmark(url = "https://example.com/old", title = "Old"),
            BrowserBookmark(url = "https://example.com/keep", title = "Keep"),
        )

        val renamed = updateBrowserBookmark(existing, "https://example.com/old", "  New   Name  ")
        assertEquals("New Name", renamed.first { it.url == "https://example.com/old" }.title)
        assertEquals(listOf("https://example.com/old", "https://example.com/keep"), renamed.map { it.url })

        val moved = updateBrowserBookmark(renamed, "https://example.com/old", "Moved", " https://example.com/new ")
        assertEquals(listOf("https://example.com/new", "https://example.com/keep"), moved.map { it.url })
        assertEquals("Moved", moved.first().title)
    }

    @Test
    fun `update rejects a bad url and drops a colliding row`() {
        val existing = listOf(
            BrowserBookmark(url = "https://example.com/a", title = "A"),
            BrowserBookmark(url = "https://example.com/b", title = "B"),
        )

        assertEquals(existing, updateBrowserBookmark(existing, "https://example.com/a", "Nope", "javascript:alert(1)"))

        val collided = updateBrowserBookmark(existing, "https://example.com/a", "Now B", "https://example.com/b")
        assertEquals(listOf(BrowserBookmark(url = "https://example.com/b", title = "Now B", addedAtMs = 0)), collided)
    }

    @Test
    fun `only http urls can be bookmarked`() {
        assertTrue(canBookmarkUrl("https://example.com"))
        assertTrue(canBookmarkUrl("http://example.com"))
        assertFalse(canBookmarkUrl(""))
        assertFalse(canBookmarkUrl("about:blank"))
    }

    @Test
    fun `resolveBrowserStartUrl prefers nav then last then home`() {
        assertEquals(
            "https://nav.example/path",
            resolveBrowserStartUrl(" https://nav.example/path ", "https://last.example", "https://home.example"),
        )
        assertEquals(
            "https://last.example/page",
            resolveBrowserStartUrl("", " https://last.example/page ", "https://home.example"),
        )
        assertEquals(
            "https://home.example/",
            resolveBrowserStartUrl("", "about:blank", " https://home.example/ "),
        )
        assertEquals("", resolveBrowserStartUrl("", "about:blank", "about:blank"))
        assertEquals("", resolveBrowserStartUrl("  ", "", ""))
    }

    @Test
    fun `blank or about urls show the start surface`() {
        assertTrue(browserShowsStart(""))
        assertTrue(browserShowsStart("about:blank"))
        assertTrue(browserShowsStart("", "about:blank"))
        assertFalse(browserShowsStart("https://example.com"))
        assertFalse(browserShowsStart("https://example.com", ""))
        assertFalse(browserShowsStart("https://example.com", "about:blank"))
        assertFalse(browserShowsStart("", "blob:https://example.com/result"))
        assertFalse(browserShowsStart("", "data:text/plain,result"))
        assertTrue(browserShowsStart("", "javascript:alert(1)"))
    }
}
