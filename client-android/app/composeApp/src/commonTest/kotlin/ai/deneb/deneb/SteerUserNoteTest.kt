package ai.deneb.deneb

import ai.deneb.ui.chat.History
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class SteerUserNoteTest {
    @Test
    fun insertsBeforeTheLiveAssistant() {
        val history = listOf(
            History(role = History.Role.USER, content = "first"),
            History(id = "a1", role = History.Role.ASSISTANT, content = "partial"),
        )

        val next = history.withSteerUserNote("내일 말고 모레")

        assertEquals(listOf("first", "내일 말고 모레", "partial"), next.map { it.content })
        assertEquals(
            listOf(History.Role.USER, History.Role.USER, History.Role.ASSISTANT),
            next.map { it.role },
        )
    }

    @Test
    fun skipsDuplicateRawOrTimestampPrefixedNote() {
        val raw = listOf(History(role = History.Role.USER, content = "내일 말고 모레"))
        val stamped = listOf(
            History(role = History.Role.USER, content = "[2026-08-16T19:32:00+09:00] 내일 말고 모레"),
        )

        assertEquals(raw, raw.withSteerUserNote("내일 말고 모레"))
        assertEquals(stamped, stamped.withSteerUserNote("내일 말고 모레"))
    }

    @Test
    fun appendsWhenNoAssistantRowExists() {
        val history = listOf(History(role = History.Role.USER, content = "first"))

        val next = history.withSteerUserNote("  끼어들기  ")

        assertEquals(listOf("first", "끼어들기"), next.map { it.content })
    }

    @Test
    fun timestampPrefixMatchRejectsUnrelatedUserRows() {
        assertTrue(isSameSteerUserContent("[2026-08-16T19:32:00+09:00] note", "note"))
        assertFalse(isSameSteerUserContent("[2026-08-16T19:32:00+09:00] other", "note"))
        assertFalse(isSameSteerUserContent("plain", "note"))
    }
}
