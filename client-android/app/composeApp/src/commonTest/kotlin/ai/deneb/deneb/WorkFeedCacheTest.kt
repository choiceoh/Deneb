package ai.deneb.deneb

import ai.deneb.ui.chat.WorkFeedItem
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class WorkFeedCacheTest {

    private val items = listOf(
        WorkFeedItem(id = "wf_1", title = "고흥 솔라케이블", summary = "선급금 미입금", status = "unread"),
        WorkFeedItem(id = "wf_2", title = "LG 모듈 계약", summary = "내일 마감", status = "unread"),
    )

    @Test
    fun roundTripsUnderMatchingOwner() {
        val json = encodeWorkFeedCache(items, owner = "https://gw#abc")
        assertEquals(items, decodeWorkFeedCache(json, expectedOwner = "https://gw#abc"))
    }

    @Test
    fun rejectsMismatchedOwner() {
        // The owner fingerprint stops a prior account's cached feed from rendering
        // under new credentials (mirrors the mail cache guard).
        val json = encodeWorkFeedCache(items, owner = "https://gw#abc")
        assertNull(decodeWorkFeedCache(json, expectedOwner = "https://other#xyz"))
    }

    @Test
    fun emptyFeedDecodesToNull() {
        // An empty cache is "no last-known briefing" — the home shows its own empty
        // state rather than a stale-but-empty render.
        val json = encodeWorkFeedCache(emptyList(), owner = "https://gw#abc")
        assertNull(decodeWorkFeedCache(json, expectedOwner = "https://gw#abc"))
    }
}
