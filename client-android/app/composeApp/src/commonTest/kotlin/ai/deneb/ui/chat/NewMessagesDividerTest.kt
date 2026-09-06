package ai.deneb.ui.chat

import kotlin.test.Test
import kotlin.test.assertEquals

class NewMessagesDividerTest {
    private fun msg(ts: Long, role: History.Role = History.Role.ASSISTANT) = History(role = role, content = "m$ts", timestampMs = ts)

    @Test
    fun dividerSitsAboveTheFirstMessageNewerThanTheLastVisit() {
        val history = listOf(msg(100), msg(200), msg(300), msg(400))
        assertEquals(2, newMessagesDividerIndex(history, newSinceMs = 250))
    }

    @Test
    fun noDividerWhenNothingIsNewOrNoPreviousVisit() {
        val history = listOf(msg(100), msg(200))
        assertEquals(-1, newMessagesDividerIndex(history, newSinceMs = 200))
        assertEquals(-1, newMessagesDividerIndex(history, newSinceMs = 0))
        assertEquals(-1, newMessagesDividerIndex(emptyList(), newSinceMs = 50))
    }

    @Test
    fun noDividerOnTopWhenEveryMessageIsNew() {
        // A brand-new conversation is all "new"; a divider above it says nothing.
        assertEquals(-1, newMessagesDividerIndex(listOf(msg(300), msg(400)), newSinceMs = 250))
    }

    @Test
    fun liveRowsWithoutTimestampNeverAnchorTheDivider() {
        val history = listOf(msg(100), msg(0), msg(300))
        assertEquals(2, newMessagesDividerIndex(history, newSinceMs = 150))
    }
}
