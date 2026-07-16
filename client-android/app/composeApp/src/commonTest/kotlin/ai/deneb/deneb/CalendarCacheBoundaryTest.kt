package ai.deneb.deneb

import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class CalendarCacheBoundaryTest {

    private fun event(
        id: String,
        title: String = "Event $id",
        start: String = "2026-07-11T09:00:00+09:00",
        end: String = "2026-07-11T10:00:00+09:00",
        allDay: Boolean = false,
        local: Boolean = false,
        category: String = "",
    ) = CalendarEvent(
        id = id,
        title = title,
        location = "Room $id",
        start = start,
        end = end,
        allDay = allDay,
        local = local,
        category = category,
    )

    @Test
    fun roundTripPreservesEveryDomainField() {
        val original = listOf(
            event(
                id = "e1",
                title = "한글 일정 🚀",
                start = "2026-07-11",
                end = "2026-07-12",
                allDay = true,
                local = true,
                category = "deadline",
            ),
        )

        assertEquals(original, decodeCalendarCache(encodeCalendarCache(original, "owner"), "owner"))
    }

    @Test
    fun ownerComparisonIsExactAndCaseSensitive() {
        val encoded = encodeCalendarCache(listOf(event("e")), "Owner")

        assertEquals(listOf(event("e")), decodeCalendarCache(encoded, "Owner"))
        assertNull(decodeCalendarCache(encoded, "owner"))
        assertNull(decodeCalendarCache(encoded, " Owner"))
        assertNull(decodeCalendarCache(encoded, "Owner "))
    }

    @Test
    fun emptyOwnerMatchesOnlyEmptyExpectedOwner() {
        val encoded = encodeCalendarCache(listOf(event("e")), "")

        assertEquals(1, decodeCalendarCache(encoded, "")?.size)
        assertNull(decodeCalendarCache(encoded, "nonempty"))
    }

    @Test
    fun emptyEventListMeansNoUsableCache() {
        assertNull(decodeCalendarCache(encodeCalendarCache(emptyList(), "owner"), "owner"))
    }

    @Test
    fun malformedAndWrongTopLevelShapesFailClosed() {
        for (raw in listOf("", "not-json", "null", "[]", "42", "\"text\"")) {
            assertNull(decodeCalendarCache(raw, "owner"), raw)
        }
    }

    @Test
    fun missingOwnerDefaultsEmptyAndCannotCrossOwnerBoundary() {
        val raw = """{"events":[{"id":"e","title":"T","location":"L","start":"S","end":"E","allDay":false}]}"""

        assertEquals(1, decodeCalendarCache(raw, "")?.size)
        assertNull(decodeCalendarCache(raw, "owner"))
    }

    @Test
    fun nonStringOwnerFailsClosed() {
        val event = """{"id":"e","title":"T","location":"L","start":"S","end":"E","allDay":false}"""
        for (owner in listOf("null", "42", "true", "{}", "[]")) {
            assertNull(decodeCalendarCache("""{"owner":$owner,"events":[$event]}""", ""), owner)
        }
    }

    @Test
    fun missingEventsDefaultsEmptyAndReturnsNull() {
        assertNull(decodeCalendarCache("""{"owner":"owner"}""", "owner"))
    }

    @Test
    fun explicitNullEventsRejectsEnvelope() {
        assertNull(decodeCalendarCache("""{"owner":"owner","events":null}""", "owner"))
    }

    @Test
    fun oneMalformedEventRejectsWholeEnvelope() {
        val raw = """
            {"owner":"owner","events":[
              {"id":"good","title":"T","location":"L","start":"S","end":"E","allDay":false},
              {"id":"bad","title":7,"location":"L","start":"S","end":"E","allDay":false}
            ]}
        """.trimIndent()

        assertNull(decodeCalendarCache(raw, "owner"))
    }

    @Test
    fun unknownEnvelopeAndEventFieldsAreIgnored() {
        val raw = """
            {"owner":"owner","futureEnvelope":true,"events":[{
              "id":"e","title":"T","location":"L","start":"S","end":"E","allDay":false,
              "futureEvent":{"nested":true}
            }]}
        """.trimIndent()

        assertEquals("e", decodeCalendarCache(raw, "owner")?.single()?.id)
    }

    @Test
    fun optionalLocalAndCategoryDefaultSafely() {
        val raw = """{"owner":"owner","events":[{"id":"e","title":"T","location":"L","start":"S","end":"E","allDay":false}]}"""

        val decoded = decodeCalendarCache(raw, "owner")!!.single()

        assertFalse(decoded.local)
        assertEquals("", decoded.category)
    }

    @Test
    fun explicitNullOptionalFieldsRejectEnvelopeRatherThanCoercing() {
        val raw = """{"owner":"owner","events":[{"id":"e","title":"T","location":"L","start":"S","end":"E","allDay":false,"local":null,"category":null}]}"""

        assertNull(decodeCalendarCache(raw, "owner"))
    }

    @Test
    fun encoderCapsAtFirstOneHundredEvents() {
        val original = (0 until 130).map { event("e$it") }

        val decoded = decodeCalendarCache(encodeCalendarCache(original, "owner"), "owner")!!

        assertEquals(100, decoded.size)
        assertEquals("e0", decoded.first().id)
        assertEquals("e99", decoded.last().id)
    }

    @Test
    fun decoderAlsoCapsMaliciousOversizedPersistedEnvelope() {
        val eventJson = (0 until 140).joinToString(",") {
            """{"id":"e$it","title":"T","location":"L","start":"S","end":"E","allDay":false}"""
        }
        val raw = """{"owner":"owner","events":[$eventJson]}"""

        val decoded = decodeCalendarCache(raw, "owner")!!

        assertEquals(100, decoded.size)
        assertEquals("e99", decoded.last().id)
    }

    @Test
    fun duplicateIdsArePreservedBecauseCacheDoesNotInventDedupPolicy() {
        val original = listOf(event("same", title = "first"), event("same", title = "second"))

        val decoded = decodeCalendarCache(encodeCalendarCache(original, "owner"), "owner")!!

        assertEquals(listOf("first", "second"), decoded.map { it.title })
    }

    @Test
    fun eventOrderIsPreservedExactly() {
        val original = listOf(event("z"), event("a"), event("m"))

        assertEquals(listOf("z", "a", "m"), decodeCalendarCache(encodeCalendarCache(original, "owner"), "owner")?.map { it.id })
    }

    @Test
    fun blankRequiredStringsRoundTripWithoutHiddenValidation() {
        val blank = CalendarEvent("", "", "", "", "", allDay = false)

        assertEquals(listOf(blank), decodeCalendarCache(encodeCalendarCache(listOf(blank), "owner"), "owner"))
    }

    @Test
    fun extremeUnicodeAndControlEscapesRoundTrip() {
        val original = event("e", title = "quote \" slash \\ newline\n 한글 🚀")

        assertEquals(original.title, decodeCalendarCache(encodeCalendarCache(listOf(original), "owner"), "owner")?.single()?.title)
    }

    @Test
    fun ownerIsEscapedAndNeverConfusedWithJsonStructure() {
        val owner = "owner\"}\n{malicious"
        val encoded = encodeCalendarCache(listOf(event("e")), owner)

        assertEquals(1, decodeCalendarCache(encoded, owner)?.size)
        assertNull(decodeCalendarCache(encoded, "owner"))
    }

    @Test
    fun encodedJsonDoesNotMutateInputCollection() {
        val original = mutableListOf(event("e1"), event("e2"))
        val before = original.toList()

        encodeCalendarCache(original, "owner")

        assertEquals(before, original)
    }

    @Test
    fun reencodingDecodedCacheIsStableAtDomainLevel() {
        val original = listOf(event("e1", local = true), event("e2", category = "mine"))
        val once = decodeCalendarCache(encodeCalendarCache(original, "owner"), "owner")!!
        val twice = decodeCalendarCache(encodeCalendarCache(once, "owner"), "owner")!!

        assertEquals(once, twice)
    }

    @Test
    fun truncationDoesNotSortByDatesOrIds() {
        val original = (0 until 110).map { index ->
            event(id = "e${109 - index}", start = "date-${109 - index}", end = "end")
        }

        val decoded = decodeCalendarCache(encodeCalendarCache(original, "owner"), "owner")!!

        assertEquals("e109", decoded.first().id)
        assertEquals("e10", decoded.last().id)
    }

    @Test
    fun allDayAndTimedEventsRemainDistinct() {
        val original = listOf(event("all", allDay = true), event("timed", allDay = false))

        val decoded = decodeCalendarCache(encodeCalendarCache(original, "owner"), "owner")!!

        assertTrue(decoded.first().allDay)
        assertFalse(decoded.last().allDay)
    }

    @Test
    fun genericJsonEncoderShapeCanBeDecodedByCacheCodec() {
        val raw = Json.encodeToString(
            mapOf(
                "owner" to "owner",
                "events" to "not-an-array",
            ),
        )

        assertNull(decodeCalendarCache(raw, "owner"))
    }
}
