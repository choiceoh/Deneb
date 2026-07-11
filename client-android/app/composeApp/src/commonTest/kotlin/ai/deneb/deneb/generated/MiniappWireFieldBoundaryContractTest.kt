package ai.deneb.deneb.generated

import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertIs

/**
 * Field-isolated boundary contracts for every generated miniapp DTO property.
 *
 * The all-fields fixtures protect complete snapshots; these tests isolate each
 * field so one generator mapping cannot hide behind defaults from neighboring
 * fields. Text normalization, numeric width, collection cardinality, nested
 * shape acceptance, and wrong-shape rejection are verified independently.
 */
class MiniappWireFieldBoundaryContractTest {
    private val json = Json {
        ignoreUnknownKeys = true
        coerceInputValues = true
        encodeDefaults = true
    }

    private fun <T> roundTrip(
        serializer: KSerializer<T>,
        field: String,
        value: JsonElement,
    ): JsonObject {
        val input = JsonObject(mapOf(field to value))
        val decoded = json.decodeFromJsonElement(serializer, input)
        return json.encodeToJsonElement(serializer, decoded).jsonObject
    }

    private fun <T> rejectsWrongShape(
        serializer: KSerializer<T>,
        field: String,
        value: JsonElement,
    ) {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(serializer, JsonObject(mapOf(field to value)))
        }
    }

    @Test
    fun calendarAttendeeOutEmailPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CalendarAttendeeOut.serializer(), "email", value)

        assertEquals(value, encoded["email"])
    }

    @Test
    fun calendarAttendeeOutEmailRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarAttendeeOut.serializer(), "email", JsonObject(emptyMap()))
    }

    @Test
    fun calendarAttendeeOutDisplayNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CalendarAttendeeOut.serializer(), "displayName", value)

        assertEquals(value, encoded["displayName"])
    }

    @Test
    fun calendarAttendeeOutDisplayNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarAttendeeOut.serializer(), "displayName", JsonObject(emptyMap()))
    }

    @Test
    fun calendarAttendeeOutResponseStatusPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CalendarAttendeeOut.serializer(), "responseStatus", value)

        assertEquals(value, encoded["responseStatus"])
    }

    @Test
    fun calendarAttendeeOutResponseStatusRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarAttendeeOut.serializer(), "responseStatus", JsonObject(emptyMap()))
    }

    @Test
    fun calendarAttendeeOutSelfPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(CalendarAttendeeOut.serializer(), "self", value)

        assertEquals(value, encoded["self"])
    }

    @Test
    fun calendarAttendeeOutSelfRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarAttendeeOut.serializer(), "self", JsonPrimitive(1))
    }

    @Test
    fun calendarAttendeeOutOrganizerPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(CalendarAttendeeOut.serializer(), "organizer", value)

        assertEquals(value, encoded["organizer"])
    }

    @Test
    fun calendarAttendeeOutOrganizerRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarAttendeeOut.serializer(), "organizer", JsonPrimitive(1))
    }

    @Test
    fun calendarConferenceOutSolutionPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CalendarConferenceOut.serializer(), "solution", value)

        assertEquals(value, encoded["solution"])
    }

    @Test
    fun calendarConferenceOutSolutionRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarConferenceOut.serializer(), "solution", JsonObject(emptyMap()))
    }

    @Test
    fun calendarConferenceOutUriPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CalendarConferenceOut.serializer(), "uri", value)

        assertEquals(value, encoded["uri"])
    }

    @Test
    fun calendarConferenceOutUriRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarConferenceOut.serializer(), "uri", JsonObject(emptyMap()))
    }

    @Test
    fun calendarEventOutIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CalendarEventOut.serializer(), "id", value)

        assertEquals(value, encoded["id"])
    }

    @Test
    fun calendarEventOutIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarEventOut.serializer(), "id", JsonObject(emptyMap()))
    }

    @Test
    fun calendarEventOutSummaryPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CalendarEventOut.serializer(), "summary", value)

        assertEquals(value, encoded["summary"])
    }

    @Test
    fun calendarEventOutSummaryRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarEventOut.serializer(), "summary", JsonObject(emptyMap()))
    }

    @Test
    fun calendarEventOutDescriptionPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CalendarEventOut.serializer(), "description", value)

        assertEquals(value, encoded["description"])
    }

    @Test
    fun calendarEventOutDescriptionRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarEventOut.serializer(), "description", JsonObject(emptyMap()))
    }

    @Test
    fun calendarEventOutLocationPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CalendarEventOut.serializer(), "location", value)

        assertEquals(value, encoded["location"])
    }

    @Test
    fun calendarEventOutLocationRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarEventOut.serializer(), "location", JsonObject(emptyMap()))
    }

    @Test
    fun calendarEventOutStartPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CalendarEventOut.serializer(), "start", value)

        assertEquals(value, encoded["start"])
    }

    @Test
    fun calendarEventOutStartRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarEventOut.serializer(), "start", JsonObject(emptyMap()))
    }

    @Test
    fun calendarEventOutEndPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CalendarEventOut.serializer(), "end", value)

        assertEquals(value, encoded["end"])
    }

    @Test
    fun calendarEventOutEndRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarEventOut.serializer(), "end", JsonObject(emptyMap()))
    }

    @Test
    fun calendarEventOutAllDayPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(CalendarEventOut.serializer(), "allDay", value)

        assertEquals(value, encoded["allDay"])
    }

    @Test
    fun calendarEventOutAllDayRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarEventOut.serializer(), "allDay", JsonPrimitive(1))
    }

    @Test
    fun calendarEventOutStatusPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CalendarEventOut.serializer(), "status", value)

        assertEquals(value, encoded["status"])
    }

    @Test
    fun calendarEventOutStatusRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarEventOut.serializer(), "status", JsonObject(emptyMap()))
    }

    @Test
    fun calendarEventOutHtmlLinkPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CalendarEventOut.serializer(), "htmlLink", value)

        assertEquals(value, encoded["htmlLink"])
    }

    @Test
    fun calendarEventOutHtmlLinkRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarEventOut.serializer(), "htmlLink", JsonObject(emptyMap()))
    }

    @Test
    fun calendarEventOutLocalPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(CalendarEventOut.serializer(), "local", value)

        assertEquals(value, encoded["local"])
    }

    @Test
    fun calendarEventOutLocalRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarEventOut.serializer(), "local", JsonPrimitive(1))
    }

    @Test
    fun calendarEventOutCategoryPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CalendarEventOut.serializer(), "category", value)

        assertEquals(value, encoded["category"])
    }

    @Test
    fun calendarEventOutCategoryRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarEventOut.serializer(), "category", JsonObject(emptyMap()))
    }

    @Test
    fun calendarEventOutOrganizerPreservesItsBoundaryValue() {
        val value = JsonObject(emptyMap())
        val encoded = roundTrip(CalendarEventOut.serializer(), "organizer", value)

        assertIs<JsonObject>(encoded["organizer"])
    }

    @Test
    fun calendarEventOutOrganizerRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarEventOut.serializer(), "organizer", JsonPrimitive("not-an-object"))
    }

    @Test
    fun calendarEventOutAttendeesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(CalendarEventOut.serializer(), "attendees", value)
        val encodedValues = encoded.getValue("attendees").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun calendarEventOutAttendeesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarEventOut.serializer(), "attendees", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun calendarEventOutConferencePreservesItsBoundaryValue() {
        val value = JsonObject(emptyMap())
        val encoded = roundTrip(CalendarEventOut.serializer(), "conference", value)

        assertIs<JsonObject>(encoded["conference"])
    }

    @Test
    fun calendarEventOutConferenceRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarEventOut.serializer(), "conference", JsonPrimitive("not-an-object"))
    }

    @Test
    fun calendarEventOutHasMeetPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(CalendarEventOut.serializer(), "hasMeet", value)

        assertEquals(value, encoded["hasMeet"])
    }

    @Test
    fun calendarEventOutHasMeetRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarEventOut.serializer(), "hasMeet", JsonPrimitive(1))
    }

    @Test
    fun calendarProposalOutIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CalendarProposalOut.serializer(), "id", value)

        assertEquals(value, encoded["id"])
    }

    @Test
    fun calendarProposalOutIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarProposalOut.serializer(), "id", JsonObject(emptyMap()))
    }

    @Test
    fun calendarProposalOutTitlePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CalendarProposalOut.serializer(), "title", value)

        assertEquals(value, encoded["title"])
    }

    @Test
    fun calendarProposalOutTitleRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarProposalOut.serializer(), "title", JsonObject(emptyMap()))
    }

    @Test
    fun calendarProposalOutStartPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CalendarProposalOut.serializer(), "start", value)

        assertEquals(value, encoded["start"])
    }

    @Test
    fun calendarProposalOutStartRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarProposalOut.serializer(), "start", JsonObject(emptyMap()))
    }

    @Test
    fun calendarProposalOutAllDayPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(CalendarProposalOut.serializer(), "allDay", value)

        assertEquals(value, encoded["allDay"])
    }

    @Test
    fun calendarProposalOutAllDayRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarProposalOut.serializer(), "allDay", JsonPrimitive(1))
    }

    @Test
    fun calendarProposalOutKindPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CalendarProposalOut.serializer(), "kind", value)

        assertEquals(value, encoded["kind"])
    }

    @Test
    fun calendarProposalOutKindRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarProposalOut.serializer(), "kind", JsonObject(emptyMap()))
    }

    @Test
    fun calendarProposalOutSourceSubjectPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CalendarProposalOut.serializer(), "sourceSubject", value)

        assertEquals(value, encoded["sourceSubject"])
    }

    @Test
    fun calendarProposalOutSourceSubjectRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarProposalOut.serializer(), "sourceSubject", JsonObject(emptyMap()))
    }

    @Test
    fun calendarProposalOutSourceFromPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CalendarProposalOut.serializer(), "sourceFrom", value)

        assertEquals(value, encoded["sourceFrom"])
    }

    @Test
    fun calendarProposalOutSourceFromRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalendarProposalOut.serializer(), "sourceFrom", JsonObject(emptyMap()))
    }

    @Test
    fun contactRowNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ContactRow.serializer(), "name", value)

        assertEquals(value, encoded["name"])
    }

    @Test
    fun contactRowNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ContactRow.serializer(), "name", JsonObject(emptyMap()))
    }

    @Test
    fun contactRowPhonesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(ContactRow.serializer(), "phones", value)

        assertEquals(value, encoded["phones"])
    }

    @Test
    fun contactRowPhonesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ContactRow.serializer(), "phones", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun contactRowEmailsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(ContactRow.serializer(), "emails", value)

        assertEquals(value, encoded["emails"])
    }

    @Test
    fun contactRowEmailsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ContactRow.serializer(), "emails", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun contactRowOrgPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ContactRow.serializer(), "org", value)

        assertEquals(value, encoded["org"])
    }

    @Test
    fun contactRowOrgRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ContactRow.serializer(), "org", JsonObject(emptyMap()))
    }

    @Test
    fun dashboardItemTitlePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(DashboardItem.serializer(), "title", value)

        assertEquals(value, encoded["title"])
    }

    @Test
    fun dashboardItemTitleRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(DashboardItem.serializer(), "title", JsonObject(emptyMap()))
    }

    @Test
    fun dashboardItemSubtitlePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(DashboardItem.serializer(), "subtitle", value)

        assertEquals(value, encoded["subtitle"])
    }

    @Test
    fun dashboardItemSubtitleRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(DashboardItem.serializer(), "subtitle", JsonObject(emptyMap()))
    }

    @Test
    fun dashboardItemSourcePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(DashboardItem.serializer(), "source", value)

        assertEquals(value, encoded["source"])
    }

    @Test
    fun dashboardItemSourceRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(DashboardItem.serializer(), "source", JsonObject(emptyMap()))
    }

    @Test
    fun dashboardItemRefTypePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(DashboardItem.serializer(), "refType", value)

        assertEquals(value, encoded["refType"])
    }

    @Test
    fun dashboardItemRefTypeRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(DashboardItem.serializer(), "refType", JsonObject(emptyMap()))
    }

    @Test
    fun dashboardItemRefIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(DashboardItem.serializer(), "refId", value)

        assertEquals(value, encoded["refId"])
    }

    @Test
    fun dashboardItemRefIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(DashboardItem.serializer(), "refId", JsonObject(emptyMap()))
    }

    @Test
    fun dashboardItemWhenMsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(DashboardItem.serializer(), "whenMs", value)

        assertEquals(value, encoded["whenMs"])
    }

    @Test
    fun dashboardItemWhenMsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(DashboardItem.serializer(), "whenMs", JsonPrimitive("not-a-number"))
    }

    @Test
    fun dashboardOutLanesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(DashboardOut.serializer(), "lanes", value)
        val encodedValues = encoded.getValue("lanes").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun dashboardOutLanesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(DashboardOut.serializer(), "lanes", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun filesEntryOutTagPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(FilesEntryOut.serializer(), "tag", value)

        assertEquals(value, encoded["tag"])
    }

    @Test
    fun filesEntryOutTagRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(FilesEntryOut.serializer(), "tag", JsonObject(emptyMap()))
    }

    @Test
    fun filesEntryOutNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(FilesEntryOut.serializer(), "name", value)

        assertEquals(value, encoded["name"])
    }

    @Test
    fun filesEntryOutNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(FilesEntryOut.serializer(), "name", JsonObject(emptyMap()))
    }

    @Test
    fun filesEntryOutPathDisplayPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(FilesEntryOut.serializer(), "pathDisplay", value)

        assertEquals(value, encoded["pathDisplay"])
    }

    @Test
    fun filesEntryOutPathDisplayRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(FilesEntryOut.serializer(), "pathDisplay", JsonObject(emptyMap()))
    }

    @Test
    fun filesEntryOutPathLowerPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(FilesEntryOut.serializer(), "pathLower", value)

        assertEquals(value, encoded["pathLower"])
    }

    @Test
    fun filesEntryOutPathLowerRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(FilesEntryOut.serializer(), "pathLower", JsonObject(emptyMap()))
    }

    @Test
    fun filesEntryOutIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(FilesEntryOut.serializer(), "id", value)

        assertEquals(value, encoded["id"])
    }

    @Test
    fun filesEntryOutIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(FilesEntryOut.serializer(), "id", JsonObject(emptyMap()))
    }

    @Test
    fun filesEntryOutSizePreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(FilesEntryOut.serializer(), "size", value)

        assertEquals(value, encoded["size"])
    }

    @Test
    fun filesEntryOutSizeRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(FilesEntryOut.serializer(), "size", JsonPrimitive("not-a-number"))
    }

    @Test
    fun filesEntryOutServerModifiedPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(FilesEntryOut.serializer(), "serverModified", value)

        assertEquals(value, encoded["serverModified"])
    }

    @Test
    fun filesEntryOutServerModifiedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(FilesEntryOut.serializer(), "serverModified", JsonObject(emptyMap()))
    }

    @Test
    fun filesListOutEntriesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(FilesListOut.serializer(), "entries", value)
        val encodedValues = encoded.getValue("entries").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun filesListOutEntriesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(FilesListOut.serializer(), "entries", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun filesListOutPathPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(FilesListOut.serializer(), "path", value)

        assertEquals(value, encoded["path"])
    }

    @Test
    fun filesListOutPathRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(FilesListOut.serializer(), "path", JsonObject(emptyMap()))
    }

    @Test
    fun filesShareOutUrlPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(FilesShareOut.serializer(), "url", value)

        assertEquals(value, encoded["url"])
    }

    @Test
    fun filesShareOutUrlRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(FilesShareOut.serializer(), "url", JsonObject(emptyMap()))
    }

    @Test
    fun filesUploadOutEntryPreservesItsBoundaryValue() {
        val value = JsonObject(emptyMap())
        val encoded = roundTrip(FilesUploadOut.serializer(), "entry", value)

        assertIs<JsonObject>(encoded["entry"])
    }

    @Test
    fun filesUploadOutEntryRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(FilesUploadOut.serializer(), "entry", JsonPrimitive("not-an-object"))
    }

    @Test
    fun laneOutKeyPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(LaneOut.serializer(), "key", value)

        assertEquals(value, encoded["key"])
    }

    @Test
    fun laneOutKeyRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(LaneOut.serializer(), "key", JsonObject(emptyMap()))
    }

    @Test
    fun laneOutNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(LaneOut.serializer(), "name", value)

        assertEquals(value, encoded["name"])
    }

    @Test
    fun laneOutNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(LaneOut.serializer(), "name", JsonObject(emptyMap()))
    }

    @Test
    fun laneOutItemsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(LaneOut.serializer(), "items", value)
        val encodedValues = encoded.getValue("items").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun laneOutItemsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(LaneOut.serializer(), "items", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun mailAnalysisOutIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailAnalysisOut.serializer(), "id", value)

        assertEquals(value, encoded["id"])
    }

    @Test
    fun mailAnalysisOutIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailAnalysisOut.serializer(), "id", JsonObject(emptyMap()))
    }

    @Test
    fun mailAnalysisOutSubjectPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailAnalysisOut.serializer(), "subject", value)

        assertEquals(value, encoded["subject"])
    }

    @Test
    fun mailAnalysisOutSubjectRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailAnalysisOut.serializer(), "subject", JsonObject(emptyMap()))
    }

    @Test
    fun mailAnalysisOutFromPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailAnalysisOut.serializer(), "from", value)

        assertEquals(value, encoded["from"])
    }

    @Test
    fun mailAnalysisOutFromRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailAnalysisOut.serializer(), "from", JsonObject(emptyMap()))
    }

    @Test
    fun mailAnalysisOutDatePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailAnalysisOut.serializer(), "date", value)

        assertEquals(value, encoded["date"])
    }

    @Test
    fun mailAnalysisOutDateRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailAnalysisOut.serializer(), "date", JsonObject(emptyMap()))
    }

    @Test
    fun mailAnalysisOutAnalysisPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailAnalysisOut.serializer(), "analysis", value)

        assertEquals(value, encoded["analysis"])
    }

    @Test
    fun mailAnalysisOutAnalysisRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailAnalysisOut.serializer(), "analysis", JsonObject(emptyMap()))
    }

    @Test
    fun mailAnalysisOutRelatedProjectsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(MailAnalysisOut.serializer(), "relatedProjects", value)
        val encodedValues = encoded.getValue("relatedProjects").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun mailAnalysisOutRelatedProjectsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailAnalysisOut.serializer(), "relatedProjects", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun mailAnalysisOutDurationMsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(MailAnalysisOut.serializer(), "durationMs", value)

        assertEquals(value, encoded["durationMs"])
    }

    @Test
    fun mailAnalysisOutDurationMsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailAnalysisOut.serializer(), "durationMs", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailAnalysisOutCachedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(MailAnalysisOut.serializer(), "cached", value)

        assertEquals(value, encoded["cached"])
    }

    @Test
    fun mailAnalysisOutCachedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailAnalysisOut.serializer(), "cached", JsonPrimitive(1))
    }

    @Test
    fun mailAnalysisOutCreatedAtPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailAnalysisOut.serializer(), "createdAt", value)

        assertEquals(value, encoded["createdAt"])
    }

    @Test
    fun mailAnalysisOutCreatedAtRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailAnalysisOut.serializer(), "createdAt", JsonObject(emptyMap()))
    }

    @Test
    fun mailAnalysisOutAnalysisStatusPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailAnalysisOut.serializer(), "analysisStatus", value)

        assertEquals(value, encoded["analysisStatus"])
    }

    @Test
    fun mailAnalysisOutAnalysisStatusRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailAnalysisOut.serializer(), "analysisStatus", JsonObject(emptyMap()))
    }

    @Test
    fun mailAnalysisOutAnalysisQualityPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailAnalysisOut.serializer(), "analysisQuality", value)

        assertEquals(value, encoded["analysisQuality"])
    }

    @Test
    fun mailAnalysisOutAnalysisQualityRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailAnalysisOut.serializer(), "analysisQuality", JsonObject(emptyMap()))
    }

    @Test
    fun mailAnalysisOutFeedStatusPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailAnalysisOut.serializer(), "feedStatus", value)

        assertEquals(value, encoded["feedStatus"])
    }

    @Test
    fun mailAnalysisOutFeedStatusRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailAnalysisOut.serializer(), "feedStatus", JsonObject(emptyMap()))
    }

    @Test
    fun mailAnalysisOutCalendarProposalCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailAnalysisOut.serializer(), "calendarProposalCount", value)

        assertEquals(value, encoded["calendarProposalCount"])
    }

    @Test
    fun mailAnalysisOutCalendarProposalCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailAnalysisOut.serializer(), "calendarProposalCount", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailAnalysisOutTodoCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailAnalysisOut.serializer(), "todoCount", value)

        assertEquals(value, encoded["todoCount"])
    }

    @Test
    fun mailAnalysisOutTodoCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailAnalysisOut.serializer(), "todoCount", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailAnalysisOutWorkStateHintPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailAnalysisOut.serializer(), "workStateHint", value)

        assertEquals(value, encoded["workStateHint"])
    }

    @Test
    fun mailAnalysisOutWorkStateHintRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailAnalysisOut.serializer(), "workStateHint", JsonObject(emptyMap()))
    }

    @Test
    fun mailAttachmentOutIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailAttachmentOut.serializer(), "id", value)

        assertEquals(value, encoded["id"])
    }

    @Test
    fun mailAttachmentOutIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailAttachmentOut.serializer(), "id", JsonObject(emptyMap()))
    }

    @Test
    fun mailAttachmentOutFilenamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailAttachmentOut.serializer(), "filename", value)

        assertEquals(value, encoded["filename"])
    }

    @Test
    fun mailAttachmentOutFilenameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailAttachmentOut.serializer(), "filename", JsonObject(emptyMap()))
    }

    @Test
    fun mailAttachmentOutMimeTypePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailAttachmentOut.serializer(), "mimeType", value)

        assertEquals(value, encoded["mimeType"])
    }

    @Test
    fun mailAttachmentOutMimeTypeRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailAttachmentOut.serializer(), "mimeType", JsonObject(emptyMap()))
    }

    @Test
    fun mailAttachmentOutSizePreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailAttachmentOut.serializer(), "size", value)

        assertEquals(value, encoded["size"])
    }

    @Test
    fun mailAttachmentOutSizeRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailAttachmentOut.serializer(), "size", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailAttachmentOutTruncatedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(MailAttachmentOut.serializer(), "truncated", value)

        assertEquals(value, encoded["truncated"])
    }

    @Test
    fun mailAttachmentOutTruncatedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailAttachmentOut.serializer(), "truncated", JsonPrimitive(1))
    }

    @Test
    fun mailMessageOutIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailMessageOut.serializer(), "id", value)

        assertEquals(value, encoded["id"])
    }

    @Test
    fun mailMessageOutIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "id", JsonObject(emptyMap()))
    }

    @Test
    fun mailMessageOutThreadIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailMessageOut.serializer(), "threadId", value)

        assertEquals(value, encoded["threadId"])
    }

    @Test
    fun mailMessageOutThreadIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "threadId", JsonObject(emptyMap()))
    }

    @Test
    fun mailMessageOutFromPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailMessageOut.serializer(), "from", value)

        assertEquals(value, encoded["from"])
    }

    @Test
    fun mailMessageOutFromRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "from", JsonObject(emptyMap()))
    }

    @Test
    fun mailMessageOutToPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailMessageOut.serializer(), "to", value)

        assertEquals(value, encoded["to"])
    }

    @Test
    fun mailMessageOutToRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "to", JsonObject(emptyMap()))
    }

    @Test
    fun mailMessageOutCcPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailMessageOut.serializer(), "cc", value)

        assertEquals(value, encoded["cc"])
    }

    @Test
    fun mailMessageOutCcRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "cc", JsonObject(emptyMap()))
    }

    @Test
    fun mailMessageOutSubjectPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailMessageOut.serializer(), "subject", value)

        assertEquals(value, encoded["subject"])
    }

    @Test
    fun mailMessageOutSubjectRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "subject", JsonObject(emptyMap()))
    }

    @Test
    fun mailMessageOutDatePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailMessageOut.serializer(), "date", value)

        assertEquals(value, encoded["date"])
    }

    @Test
    fun mailMessageOutDateRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "date", JsonObject(emptyMap()))
    }

    @Test
    fun mailMessageOutIsUnreadPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(MailMessageOut.serializer(), "isUnread", value)

        assertEquals(value, encoded["isUnread"])
    }

    @Test
    fun mailMessageOutIsUnreadRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "isUnread", JsonPrimitive(1))
    }

    @Test
    fun mailMessageOutBodyPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailMessageOut.serializer(), "body", value)

        assertEquals(value, encoded["body"])
    }

    @Test
    fun mailMessageOutBodyRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "body", JsonObject(emptyMap()))
    }

    @Test
    fun mailMessageOutBodyTotalPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailMessageOut.serializer(), "bodyTotal", value)

        assertEquals(value, encoded["bodyTotal"])
    }

    @Test
    fun mailMessageOutBodyTotalRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "bodyTotal", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailMessageOutRawBodyPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailMessageOut.serializer(), "rawBody", value)

        assertEquals(value, encoded["rawBody"])
    }

    @Test
    fun mailMessageOutRawBodyRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "rawBody", JsonObject(emptyMap()))
    }

    @Test
    fun mailMessageOutRawBodyTotalPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailMessageOut.serializer(), "rawBodyTotal", value)

        assertEquals(value, encoded["rawBodyTotal"])
    }

    @Test
    fun mailMessageOutRawBodyTotalRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "rawBodyTotal", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailMessageOutBodyCleanedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(MailMessageOut.serializer(), "bodyCleaned", value)

        assertEquals(value, encoded["bodyCleaned"])
    }

    @Test
    fun mailMessageOutBodyCleanedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "bodyCleaned", JsonPrimitive(1))
    }

    @Test
    fun mailMessageOutBodyHiddenBlockCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailMessageOut.serializer(), "bodyHiddenBlockCount", value)

        assertEquals(value, encoded["bodyHiddenBlockCount"])
    }

    @Test
    fun mailMessageOutBodyHiddenBlockCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "bodyHiddenBlockCount", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailMessageOutBodyHiddenLineCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailMessageOut.serializer(), "bodyHiddenLineCount", value)

        assertEquals(value, encoded["bodyHiddenLineCount"])
    }

    @Test
    fun mailMessageOutBodyHiddenLineCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "bodyHiddenLineCount", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailMessageOutLabelsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(MailMessageOut.serializer(), "labels", value)

        assertEquals(value, encoded["labels"])
    }

    @Test
    fun mailMessageOutLabelsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "labels", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun mailMessageOutAttachmentsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(MailMessageOut.serializer(), "attachments", value)
        val encodedValues = encoded.getValue("attachments").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun mailMessageOutAttachmentsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "attachments", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun mailMessageOutAnalysisStatusPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailMessageOut.serializer(), "analysisStatus", value)

        assertEquals(value, encoded["analysisStatus"])
    }

    @Test
    fun mailMessageOutAnalysisStatusRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "analysisStatus", JsonObject(emptyMap()))
    }

    @Test
    fun mailMessageOutAnalysisQualityPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailMessageOut.serializer(), "analysisQuality", value)

        assertEquals(value, encoded["analysisQuality"])
    }

    @Test
    fun mailMessageOutAnalysisQualityRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "analysisQuality", JsonObject(emptyMap()))
    }

    @Test
    fun mailMessageOutFeedStatusPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailMessageOut.serializer(), "feedStatus", value)

        assertEquals(value, encoded["feedStatus"])
    }

    @Test
    fun mailMessageOutFeedStatusRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "feedStatus", JsonObject(emptyMap()))
    }

    @Test
    fun mailMessageOutCalendarProposalCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailMessageOut.serializer(), "calendarProposalCount", value)

        assertEquals(value, encoded["calendarProposalCount"])
    }

    @Test
    fun mailMessageOutCalendarProposalCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "calendarProposalCount", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailMessageOutTodoCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailMessageOut.serializer(), "todoCount", value)

        assertEquals(value, encoded["todoCount"])
    }

    @Test
    fun mailMessageOutTodoCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "todoCount", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailMessageOutWorkStateHintPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailMessageOut.serializer(), "workStateHint", value)

        assertEquals(value, encoded["workStateHint"])
    }

    @Test
    fun mailMessageOutWorkStateHintRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "workStateHint", JsonObject(emptyMap()))
    }

    @Test
    fun mailMessageOutRelatedProjectsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(MailMessageOut.serializer(), "relatedProjects", value)
        val encodedValues = encoded.getValue("relatedProjects").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun mailMessageOutRelatedProjectsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailMessageOut.serializer(), "relatedProjects", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun mailNativeMailboxOutNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailNativeMailboxOut.serializer(), "name", value)

        assertEquals(value, encoded["name"])
    }

    @Test
    fun mailNativeMailboxOutNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativeMailboxOut.serializer(), "name", JsonObject(emptyMap()))
    }

    @Test
    fun mailNativeMailboxOutTotalPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailNativeMailboxOut.serializer(), "total", value)

        assertEquals(value, encoded["total"])
    }

    @Test
    fun mailNativeMailboxOutTotalRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativeMailboxOut.serializer(), "total", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailNativeMailboxOutUnreadPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailNativeMailboxOut.serializer(), "unread", value)

        assertEquals(value, encoded["unread"])
    }

    @Test
    fun mailNativeMailboxOutUnreadRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativeMailboxOut.serializer(), "unread", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailNativeMailboxOutLocallyReadPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailNativeMailboxOut.serializer(), "locallyRead", value)

        assertEquals(value, encoded["locallyRead"])
    }

    @Test
    fun mailNativeMailboxOutLocallyReadRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativeMailboxOut.serializer(), "locallyRead", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailNativeMailboxOutLocallyArchivedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailNativeMailboxOut.serializer(), "locallyArchived", value)

        assertEquals(value, encoded["locallyArchived"])
    }

    @Test
    fun mailNativeMailboxOutLocallyArchivedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativeMailboxOut.serializer(), "locallyArchived", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailNativeMailboxOutLocallyTrashedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailNativeMailboxOut.serializer(), "locallyTrashed", value)

        assertEquals(value, encoded["locallyTrashed"])
    }

    @Test
    fun mailNativeMailboxOutLocallyTrashedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativeMailboxOut.serializer(), "locallyTrashed", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailNativeMailboxOutLatestUidPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailNativeMailboxOut.serializer(), "latestUid", value)

        assertEquals(value, encoded["latestUid"])
    }

    @Test
    fun mailNativeMailboxOutLatestUidRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativeMailboxOut.serializer(), "latestUid", JsonObject(emptyMap()))
    }

    @Test
    fun mailNativeMailboxOutAttachmentCapablePreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(MailNativeMailboxOut.serializer(), "attachmentCapable", value)

        assertEquals(value, encoded["attachmentCapable"])
    }

    @Test
    fun mailNativeMailboxOutAttachmentCapableRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativeMailboxOut.serializer(), "attachmentCapable", JsonPrimitive(1))
    }

    @Test
    fun mailNativeOverlayOutMessagesPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailNativeOverlayOut.serializer(), "messages", value)

        assertEquals(value, encoded["messages"])
    }

    @Test
    fun mailNativeOverlayOutMessagesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativeOverlayOut.serializer(), "messages", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailNativeOverlayOutReadPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailNativeOverlayOut.serializer(), "read", value)

        assertEquals(value, encoded["read"])
    }

    @Test
    fun mailNativeOverlayOutReadRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativeOverlayOut.serializer(), "read", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailNativeOverlayOutArchivedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailNativeOverlayOut.serializer(), "archived", value)

        assertEquals(value, encoded["archived"])
    }

    @Test
    fun mailNativeOverlayOutArchivedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativeOverlayOut.serializer(), "archived", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailNativeOverlayOutTrashedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailNativeOverlayOut.serializer(), "trashed", value)

        assertEquals(value, encoded["trashed"])
    }

    @Test
    fun mailNativeOverlayOutTrashedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativeOverlayOut.serializer(), "trashed", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailNativePipelineOutMessagesPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailNativePipelineOut.serializer(), "messages", value)

        assertEquals(value, encoded["messages"])
    }

    @Test
    fun mailNativePipelineOutMessagesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativePipelineOut.serializer(), "messages", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailNativePipelineOutAnalyzedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailNativePipelineOut.serializer(), "analyzed", value)

        assertEquals(value, encoded["analyzed"])
    }

    @Test
    fun mailNativePipelineOutAnalyzedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativePipelineOut.serializer(), "analyzed", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailNativePipelineOutAnalyzingPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailNativePipelineOut.serializer(), "analyzing", value)

        assertEquals(value, encoded["analyzing"])
    }

    @Test
    fun mailNativePipelineOutAnalyzingRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativePipelineOut.serializer(), "analyzing", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailNativePipelineOutFailedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailNativePipelineOut.serializer(), "failed", value)

        assertEquals(value, encoded["failed"])
    }

    @Test
    fun mailNativePipelineOutFailedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativePipelineOut.serializer(), "failed", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailNativePipelineOutFeedCreatedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailNativePipelineOut.serializer(), "feedCreated", value)

        assertEquals(value, encoded["feedCreated"])
    }

    @Test
    fun mailNativePipelineOutFeedCreatedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativePipelineOut.serializer(), "feedCreated", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailNativePipelineOutFeedMissingPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailNativePipelineOut.serializer(), "feedMissing", value)

        assertEquals(value, encoded["feedMissing"])
    }

    @Test
    fun mailNativePipelineOutFeedMissingRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativePipelineOut.serializer(), "feedMissing", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailNativePipelineOutCalendarCandidatesPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailNativePipelineOut.serializer(), "calendarCandidates", value)

        assertEquals(value, encoded["calendarCandidates"])
    }

    @Test
    fun mailNativePipelineOutCalendarCandidatesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativePipelineOut.serializer(), "calendarCandidates", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailNativePipelineOutTodoCandidatesPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailNativePipelineOut.serializer(), "todoCandidates", value)

        assertEquals(value, encoded["todoCandidates"])
    }

    @Test
    fun mailNativePipelineOutTodoCandidatesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativePipelineOut.serializer(), "todoCandidates", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailNativePipelineOutUpdatedAtPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailNativePipelineOut.serializer(), "updatedAt", value)

        assertEquals(value, encoded["updatedAt"])
    }

    @Test
    fun mailNativePipelineOutUpdatedAtRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativePipelineOut.serializer(), "updatedAt", JsonObject(emptyMap()))
    }

    @Test
    fun mailNativePipelineOutErrorPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailNativePipelineOut.serializer(), "error", value)

        assertEquals(value, encoded["error"])
    }

    @Test
    fun mailNativePipelineOutErrorRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativePipelineOut.serializer(), "error", JsonObject(emptyMap()))
    }

    @Test
    fun mailNativeStatusOutSourcePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailNativeStatusOut.serializer(), "source", value)

        assertEquals(value, encoded["source"])
    }

    @Test
    fun mailNativeStatusOutSourceRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativeStatusOut.serializer(), "source", JsonObject(emptyMap()))
    }

    @Test
    fun mailNativeStatusOutAvailablePreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(MailNativeStatusOut.serializer(), "available", value)

        assertEquals(value, encoded["available"])
    }

    @Test
    fun mailNativeStatusOutAvailableRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativeStatusOut.serializer(), "available", JsonPrimitive(1))
    }

    @Test
    fun mailNativeStatusOutOfflineCapablePreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(MailNativeStatusOut.serializer(), "offlineCapable", value)

        assertEquals(value, encoded["offlineCapable"])
    }

    @Test
    fun mailNativeStatusOutOfflineCapableRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativeStatusOut.serializer(), "offlineCapable", JsonPrimitive(1))
    }

    @Test
    fun mailNativeStatusOutMailboxesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(MailNativeStatusOut.serializer(), "mailboxes", value)
        val encodedValues = encoded.getValue("mailboxes").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun mailNativeStatusOutMailboxesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativeStatusOut.serializer(), "mailboxes", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun mailNativeStatusOutOverlayPreservesItsBoundaryValue() {
        val value = JsonObject(emptyMap())
        val encoded = roundTrip(MailNativeStatusOut.serializer(), "overlay", value)

        assertIs<JsonObject>(encoded["overlay"])
    }

    @Test
    fun mailNativeStatusOutOverlayRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativeStatusOut.serializer(), "overlay", JsonPrimitive("not-an-object"))
    }

    @Test
    fun mailNativeStatusOutPipelinePreservesItsBoundaryValue() {
        val value = JsonObject(emptyMap())
        val encoded = roundTrip(MailNativeStatusOut.serializer(), "pipeline", value)

        assertIs<JsonObject>(encoded["pipeline"])
    }

    @Test
    fun mailNativeStatusOutPipelineRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativeStatusOut.serializer(), "pipeline", JsonPrimitive("not-an-object"))
    }

    @Test
    fun mailNativeStatusOutGeneratedAtPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailNativeStatusOut.serializer(), "generatedAt", value)

        assertEquals(value, encoded["generatedAt"])
    }

    @Test
    fun mailNativeStatusOutGeneratedAtRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativeStatusOut.serializer(), "generatedAt", JsonObject(emptyMap()))
    }

    @Test
    fun mailNativeStatusOutErrorPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailNativeStatusOut.serializer(), "error", value)

        assertEquals(value, encoded["error"])
    }

    @Test
    fun mailNativeStatusOutErrorRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailNativeStatusOut.serializer(), "error", JsonObject(emptyMap()))
    }

    @Test
    fun mailRowOutIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailRowOut.serializer(), "id", value)

        assertEquals(value, encoded["id"])
    }

    @Test
    fun mailRowOutIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailRowOut.serializer(), "id", JsonObject(emptyMap()))
    }

    @Test
    fun mailRowOutThreadIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailRowOut.serializer(), "threadId", value)

        assertEquals(value, encoded["threadId"])
    }

    @Test
    fun mailRowOutThreadIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailRowOut.serializer(), "threadId", JsonObject(emptyMap()))
    }

    @Test
    fun mailRowOutFromPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailRowOut.serializer(), "from", value)

        assertEquals(value, encoded["from"])
    }

    @Test
    fun mailRowOutFromRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailRowOut.serializer(), "from", JsonObject(emptyMap()))
    }

    @Test
    fun mailRowOutSubjectPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailRowOut.serializer(), "subject", value)

        assertEquals(value, encoded["subject"])
    }

    @Test
    fun mailRowOutSubjectRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailRowOut.serializer(), "subject", JsonObject(emptyMap()))
    }

    @Test
    fun mailRowOutSnippetPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailRowOut.serializer(), "snippet", value)

        assertEquals(value, encoded["snippet"])
    }

    @Test
    fun mailRowOutSnippetRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailRowOut.serializer(), "snippet", JsonObject(emptyMap()))
    }

    @Test
    fun mailRowOutDatePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailRowOut.serializer(), "date", value)

        assertEquals(value, encoded["date"])
    }

    @Test
    fun mailRowOutDateRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailRowOut.serializer(), "date", JsonObject(emptyMap()))
    }

    @Test
    fun mailRowOutIsUnreadPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(MailRowOut.serializer(), "isUnread", value)

        assertEquals(value, encoded["isUnread"])
    }

    @Test
    fun mailRowOutIsUnreadRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailRowOut.serializer(), "isUnread", JsonPrimitive(1))
    }

    @Test
    fun mailRowOutLabelsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(MailRowOut.serializer(), "labels", value)

        assertEquals(value, encoded["labels"])
    }

    @Test
    fun mailRowOutLabelsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailRowOut.serializer(), "labels", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun mailRowOutMailboxPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailRowOut.serializer(), "mailbox", value)

        assertEquals(value, encoded["mailbox"])
    }

    @Test
    fun mailRowOutMailboxRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailRowOut.serializer(), "mailbox", JsonObject(emptyMap()))
    }

    @Test
    fun mailRowOutHasAttachmentPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(MailRowOut.serializer(), "hasAttachment", value)

        assertEquals(value, encoded["hasAttachment"])
    }

    @Test
    fun mailRowOutHasAttachmentRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailRowOut.serializer(), "hasAttachment", JsonPrimitive(1))
    }

    @Test
    fun mailRowOutAttachmentCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailRowOut.serializer(), "attachmentCount", value)

        assertEquals(value, encoded["attachmentCount"])
    }

    @Test
    fun mailRowOutAttachmentCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailRowOut.serializer(), "attachmentCount", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailRowOutPriorityPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailRowOut.serializer(), "priority", value)

        assertEquals(value, encoded["priority"])
    }

    @Test
    fun mailRowOutPriorityRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailRowOut.serializer(), "priority", JsonObject(emptyMap()))
    }

    @Test
    fun mailRowOutPriorityHintPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailRowOut.serializer(), "priorityHint", value)

        assertEquals(value, encoded["priorityHint"])
    }

    @Test
    fun mailRowOutPriorityHintRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailRowOut.serializer(), "priorityHint", JsonObject(emptyMap()))
    }

    @Test
    fun mailRowOutAnalysisStatusPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailRowOut.serializer(), "analysisStatus", value)

        assertEquals(value, encoded["analysisStatus"])
    }

    @Test
    fun mailRowOutAnalysisStatusRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailRowOut.serializer(), "analysisStatus", JsonObject(emptyMap()))
    }

    @Test
    fun mailRowOutAnalysisQualityPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailRowOut.serializer(), "analysisQuality", value)

        assertEquals(value, encoded["analysisQuality"])
    }

    @Test
    fun mailRowOutAnalysisQualityRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailRowOut.serializer(), "analysisQuality", JsonObject(emptyMap()))
    }

    @Test
    fun mailRowOutFeedStatusPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailRowOut.serializer(), "feedStatus", value)

        assertEquals(value, encoded["feedStatus"])
    }

    @Test
    fun mailRowOutFeedStatusRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailRowOut.serializer(), "feedStatus", JsonObject(emptyMap()))
    }

    @Test
    fun mailRowOutCalendarProposalCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailRowOut.serializer(), "calendarProposalCount", value)

        assertEquals(value, encoded["calendarProposalCount"])
    }

    @Test
    fun mailRowOutCalendarProposalCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailRowOut.serializer(), "calendarProposalCount", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailRowOutTodoCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MailRowOut.serializer(), "todoCount", value)

        assertEquals(value, encoded["todoCount"])
    }

    @Test
    fun mailRowOutTodoCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailRowOut.serializer(), "todoCount", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailRowOutWorkStateHintPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailRowOut.serializer(), "workStateHint", value)

        assertEquals(value, encoded["workStateHint"])
    }

    @Test
    fun mailRowOutWorkStateHintRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailRowOut.serializer(), "workStateHint", JsonObject(emptyMap()))
    }

    @Test
    fun mailRowOutRelatedProjectsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(MailRowOut.serializer(), "relatedProjects", value)
        val encodedValues = encoded.getValue("relatedProjects").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun mailRowOutRelatedProjectsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailRowOut.serializer(), "relatedProjects", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun marketQuoteSymbolPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MarketQuote.serializer(), "symbol", value)

        assertEquals(value, encoded["symbol"])
    }

    @Test
    fun marketQuoteSymbolRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MarketQuote.serializer(), "symbol", JsonObject(emptyMap()))
    }

    @Test
    fun marketQuoteLabelPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MarketQuote.serializer(), "label", value)

        assertEquals(value, encoded["label"])
    }

    @Test
    fun marketQuoteLabelRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MarketQuote.serializer(), "label", JsonObject(emptyMap()))
    }

    @Test
    fun marketQuotePricePreservesItsBoundaryValue() {
        val value = JsonPrimitive(Double.MAX_VALUE)
        val encoded = roundTrip(MarketQuote.serializer(), "price", value)

        assertEquals(value, encoded["price"])
    }

    @Test
    fun marketQuotePriceRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MarketQuote.serializer(), "price", JsonPrimitive("not-a-number"))
    }

    @Test
    fun marketQuotePrevClosePreservesItsBoundaryValue() {
        val value = JsonPrimitive(Double.MAX_VALUE)
        val encoded = roundTrip(MarketQuote.serializer(), "prevClose", value)

        assertEquals(value, encoded["prevClose"])
    }

    @Test
    fun marketQuotePrevCloseRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MarketQuote.serializer(), "prevClose", JsonPrimitive("not-a-number"))
    }

    @Test
    fun marketQuoteChangePctPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Double.MAX_VALUE)
        val encoded = roundTrip(MarketQuote.serializer(), "changePct", value)

        assertEquals(value, encoded["changePct"])
    }

    @Test
    fun marketQuoteChangePctRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MarketQuote.serializer(), "changePct", JsonPrimitive("not-a-number"))
    }

    @Test
    fun marketQuoteCurrencyPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MarketQuote.serializer(), "currency", value)

        assertEquals(value, encoded["currency"])
    }

    @Test
    fun marketQuoteCurrencyRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MarketQuote.serializer(), "currency", JsonObject(emptyMap()))
    }

    @Test
    fun marketSummaryQuotesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(MarketSummary.serializer(), "quotes", value)
        val encodedValues = encoded.getValue("quotes").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun marketSummaryQuotesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MarketSummary.serializer(), "quotes", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun marketSummaryAsOfPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(MarketSummary.serializer(), "asOf", value)

        assertEquals(value, encoded["asOf"])
    }

    @Test
    fun marketSummaryAsOfRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MarketSummary.serializer(), "asOf", JsonPrimitive("not-a-number"))
    }

    @Test
    fun marketSummaryStalePreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(MarketSummary.serializer(), "stale", value)

        assertEquals(value, encoded["stale"])
    }

    @Test
    fun marketSummaryStaleRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MarketSummary.serializer(), "stale", JsonPrimitive(1))
    }

    @Test
    fun memberOutNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MemberOut.serializer(), "name", value)

        assertEquals(value, encoded["name"])
    }

    @Test
    fun memberOutNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MemberOut.serializer(), "name", JsonObject(emptyMap()))
    }

    @Test
    fun memberOutRankPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MemberOut.serializer(), "rank", value)

        assertEquals(value, encoded["rank"])
    }

    @Test
    fun memberOutRankRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MemberOut.serializer(), "rank", JsonObject(emptyMap()))
    }

    @Test
    fun memberOutPositionPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MemberOut.serializer(), "position", value)

        assertEquals(value, encoded["position"])
    }

    @Test
    fun memberOutPositionRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MemberOut.serializer(), "position", JsonObject(emptyMap()))
    }

    @Test
    fun memberOutPhonesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(MemberOut.serializer(), "phones", value)

        assertEquals(value, encoded["phones"])
    }

    @Test
    fun memberOutPhonesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MemberOut.serializer(), "phones", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun memberOutEmailsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(MemberOut.serializer(), "emails", value)

        assertEquals(value, encoded["emails"])
    }

    @Test
    fun memberOutEmailsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MemberOut.serializer(), "emails", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun memberOutPersonPathPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MemberOut.serializer(), "personPath", value)

        assertEquals(value, encoded["personPath"])
    }

    @Test
    fun memberOutPersonPathRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MemberOut.serializer(), "personPath", JsonObject(emptyMap()))
    }

    @Test
    fun memoryCategoryRowNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MemoryCategoryRow.serializer(), "name", value)

        assertEquals(value, encoded["name"])
    }

    @Test
    fun memoryCategoryRowNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MemoryCategoryRow.serializer(), "name", JsonObject(emptyMap()))
    }

    @Test
    fun memoryCategoryRowPageCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MemoryCategoryRow.serializer(), "pageCount", value)

        assertEquals(value, encoded["pageCount"])
    }

    @Test
    fun memoryCategoryRowPageCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MemoryCategoryRow.serializer(), "pageCount", JsonPrimitive("not-a-number"))
    }

    @Test
    fun memoryPageRowPathPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MemoryPageRow.serializer(), "path", value)

        assertEquals(value, encoded["path"])
    }

    @Test
    fun memoryPageRowPathRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MemoryPageRow.serializer(), "path", JsonObject(emptyMap()))
    }

    @Test
    fun memoryPageRowTitlePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MemoryPageRow.serializer(), "title", value)

        assertEquals(value, encoded["title"])
    }

    @Test
    fun memoryPageRowTitleRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MemoryPageRow.serializer(), "title", JsonObject(emptyMap()))
    }

    @Test
    fun memoryPageRowSummaryPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MemoryPageRow.serializer(), "summary", value)

        assertEquals(value, encoded["summary"])
    }

    @Test
    fun memoryPageRowSummaryRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MemoryPageRow.serializer(), "summary", JsonObject(emptyMap()))
    }

    @Test
    fun memoryPageRowUpdatedPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MemoryPageRow.serializer(), "updated", value)

        assertEquals(value, encoded["updated"])
    }

    @Test
    fun memoryPageRowUpdatedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MemoryPageRow.serializer(), "updated", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronDetailIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronDetail.serializer(), "id", value)

        assertEquals(value, encoded["id"])
    }

    @Test
    fun miniappCronDetailIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "id", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronDetailNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronDetail.serializer(), "name", value)

        assertEquals(value, encoded["name"])
    }

    @Test
    fun miniappCronDetailNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "name", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronDetailEnabledPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(MiniappCronDetail.serializer(), "enabled", value)

        assertEquals(value, encoded["enabled"])
    }

    @Test
    fun miniappCronDetailEnabledRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "enabled", JsonPrimitive(1))
    }

    @Test
    fun miniappCronDetailAgentIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronDetail.serializer(), "agentId", value)

        assertEquals(value, encoded["agentId"])
    }

    @Test
    fun miniappCronDetailAgentIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "agentId", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronDetailSessionTargetPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronDetail.serializer(), "sessionTarget", value)

        assertEquals(value, encoded["sessionTarget"])
    }

    @Test
    fun miniappCronDetailSessionTargetRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "sessionTarget", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronDetailSchedulePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronDetail.serializer(), "schedule", value)

        assertEquals(value, encoded["schedule"])
    }

    @Test
    fun miniappCronDetailScheduleRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "schedule", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronDetailScheduleSpecPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronDetail.serializer(), "scheduleSpec", value)

        assertEquals(value, encoded["scheduleSpec"])
    }

    @Test
    fun miniappCronDetailScheduleSpecRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "scheduleSpec", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronDetailScheduleKindPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronDetail.serializer(), "scheduleKind", value)

        assertEquals(value, encoded["scheduleKind"])
    }

    @Test
    fun miniappCronDetailScheduleKindRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "scheduleKind", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronDetailTimezonePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronDetail.serializer(), "timezone", value)

        assertEquals(value, encoded["timezone"])
    }

    @Test
    fun miniappCronDetailTimezoneRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "timezone", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronDetailCronExprPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronDetail.serializer(), "cronExpr", value)

        assertEquals(value, encoded["cronExpr"])
    }

    @Test
    fun miniappCronDetailCronExprRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "cronExpr", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronDetailStaggerMsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(MiniappCronDetail.serializer(), "staggerMs", value)

        assertEquals(value, encoded["staggerMs"])
    }

    @Test
    fun miniappCronDetailStaggerMsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "staggerMs", JsonPrimitive("not-a-number"))
    }

    @Test
    fun miniappCronDetailPayloadKindPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronDetail.serializer(), "payloadKind", value)

        assertEquals(value, encoded["payloadKind"])
    }

    @Test
    fun miniappCronDetailPayloadKindRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "payloadKind", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronDetailPromptPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronDetail.serializer(), "prompt", value)

        assertEquals(value, encoded["prompt"])
    }

    @Test
    fun miniappCronDetailPromptRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "prompt", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronDetailModelPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronDetail.serializer(), "model", value)

        assertEquals(value, encoded["model"])
    }

    @Test
    fun miniappCronDetailModelRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "model", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronDetailThinkingPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronDetail.serializer(), "thinking", value)

        assertEquals(value, encoded["thinking"])
    }

    @Test
    fun miniappCronDetailThinkingRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "thinking", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronDetailTimeoutSecondsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MiniappCronDetail.serializer(), "timeoutSeconds", value)

        assertEquals(value, encoded["timeoutSeconds"])
    }

    @Test
    fun miniappCronDetailTimeoutSecondsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "timeoutSeconds", JsonPrimitive("not-a-number"))
    }

    @Test
    fun miniappCronDetailLightContextPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(MiniappCronDetail.serializer(), "lightContext", value)

        assertEquals(value, encoded["lightContext"])
    }

    @Test
    fun miniappCronDetailLightContextRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "lightContext", JsonPrimitive(1))
    }

    @Test
    fun miniappCronDetailRetryCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MiniappCronDetail.serializer(), "retryCount", value)

        assertEquals(value, encoded["retryCount"])
    }

    @Test
    fun miniappCronDetailRetryCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "retryCount", JsonPrimitive("not-a-number"))
    }

    @Test
    fun miniappCronDetailDeliveryChannelPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronDetail.serializer(), "deliveryChannel", value)

        assertEquals(value, encoded["deliveryChannel"])
    }

    @Test
    fun miniappCronDetailDeliveryChannelRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "deliveryChannel", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronDetailDeliveryToPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronDetail.serializer(), "deliveryTo", value)

        assertEquals(value, encoded["deliveryTo"])
    }

    @Test
    fun miniappCronDetailDeliveryToRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "deliveryTo", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronDetailDeliveryThreadIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronDetail.serializer(), "deliveryThreadId", value)

        assertEquals(value, encoded["deliveryThreadId"])
    }

    @Test
    fun miniappCronDetailDeliveryThreadIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "deliveryThreadId", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronDetailFailureAlertAfterPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MiniappCronDetail.serializer(), "failureAlertAfter", value)

        assertEquals(value, encoded["failureAlertAfter"])
    }

    @Test
    fun miniappCronDetailFailureAlertAfterRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "failureAlertAfter", JsonPrimitive("not-a-number"))
    }

    @Test
    fun miniappCronDetailNextRunAtMsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(MiniappCronDetail.serializer(), "nextRunAtMs", value)

        assertEquals(value, encoded["nextRunAtMs"])
    }

    @Test
    fun miniappCronDetailNextRunAtMsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "nextRunAtMs", JsonPrimitive("not-a-number"))
    }

    @Test
    fun miniappCronDetailLastSessionKeyPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronDetail.serializer(), "lastSessionKey", value)

        assertEquals(value, encoded["lastSessionKey"])
    }

    @Test
    fun miniappCronDetailLastSessionKeyRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "lastSessionKey", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronDetailLastDeliveryStatusPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronDetail.serializer(), "lastDeliveryStatus", value)

        assertEquals(value, encoded["lastDeliveryStatus"])
    }

    @Test
    fun miniappCronDetailLastDeliveryStatusRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "lastDeliveryStatus", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronDetailLastErrorPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronDetail.serializer(), "lastError", value)

        assertEquals(value, encoded["lastError"])
    }

    @Test
    fun miniappCronDetailLastErrorRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "lastError", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronDetailConsecutiveErrorsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MiniappCronDetail.serializer(), "consecutiveErrors", value)

        assertEquals(value, encoded["consecutiveErrors"])
    }

    @Test
    fun miniappCronDetailConsecutiveErrorsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "consecutiveErrors", JsonPrimitive("not-a-number"))
    }

    @Test
    fun miniappCronDetailAutoDisabledAtMsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(MiniappCronDetail.serializer(), "autoDisabledAtMs", value)

        assertEquals(value, encoded["autoDisabledAtMs"])
    }

    @Test
    fun miniappCronDetailAutoDisabledAtMsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "autoDisabledAtMs", JsonPrimitive("not-a-number"))
    }

    @Test
    fun miniappCronDetailCreatedAtMsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(MiniappCronDetail.serializer(), "createdAtMs", value)

        assertEquals(value, encoded["createdAtMs"])
    }

    @Test
    fun miniappCronDetailCreatedAtMsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "createdAtMs", JsonPrimitive("not-a-number"))
    }

    @Test
    fun miniappCronDetailUpdatedAtMsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(MiniappCronDetail.serializer(), "updatedAtMs", value)

        assertEquals(value, encoded["updatedAtMs"])
    }

    @Test
    fun miniappCronDetailUpdatedAtMsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronDetail.serializer(), "updatedAtMs", JsonPrimitive("not-a-number"))
    }

    @Test
    fun miniappCronRowIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronRow.serializer(), "id", value)

        assertEquals(value, encoded["id"])
    }

    @Test
    fun miniappCronRowIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronRow.serializer(), "id", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronRowNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronRow.serializer(), "name", value)

        assertEquals(value, encoded["name"])
    }

    @Test
    fun miniappCronRowNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronRow.serializer(), "name", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronRowEnabledPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(MiniappCronRow.serializer(), "enabled", value)

        assertEquals(value, encoded["enabled"])
    }

    @Test
    fun miniappCronRowEnabledRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronRow.serializer(), "enabled", JsonPrimitive(1))
    }

    @Test
    fun miniappCronRowSchedulePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronRow.serializer(), "schedule", value)

        assertEquals(value, encoded["schedule"])
    }

    @Test
    fun miniappCronRowScheduleRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronRow.serializer(), "schedule", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronRowPayloadKindPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronRow.serializer(), "payloadKind", value)

        assertEquals(value, encoded["payloadKind"])
    }

    @Test
    fun miniappCronRowPayloadKindRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronRow.serializer(), "payloadKind", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronRowPayloadPreviewPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronRow.serializer(), "payloadPreview", value)

        assertEquals(value, encoded["payloadPreview"])
    }

    @Test
    fun miniappCronRowPayloadPreviewRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronRow.serializer(), "payloadPreview", JsonObject(emptyMap()))
    }

    @Test
    fun miniappCronRowNextRunAtMsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(MiniappCronRow.serializer(), "nextRunAtMs", value)

        assertEquals(value, encoded["nextRunAtMs"])
    }

    @Test
    fun miniappCronRowNextRunAtMsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronRow.serializer(), "nextRunAtMs", JsonPrimitive("not-a-number"))
    }

    @Test
    fun miniappCronRowConsecutiveErrorsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(MiniappCronRow.serializer(), "consecutiveErrors", value)

        assertEquals(value, encoded["consecutiveErrors"])
    }

    @Test
    fun miniappCronRowConsecutiveErrorsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronRow.serializer(), "consecutiveErrors", JsonPrimitive("not-a-number"))
    }

    @Test
    fun miniappCronRowAutoDisabledAtMsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(MiniappCronRow.serializer(), "autoDisabledAtMs", value)

        assertEquals(value, encoded["autoDisabledAtMs"])
    }

    @Test
    fun miniappCronRowAutoDisabledAtMsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronRow.serializer(), "autoDisabledAtMs", JsonPrimitive("not-a-number"))
    }

    @Test
    fun miniappCronRowLastErrorPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MiniappCronRow.serializer(), "lastError", value)

        assertEquals(value, encoded["lastError"])
    }

    @Test
    fun miniappCronRowLastErrorRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MiniappCronRow.serializer(), "lastError", JsonObject(emptyMap()))
    }

    @Test
    fun modelAddResultOkPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(ModelAddResult.serializer(), "ok", value)

        assertEquals(value, encoded["ok"])
    }

    @Test
    fun modelAddResultOkRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelAddResult.serializer(), "ok", JsonPrimitive(1))
    }

    @Test
    fun modelAddResultIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ModelAddResult.serializer(), "id", value)

        assertEquals(value, encoded["id"])
    }

    @Test
    fun modelAddResultIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelAddResult.serializer(), "id", JsonObject(emptyMap()))
    }

    @Test
    fun modelAddResultProviderPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ModelAddResult.serializer(), "provider", value)

        assertEquals(value, encoded["provider"])
    }

    @Test
    fun modelAddResultProviderRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelAddResult.serializer(), "provider", JsonObject(emptyMap()))
    }

    @Test
    fun modelAddResultEndpointPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ModelAddResult.serializer(), "endpoint", value)

        assertEquals(value, encoded["endpoint"])
    }

    @Test
    fun modelAddResultEndpointRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelAddResult.serializer(), "endpoint", JsonObject(emptyMap()))
    }

    @Test
    fun modelAddResultModelPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ModelAddResult.serializer(), "model", value)

        assertEquals(value, encoded["model"])
    }

    @Test
    fun modelAddResultModelRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelAddResult.serializer(), "model", JsonObject(emptyMap()))
    }

    @Test
    fun modelAddResultAddedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(ModelAddResult.serializer(), "added", value)

        assertEquals(value, encoded["added"])
    }

    @Test
    fun modelAddResultAddedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelAddResult.serializer(), "added", JsonPrimitive(1))
    }

    @Test
    fun modelDeleteResultOkPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(ModelDeleteResult.serializer(), "ok", value)

        assertEquals(value, encoded["ok"])
    }

    @Test
    fun modelDeleteResultOkRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelDeleteResult.serializer(), "ok", JsonPrimitive(1))
    }

    @Test
    fun modelDeleteResultIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ModelDeleteResult.serializer(), "id", value)

        assertEquals(value, encoded["id"])
    }

    @Test
    fun modelDeleteResultIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelDeleteResult.serializer(), "id", JsonObject(emptyMap()))
    }

    @Test
    fun modelDeleteResultRemovedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(ModelDeleteResult.serializer(), "removed", value)

        assertEquals(value, encoded["removed"])
    }

    @Test
    fun modelDeleteResultRemovedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelDeleteResult.serializer(), "removed", JsonPrimitive(1))
    }

    @Test
    fun modelDeleteResultClearedRolesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(ModelDeleteResult.serializer(), "clearedRoles", value)

        assertEquals(value, encoded["clearedRoles"])
    }

    @Test
    fun modelDeleteResultClearedRolesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelDeleteResult.serializer(), "clearedRoles", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun modelDeleteResultCurrentPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ModelDeleteResult.serializer(), "current", value)

        assertEquals(value, encoded["current"])
    }

    @Test
    fun modelDeleteResultCurrentRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelDeleteResult.serializer(), "current", JsonObject(emptyMap()))
    }

    @Test
    fun modelOptionIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ModelOption.serializer(), "id", value)

        assertEquals(value, encoded["id"])
    }

    @Test
    fun modelOptionIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelOption.serializer(), "id", JsonObject(emptyMap()))
    }

    @Test
    fun modelOptionLabelPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ModelOption.serializer(), "label", value)

        assertEquals(value, encoded["label"])
    }

    @Test
    fun modelOptionLabelRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelOption.serializer(), "label", JsonObject(emptyMap()))
    }

    @Test
    fun modelOptionProviderPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ModelOption.serializer(), "provider", value)

        assertEquals(value, encoded["provider"])
    }

    @Test
    fun modelOptionProviderRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelOption.serializer(), "provider", JsonObject(emptyMap()))
    }

    @Test
    fun modelOptionDisplayPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ModelOption.serializer(), "display", value)

        assertEquals(value, encoded["display"])
    }

    @Test
    fun modelOptionDisplayRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelOption.serializer(), "display", JsonObject(emptyMap()))
    }

    @Test
    fun modelOptionHealthPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ModelOption.serializer(), "health", value)

        assertEquals(value, encoded["health"])
    }

    @Test
    fun modelOptionHealthRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelOption.serializer(), "health", JsonObject(emptyMap()))
    }

    @Test
    fun modelOptionCurrentPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(ModelOption.serializer(), "current", value)

        assertEquals(value, encoded["current"])
    }

    @Test
    fun modelOptionCurrentRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelOption.serializer(), "current", JsonPrimitive(1))
    }

    @Test
    fun modelOptionCustomPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(ModelOption.serializer(), "custom", value)

        assertEquals(value, encoded["custom"])
    }

    @Test
    fun modelOptionCustomRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelOption.serializer(), "custom", JsonPrimitive(1))
    }

    @Test
    fun modelOptionDeletablePreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(ModelOption.serializer(), "deletable", value)

        assertEquals(value, encoded["deletable"])
    }

    @Test
    fun modelOptionDeletableRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelOption.serializer(), "deletable", JsonPrimitive(1))
    }

    @Test
    fun modelOptionUnhealthyPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(ModelOption.serializer(), "unhealthy", value)

        assertEquals(value, encoded["unhealthy"])
    }

    @Test
    fun modelOptionUnhealthyRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelOption.serializer(), "unhealthy", JsonPrimitive(1))
    }

    @Test
    fun modelOptionNotePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ModelOption.serializer(), "note", value)

        assertEquals(value, encoded["note"])
    }

    @Test
    fun modelOptionNoteRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelOption.serializer(), "note", JsonObject(emptyMap()))
    }

    @Test
    fun modelSectionTitlePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ModelSection.serializer(), "title", value)

        assertEquals(value, encoded["title"])
    }

    @Test
    fun modelSectionTitleRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelSection.serializer(), "title", JsonObject(emptyMap()))
    }

    @Test
    fun modelSectionModelsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(ModelSection.serializer(), "models", value)
        val encodedValues = encoded.getValue("models").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun modelSectionModelsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelSection.serializer(), "models", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun modelsListResultCurrentPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ModelsListResult.serializer(), "current", value)

        assertEquals(value, encoded["current"])
    }

    @Test
    fun modelsListResultCurrentRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelsListResult.serializer(), "current", JsonObject(emptyMap()))
    }

    @Test
    fun modelsListResultRolesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(ModelsListResult.serializer(), "roles", value)
        val encodedValues = encoded.getValue("roles").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun modelsListResultRolesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelsListResult.serializer(), "roles", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun modelsListResultSectionsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(ModelsListResult.serializer(), "sections", value)
        val encodedValues = encoded.getValue("sections").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun modelsListResultSectionsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelsListResult.serializer(), "sections", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun modelsListResultAdvisoriesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(ModelsListResult.serializer(), "advisories", value)

        assertEquals(value, encoded["advisories"])
    }

    @Test
    fun modelsListResultAdvisoriesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelsListResult.serializer(), "advisories", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun modelsListResultMainHasVisionPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(ModelsListResult.serializer(), "mainHasVision", value)

        assertEquals(value, encoded["mainHasVision"])
    }

    @Test
    fun modelsListResultMainHasVisionRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelsListResult.serializer(), "mainHasVision", JsonPrimitive(1))
    }

    @Test
    fun notebookListOutNotebooksPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(NotebookListOut.serializer(), "notebooks", value)
        val encodedValues = encoded.getValue("notebooks").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun notebookListOutNotebooksRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NotebookListOut.serializer(), "notebooks", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun notebookOutIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(NotebookOut.serializer(), "id", value)

        assertEquals(value, encoded["id"])
    }

    @Test
    fun notebookOutIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NotebookOut.serializer(), "id", JsonObject(emptyMap()))
    }

    @Test
    fun notebookOutNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(NotebookOut.serializer(), "name", value)

        assertEquals(value, encoded["name"])
    }

    @Test
    fun notebookOutNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NotebookOut.serializer(), "name", JsonObject(emptyMap()))
    }

    @Test
    fun notebookOutDescriptionPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(NotebookOut.serializer(), "description", value)

        assertEquals(value, encoded["description"])
    }

    @Test
    fun notebookOutDescriptionRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NotebookOut.serializer(), "description", JsonObject(emptyMap()))
    }

    @Test
    fun notebookOutDealRefPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(NotebookOut.serializer(), "dealRef", value)

        assertEquals(value, encoded["dealRef"])
    }

    @Test
    fun notebookOutDealRefRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NotebookOut.serializer(), "dealRef", JsonObject(emptyMap()))
    }

    @Test
    fun notebookOutModePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(NotebookOut.serializer(), "mode", value)

        assertEquals(value, encoded["mode"])
    }

    @Test
    fun notebookOutModeRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NotebookOut.serializer(), "mode", JsonObject(emptyMap()))
    }

    @Test
    fun notebookOutSourcesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(NotebookOut.serializer(), "sources", value)
        val encodedValues = encoded.getValue("sources").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun notebookOutSourcesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NotebookOut.serializer(), "sources", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun notebookOutUpdatedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(NotebookOut.serializer(), "updated", value)

        assertEquals(value, encoded["updated"])
    }

    @Test
    fun notebookOutUpdatedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NotebookOut.serializer(), "updated", JsonPrimitive("not-a-number"))
    }

    @Test
    fun notebookSourceOutCitePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(NotebookSourceOut.serializer(), "cite", value)

        assertEquals(value, encoded["cite"])
    }

    @Test
    fun notebookSourceOutCiteRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NotebookSourceOut.serializer(), "cite", JsonObject(emptyMap()))
    }

    @Test
    fun notebookSourceOutKindPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(NotebookSourceOut.serializer(), "kind", value)

        assertEquals(value, encoded["kind"])
    }

    @Test
    fun notebookSourceOutKindRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NotebookSourceOut.serializer(), "kind", JsonObject(emptyMap()))
    }

    @Test
    fun notebookSourceOutRefPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(NotebookSourceOut.serializer(), "ref", value)

        assertEquals(value, encoded["ref"])
    }

    @Test
    fun notebookSourceOutRefRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NotebookSourceOut.serializer(), "ref", JsonObject(emptyMap()))
    }

    @Test
    fun notebookSourceOutTitlePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(NotebookSourceOut.serializer(), "title", value)

        assertEquals(value, encoded["title"])
    }

    @Test
    fun notebookSourceOutTitleRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NotebookSourceOut.serializer(), "title", JsonObject(emptyMap()))
    }

    @Test
    fun notebookSourceOutTextPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(NotebookSourceOut.serializer(), "text", value)

        assertEquals(value, encoded["text"])
    }

    @Test
    fun notebookSourceOutTextRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NotebookSourceOut.serializer(), "text", JsonObject(emptyMap()))
    }

    @Test
    fun notebookSummaryOutIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(NotebookSummaryOut.serializer(), "id", value)

        assertEquals(value, encoded["id"])
    }

    @Test
    fun notebookSummaryOutIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NotebookSummaryOut.serializer(), "id", JsonObject(emptyMap()))
    }

    @Test
    fun notebookSummaryOutNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(NotebookSummaryOut.serializer(), "name", value)

        assertEquals(value, encoded["name"])
    }

    @Test
    fun notebookSummaryOutNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NotebookSummaryOut.serializer(), "name", JsonObject(emptyMap()))
    }

    @Test
    fun notebookSummaryOutDescriptionPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(NotebookSummaryOut.serializer(), "description", value)

        assertEquals(value, encoded["description"])
    }

    @Test
    fun notebookSummaryOutDescriptionRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NotebookSummaryOut.serializer(), "description", JsonObject(emptyMap()))
    }

    @Test
    fun notebookSummaryOutDealRefPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(NotebookSummaryOut.serializer(), "dealRef", value)

        assertEquals(value, encoded["dealRef"])
    }

    @Test
    fun notebookSummaryOutDealRefRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NotebookSummaryOut.serializer(), "dealRef", JsonObject(emptyMap()))
    }

    @Test
    fun notebookSummaryOutProjectRefsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(NotebookSummaryOut.serializer(), "projectRefs", value)

        assertEquals(value, encoded["projectRefs"])
    }

    @Test
    fun notebookSummaryOutProjectRefsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NotebookSummaryOut.serializer(), "projectRefs", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun notebookSummaryOutSourceCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(NotebookSummaryOut.serializer(), "sourceCount", value)

        assertEquals(value, encoded["sourceCount"])
    }

    @Test
    fun notebookSummaryOutSourceCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NotebookSummaryOut.serializer(), "sourceCount", JsonPrimitive("not-a-number"))
    }

    @Test
    fun notebookSummaryOutUpdatedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(NotebookSummaryOut.serializer(), "updated", value)

        assertEquals(value, encoded["updated"])
    }

    @Test
    fun notebookSummaryOutUpdatedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NotebookSummaryOut.serializer(), "updated", JsonPrimitive("not-a-number"))
    }

    @Test
    fun orgNodeOutIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(OrgNodeOut.serializer(), "id", value)

        assertEquals(value, encoded["id"])
    }

    @Test
    fun orgNodeOutIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(OrgNodeOut.serializer(), "id", JsonObject(emptyMap()))
    }

    @Test
    fun orgNodeOutNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(OrgNodeOut.serializer(), "name", value)

        assertEquals(value, encoded["name"])
    }

    @Test
    fun orgNodeOutNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(OrgNodeOut.serializer(), "name", JsonObject(emptyMap()))
    }

    @Test
    fun orgNodeOutTypePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(OrgNodeOut.serializer(), "type", value)

        assertEquals(value, encoded["type"])
    }

    @Test
    fun orgNodeOutTypeRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(OrgNodeOut.serializer(), "type", JsonObject(emptyMap()))
    }

    @Test
    fun orgNodeOutParentIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(OrgNodeOut.serializer(), "parentId", value)

        assertEquals(value, encoded["parentId"])
    }

    @Test
    fun orgNodeOutParentIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(OrgNodeOut.serializer(), "parentId", JsonObject(emptyMap()))
    }

    @Test
    fun orgNodeOutLanePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(OrgNodeOut.serializer(), "lane", value)

        assertEquals(value, encoded["lane"])
    }

    @Test
    fun orgNodeOutLaneRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(OrgNodeOut.serializer(), "lane", JsonObject(emptyMap()))
    }

    @Test
    fun orgNodeOutMembersPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(OrgNodeOut.serializer(), "members", value)
        val encodedValues = encoded.getValue("members").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun orgNodeOutMembersRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(OrgNodeOut.serializer(), "members", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun orgNodeOutKeywordsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(OrgNodeOut.serializer(), "keywords", value)

        assertEquals(value, encoded["keywords"])
    }

    @Test
    fun orgNodeOutKeywordsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(OrgNodeOut.serializer(), "keywords", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun orgNodeOutCompaniesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(OrgNodeOut.serializer(), "companies", value)

        assertEquals(value, encoded["companies"])
    }

    @Test
    fun orgNodeOutCompaniesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(OrgNodeOut.serializer(), "companies", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun orgSaveOutSavedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(OrgSaveOut.serializer(), "saved", value)

        assertEquals(value, encoded["saved"])
    }

    @Test
    fun orgSaveOutSavedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(OrgSaveOut.serializer(), "saved", JsonPrimitive(1))
    }

    @Test
    fun orgSaveOutNodeCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(OrgSaveOut.serializer(), "nodeCount", value)

        assertEquals(value, encoded["nodeCount"])
    }

    @Test
    fun orgSaveOutNodeCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(OrgSaveOut.serializer(), "nodeCount", JsonPrimitive("not-a-number"))
    }

    @Test
    fun orgSaveOutHasLanesPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(OrgSaveOut.serializer(), "hasLanes", value)

        assertEquals(value, encoded["hasLanes"])
    }

    @Test
    fun orgSaveOutHasLanesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(OrgSaveOut.serializer(), "hasLanes", JsonPrimitive(1))
    }

    @Test
    fun orgTreeOutNodesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(OrgTreeOut.serializer(), "nodes", value)
        val encodedValues = encoded.getValue("nodes").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun orgTreeOutNodesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(OrgTreeOut.serializer(), "nodes", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun personRowEmailPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PersonRow.serializer(), "email", value)

        assertEquals(value, encoded["email"])
    }

    @Test
    fun personRowEmailRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PersonRow.serializer(), "email", JsonObject(emptyMap()))
    }

    @Test
    fun personRowNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PersonRow.serializer(), "name", value)

        assertEquals(value, encoded["name"])
    }

    @Test
    fun personRowNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PersonRow.serializer(), "name", JsonObject(emptyMap()))
    }

    @Test
    fun personRowMessageCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(PersonRow.serializer(), "messageCount", value)

        assertEquals(value, encoded["messageCount"])
    }

    @Test
    fun personRowMessageCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PersonRow.serializer(), "messageCount", JsonPrimitive("not-a-number"))
    }

    @Test
    fun personRowLastSeenPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PersonRow.serializer(), "lastSeen", value)

        assertEquals(value, encoded["lastSeen"])
    }

    @Test
    fun personRowLastSeenRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PersonRow.serializer(), "lastSeen", JsonObject(emptyMap()))
    }

    @Test
    fun personRowLastSubjectPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PersonRow.serializer(), "lastSubject", value)

        assertEquals(value, encoded["lastSubject"])
    }

    @Test
    fun personRowLastSubjectRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PersonRow.serializer(), "lastSubject", JsonObject(emptyMap()))
    }

    @Test
    fun personRowWikiPathPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PersonRow.serializer(), "wikiPath", value)

        assertEquals(value, encoded["wikiPath"])
    }

    @Test
    fun personRowWikiPathRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PersonRow.serializer(), "wikiPath", JsonObject(emptyMap()))
    }

    @Test
    fun personRowWikiSummaryPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PersonRow.serializer(), "wikiSummary", value)

        assertEquals(value, encoded["wikiSummary"])
    }

    @Test
    fun personRowWikiSummaryRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PersonRow.serializer(), "wikiSummary", JsonObject(emptyMap()))
    }

    @Test
    fun projectDigestRowProjectPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ProjectDigestRow.serializer(), "project", value)

        assertEquals(value, encoded["project"])
    }

    @Test
    fun projectDigestRowProjectRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ProjectDigestRow.serializer(), "project", JsonObject(emptyMap()))
    }

    @Test
    fun projectDigestRowHeadlinePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ProjectDigestRow.serializer(), "headline", value)

        assertEquals(value, encoded["headline"])
    }

    @Test
    fun projectDigestRowHeadlineRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ProjectDigestRow.serializer(), "headline", JsonObject(emptyMap()))
    }

    @Test
    fun projectDigestRowBulletsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(ProjectDigestRow.serializer(), "bullets", value)

        assertEquals(value, encoded["bullets"])
    }

    @Test
    fun projectDigestRowBulletsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ProjectDigestRow.serializer(), "bullets", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun projectDigestRowDuePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ProjectDigestRow.serializer(), "due", value)

        assertEquals(value, encoded["due"])
    }

    @Test
    fun projectDigestRowDueRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ProjectDigestRow.serializer(), "due", JsonObject(emptyMap()))
    }

    @Test
    fun projectDigestRowUpdatedAtMsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(ProjectDigestRow.serializer(), "updatedAtMs", value)

        assertEquals(value, encoded["updatedAtMs"])
    }

    @Test
    fun projectDigestRowUpdatedAtMsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ProjectDigestRow.serializer(), "updatedAtMs", JsonPrimitive("not-a-number"))
    }

    @Test
    fun projectDigestRowPathPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ProjectDigestRow.serializer(), "path", value)

        assertEquals(value, encoded["path"])
    }

    @Test
    fun projectDigestRowPathRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ProjectDigestRow.serializer(), "path", JsonObject(emptyMap()))
    }

    @Test
    fun projectDigestRowCodePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ProjectDigestRow.serializer(), "code", value)

        assertEquals(value, encoded["code"])
    }

    @Test
    fun projectDigestRowCodeRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ProjectDigestRow.serializer(), "code", JsonObject(emptyMap()))
    }

    @Test
    fun projectDigestRowClientPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ProjectDigestRow.serializer(), "client", value)

        assertEquals(value, encoded["client"])
    }

    @Test
    fun projectDigestRowClientRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ProjectDigestRow.serializer(), "client", JsonObject(emptyMap()))
    }

    @Test
    fun projectDigestRowRefsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(ProjectDigestRow.serializer(), "refs", value)

        assertEquals(value, encoded["refs"])
    }

    @Test
    fun projectDigestRowRefsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ProjectDigestRow.serializer(), "refs", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun projectDigestsOutDigestsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(ProjectDigestsOut.serializer(), "digests", value)
        val encodedValues = encoded.getValue("digests").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun projectDigestsOutDigestsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ProjectDigestsOut.serializer(), "digests", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun projectLinkedOutMailPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(ProjectLinkedOut.serializer(), "mail", value)

        assertEquals(value, encoded["mail"])
    }

    @Test
    fun projectLinkedOutMailRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ProjectLinkedOut.serializer(), "mail", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun projectLinkedOutCalendarPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(ProjectLinkedOut.serializer(), "calendar", value)

        assertEquals(value, encoded["calendar"])
    }

    @Test
    fun projectLinkedOutCalendarRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ProjectLinkedOut.serializer(), "calendar", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun projectLinkedOutTodoPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(ProjectLinkedOut.serializer(), "todo", value)

        assertEquals(value, encoded["todo"])
    }

    @Test
    fun projectLinkedOutTodoRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ProjectLinkedOut.serializer(), "todo", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun projectLinkedOutWorkfeedPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(ProjectLinkedOut.serializer(), "workfeed", value)

        assertEquals(value, encoded["workfeed"])
    }

    @Test
    fun projectLinkedOutWorkfeedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ProjectLinkedOut.serializer(), "workfeed", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun projectLinkedOutNotebookPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(ProjectLinkedOut.serializer(), "notebook", value)

        assertEquals(value, encoded["notebook"])
    }

    @Test
    fun projectLinkedOutNotebookRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ProjectLinkedOut.serializer(), "notebook", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun projectRefPathPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ProjectRef.serializer(), "path", value)

        assertEquals(value, encoded["path"])
    }

    @Test
    fun projectRefPathRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ProjectRef.serializer(), "path", JsonObject(emptyMap()))
    }

    @Test
    fun projectRefTitlePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ProjectRef.serializer(), "title", value)

        assertEquals(value, encoded["title"])
    }

    @Test
    fun projectRefTitleRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ProjectRef.serializer(), "title", JsonObject(emptyMap()))
    }

    @Test
    fun projectRefSummaryPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ProjectRef.serializer(), "summary", value)

        assertEquals(value, encoded["summary"])
    }

    @Test
    fun projectRefSummaryRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ProjectRef.serializer(), "summary", JsonObject(emptyMap()))
    }

    @Test
    fun promptDetailOutIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PromptDetailOut.serializer(), "id", value)

        assertEquals(value, encoded["id"])
    }

    @Test
    fun promptDetailOutIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptDetailOut.serializer(), "id", JsonObject(emptyMap()))
    }

    @Test
    fun promptDetailOutTitlePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PromptDetailOut.serializer(), "title", value)

        assertEquals(value, encoded["title"])
    }

    @Test
    fun promptDetailOutTitleRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptDetailOut.serializer(), "title", JsonObject(emptyMap()))
    }

    @Test
    fun promptDetailOutDescriptionPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PromptDetailOut.serializer(), "description", value)

        assertEquals(value, encoded["description"])
    }

    @Test
    fun promptDetailOutDescriptionRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptDetailOut.serializer(), "description", JsonObject(emptyMap()))
    }

    @Test
    fun promptDetailOutCategoryPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PromptDetailOut.serializer(), "category", value)

        assertEquals(value, encoded["category"])
    }

    @Test
    fun promptDetailOutCategoryRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptDetailOut.serializer(), "category", JsonObject(emptyMap()))
    }

    @Test
    fun promptDetailOutTextPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PromptDetailOut.serializer(), "text", value)

        assertEquals(value, encoded["text"])
    }

    @Test
    fun promptDetailOutTextRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptDetailOut.serializer(), "text", JsonObject(emptyMap()))
    }

    @Test
    fun promptDetailOutDefaultTextPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PromptDetailOut.serializer(), "defaultText", value)

        assertEquals(value, encoded["defaultText"])
    }

    @Test
    fun promptDetailOutDefaultTextRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptDetailOut.serializer(), "defaultText", JsonObject(emptyMap()))
    }

    @Test
    fun promptDetailOutEditablePreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(PromptDetailOut.serializer(), "editable", value)

        assertEquals(value, encoded["editable"])
    }

    @Test
    fun promptDetailOutEditableRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptDetailOut.serializer(), "editable", JsonPrimitive(1))
    }

    @Test
    fun promptDetailOutOverriddenPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(PromptDetailOut.serializer(), "overridden", value)

        assertEquals(value, encoded["overridden"])
    }

    @Test
    fun promptDetailOutOverriddenRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptDetailOut.serializer(), "overridden", JsonPrimitive(1))
    }

    @Test
    fun promptDetailOutUpdatedAtMsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(PromptDetailOut.serializer(), "updatedAtMs", value)

        assertEquals(value, encoded["updatedAtMs"])
    }

    @Test
    fun promptDetailOutUpdatedAtMsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptDetailOut.serializer(), "updatedAtMs", JsonPrimitive("not-a-number"))
    }

    @Test
    fun promptListResponsePromptsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(PromptListResponse.serializer(), "prompts", value)
        val encodedValues = encoded.getValue("prompts").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun promptListResponsePromptsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptListResponse.serializer(), "prompts", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun promptListResponseCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(PromptListResponse.serializer(), "count", value)

        assertEquals(value, encoded["count"])
    }

    @Test
    fun promptListResponseCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptListResponse.serializer(), "count", JsonPrimitive("not-a-number"))
    }

    @Test
    fun promptRowIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PromptRow.serializer(), "id", value)

        assertEquals(value, encoded["id"])
    }

    @Test
    fun promptRowIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptRow.serializer(), "id", JsonObject(emptyMap()))
    }

    @Test
    fun promptRowTitlePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PromptRow.serializer(), "title", value)

        assertEquals(value, encoded["title"])
    }

    @Test
    fun promptRowTitleRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptRow.serializer(), "title", JsonObject(emptyMap()))
    }

    @Test
    fun promptRowDescriptionPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PromptRow.serializer(), "description", value)

        assertEquals(value, encoded["description"])
    }

    @Test
    fun promptRowDescriptionRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptRow.serializer(), "description", JsonObject(emptyMap()))
    }

    @Test
    fun promptRowCategoryPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PromptRow.serializer(), "category", value)

        assertEquals(value, encoded["category"])
    }

    @Test
    fun promptRowCategoryRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptRow.serializer(), "category", JsonObject(emptyMap()))
    }

    @Test
    fun promptRowEditablePreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(PromptRow.serializer(), "editable", value)

        assertEquals(value, encoded["editable"])
    }

    @Test
    fun promptRowEditableRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptRow.serializer(), "editable", JsonPrimitive(1))
    }

    @Test
    fun promptRowOverriddenPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(PromptRow.serializer(), "overridden", value)

        assertEquals(value, encoded["overridden"])
    }

    @Test
    fun promptRowOverriddenRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptRow.serializer(), "overridden", JsonPrimitive(1))
    }

    @Test
    fun promptRowUpdatedAtMsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(PromptRow.serializer(), "updatedAtMs", value)

        assertEquals(value, encoded["updatedAtMs"])
    }

    @Test
    fun promptRowUpdatedAtMsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptRow.serializer(), "updatedAtMs", JsonPrimitive("not-a-number"))
    }

    @Test
    fun promptTunerReportRanPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(PromptTunerReport.serializer(), "ran", value)

        assertEquals(value, encoded["ran"])
    }

    @Test
    fun promptTunerReportRanRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptTunerReport.serializer(), "ran", JsonPrimitive(1))
    }

    @Test
    fun promptTunerReportChangedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(PromptTunerReport.serializer(), "changed", value)

        assertEquals(value, encoded["changed"])
    }

    @Test
    fun promptTunerReportChangedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptTunerReport.serializer(), "changed", JsonPrimitive(1))
    }

    @Test
    fun promptTunerReportReasonPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PromptTunerReport.serializer(), "reason", value)

        assertEquals(value, encoded["reason"])
    }

    @Test
    fun promptTunerReportReasonRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptTunerReport.serializer(), "reason", JsonObject(emptyMap()))
    }

    @Test
    fun promptTunerReportErrorPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PromptTunerReport.serializer(), "error", value)

        assertEquals(value, encoded["error"])
    }

    @Test
    fun promptTunerReportErrorRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptTunerReport.serializer(), "error", JsonObject(emptyMap()))
    }

    @Test
    fun promptTunerReportLeafSummariesPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(PromptTunerReport.serializer(), "leafSummaries", value)

        assertEquals(value, encoded["leafSummaries"])
    }

    @Test
    fun promptTunerReportLeafSummariesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptTunerReport.serializer(), "leafSummaries", JsonPrimitive("not-a-number"))
    }

    @Test
    fun promptTunerReportMinSummariesPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(PromptTunerReport.serializer(), "minSummaries", value)

        assertEquals(value, encoded["minSummaries"])
    }

    @Test
    fun promptTunerReportMinSummariesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptTunerReport.serializer(), "minSummaries", JsonPrimitive("not-a-number"))
    }

    @Test
    fun promptTunerReportProposedPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(PromptTunerReport.serializer(), "proposed", value)

        assertEquals(value, encoded["proposed"])
    }

    @Test
    fun promptTunerReportProposedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptTunerReport.serializer(), "proposed", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun promptTunerReportAddedPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(PromptTunerReport.serializer(), "added", value)

        assertEquals(value, encoded["added"])
    }

    @Test
    fun promptTunerReportAddedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptTunerReport.serializer(), "added", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun promptTunerReportBeforeCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(PromptTunerReport.serializer(), "beforeCount", value)

        assertEquals(value, encoded["beforeCount"])
    }

    @Test
    fun promptTunerReportBeforeCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptTunerReport.serializer(), "beforeCount", JsonPrimitive("not-a-number"))
    }

    @Test
    fun promptTunerReportAfterCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(PromptTunerReport.serializer(), "afterCount", value)

        assertEquals(value, encoded["afterCount"])
    }

    @Test
    fun promptTunerReportAfterCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptTunerReport.serializer(), "afterCount", JsonPrimitive("not-a-number"))
    }

    @Test
    fun promptTunerRunResponseTargetPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PromptTunerRunResponse.serializer(), "target", value)

        assertEquals(value, encoded["target"])
    }

    @Test
    fun promptTunerRunResponseTargetRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptTunerRunResponse.serializer(), "target", JsonObject(emptyMap()))
    }

    @Test
    fun promptTunerRunResponseReportPreservesItsBoundaryValue() {
        val value = JsonObject(emptyMap())
        val encoded = roundTrip(PromptTunerRunResponse.serializer(), "report", value)

        assertIs<JsonObject>(encoded["report"])
    }

    @Test
    fun promptTunerRunResponseReportRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PromptTunerRunResponse.serializer(), "report", JsonPrimitive("not-an-object"))
    }

    @Test
    fun propusLifecycleSummarySystemPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "system", value)

        assertEquals(value, encoded["system"])
    }

    @Test
    fun propusLifecycleSummarySystemRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "system", JsonObject(emptyMap()))
    }

    @Test
    fun propusLifecycleSummaryStatePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "state", value)

        assertEquals(value, encoded["state"])
    }

    @Test
    fun propusLifecycleSummaryStateRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "state", JsonObject(emptyMap()))
    }

    @Test
    fun propusLifecycleSummaryTotalPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "total", value)

        assertEquals(value, encoded["total"])
    }

    @Test
    fun propusLifecycleSummaryTotalRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "total", JsonPrimitive("not-a-number"))
    }

    @Test
    fun propusLifecycleSummaryGenesisPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "genesis", value)

        assertEquals(value, encoded["genesis"])
    }

    @Test
    fun propusLifecycleSummaryGenesisRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "genesis", JsonPrimitive("not-a-number"))
    }

    @Test
    fun propusLifecycleSummaryEvolvedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "evolved", value)

        assertEquals(value, encoded["evolved"])
    }

    @Test
    fun propusLifecycleSummaryEvolvedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "evolved", JsonPrimitive("not-a-number"))
    }

    @Test
    fun propusLifecycleSummaryReviewPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "review", value)

        assertEquals(value, encoded["review"])
    }

    @Test
    fun propusLifecycleSummaryReviewRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "review", JsonPrimitive("not-a-number"))
    }

    @Test
    fun propusLifecycleSummaryRejectedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "rejected", value)

        assertEquals(value, encoded["rejected"])
    }

    @Test
    fun propusLifecycleSummaryRejectedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "rejected", JsonPrimitive("not-a-number"))
    }

    @Test
    fun propusLifecycleSummaryRolledBackPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "rolledBack", value)

        assertEquals(value, encoded["rolledBack"])
    }

    @Test
    fun propusLifecycleSummaryRolledBackRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "rolledBack", JsonPrimitive("not-a-number"))
    }

    @Test
    fun propusLifecycleSummaryAttentionPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "attention", value)

        assertEquals(value, encoded["attention"])
    }

    @Test
    fun propusLifecycleSummaryAttentionRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "attention", JsonPrimitive("not-a-number"))
    }

    @Test
    fun propusLifecycleSummaryLatestAtPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "latestAt", value)

        assertEquals(value, encoded["latestAt"])
    }

    @Test
    fun propusLifecycleSummaryLatestAtRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "latestAt", JsonPrimitive("not-a-number"))
    }

    @Test
    fun propusLifecycleSummaryLatestTypePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "latestType", value)

        assertEquals(value, encoded["latestType"])
    }

    @Test
    fun propusLifecycleSummaryLatestTypeRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "latestType", JsonObject(emptyMap()))
    }

    @Test
    fun propusLifecycleSummaryLatestSkillPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "latestSkill", value)

        assertEquals(value, encoded["latestSkill"])
    }

    @Test
    fun propusLifecycleSummaryLatestSkillRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "latestSkill", JsonObject(emptyMap()))
    }

    @Test
    fun propusLifecycleSummaryDoctrineVersionPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "doctrineVersion", value)

        assertEquals(value, encoded["doctrineVersion"])
    }

    @Test
    fun propusLifecycleSummaryDoctrineVersionRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "doctrineVersion", JsonObject(emptyMap()))
    }

    @Test
    fun propusLifecycleSummaryDoctrinePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "doctrine", value)

        assertEquals(value, encoded["doctrine"])
    }

    @Test
    fun propusLifecycleSummaryDoctrineRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "doctrine", JsonObject(emptyMap()))
    }

    @Test
    fun propusLifecycleSummarySourcePapersPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "sourcePapers", value)

        assertEquals(value, encoded["sourcePapers"])
    }

    @Test
    fun propusLifecycleSummarySourcePapersRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "sourcePapers", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun propusLifecycleSummaryFilteredSourcesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "filteredSources", value)

        assertEquals(value, encoded["filteredSources"])
    }

    @Test
    fun propusLifecycleSummaryFilteredSourcesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "filteredSources", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun propusLifecycleSummaryPrinciplesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "principles", value)

        assertEquals(value, encoded["principles"])
    }

    @Test
    fun propusLifecycleSummaryPrinciplesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "principles", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun propusLifecycleSummaryQualityGatesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "qualityGates", value)

        assertEquals(value, encoded["qualityGates"])
    }

    @Test
    fun propusLifecycleSummaryQualityGatesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "qualityGates", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun propusLifecycleSummaryNextActionsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "nextActions", value)

        assertEquals(value, encoded["nextActions"])
    }

    @Test
    fun propusLifecycleSummaryNextActionsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "nextActions", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun propusLifecycleSummaryCoverageStatePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "coverageState", value)

        assertEquals(value, encoded["coverageState"])
    }

    @Test
    fun propusLifecycleSummaryCoverageStateRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "coverageState", JsonObject(emptyMap()))
    }

    @Test
    fun propusLifecycleSummaryCoverageGapsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "coverageGaps", value)

        assertEquals(value, encoded["coverageGaps"])
    }

    @Test
    fun propusLifecycleSummaryCoverageGapsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "coverageGaps", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun propusLifecycleSummaryNextCuePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "nextCue", value)

        assertEquals(value, encoded["nextCue"])
    }

    @Test
    fun propusLifecycleSummaryNextCueRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "nextCue", JsonObject(emptyMap()))
    }

    @Test
    fun propusLifecycleSummaryQualityGatePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "qualityGate", value)

        assertEquals(value, encoded["qualityGate"])
    }

    @Test
    fun propusLifecycleSummaryQualityGateRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "qualityGate", JsonObject(emptyMap()))
    }

    @Test
    fun propusLifecycleSummaryAttentionCuePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(PropusLifecycleSummary.serializer(), "attentionCue", value)

        assertEquals(value, encoded["attentionCue"])
    }

    @Test
    fun propusLifecycleSummaryAttentionCueRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PropusLifecycleSummary.serializer(), "attentionCue", JsonObject(emptyMap()))
    }

    @Test
    fun qATurnQPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(QATurn.serializer(), "q", value)

        assertEquals(value, encoded["q"])
    }

    @Test
    fun qATurnQRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(QATurn.serializer(), "q", JsonObject(emptyMap()))
    }

    @Test
    fun qATurnAPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(QATurn.serializer(), "a", value)

        assertEquals(value, encoded["a"])
    }

    @Test
    fun qATurnARejectsAnIncompatibleWireShape() {
        rejectsWrongShape(QATurn.serializer(), "a", JsonObject(emptyMap()))
    }

    @Test
    fun roleModelRolePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(RoleModel.serializer(), "role", value)

        assertEquals(value, encoded["role"])
    }

    @Test
    fun roleModelRoleRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(RoleModel.serializer(), "role", JsonObject(emptyMap()))
    }

    @Test
    fun roleModelModelPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(RoleModel.serializer(), "model", value)

        assertEquals(value, encoded["model"])
    }

    @Test
    fun roleModelModelRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(RoleModel.serializer(), "model", JsonObject(emptyMap()))
    }

    @Test
    fun searchAllResultWikiPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(SearchAllResult.serializer(), "wiki", value)
        val encodedValues = encoded.getValue("wiki").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun searchAllResultWikiRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SearchAllResult.serializer(), "wiki", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun searchAllResultDiaryPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(SearchAllResult.serializer(), "diary", value)
        val encodedValues = encoded.getValue("diary").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun searchAllResultDiaryRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SearchAllResult.serializer(), "diary", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun searchAllResultPeoplePreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(SearchAllResult.serializer(), "people", value)
        val encodedValues = encoded.getValue("people").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun searchAllResultPeopleRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SearchAllResult.serializer(), "people", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun searchDiaryHitFilePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SearchDiaryHit.serializer(), "file", value)

        assertEquals(value, encoded["file"])
    }

    @Test
    fun searchDiaryHitFileRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SearchDiaryHit.serializer(), "file", JsonObject(emptyMap()))
    }

    @Test
    fun searchDiaryHitHeaderPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SearchDiaryHit.serializer(), "header", value)

        assertEquals(value, encoded["header"])
    }

    @Test
    fun searchDiaryHitHeaderRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SearchDiaryHit.serializer(), "header", JsonObject(emptyMap()))
    }

    @Test
    fun searchDiaryHitContentPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SearchDiaryHit.serializer(), "content", value)

        assertEquals(value, encoded["content"])
    }

    @Test
    fun searchDiaryHitContentRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SearchDiaryHit.serializer(), "content", JsonObject(emptyMap()))
    }

    @Test
    fun searchDiaryHitAtPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(SearchDiaryHit.serializer(), "at", value)

        assertEquals(value, encoded["at"])
    }

    @Test
    fun searchDiaryHitAtRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SearchDiaryHit.serializer(), "at", JsonPrimitive("not-a-number"))
    }

    @Test
    fun searchDiaryHitScorePreservesItsBoundaryValue() {
        val value = JsonPrimitive(Double.MAX_VALUE)
        val encoded = roundTrip(SearchDiaryHit.serializer(), "score", value)

        assertEquals(value, encoded["score"])
    }

    @Test
    fun searchDiaryHitScoreRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SearchDiaryHit.serializer(), "score", JsonPrimitive("not-a-number"))
    }

    @Test
    fun searchWikiHitPathPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SearchWikiHit.serializer(), "path", value)

        assertEquals(value, encoded["path"])
    }

    @Test
    fun searchWikiHitPathRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SearchWikiHit.serializer(), "path", JsonObject(emptyMap()))
    }

    @Test
    fun searchWikiHitTitlePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SearchWikiHit.serializer(), "title", value)

        assertEquals(value, encoded["title"])
    }

    @Test
    fun searchWikiHitTitleRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SearchWikiHit.serializer(), "title", JsonObject(emptyMap()))
    }

    @Test
    fun searchWikiHitSummaryPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SearchWikiHit.serializer(), "summary", value)

        assertEquals(value, encoded["summary"])
    }

    @Test
    fun searchWikiHitSummaryRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SearchWikiHit.serializer(), "summary", JsonObject(emptyMap()))
    }

    @Test
    fun searchWikiHitCategoryPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SearchWikiHit.serializer(), "category", value)

        assertEquals(value, encoded["category"])
    }

    @Test
    fun searchWikiHitCategoryRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SearchWikiHit.serializer(), "category", JsonObject(emptyMap()))
    }

    @Test
    fun searchWikiHitSnippetPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SearchWikiHit.serializer(), "snippet", value)

        assertEquals(value, encoded["snippet"])
    }

    @Test
    fun searchWikiHitSnippetRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SearchWikiHit.serializer(), "snippet", JsonObject(emptyMap()))
    }

    @Test
    fun searchWikiHitScorePreservesItsBoundaryValue() {
        val value = JsonPrimitive(Double.MAX_VALUE)
        val encoded = roundTrip(SearchWikiHit.serializer(), "score", value)

        assertEquals(value, encoded["score"])
    }

    @Test
    fun searchWikiHitScoreRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SearchWikiHit.serializer(), "score", JsonPrimitive("not-a-number"))
    }

    @Test
    fun selfCorrectionCandidateIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SelfCorrectionCandidate.serializer(), "id", value)

        assertEquals(value, encoded["id"])
    }

    @Test
    fun selfCorrectionCandidateIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfCorrectionCandidate.serializer(), "id", JsonObject(emptyMap()))
    }

    @Test
    fun selfCorrectionCandidateStatusPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SelfCorrectionCandidate.serializer(), "status", value)

        assertEquals(value, encoded["status"])
    }

    @Test
    fun selfCorrectionCandidateStatusRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfCorrectionCandidate.serializer(), "status", JsonObject(emptyMap()))
    }

    @Test
    fun selfCorrectionCandidateScopePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SelfCorrectionCandidate.serializer(), "scope", value)

        assertEquals(value, encoded["scope"])
    }

    @Test
    fun selfCorrectionCandidateScopeRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfCorrectionCandidate.serializer(), "scope", JsonObject(emptyMap()))
    }

    @Test
    fun selfCorrectionCandidateSkillNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SelfCorrectionCandidate.serializer(), "skillName", value)

        assertEquals(value, encoded["skillName"])
    }

    @Test
    fun selfCorrectionCandidateSkillNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfCorrectionCandidate.serializer(), "skillName", JsonObject(emptyMap()))
    }

    @Test
    fun selfCorrectionCandidateSessionKeyPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SelfCorrectionCandidate.serializer(), "sessionKey", value)

        assertEquals(value, encoded["sessionKey"])
    }

    @Test
    fun selfCorrectionCandidateSessionKeyRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfCorrectionCandidate.serializer(), "sessionKey", JsonObject(emptyMap()))
    }

    @Test
    fun selfCorrectionCandidateTitlePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SelfCorrectionCandidate.serializer(), "title", value)

        assertEquals(value, encoded["title"])
    }

    @Test
    fun selfCorrectionCandidateTitleRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfCorrectionCandidate.serializer(), "title", JsonObject(emptyMap()))
    }

    @Test
    fun selfCorrectionCandidateCandidatePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SelfCorrectionCandidate.serializer(), "candidate", value)

        assertEquals(value, encoded["candidate"])
    }

    @Test
    fun selfCorrectionCandidateCandidateRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfCorrectionCandidate.serializer(), "candidate", JsonObject(emptyMap()))
    }

    @Test
    fun selfCorrectionCandidateEvidencePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SelfCorrectionCandidate.serializer(), "evidence", value)

        assertEquals(value, encoded["evidence"])
    }

    @Test
    fun selfCorrectionCandidateEvidenceRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfCorrectionCandidate.serializer(), "evidence", JsonObject(emptyMap()))
    }

    @Test
    fun selfCorrectionCandidateReasonPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SelfCorrectionCandidate.serializer(), "reason", value)

        assertEquals(value, encoded["reason"])
    }

    @Test
    fun selfCorrectionCandidateReasonRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfCorrectionCandidate.serializer(), "reason", JsonObject(emptyMap()))
    }

    @Test
    fun selfCorrectionCandidateTargetFilesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(SelfCorrectionCandidate.serializer(), "targetFiles", value)

        assertEquals(value, encoded["targetFiles"])
    }

    @Test
    fun selfCorrectionCandidateTargetFilesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfCorrectionCandidate.serializer(), "targetFiles", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun selfCorrectionCandidateProposedChangePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SelfCorrectionCandidate.serializer(), "proposedChange", value)

        assertEquals(value, encoded["proposedChange"])
    }

    @Test
    fun selfCorrectionCandidateProposedChangeRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfCorrectionCandidate.serializer(), "proposedChange", JsonObject(emptyMap()))
    }

    @Test
    fun selfCorrectionCandidateRiskPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SelfCorrectionCandidate.serializer(), "risk", value)

        assertEquals(value, encoded["risk"])
    }

    @Test
    fun selfCorrectionCandidateRiskRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfCorrectionCandidate.serializer(), "risk", JsonObject(emptyMap()))
    }

    @Test
    fun selfCorrectionCandidateSourcePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SelfCorrectionCandidate.serializer(), "source", value)

        assertEquals(value, encoded["source"])
    }

    @Test
    fun selfCorrectionCandidateSourceRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfCorrectionCandidate.serializer(), "source", JsonObject(emptyMap()))
    }

    @Test
    fun selfCorrectionCandidateReviewerPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SelfCorrectionCandidate.serializer(), "reviewer", value)

        assertEquals(value, encoded["reviewer"])
    }

    @Test
    fun selfCorrectionCandidateReviewerRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfCorrectionCandidate.serializer(), "reviewer", JsonObject(emptyMap()))
    }

    @Test
    fun selfCorrectionCandidateReviewNotePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SelfCorrectionCandidate.serializer(), "reviewNote", value)

        assertEquals(value, encoded["reviewNote"])
    }

    @Test
    fun selfCorrectionCandidateReviewNoteRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfCorrectionCandidate.serializer(), "reviewNote", JsonObject(emptyMap()))
    }

    @Test
    fun selfCorrectionCandidateEvidenceKindsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(SelfCorrectionCandidate.serializer(), "evidenceKinds", value)

        assertEquals(value, encoded["evidenceKinds"])
    }

    @Test
    fun selfCorrectionCandidateEvidenceKindsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfCorrectionCandidate.serializer(), "evidenceKinds", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun selfCorrectionCandidateReviewActionsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(SelfCorrectionCandidate.serializer(), "reviewActions", value)

        assertEquals(value, encoded["reviewActions"])
    }

    @Test
    fun selfCorrectionCandidateReviewActionsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfCorrectionCandidate.serializer(), "reviewActions", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun selfCorrectionCandidateCreatedAtPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(SelfCorrectionCandidate.serializer(), "createdAt", value)

        assertEquals(value, encoded["createdAt"])
    }

    @Test
    fun selfCorrectionCandidateCreatedAtRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfCorrectionCandidate.serializer(), "createdAt", JsonPrimitive("not-a-number"))
    }

    @Test
    fun selfCorrectionCandidateUpdatedAtPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(SelfCorrectionCandidate.serializer(), "updatedAt", value)

        assertEquals(value, encoded["updatedAt"])
    }

    @Test
    fun selfCorrectionCandidateUpdatedAtRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfCorrectionCandidate.serializer(), "updatedAt", JsonPrimitive("not-a-number"))
    }

    @Test
    fun selfImprovementCodingFunnelLastCaptureAtPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(SelfImprovementCodingFunnel.serializer(), "lastCaptureAt", value)

        assertEquals(value, encoded["lastCaptureAt"])
    }

    @Test
    fun selfImprovementCodingFunnelLastCaptureAtRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfImprovementCodingFunnel.serializer(), "lastCaptureAt", JsonPrimitive("not-a-number"))
    }

    @Test
    fun selfImprovementCodingFunnelLastReviewAtPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(SelfImprovementCodingFunnel.serializer(), "lastReviewAt", value)

        assertEquals(value, encoded["lastReviewAt"])
    }

    @Test
    fun selfImprovementCodingFunnelLastReviewAtRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfImprovementCodingFunnel.serializer(), "lastReviewAt", JsonPrimitive("not-a-number"))
    }

    @Test
    fun selfImprovementCodingFunnelRejections7dPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(SelfImprovementCodingFunnel.serializer(), "rejections7d", value)

        assertEquals(value, encoded["rejections7d"])
    }

    @Test
    fun selfImprovementCodingFunnelRejections7dRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfImprovementCodingFunnel.serializer(), "rejections7d", JsonPrimitive("not-a-number"))
    }

    @Test
    fun selfImprovementCodingFunnelPromotableRejections7dPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(SelfImprovementCodingFunnel.serializer(), "promotableRejections7d", value)

        assertEquals(value, encoded["promotableRejections7d"])
    }

    @Test
    fun selfImprovementCodingFunnelPromotableRejections7dRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfImprovementCodingFunnel.serializer(), "promotableRejections7d", JsonPrimitive("not-a-number"))
    }

    @Test
    fun selfImprovementCodingFunnelLastRejectionAtPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(SelfImprovementCodingFunnel.serializer(), "lastRejectionAt", value)

        assertEquals(value, encoded["lastRejectionAt"])
    }

    @Test
    fun selfImprovementCodingFunnelLastRejectionAtRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfImprovementCodingFunnel.serializer(), "lastRejectionAt", JsonPrimitive("not-a-number"))
    }

    @Test
    fun selfImprovementCodingFunnelLastNudgeAtPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(SelfImprovementCodingFunnel.serializer(), "lastNudgeAt", value)

        assertEquals(value, encoded["lastNudgeAt"])
    }

    @Test
    fun selfImprovementCodingFunnelLastNudgeAtRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfImprovementCodingFunnel.serializer(), "lastNudgeAt", JsonPrimitive("not-a-number"))
    }

    @Test
    fun selfImprovementCodingListResponseCandidatesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(SelfImprovementCodingListResponse.serializer(), "candidates", value)
        val encodedValues = encoded.getValue("candidates").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun selfImprovementCodingListResponseCandidatesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfImprovementCodingListResponse.serializer(), "candidates", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun selfImprovementCodingListResponseCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(SelfImprovementCodingListResponse.serializer(), "count", value)

        assertEquals(value, encoded["count"])
    }

    @Test
    fun selfImprovementCodingListResponseCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfImprovementCodingListResponse.serializer(), "count", JsonPrimitive("not-a-number"))
    }

    @Test
    fun selfImprovementCodingListResponseStatusCountsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(SelfImprovementCodingListResponse.serializer(), "statusCounts", value)
        val encodedValues = encoded.getValue("statusCounts").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun selfImprovementCodingListResponseStatusCountsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfImprovementCodingListResponse.serializer(), "statusCounts", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun selfImprovementCodingListResponseFunnelPreservesItsBoundaryValue() {
        val value = JsonObject(emptyMap())
        val encoded = roundTrip(SelfImprovementCodingListResponse.serializer(), "funnel", value)

        assertIs<JsonObject>(encoded["funnel"])
    }

    @Test
    fun selfImprovementCodingListResponseFunnelRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfImprovementCodingListResponse.serializer(), "funnel", JsonPrimitive("not-an-object"))
    }

    @Test
    fun selfImprovementCodingStatusCountStatusPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SelfImprovementCodingStatusCount.serializer(), "status", value)

        assertEquals(value, encoded["status"])
    }

    @Test
    fun selfImprovementCodingStatusCountStatusRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfImprovementCodingStatusCount.serializer(), "status", JsonObject(emptyMap()))
    }

    @Test
    fun selfImprovementCodingStatusCountCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(SelfImprovementCodingStatusCount.serializer(), "count", value)

        assertEquals(value, encoded["count"])
    }

    @Test
    fun selfImprovementCodingStatusCountCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SelfImprovementCodingStatusCount.serializer(), "count", JsonPrimitive("not-a-number"))
    }

    @Test
    fun senderRecentOutCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(SenderRecentOut.serializer(), "count", value)

        assertEquals(value, encoded["count"])
    }

    @Test
    fun senderRecentOutCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SenderRecentOut.serializer(), "count", JsonPrimitive("not-a-number"))
    }

    @Test
    fun senderRecentOutLastReceivedAtPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SenderRecentOut.serializer(), "lastReceivedAt", value)

        assertEquals(value, encoded["lastReceivedAt"])
    }

    @Test
    fun senderRecentOutLastReceivedAtRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SenderRecentOut.serializer(), "lastReceivedAt", JsonObject(emptyMap()))
    }

    @Test
    fun senderRecentOutWindowDaysPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(SenderRecentOut.serializer(), "windowDays", value)

        assertEquals(value, encoded["windowDays"])
    }

    @Test
    fun senderRecentOutWindowDaysRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SenderRecentOut.serializer(), "windowDays", JsonPrimitive("not-a-number"))
    }

    @Test
    fun senderRecentOutTruncatedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(SenderRecentOut.serializer(), "truncated", value)

        assertEquals(value, encoded["truncated"])
    }

    @Test
    fun senderRecentOutTruncatedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SenderRecentOut.serializer(), "truncated", JsonPrimitive(1))
    }

    @Test
    fun senderWikiHitOutPathPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SenderWikiHitOut.serializer(), "path", value)

        assertEquals(value, encoded["path"])
    }

    @Test
    fun senderWikiHitOutPathRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SenderWikiHitOut.serializer(), "path", JsonObject(emptyMap()))
    }

    @Test
    fun senderWikiHitOutTitlePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SenderWikiHitOut.serializer(), "title", value)

        assertEquals(value, encoded["title"])
    }

    @Test
    fun senderWikiHitOutTitleRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SenderWikiHitOut.serializer(), "title", JsonObject(emptyMap()))
    }

    @Test
    fun senderWikiHitOutSummaryPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SenderWikiHitOut.serializer(), "summary", value)

        assertEquals(value, encoded["summary"])
    }

    @Test
    fun senderWikiHitOutSummaryRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SenderWikiHitOut.serializer(), "summary", JsonObject(emptyMap()))
    }

    @Test
    fun senderWikiHitOutCategoryPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SenderWikiHitOut.serializer(), "category", value)

        assertEquals(value, encoded["category"])
    }

    @Test
    fun senderWikiHitOutCategoryRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SenderWikiHitOut.serializer(), "category", JsonObject(emptyMap()))
    }

    @Test
    fun sessionRowOutKeyPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SessionRowOut.serializer(), "key", value)

        assertEquals(value, encoded["key"])
    }

    @Test
    fun sessionRowOutKeyRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SessionRowOut.serializer(), "key", JsonObject(emptyMap()))
    }

    @Test
    fun sessionRowOutKindPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SessionRowOut.serializer(), "kind", value)

        assertEquals(value, encoded["kind"])
    }

    @Test
    fun sessionRowOutKindRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SessionRowOut.serializer(), "kind", JsonObject(emptyMap()))
    }

    @Test
    fun sessionRowOutStatusPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SessionRowOut.serializer(), "status", value)

        assertEquals(value, encoded["status"])
    }

    @Test
    fun sessionRowOutStatusRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SessionRowOut.serializer(), "status", JsonObject(emptyMap()))
    }

    @Test
    fun sessionRowOutChannelPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SessionRowOut.serializer(), "channel", value)

        assertEquals(value, encoded["channel"])
    }

    @Test
    fun sessionRowOutChannelRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SessionRowOut.serializer(), "channel", JsonObject(emptyMap()))
    }

    @Test
    fun sessionRowOutModelPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SessionRowOut.serializer(), "model", value)

        assertEquals(value, encoded["model"])
    }

    @Test
    fun sessionRowOutModelRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SessionRowOut.serializer(), "model", JsonObject(emptyMap()))
    }

    @Test
    fun sessionRowOutLabelPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SessionRowOut.serializer(), "label", value)

        assertEquals(value, encoded["label"])
    }

    @Test
    fun sessionRowOutLabelRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SessionRowOut.serializer(), "label", JsonObject(emptyMap()))
    }

    @Test
    fun sessionRowOutUpdatedAtMsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(SessionRowOut.serializer(), "updatedAtMs", value)

        assertEquals(value, encoded["updatedAtMs"])
    }

    @Test
    fun sessionRowOutUpdatedAtMsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SessionRowOut.serializer(), "updatedAtMs", JsonPrimitive("not-a-number"))
    }

    @Test
    fun sessionRowOutStartedAtMsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(SessionRowOut.serializer(), "startedAtMs", value)

        assertEquals(value, encoded["startedAtMs"])
    }

    @Test
    fun sessionRowOutStartedAtMsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SessionRowOut.serializer(), "startedAtMs", JsonPrimitive("not-a-number"))
    }

    @Test
    fun sessionRowOutRuntimeMsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(SessionRowOut.serializer(), "runtimeMs", value)

        assertEquals(value, encoded["runtimeMs"])
    }

    @Test
    fun sessionRowOutRuntimeMsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SessionRowOut.serializer(), "runtimeMs", JsonPrimitive("not-a-number"))
    }

    @Test
    fun sessionRowOutTotalTokensPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(SessionRowOut.serializer(), "totalTokens", value)

        assertEquals(value, encoded["totalTokens"])
    }

    @Test
    fun sessionRowOutTotalTokensRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SessionRowOut.serializer(), "totalTokens", JsonPrimitive("not-a-number"))
    }

    @Test
    fun skillDetailResponseSkillPreservesItsBoundaryValue() {
        val value = JsonObject(emptyMap())
        val encoded = roundTrip(SkillDetailResponse.serializer(), "skill", value)

        assertIs<JsonObject>(encoded["skill"])
    }

    @Test
    fun skillDetailResponseSkillRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillDetailResponse.serializer(), "skill", JsonPrimitive("not-an-object"))
    }

    @Test
    fun skillDetailResponseBodyPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SkillDetailResponse.serializer(), "body", value)

        assertEquals(value, encoded["body"])
    }

    @Test
    fun skillDetailResponseBodyRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillDetailResponse.serializer(), "body", JsonObject(emptyMap()))
    }

    @Test
    fun skillDetailResponseBodyTruncatedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(SkillDetailResponse.serializer(), "bodyTruncated", value)

        assertEquals(value, encoded["bodyTruncated"])
    }

    @Test
    fun skillDetailResponseBodyTruncatedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillDetailResponse.serializer(), "bodyTruncated", JsonPrimitive(1))
    }

    @Test
    fun skillDetailResponsePathPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SkillDetailResponse.serializer(), "path", value)

        assertEquals(value, encoded["path"])
    }

    @Test
    fun skillDetailResponsePathRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillDetailResponse.serializer(), "path", JsonObject(emptyMap()))
    }

    @Test
    fun skillLifecycleEventTypePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SkillLifecycleEvent.serializer(), "type", value)

        assertEquals(value, encoded["type"])
    }

    @Test
    fun skillLifecycleEventTypeRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillLifecycleEvent.serializer(), "type", JsonObject(emptyMap()))
    }

    @Test
    fun skillLifecycleEventSkillNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SkillLifecycleEvent.serializer(), "skillName", value)

        assertEquals(value, encoded["skillName"])
    }

    @Test
    fun skillLifecycleEventSkillNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillLifecycleEvent.serializer(), "skillName", JsonObject(emptyMap()))
    }

    @Test
    fun skillLifecycleEventAtPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(SkillLifecycleEvent.serializer(), "at", value)

        assertEquals(value, encoded["at"])
    }

    @Test
    fun skillLifecycleEventAtRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillLifecycleEvent.serializer(), "at", JsonPrimitive("not-a-number"))
    }

    @Test
    fun skillLifecycleEventVersionPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SkillLifecycleEvent.serializer(), "version", value)

        assertEquals(value, encoded["version"])
    }

    @Test
    fun skillLifecycleEventVersionRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillLifecycleEvent.serializer(), "version", JsonObject(emptyMap()))
    }

    @Test
    fun skillLifecycleEventDetailPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SkillLifecycleEvent.serializer(), "detail", value)

        assertEquals(value, encoded["detail"])
    }

    @Test
    fun skillLifecycleEventDetailRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillLifecycleEvent.serializer(), "detail", JsonObject(emptyMap()))
    }

    @Test
    fun skillLifecycleEventRoutePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SkillLifecycleEvent.serializer(), "route", value)

        assertEquals(value, encoded["route"])
    }

    @Test
    fun skillLifecycleEventRouteRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillLifecycleEvent.serializer(), "route", JsonObject(emptyMap()))
    }

    @Test
    fun skillLifecycleEventEvidencePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SkillLifecycleEvent.serializer(), "evidence", value)

        assertEquals(value, encoded["evidence"])
    }

    @Test
    fun skillLifecycleEventEvidenceRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillLifecycleEvent.serializer(), "evidence", JsonObject(emptyMap()))
    }

    @Test
    fun skillLifecycleEventTargetSignaturePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SkillLifecycleEvent.serializer(), "targetSignature", value)

        assertEquals(value, encoded["targetSignature"])
    }

    @Test
    fun skillLifecycleEventTargetSignatureRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillLifecycleEvent.serializer(), "targetSignature", JsonObject(emptyMap()))
    }

    @Test
    fun skillLifecycleEventEditedSurfacePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SkillLifecycleEvent.serializer(), "editedSurface", value)

        assertEquals(value, encoded["editedSurface"])
    }

    @Test
    fun skillLifecycleEventEditedSurfaceRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillLifecycleEvent.serializer(), "editedSurface", JsonObject(emptyMap()))
    }

    @Test
    fun skillLifecycleEventExpectedBehaviorChangePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SkillLifecycleEvent.serializer(), "expectedBehaviorChange", value)

        assertEquals(value, encoded["expectedBehaviorChange"])
    }

    @Test
    fun skillLifecycleEventExpectedBehaviorChangeRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillLifecycleEvent.serializer(), "expectedBehaviorChange", JsonObject(emptyMap()))
    }

    @Test
    fun skillLifecycleEventRegressionRiskPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SkillLifecycleEvent.serializer(), "regressionRisk", value)

        assertEquals(value, encoded["regressionRisk"])
    }

    @Test
    fun skillLifecycleEventRegressionRiskRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillLifecycleEvent.serializer(), "regressionRisk", JsonObject(emptyMap()))
    }

    @Test
    fun skillRowNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SkillRow.serializer(), "name", value)

        assertEquals(value, encoded["name"])
    }

    @Test
    fun skillRowNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillRow.serializer(), "name", JsonObject(emptyMap()))
    }

    @Test
    fun skillRowDescriptionPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SkillRow.serializer(), "description", value)

        assertEquals(value, encoded["description"])
    }

    @Test
    fun skillRowDescriptionRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillRow.serializer(), "description", JsonObject(emptyMap()))
    }

    @Test
    fun skillRowCategoryPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SkillRow.serializer(), "category", value)

        assertEquals(value, encoded["category"])
    }

    @Test
    fun skillRowCategoryRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillRow.serializer(), "category", JsonObject(emptyMap()))
    }

    @Test
    fun skillRowHomepagePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SkillRow.serializer(), "homepage", value)

        assertEquals(value, encoded["homepage"])
    }

    @Test
    fun skillRowHomepageRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillRow.serializer(), "homepage", JsonObject(emptyMap()))
    }

    @Test
    fun skillRowTagsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(SkillRow.serializer(), "tags", value)

        assertEquals(value, encoded["tags"])
    }

    @Test
    fun skillRowTagsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillRow.serializer(), "tags", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun skillRowRelatedSkillsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(SkillRow.serializer(), "relatedSkills", value)

        assertEquals(value, encoded["relatedSkills"])
    }

    @Test
    fun skillRowRelatedSkillsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillRow.serializer(), "relatedSkills", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun skillRowSourcePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SkillRow.serializer(), "source", value)

        assertEquals(value, encoded["source"])
    }

    @Test
    fun skillRowSourceRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillRow.serializer(), "source", JsonObject(emptyMap()))
    }

    @Test
    fun skillRowVersionPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SkillRow.serializer(), "version", value)

        assertEquals(value, encoded["version"])
    }

    @Test
    fun skillRowVersionRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillRow.serializer(), "version", JsonObject(emptyMap()))
    }

    @Test
    fun skillRowOriginPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SkillRow.serializer(), "origin", value)

        assertEquals(value, encoded["origin"])
    }

    @Test
    fun skillRowOriginRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillRow.serializer(), "origin", JsonObject(emptyMap()))
    }

    @Test
    fun skillRowCreatedAtPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(SkillRow.serializer(), "createdAt", value)

        assertEquals(value, encoded["createdAt"])
    }

    @Test
    fun skillRowCreatedAtRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillRow.serializer(), "createdAt", JsonPrimitive("not-a-number"))
    }

    @Test
    fun skillRowEvolveCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(SkillRow.serializer(), "evolveCount", value)

        assertEquals(value, encoded["evolveCount"])
    }

    @Test
    fun skillRowEvolveCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillRow.serializer(), "evolveCount", JsonPrimitive("not-a-number"))
    }

    @Test
    fun skillRowLastEvolvedAtPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(SkillRow.serializer(), "lastEvolvedAt", value)

        assertEquals(value, encoded["lastEvolvedAt"])
    }

    @Test
    fun skillRowLastEvolvedAtRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillRow.serializer(), "lastEvolvedAt", JsonPrimitive("not-a-number"))
    }

    @Test
    fun skillRowTotalUsesPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(SkillRow.serializer(), "totalUses", value)

        assertEquals(value, encoded["totalUses"])
    }

    @Test
    fun skillRowTotalUsesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillRow.serializer(), "totalUses", JsonPrimitive("not-a-number"))
    }

    @Test
    fun skillRowLastUsedAtPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(SkillRow.serializer(), "lastUsedAt", value)

        assertEquals(value, encoded["lastUsedAt"])
    }

    @Test
    fun skillRowLastUsedAtRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillRow.serializer(), "lastUsedAt", JsonPrimitive("not-a-number"))
    }

    @Test
    fun skillRowCuratorStatePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SkillRow.serializer(), "curatorState", value)

        assertEquals(value, encoded["curatorState"])
    }

    @Test
    fun skillRowCuratorStateRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillRow.serializer(), "curatorState", JsonObject(emptyMap()))
    }

    @Test
    fun skillRowEditablePreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(SkillRow.serializer(), "editable", value)

        assertEquals(value, encoded["editable"])
    }

    @Test
    fun skillRowEditableRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillRow.serializer(), "editable", JsonPrimitive(1))
    }

    @Test
    fun skillRowDeletablePreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(SkillRow.serializer(), "deletable", value)

        assertEquals(value, encoded["deletable"])
    }

    @Test
    fun skillRowDeletableRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillRow.serializer(), "deletable", JsonPrimitive(1))
    }

    @Test
    fun skillRowDependencySummaryPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(SkillRow.serializer(), "dependencySummary", value)

        assertEquals(value, encoded["dependencySummary"])
    }

    @Test
    fun skillRowDependencySummaryRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillRow.serializer(), "dependencySummary", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun skillRowInstallSummaryPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(SkillRow.serializer(), "installSummary", value)

        assertEquals(value, encoded["installSummary"])
    }

    @Test
    fun skillRowInstallSummaryRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillRow.serializer(), "installSummary", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun skillsLifecycleResponseEventsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(SkillsLifecycleResponse.serializer(), "events", value)
        val encodedValues = encoded.getValue("events").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun skillsLifecycleResponseEventsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillsLifecycleResponse.serializer(), "events", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun skillsLifecycleResponseCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(SkillsLifecycleResponse.serializer(), "count", value)

        assertEquals(value, encoded["count"])
    }

    @Test
    fun skillsLifecycleResponseCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillsLifecycleResponse.serializer(), "count", JsonPrimitive("not-a-number"))
    }

    @Test
    fun skillsLifecycleResponseSummaryPreservesItsBoundaryValue() {
        val value = JsonObject(emptyMap())
        val encoded = roundTrip(SkillsLifecycleResponse.serializer(), "summary", value)

        assertIs<JsonObject>(encoded["summary"])
    }

    @Test
    fun skillsLifecycleResponseSummaryRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillsLifecycleResponse.serializer(), "summary", JsonPrimitive("not-an-object"))
    }

    @Test
    fun skillsListResponseSkillsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(SkillsListResponse.serializer(), "skills", value)
        val encodedValues = encoded.getValue("skills").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun skillsListResponseSkillsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillsListResponse.serializer(), "skills", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun skillsListResponseCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(SkillsListResponse.serializer(), "count", value)

        assertEquals(value, encoded["count"])
    }

    @Test
    fun skillsListResponseCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SkillsListResponse.serializer(), "count", JsonPrimitive("not-a-number"))
    }

    @Test
    fun todoOutIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(TodoOut.serializer(), "id", value)

        assertEquals(value, encoded["id"])
    }

    @Test
    fun todoOutIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TodoOut.serializer(), "id", JsonObject(emptyMap()))
    }

    @Test
    fun todoOutTitlePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(TodoOut.serializer(), "title", value)

        assertEquals(value, encoded["title"])
    }

    @Test
    fun todoOutTitleRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TodoOut.serializer(), "title", JsonObject(emptyMap()))
    }

    @Test
    fun todoOutNotePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(TodoOut.serializer(), "note", value)

        assertEquals(value, encoded["note"])
    }

    @Test
    fun todoOutNoteRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TodoOut.serializer(), "note", JsonObject(emptyMap()))
    }

    @Test
    fun todoOutDuePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(TodoOut.serializer(), "due", value)

        assertEquals(value, encoded["due"])
    }

    @Test
    fun todoOutDueRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TodoOut.serializer(), "due", JsonObject(emptyMap()))
    }

    @Test
    fun todoOutDueAllDayPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(TodoOut.serializer(), "dueAllDay", value)

        assertEquals(value, encoded["dueAllDay"])
    }

    @Test
    fun todoOutDueAllDayRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TodoOut.serializer(), "dueAllDay", JsonPrimitive(1))
    }

    @Test
    fun todoOutDonePreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(TodoOut.serializer(), "done", value)

        assertEquals(value, encoded["done"])
    }

    @Test
    fun todoOutDoneRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TodoOut.serializer(), "done", JsonPrimitive(1))
    }

    @Test
    fun todoOutDoneAtPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(TodoOut.serializer(), "doneAt", value)

        assertEquals(value, encoded["doneAt"])
    }

    @Test
    fun todoOutDoneAtRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TodoOut.serializer(), "doneAt", JsonObject(emptyMap()))
    }

    @Test
    fun topicDocOutKeyPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(TopicDocOut.serializer(), "key", value)

        assertEquals(value, encoded["key"])
    }

    @Test
    fun topicDocOutKeyRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TopicDocOut.serializer(), "key", JsonObject(emptyMap()))
    }

    @Test
    fun topicDocOutNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(TopicDocOut.serializer(), "name", value)

        assertEquals(value, encoded["name"])
    }

    @Test
    fun topicDocOutNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TopicDocOut.serializer(), "name", JsonObject(emptyMap()))
    }

    @Test
    fun topicDocOutContentPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(TopicDocOut.serializer(), "content", value)

        assertEquals(value, encoded["content"])
    }

    @Test
    fun topicDocOutContentRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TopicDocOut.serializer(), "content", JsonObject(emptyMap()))
    }

    @Test
    fun topicDocOutSizePreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(TopicDocOut.serializer(), "size", value)

        assertEquals(value, encoded["size"])
    }

    @Test
    fun topicDocOutSizeRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TopicDocOut.serializer(), "size", JsonPrimitive("not-a-number"))
    }

    @Test
    fun topicDocOutModifiedPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(TopicDocOut.serializer(), "modified", value)

        assertEquals(value, encoded["modified"])
    }

    @Test
    fun topicDocOutModifiedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TopicDocOut.serializer(), "modified", JsonObject(emptyMap()))
    }

    @Test
    fun topicDocWriteOutKeyPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(TopicDocWriteOut.serializer(), "key", value)

        assertEquals(value, encoded["key"])
    }

    @Test
    fun topicDocWriteOutKeyRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TopicDocWriteOut.serializer(), "key", JsonObject(emptyMap()))
    }

    @Test
    fun topicDocWriteOutNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(TopicDocWriteOut.serializer(), "name", value)

        assertEquals(value, encoded["name"])
    }

    @Test
    fun topicDocWriteOutNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TopicDocWriteOut.serializer(), "name", JsonObject(emptyMap()))
    }

    @Test
    fun topicDocWriteOutSizePreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(TopicDocWriteOut.serializer(), "size", value)

        assertEquals(value, encoded["size"])
    }

    @Test
    fun topicDocWriteOutSizeRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TopicDocWriteOut.serializer(), "size", JsonPrimitive("not-a-number"))
    }

    @Test
    fun topicDocWriteOutModifiedPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(TopicDocWriteOut.serializer(), "modified", value)

        assertEquals(value, encoded["modified"])
    }

    @Test
    fun topicDocWriteOutModifiedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TopicDocWriteOut.serializer(), "modified", JsonObject(emptyMap()))
    }

    @Test
    fun topicDocWriteOutAppliedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(TopicDocWriteOut.serializer(), "applied", value)

        assertEquals(value, encoded["applied"])
    }

    @Test
    fun topicDocWriteOutAppliedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TopicDocWriteOut.serializer(), "applied", JsonPrimitive(1))
    }

    @Test
    fun transcriptAttachmentOutTypePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(TranscriptAttachmentOut.serializer(), "type", value)

        assertEquals(value, encoded["type"])
    }

    @Test
    fun transcriptAttachmentOutTypeRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TranscriptAttachmentOut.serializer(), "type", JsonObject(emptyMap()))
    }

    @Test
    fun transcriptAttachmentOutMimeTypePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(TranscriptAttachmentOut.serializer(), "mimeType", value)

        assertEquals(value, encoded["mimeType"])
    }

    @Test
    fun transcriptAttachmentOutMimeTypeRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TranscriptAttachmentOut.serializer(), "mimeType", JsonObject(emptyMap()))
    }

    @Test
    fun transcriptAttachmentOutUrlPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(TranscriptAttachmentOut.serializer(), "url", value)

        assertEquals(value, encoded["url"])
    }

    @Test
    fun transcriptAttachmentOutUrlRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TranscriptAttachmentOut.serializer(), "url", JsonObject(emptyMap()))
    }

    @Test
    fun transcriptAttachmentOutDataPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(TranscriptAttachmentOut.serializer(), "data", value)

        assertEquals(value, encoded["data"])
    }

    @Test
    fun transcriptAttachmentOutDataRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TranscriptAttachmentOut.serializer(), "data", JsonObject(emptyMap()))
    }

    @Test
    fun transcriptAttachmentOutNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(TranscriptAttachmentOut.serializer(), "name", value)

        assertEquals(value, encoded["name"])
    }

    @Test
    fun transcriptAttachmentOutNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TranscriptAttachmentOut.serializer(), "name", JsonObject(emptyMap()))
    }

    @Test
    fun transcriptAttachmentOutSizePreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(TranscriptAttachmentOut.serializer(), "size", value)

        assertEquals(value, encoded["size"])
    }

    @Test
    fun transcriptAttachmentOutSizeRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TranscriptAttachmentOut.serializer(), "size", JsonPrimitive("not-a-number"))
    }

    @Test
    fun transcriptMsgOutIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(TranscriptMsgOut.serializer(), "id", value)

        assertEquals(value, encoded["id"])
    }

    @Test
    fun transcriptMsgOutIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TranscriptMsgOut.serializer(), "id", JsonObject(emptyMap()))
    }

    @Test
    fun transcriptMsgOutRolePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(TranscriptMsgOut.serializer(), "role", value)

        assertEquals(value, encoded["role"])
    }

    @Test
    fun transcriptMsgOutRoleRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TranscriptMsgOut.serializer(), "role", JsonObject(emptyMap()))
    }

    @Test
    fun transcriptMsgOutContentPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(TranscriptMsgOut.serializer(), "content", value)

        assertEquals(value, encoded["content"])
    }

    @Test
    fun transcriptMsgOutContentRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TranscriptMsgOut.serializer(), "content", JsonObject(emptyMap()))
    }

    @Test
    fun transcriptMsgOutAttachmentsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(TranscriptMsgOut.serializer(), "attachments", value)
        val encodedValues = encoded.getValue("attachments").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun transcriptMsgOutAttachmentsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TranscriptMsgOut.serializer(), "attachments", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun transcriptMsgOutTimestampMsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(TranscriptMsgOut.serializer(), "timestampMs", value)

        assertEquals(value, encoded["timestampMs"])
    }

    @Test
    fun transcriptMsgOutTimestampMsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TranscriptMsgOut.serializer(), "timestampMs", JsonPrimitive("not-a-number"))
    }

    @Test
    fun wormholeModelOutNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(WormholeModelOut.serializer(), "name", value)

        assertEquals(value, encoded["name"])
    }

    @Test
    fun wormholeModelOutNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WormholeModelOut.serializer(), "name", JsonObject(emptyMap()))
    }

    @Test
    fun wormholeModelOutProtocolPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(WormholeModelOut.serializer(), "protocol", value)

        assertEquals(value, encoded["protocol"])
    }

    @Test
    fun wormholeModelOutProtocolRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WormholeModelOut.serializer(), "protocol", JsonObject(emptyMap()))
    }

    @Test
    fun wormholeModelOutLocalPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(WormholeModelOut.serializer(), "local", value)

        assertEquals(value, encoded["local"])
    }

    @Test
    fun wormholeModelOutLocalRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WormholeModelOut.serializer(), "local", JsonPrimitive(1))
    }

    @Test
    fun wormholeModelOutThinkingPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(WormholeModelOut.serializer(), "thinking", value)

        assertEquals(value, encoded["thinking"])
    }

    @Test
    fun wormholeModelOutThinkingRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WormholeModelOut.serializer(), "thinking", JsonPrimitive(1))
    }

    @Test
    fun wormholeModelOutSourcePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(WormholeModelOut.serializer(), "source", value)

        assertEquals(value, encoded["source"])
    }

    @Test
    fun wormholeModelOutSourceRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WormholeModelOut.serializer(), "source", JsonObject(emptyMap()))
    }

    @Test
    fun wormholeModelOutKeyHealthPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(WormholeModelOut.serializer(), "keyHealth", value)

        assertEquals(value, encoded["keyHealth"])
    }

    @Test
    fun wormholeModelOutKeyHealthRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WormholeModelOut.serializer(), "keyHealth", JsonObject(emptyMap()))
    }

    @Test
    fun wormholeStatusOutReachablePreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(WormholeStatusOut.serializer(), "reachable", value)

        assertEquals(value, encoded["reachable"])
    }

    @Test
    fun wormholeStatusOutReachableRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WormholeStatusOut.serializer(), "reachable", JsonPrimitive(1))
    }

    @Test
    fun wormholeStatusOutListenPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(WormholeStatusOut.serializer(), "listen", value)

        assertEquals(value, encoded["listen"])
    }

    @Test
    fun wormholeStatusOutListenRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WormholeStatusOut.serializer(), "listen", JsonObject(emptyMap()))
    }

    @Test
    fun wormholeStatusOutLocalOnlyPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(WormholeStatusOut.serializer(), "localOnly", value)

        assertEquals(value, encoded["localOnly"])
    }

    @Test
    fun wormholeStatusOutLocalOnlyRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WormholeStatusOut.serializer(), "localOnly", JsonPrimitive(1))
    }

    @Test
    fun wormholeStatusOutEffortRoutingPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(WormholeStatusOut.serializer(), "effortRouting", value)

        assertEquals(value, encoded["effortRouting"])
    }

    @Test
    fun wormholeStatusOutEffortRoutingRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WormholeStatusOut.serializer(), "effortRouting", JsonPrimitive(1))
    }

    @Test
    fun wormholeStatusOutAutoPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(WormholeStatusOut.serializer(), "auto", value)

        assertEquals(value, encoded["auto"])
    }

    @Test
    fun wormholeStatusOutAutoRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WormholeStatusOut.serializer(), "auto", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun wormholeStatusOutModelsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(WormholeStatusOut.serializer(), "models", value)
        val encodedValues = encoded.getValue("models").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun wormholeStatusOutModelsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WormholeStatusOut.serializer(), "models", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }
}
