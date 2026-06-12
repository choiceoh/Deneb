package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertSame
import kotlin.test.assertTrue

/**
 * The read-overlay that keeps a mail the user has read from re-showing its unread
 * dot after a list refetch. The gateway caches list_recent for 30s and mark_read
 * does not invalidate it, so a phone back-nav (which recomposes the list and
 * re-runs refreshMail) would otherwise replay a stale isUnread=true. These cover
 * the two pure helpers behind that fix; the RPC plumbing is exercised separately.
 */
class DenebClientMailReadOverlayTest {

    private fun row(id: String, unread: Boolean) = MailMessage(
        id = id, from = "a@b.com", subject = "s", snippet = "x",
        date = "2026-06-11T00:00:00Z", unread = unread,
    )

    @Test
    fun overlay_clears_the_dot_only_for_read_ids() {
        val out = applyReadOverlay(listOf(row("1", true), row("2", true), row("3", false)), setOf("1"))
        assertFalse(out.first { it.id == "1" }.unread, "read id shows no dot")
        assertTrue(out.first { it.id == "2" }.unread, "an unrelated unread mail keeps its dot")
        assertFalse(out.first { it.id == "3" }.unread, "an already-read mail is untouched")
    }

    @Test
    fun empty_overlay_returns_the_same_instance() {
        val rows = listOf(row("1", true))
        assertSame(rows, applyReadOverlay(rows, emptySet()), "no overlay must not allocate a new list")
    }

    @Test
    fun overlay_survives_a_cache_stale_unread_payload() {
        // Reproduces the bug input: the gateway's 30s list cache replays
        // isUnread=true for a message mark_read already cleared.
        val cachedStale = listOf(row("m1", true))
        assertFalse(applyReadOverlay(cachedStale, setOf("m1")).single().unread,
            "a refetch within the cache window must not resurrect the dot")
    }

    @Test
    fun recordReadId_caps_the_set_evicting_the_oldest() {
        val set = LinkedHashSet<String>()
        recordReadId(set, "old", max = 2)
        recordReadId(set, "mid", max = 2)
        recordReadId(set, "new", max = 2)
        assertEquals(setOf("mid", "new"), set, "over cap drops the longest-untouched id")
        assertFalse("old" in set)
    }

    @Test
    fun recordReadId_reread_refreshes_recency() {
        val set = LinkedHashSet<String>()
        recordReadId(set, "a", max = 2)
        recordReadId(set, "b", max = 2)
        recordReadId(set, "a", max = 2) // touch "a" again -> "b" becomes oldest
        recordReadId(set, "c", max = 2) // evicts the oldest
        assertEquals(setOf("a", "c"), set, "recently re-read id survives; the now-oldest is evicted")
        assertFalse("b" in set)
    }
}
