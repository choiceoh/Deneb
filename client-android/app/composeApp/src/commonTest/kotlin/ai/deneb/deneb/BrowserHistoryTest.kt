package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class BrowserHistoryTest {
    @Test
    fun `record moves url to front updates title and caps at 100`() {
        var visits = emptyList<BrowserVisit>()
        for (i in 0 until 102) {
            visits = recordBrowserVisit(visits, "https://example.com/p$i", "Page $i", nowMs = i.toLong())
        }
        assertEquals(100, visits.size)
        assertEquals("https://example.com/p101", visits.first().url)
        assertEquals("Page 101", visits.first().title)
        assertTrue(visits.none { it.url == "https://example.com/p0" })
        assertTrue(visits.none { it.url == "https://example.com/p1" })

        val revisited = recordBrowserVisit(visits, "https://example.com/p10", "Updated", nowMs = 100)
        assertEquals("https://example.com/p10", revisited.first().url)
        assertEquals("Updated", revisited.first().title)
        assertEquals(1, revisited.count { it.url == "https://example.com/p10" })
    }

    @Test
    fun `record ignores non http urls`() {
        val existing = listOf(BrowserVisit(url = "https://ok.example", title = "Ok", visitedAtMs = 1))
        assertEquals(existing, recordBrowserVisit(existing, "about:blank", "Nope", nowMs = 2))
        assertEquals(existing, recordBrowserVisit(existing, "javascript:alert(1)", "Nope", nowMs = 2))
    }

    @Test
    fun `decode drops invalid rows and remove deletes by url`() {
        val raw = encodeBrowserHistory(
            listOf(
                BrowserVisit(url = "https://a.example", title = "A", visitedAtMs = 2),
                BrowserVisit(url = "mailto:x@y.z", title = "Mail", visitedAtMs = 1),
                BrowserVisit(url = "https://a.example", title = "Dup", visitedAtMs = 3),
            ),
        )
        val decoded = decodeBrowserHistory(raw)
        assertEquals(1, decoded.size)
        assertEquals("A", decoded.first().title)

        assertEquals(emptyList(), removeBrowserVisit(decoded, " https://a.example "))
        assertTrue(decodeBrowserHistory("{not-json").isEmpty())
    }

    @Test
    fun `late page titles do not reorder an existing visit`() {
        val visits = listOf(
            BrowserVisit(url = "https://new.example", title = "New", visitedAtMs = 20),
            BrowserVisit(url = "https://old.example", title = "old.example", visitedAtMs = 10),
        )

        val updated = updateBrowserVisitTitle(visits, "https://old.example", "Loaded title")

        assertEquals(visits.map { it.url }, updated.map { it.url })
        assertEquals(10, updated[1].visitedAtMs)
        assertEquals("Loaded title", updated[1].title)
    }

    @Test
    fun `same turn visit and title update preserve the new recency`() {
        val visits = listOf(
            BrowserVisit(url = "https://new.example", title = "New", visitedAtMs = 20),
            BrowserVisit(url = "https://old.example", title = "Old", visitedAtMs = 10),
        )

        val updated = applyBrowserHistoryUpdate(
            visits = visits,
            committedUrl = "https://old.example",
            currentUrl = "https://old.example",
            title = "Loaded title",
            nowMs = 30,
        )

        assertEquals("https://old.example", updated.first().url)
        assertEquals(30, updated.first().visitedAtMs)
        assertEquals("Loaded title", updated.first().title)
    }
}
