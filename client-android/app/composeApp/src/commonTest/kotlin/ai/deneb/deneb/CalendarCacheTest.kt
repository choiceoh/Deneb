package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class CalendarCacheTest {

    private val events = listOf(
        CalendarEvent(
            id = "e1",
            title = "현대차 아산 미팅",
            location = "본사 3층",
            start = "2026-06-21T10:00:00+09:00",
            end = "2026-06-21T11:00:00+09:00",
            allDay = false,
        ),
        CalendarEvent(
            id = "e2",
            title = "LG 품질방문",
            location = "구미",
            start = "2026-06-25T11:00:00+09:00",
            end = "2026-06-25T12:00:00+09:00",
            allDay = false,
            local = true,
            category = "mine",
        ),
    )

    @Test
    fun roundTripsUnderMatchingOwner() {
        val json = encodeCalendarCache(events, owner = "https://gw#abc")
        assertEquals(events, decodeCalendarCache(json, expectedOwner = "https://gw#abc"))
    }

    @Test
    fun rejectsMismatchedOwner() {
        val json = encodeCalendarCache(events, owner = "https://gw#abc")
        assertNull(decodeCalendarCache(json, expectedOwner = "https://other#xyz"))
    }

    @Test
    fun emptyDecodesToNull() {
        val json = encodeCalendarCache(emptyList(), owner = "https://gw#abc")
        assertNull(decodeCalendarCache(json, expectedOwner = "https://gw#abc"))
    }
}
