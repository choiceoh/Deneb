package ai.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class WorkFeedNavigationTest {
    @Test
    fun workFeedReferencesPreserveOpaqueItemId() {
        assertEquals("approval 한글 /?#", workFeedItemId("workfeed", "  approval 한글 /?#  "))
        assertEquals("item-1", workFeedItemId(" WorkFeed ", "item-1"))
    }

    @Test
    fun missingOrNonWorkFeedReferencesUseLegacyNavigation() {
        assertNull(workFeedItemId(null, "item-1"))
        assertNull(workFeedItemId("mail", "item-1"))
        assertNull(workFeedItemId("workfeed", "  "))
        assertNull(workFeedItemId("workfeed", null))
    }

    @Test
    fun pendingOpenWaitsForItemThenConsumesExactlyOnce() {
        val waiting = consumeFeedItemOpen("approval-42", listOf("other"))
        assertNull(waiting.openedItemId)
        assertEquals("approval-42", waiting.pendingItemId)

        val opened = consumeFeedItemOpen(waiting.pendingItemId, listOf("other", "approval-42"))
        assertEquals("approval-42", opened.openedItemId)
        assertNull(opened.pendingItemId)

        val recomposed = consumeFeedItemOpen(opened.pendingItemId, listOf("approval-42"))
        assertNull(recomposed.openedItemId)
        assertNull(recomposed.pendingItemId)
    }
}
