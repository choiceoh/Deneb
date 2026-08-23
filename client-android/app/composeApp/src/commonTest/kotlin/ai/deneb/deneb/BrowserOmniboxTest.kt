package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class BrowserOmniboxTest {
    private val bookmarks = listOf(
        BrowserBookmark(url = "https://news.ycombinator.com/", title = "Hacker News"),
        BrowserBookmark(url = "https://www.chosun.com/", title = "조선일보"),
    )
    private val history = listOf(
        BrowserVisit(url = "https://naver.com/", title = "네이버", visitedAtMs = 30),
        BrowserVisit(url = "https://www.nasa.gov/artemis", title = "Artemis", visitedAtMs = 20),
        BrowserVisit(url = "https://news.ycombinator.com/item?id=1", title = "HN comment", visitedAtMs = 10),
    )

    @Test
    fun `short or blank queries suggest nothing`() {
        assertTrue(browserOmniboxSuggestions("", bookmarks, history).isEmpty())
        assertTrue(browserOmniboxSuggestions(" ", bookmarks, history).isEmpty())
        assertTrue(browserOmniboxSuggestions("n", bookmarks, history).isEmpty())
    }

    @Test
    fun `host prefix outranks substring and title matches`() {
        val out = browserOmniboxSuggestions("na", bookmarks, history)
        assertEquals("https://naver.com/", out.first().url)
        // nasa (host prefix, history) before the HN comment (url substring).
        assertTrue(out.indexOfFirst { it.url == "https://www.nasa.gov/artemis" } < out.indexOfFirst { it.url == "https://news.ycombinator.com/item?id=1" })
    }

    @Test
    fun `bookmarks outrank history within the same tier`() {
        val out = browserOmniboxSuggestions("news", bookmarks, history)
        // Both are host-prefix tier 0: the bookmark first.
        assertEquals("https://news.ycombinator.com/", out.first().url)
        assertEquals(BrowserOmniboxSuggestion.Source.BOOKMARK, out.first().source)
    }

    @Test
    fun `duplicate urls collapse to the bookmark entry`() {
        val revisited = history + BrowserVisit(url = "https://news.ycombinator.com/", title = "HN", visitedAtMs = 40)
        val out = browserOmniboxSuggestions("ycomb", bookmarks, revisited)
        val hn = out.filter { it.url == "https://news.ycombinator.com/" }
        assertEquals(1, hn.size)
        assertEquals(BrowserOmniboxSuggestion.Source.BOOKMARK, hn.single().source)
    }

    @Test
    fun `a fully typed url is not suggested back`() {
        assertTrue(browserOmniboxSuggestions("https://naver.com/", bookmarks, history).none { it.url == "https://naver.com/" })
    }

    @Test
    fun `limit is respected and title-only matches still appear`() {
        val many = (0 until 10).map { BrowserVisit(url = "https://example.com/p$it", title = "page $it") }
        assertEquals(3, browserOmniboxSuggestions("example", emptyList(), many, limit = 3).size)
        // "artemis" appears only in the title of the nasa visit.
        assertEquals("https://www.nasa.gov/artemis", browserOmniboxSuggestions("artemis", bookmarks, history).single().url)
    }
}
