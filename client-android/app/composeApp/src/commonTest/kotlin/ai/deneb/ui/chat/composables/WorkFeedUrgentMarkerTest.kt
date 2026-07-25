package ai.deneb.ui.chat.composables

import ai.deneb.ui.chat.WorkFeedItem
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * The urgent threshold must track the gateway's workfeed.PriorityUrgent (= 4),
 * because the feed's ORDER comes from that same band: the gateway sorts priority
 * first and recency second, so an urgent card from this morning sits above a
 * routine one from ten minutes ago. If the client marked a different band the
 * dot would stop explaining the ordering it exists to explain.
 */
class WorkFeedUrgentMarkerTest {
    @Test
    fun marksOnlyTheGatewaysUrgentBand() {
        // Gateway bands: low 1, normal 2, high 3, urgent 4.
        val expected = mapOf(0 to false, 1 to false, 2 to false, 3 to false, 4 to true, 5 to true)
        val failures = mutableListOf<String>()
        for ((priority, want) in expected) {
            val got = WorkFeedItem(id = "wf$priority", priority = priority).isUrgent()
            if (got != want) failures += "priority=$priority → isUrgent()=$got, want $want"
        }
        assertTrue(failures.isEmpty(), failures.joinToString("\n"))
    }

    @Test
    fun thresholdMatchesTheGatewayConstant() {
        assertEquals(4, WORK_FEED_PRIORITY_URGENT, "workfeed.PriorityUrgent is 4 (domain/workfeed/store.go)")
    }

    /**
     * Priority 0 is the wire default for a card the gateway never scored — it must
     * not inherit the urgent marker just because the field is absent.
     */
    @Test
    fun unscoredCardIsNotUrgent() {
        assertFalse(WorkFeedItem(id = "wf-unscored").isUrgent())
    }
}
