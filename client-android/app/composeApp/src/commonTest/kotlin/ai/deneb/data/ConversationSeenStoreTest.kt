package ai.deneb.data

import com.russhwolf.settings.MapSettings
import kotlin.test.Test
import kotlin.test.assertEquals

class ConversationSeenStoreTest {
    @Test
    fun readThenWriteKeepsThePreviousVisitAndAdvancesTheClock() {
        val s = AppSettings(MapSettings())
        assertEquals(0L, s.conversationLastSeenMs("c1"))
        s.markConversationSeen("c1", 1_000)
        assertEquals(1_000L, s.conversationLastSeenMs("c1"))
        s.markConversationSeen("c1", 2_000)
        assertEquals(2_000L, s.conversationLastSeenMs("c1"))
        assertEquals(0L, s.conversationLastSeenMs("other"))
    }

    @Test
    fun blobIsBoundedAndDropsTheOldestEntries() {
        val s = AppSettings(MapSettings())
        for (i in 1..(AppSettings.MAX_CONVERSATION_SEEN + 5)) s.markConversationSeen("c$i", i.toLong())
        assertEquals(0L, s.conversationLastSeenMs("c1"))
        assertEquals((AppSettings.MAX_CONVERSATION_SEEN + 5).toLong(), s.conversationLastSeenMs("c${AppSettings.MAX_CONVERSATION_SEEN + 5}"))
    }

    @Test
    fun idsContainingEqualsSignsRoundTrip() {
        val s = AppSettings(MapSettings())
        s.markConversationSeen("a=b", 7)
        assertEquals(7L, s.conversationLastSeenMs("a=b"))
    }
}
