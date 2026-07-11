package ai.deneb.deneb

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
 * Field-isolated boundary contracts for the hand-written gateway envelopes.
 *
 * These wrappers sit between generated DTOs and state reducers. Every field
 * independently proves wide-value preservation and strict wrong-shape rejection,
 * preventing a new envelope property from being silently lost or coerced.
 */
class DenebWireFieldBoundaryContractTest {
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
    fun recentPayloadSessionsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(RecentPayload.serializer(), "sessions", value)
        val encodedValues = encoded.getValue("sessions").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun recentPayloadSessionsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(RecentPayload.serializer(), "sessions", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun transcriptPayloadMessagesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(TranscriptPayload.serializer(), "messages", value)
        val encodedValues = encoded.getValue("messages").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun transcriptPayloadMessagesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TranscriptPayload.serializer(), "messages", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun workFeedPayloadItemsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(WorkFeedPayload.serializer(), "items", value)
        val encodedValues = encoded.getValue("items").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun workFeedPayloadItemsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WorkFeedPayload.serializer(), "items", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun workFeedActionRunPayloadOkPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(WorkFeedActionRunPayload.serializer(), "ok", value)

        assertEquals(value, encoded["ok"])
    }

    @Test
    fun workFeedActionRunPayloadOkRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WorkFeedActionRunPayload.serializer(), "ok", JsonPrimitive(1))
    }

    @Test
    fun workFeedActionRunPayloadItemPreservesItsBoundaryValue() {
        val value = JsonObject(emptyMap())
        val encoded = roundTrip(WorkFeedActionRunPayload.serializer(), "item", value)

        assertIs<JsonObject>(encoded["item"])
    }

    @Test
    fun workFeedActionRunPayloadItemRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WorkFeedActionRunPayload.serializer(), "item", JsonPrimitive("not-an-object"))
    }

    @Test
    fun workFeedActionRunPayloadSessionKeyPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(WorkFeedActionRunPayload.serializer(), "sessionKey", value)

        assertEquals(value, encoded["sessionKey"])
    }

    @Test
    fun workFeedActionRunPayloadSessionKeyRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WorkFeedActionRunPayload.serializer(), "sessionKey", JsonObject(emptyMap()))
    }

    @Test
    fun workFeedActionRunPayloadPromptPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(WorkFeedActionRunPayload.serializer(), "prompt", value)

        assertEquals(value, encoded["prompt"])
    }

    @Test
    fun workFeedActionRunPayloadPromptRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WorkFeedActionRunPayload.serializer(), "prompt", JsonObject(emptyMap()))
    }

    @Test
    fun workFeedActionRunPayloadMessagePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(WorkFeedActionRunPayload.serializer(), "message", value)

        assertEquals(value, encoded["message"])
    }

    @Test
    fun workFeedActionRunPayloadMessageRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WorkFeedActionRunPayload.serializer(), "message", JsonObject(emptyMap()))
    }

    @Test
    fun workFeedActionRunPayloadRemoveFromFeedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(WorkFeedActionRunPayload.serializer(), "removeFromFeed", value)

        assertEquals(value, encoded["removeFromFeed"])
    }

    @Test
    fun workFeedActionRunPayloadRemoveFromFeedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WorkFeedActionRunPayload.serializer(), "removeFromFeed", JsonPrimitive(1))
    }

    @Test
    fun workFeedFeedbackPayloadOkPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(WorkFeedFeedbackPayload.serializer(), "ok", value)

        assertEquals(value, encoded["ok"])
    }

    @Test
    fun workFeedFeedbackPayloadOkRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WorkFeedFeedbackPayload.serializer(), "ok", JsonPrimitive(1))
    }

    @Test
    fun workFeedFeedbackPayloadItemPreservesItsBoundaryValue() {
        val value = JsonObject(emptyMap())
        val encoded = roundTrip(WorkFeedFeedbackPayload.serializer(), "item", value)

        assertIs<JsonObject>(encoded["item"])
    }

    @Test
    fun workFeedFeedbackPayloadItemRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WorkFeedFeedbackPayload.serializer(), "item", JsonPrimitive("not-an-object"))
    }

    @Test
    fun workFeedFeedbackPayloadTextPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(WorkFeedFeedbackPayload.serializer(), "text", value)

        assertEquals(value, encoded["text"])
    }

    @Test
    fun workFeedFeedbackPayloadTextRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WorkFeedFeedbackPayload.serializer(), "text", JsonObject(emptyMap()))
    }

    @Test
    fun workFeedFeedbackPayloadSessionKeyPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(WorkFeedFeedbackPayload.serializer(), "sessionKey", value)

        assertEquals(value, encoded["sessionKey"])
    }

    @Test
    fun workFeedFeedbackPayloadSessionKeyRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WorkFeedFeedbackPayload.serializer(), "sessionKey", JsonObject(emptyMap()))
    }

    @Test
    fun nativeSyncPayloadEventsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(NativeSyncPayload.serializer(), "events", value)
        val encodedValues = encoded.getValue("events").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun nativeSyncPayloadEventsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NativeSyncPayload.serializer(), "events", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun nativeSyncPayloadCursorPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(NativeSyncPayload.serializer(), "cursor", value)

        assertEquals(value, encoded["cursor"])
    }

    @Test
    fun nativeSyncPayloadCursorRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NativeSyncPayload.serializer(), "cursor", JsonPrimitive("not-a-number"))
    }

    @Test
    fun nativeSyncPayloadLatestSeqPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(NativeSyncPayload.serializer(), "latestSeq", value)

        assertEquals(value, encoded["latestSeq"])
    }

    @Test
    fun nativeSyncPayloadLatestSeqRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NativeSyncPayload.serializer(), "latestSeq", JsonPrimitive("not-a-number"))
    }

    @Test
    fun nativeSyncPayloadHasMorePreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(NativeSyncPayload.serializer(), "hasMore", value)

        assertEquals(value, encoded["hasMore"])
    }

    @Test
    fun nativeSyncPayloadHasMoreRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NativeSyncPayload.serializer(), "hasMore", JsonPrimitive(1))
    }

    @Test
    fun nativeSyncEventSeqPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(NativeSyncEvent.serializer(), "seq", value)

        assertEquals(value, encoded["seq"])
    }

    @Test
    fun nativeSyncEventSeqRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NativeSyncEvent.serializer(), "seq", JsonPrimitive("not-a-number"))
    }

    @Test
    fun nativeSyncEventTypePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(NativeSyncEvent.serializer(), "type", value)

        assertEquals(value, encoded["type"])
    }

    @Test
    fun nativeSyncEventTypeRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NativeSyncEvent.serializer(), "type", JsonObject(emptyMap()))
    }

    @Test
    fun nativeSyncEventEntityIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(NativeSyncEvent.serializer(), "entityId", value)

        assertEquals(value, encoded["entityId"])
    }

    @Test
    fun nativeSyncEventEntityIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NativeSyncEvent.serializer(), "entityId", JsonObject(emptyMap()))
    }

    @Test
    fun nativeSyncEventSessionKeyPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(NativeSyncEvent.serializer(), "sessionKey", value)

        assertEquals(value, encoded["sessionKey"])
    }

    @Test
    fun nativeSyncEventSessionKeyRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NativeSyncEvent.serializer(), "sessionKey", JsonObject(emptyMap()))
    }

    @Test
    fun nativeSyncEventWorkFeedItemIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(NativeSyncEvent.serializer(), "workFeedItemId", value)

        assertEquals(value, encoded["workFeedItemId"])
    }

    @Test
    fun nativeSyncEventWorkFeedItemIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NativeSyncEvent.serializer(), "workFeedItemId", JsonObject(emptyMap()))
    }

    @Test
    fun nativeSyncEventTimestampMsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(NativeSyncEvent.serializer(), "timestampMs", value)

        assertEquals(value, encoded["timestampMs"])
    }

    @Test
    fun nativeSyncEventTimestampMsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NativeSyncEvent.serializer(), "timestampMs", JsonPrimitive("not-a-number"))
    }

    @Test
    fun nativeSyncEventPayloadPreservesItsBoundaryValue() {
        val value = JsonObject(mapOf("nested" to JsonPrimitive("값"), "count" to JsonPrimitive(7)))
        val encoded = roundTrip(NativeSyncEvent.serializer(), "payload", value)

        assertEquals(value, encoded["payload"])
    }

    @Test
    fun nativeSyncEventPayloadRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NativeSyncEvent.serializer(), "payload", JsonPrimitive("not-an-object"))
    }

    @Test
    fun nativeSyncActionPayloadItemPreservesItsBoundaryValue() {
        val value = JsonObject(emptyMap())
        val encoded = roundTrip(NativeSyncActionPayload.serializer(), "item", value)

        assertIs<JsonObject>(encoded["item"])
    }

    @Test
    fun nativeSyncActionPayloadItemRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NativeSyncActionPayload.serializer(), "item", JsonPrimitive("not-an-object"))
    }

    @Test
    fun nativeSyncActionPayloadRemoveFromFeedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(NativeSyncActionPayload.serializer(), "removeFromFeed", value)

        assertEquals(value, encoded["removeFromFeed"])
    }

    @Test
    fun nativeSyncActionPayloadRemoveFromFeedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(NativeSyncActionPayload.serializer(), "removeFromFeed", JsonPrimitive(1))
    }

    @Test
    fun memoryListPayloadPagesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(MemoryListPayload.serializer(), "pages", value)
        val encodedValues = encoded.getValue("pages").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun memoryListPayloadPagesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MemoryListPayload.serializer(), "pages", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun diaryRecentPayloadEntriesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(DiaryRecentPayload.serializer(), "entries", value)
        val encodedValues = encoded.getValue("entries").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun diaryRecentPayloadEntriesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(DiaryRecentPayload.serializer(), "entries", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun diaryRecentRowFilePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(DiaryRecentRow.serializer(), "file", value)

        assertEquals(value, encoded["file"])
    }

    @Test
    fun diaryRecentRowFileRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(DiaryRecentRow.serializer(), "file", JsonObject(emptyMap()))
    }

    @Test
    fun diaryRecentRowHeaderPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(DiaryRecentRow.serializer(), "header", value)

        assertEquals(value, encoded["header"])
    }

    @Test
    fun diaryRecentRowHeaderRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(DiaryRecentRow.serializer(), "header", JsonObject(emptyMap()))
    }

    @Test
    fun diaryRecentRowContentPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(DiaryRecentRow.serializer(), "content", value)

        assertEquals(value, encoded["content"])
    }

    @Test
    fun diaryRecentRowContentRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(DiaryRecentRow.serializer(), "content", JsonObject(emptyMap()))
    }

    @Test
    fun diaryRecentRowAtPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(DiaryRecentRow.serializer(), "at", value)

        assertEquals(value, encoded["at"])
    }

    @Test
    fun diaryRecentRowAtRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(DiaryRecentRow.serializer(), "at", JsonPrimitive("not-a-number"))
    }

    @Test
    fun deletePagesPayloadOkPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(DeletePagesPayload.serializer(), "ok", value)

        assertEquals(value, encoded["ok"])
    }

    @Test
    fun deletePagesPayloadOkRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(DeletePagesPayload.serializer(), "ok", JsonPrimitive(1))
    }

    @Test
    fun deletePagesPayloadDeletedPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(DeletePagesPayload.serializer(), "deleted", value)

        assertEquals(value, encoded["deleted"])
    }

    @Test
    fun deletePagesPayloadDeletedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(DeletePagesPayload.serializer(), "deleted", JsonPrimitive("not-a-number"))
    }

    @Test
    fun movePagePayloadOkPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(MovePagePayload.serializer(), "ok", value)

        assertEquals(value, encoded["ok"])
    }

    @Test
    fun movePagePayloadOkRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MovePagePayload.serializer(), "ok", JsonPrimitive(1))
    }

    @Test
    fun categoriesPayloadCategoriesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(CategoriesPayload.serializer(), "categories", value)
        val encodedValues = encoded.getValue("categories").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun categoriesPayloadCategoriesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CategoriesPayload.serializer(), "categories", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun categoriesPayloadTotalPagesPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(CategoriesPayload.serializer(), "totalPages", value)

        assertEquals(value, encoded["totalPages"])
    }

    @Test
    fun categoriesPayloadTotalPagesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CategoriesPayload.serializer(), "totalPages", JsonPrimitive("not-a-number"))
    }

    @Test
    fun categoriesPayloadTotalBytesPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(CategoriesPayload.serializer(), "totalBytes", value)

        assertEquals(value, encoded["totalBytes"])
    }

    @Test
    fun categoriesPayloadTotalBytesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CategoriesPayload.serializer(), "totalBytes", JsonPrimitive("not-a-number"))
    }

    @Test
    fun cronListPayloadJobsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(CronListPayload.serializer(), "jobs", value)
        val encodedValues = encoded.getValue("jobs").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun cronListPayloadJobsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CronListPayload.serializer(), "jobs", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun modelsPayloadCurrentPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ModelsPayload.serializer(), "current", value)

        assertEquals(value, encoded["current"])
    }

    @Test
    fun modelsPayloadCurrentRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelsPayload.serializer(), "current", JsonObject(emptyMap()))
    }

    @Test
    fun modelsPayloadRolesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(ModelsPayload.serializer(), "roles", value)
        val encodedValues = encoded.getValue("roles").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun modelsPayloadRolesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelsPayload.serializer(), "roles", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun modelsPayloadSectionsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(ModelsPayload.serializer(), "sections", value)
        val encodedValues = encoded.getValue("sections").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun modelsPayloadSectionsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelsPayload.serializer(), "sections", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun modelsPayloadAdvisoriesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(ModelsPayload.serializer(), "advisories", value)

        assertEquals(value, encoded["advisories"])
    }

    @Test
    fun modelsPayloadAdvisoriesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelsPayload.serializer(), "advisories", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun modelsPayloadMainHasVisionPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(ModelsPayload.serializer(), "mainHasVision", value)

        assertEquals(value, encoded["mainHasVision"])
    }

    @Test
    fun modelsPayloadMainHasVisionRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ModelsPayload.serializer(), "mainHasVision", JsonPrimitive(1))
    }

    @Test
    fun clientHelloPayloadVersionPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ClientHelloPayload.serializer(), "version", value)

        assertEquals(value, encoded["version"])
    }

    @Test
    fun clientHelloPayloadVersionRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ClientHelloPayload.serializer(), "version", JsonObject(emptyMap()))
    }

    @Test
    fun clientHelloPayloadNativeApiVersionPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(ClientHelloPayload.serializer(), "nativeApiVersion", value)

        assertEquals(value, encoded["nativeApiVersion"])
    }

    @Test
    fun clientHelloPayloadNativeApiVersionRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ClientHelloPayload.serializer(), "nativeApiVersion", JsonPrimitive("not-a-number"))
    }

    @Test
    fun clientHelloPayloadModelPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ClientHelloPayload.serializer(), "model", value)

        assertEquals(value, encoded["model"])
    }

    @Test
    fun clientHelloPayloadModelRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ClientHelloPayload.serializer(), "model", JsonObject(emptyMap()))
    }

    @Test
    fun clientHelloPayloadCapabilitiesPreservesItsBoundaryValue() {
        val value = JsonObject(mapOf("기능" to JsonPrimitive(true), "disabled" to JsonPrimitive(false)))
        val encoded = roundTrip(ClientHelloPayload.serializer(), "capabilities", value)

        assertEquals(value, encoded["capabilities"])
    }

    @Test
    fun clientHelloPayloadCapabilitiesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ClientHelloPayload.serializer(), "capabilities", JsonPrimitive("not-an-object"))
    }

    @Test
    fun clientHelloPayloadEndpointsPreservesItsBoundaryValue() {
        val value = JsonObject(mapOf("한글 키" to JsonPrimitive("  값  "), "empty" to JsonPrimitive("")))
        val encoded = roundTrip(ClientHelloPayload.serializer(), "endpoints", value)

        assertEquals(value, encoded["endpoints"])
    }

    @Test
    fun clientHelloPayloadEndpointsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ClientHelloPayload.serializer(), "endpoints", JsonPrimitive("not-an-object"))
    }

    @Test
    fun clientHelloPayloadTsMsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(ClientHelloPayload.serializer(), "tsMs", value)

        assertEquals(value, encoded["tsMs"])
    }

    @Test
    fun clientHelloPayloadTsMsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ClientHelloPayload.serializer(), "tsMs", JsonPrimitive("not-a-number"))
    }

    @Test
    fun mailListPayloadMessagesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(MailListPayload.serializer(), "messages", value)
        val encodedValues = encoded.getValue("messages").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun mailListPayloadMessagesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailListPayload.serializer(), "messages", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun mailListPayloadNextPageTokenPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(MailListPayload.serializer(), "nextPageToken", value)

        assertEquals(value, encoded["nextPageToken"])
    }

    @Test
    fun mailListPayloadNextPageTokenRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(MailListPayload.serializer(), "nextPageToken", JsonObject(emptyMap()))
    }

    @Test
    fun okPayloadOkPreservesItsBoundaryValue() {
        val value = JsonPrimitive(true)
        val encoded = roundTrip(OkPayload.serializer(), "ok", value)

        assertEquals(value, encoded["ok"])
    }

    @Test
    fun okPayloadOkRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(OkPayload.serializer(), "ok", JsonPrimitive(1))
    }

    @Test
    fun askPayloadAnswerPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(AskPayload.serializer(), "answer", value)

        assertEquals(value, encoded["answer"])
    }

    @Test
    fun askPayloadAnswerRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(AskPayload.serializer(), "answer", JsonObject(emptyMap()))
    }

    @Test
    fun senderContextPayloadSenderPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SenderContextPayload.serializer(), "sender", value)

        assertEquals(value, encoded["sender"])
    }

    @Test
    fun senderContextPayloadSenderRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SenderContextPayload.serializer(), "sender", JsonObject(emptyMap()))
    }

    @Test
    fun senderContextPayloadEmailPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SenderContextPayload.serializer(), "email", value)

        assertEquals(value, encoded["email"])
    }

    @Test
    fun senderContextPayloadEmailRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SenderContextPayload.serializer(), "email", JsonObject(emptyMap()))
    }

    @Test
    fun senderContextPayloadDisplayNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SenderContextPayload.serializer(), "displayName", value)

        assertEquals(value, encoded["displayName"])
    }

    @Test
    fun senderContextPayloadDisplayNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SenderContextPayload.serializer(), "displayName", JsonObject(emptyMap()))
    }

    @Test
    fun senderContextPayloadRecentPreservesItsBoundaryValue() {
        val value = JsonObject(emptyMap())
        val encoded = roundTrip(SenderContextPayload.serializer(), "recent", value)

        assertIs<JsonObject>(encoded["recent"])
    }

    @Test
    fun senderContextPayloadRecentRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SenderContextPayload.serializer(), "recent", JsonPrimitive("not-an-object"))
    }

    @Test
    fun senderContextPayloadWikiHitsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(SenderContextPayload.serializer(), "wikiHits", value)
        val encodedValues = encoded.getValue("wikiHits").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun senderContextPayloadWikiHitsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SenderContextPayload.serializer(), "wikiHits", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun senderContextPayloadWikiFactsPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(SenderContextPayload.serializer(), "wikiFacts", value)

        assertEquals(value, encoded["wikiFacts"])
    }

    @Test
    fun senderContextPayloadWikiFactsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(SenderContextPayload.serializer(), "wikiFacts", JsonObject(emptyMap()))
    }

    @Test
    fun calListPayloadEventsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(CalListPayload.serializer(), "events", value)
        val encodedValues = encoded.getValue("events").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun calListPayloadEventsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalListPayload.serializer(), "events", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun calProposalsPayloadProposalsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(CalProposalsPayload.serializer(), "proposals", value)
        val encodedValues = encoded.getValue("proposals").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun calProposalsPayloadProposalsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CalProposalsPayload.serializer(), "proposals", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun todoListPayloadTodosPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(TodoListPayload.serializer(), "todos", value)
        val encodedValues = encoded.getValue("todos").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun todoListPayloadTodosRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(TodoListPayload.serializer(), "todos", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun peopleListPayloadPeoplePreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(PeopleListPayload.serializer(), "people", value)
        val encodedValues = encoded.getValue("people").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun peopleListPayloadPeopleRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(PeopleListPayload.serializer(), "people", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun contactsListPayloadContactsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(ContactsListPayload.serializer(), "contacts", value)
        val encodedValues = encoded.getValue("contacts").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun contactsListPayloadContactsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ContactsListPayload.serializer(), "contacts", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun wikiPagePayloadPathPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(WikiPagePayload.serializer(), "path", value)

        assertEquals(value, encoded["path"])
    }

    @Test
    fun wikiPagePayloadPathRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WikiPagePayload.serializer(), "path", JsonObject(emptyMap()))
    }

    @Test
    fun wikiPagePayloadTitlePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(WikiPagePayload.serializer(), "title", value)

        assertEquals(value, encoded["title"])
    }

    @Test
    fun wikiPagePayloadTitleRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WikiPagePayload.serializer(), "title", JsonObject(emptyMap()))
    }

    @Test
    fun wikiPagePayloadSummaryPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(WikiPagePayload.serializer(), "summary", value)

        assertEquals(value, encoded["summary"])
    }

    @Test
    fun wikiPagePayloadSummaryRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WikiPagePayload.serializer(), "summary", JsonObject(emptyMap()))
    }

    @Test
    fun wikiPagePayloadCategoryPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(WikiPagePayload.serializer(), "category", value)

        assertEquals(value, encoded["category"])
    }

    @Test
    fun wikiPagePayloadCategoryRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WikiPagePayload.serializer(), "category", JsonObject(emptyMap()))
    }

    @Test
    fun wikiPagePayloadCodePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(WikiPagePayload.serializer(), "code", value)

        assertEquals(value, encoded["code"])
    }

    @Test
    fun wikiPagePayloadCodeRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WikiPagePayload.serializer(), "code", JsonObject(emptyMap()))
    }

    @Test
    fun wikiPagePayloadTagsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(WikiPagePayload.serializer(), "tags", value)

        assertEquals(value, encoded["tags"])
    }

    @Test
    fun wikiPagePayloadTagsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WikiPagePayload.serializer(), "tags", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun wikiPagePayloadRelatedPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive(""))
            add(JsonPrimitive("첫 번째"))
            add(JsonPrimitive("끝\n값"))
        }
        val encoded = roundTrip(WikiPagePayload.serializer(), "related", value)

        assertEquals(value, encoded["related"])
    }

    @Test
    fun wikiPagePayloadRelatedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WikiPagePayload.serializer(), "related", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun wikiPagePayloadUpdatedPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(WikiPagePayload.serializer(), "updated", value)

        assertEquals(value, encoded["updated"])
    }

    @Test
    fun wikiPagePayloadUpdatedRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WikiPagePayload.serializer(), "updated", JsonObject(emptyMap()))
    }

    @Test
    fun wikiPagePayloadBodyPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(WikiPagePayload.serializer(), "body", value)

        assertEquals(value, encoded["body"])
    }

    @Test
    fun wikiPagePayloadBodyRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(WikiPagePayload.serializer(), "body", JsonObject(emptyMap()))
    }

    @Test
    fun captureImagePayloadTextPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CaptureImagePayload.serializer(), "text", value)

        assertEquals(value, encoded["text"])
    }

    @Test
    fun captureImagePayloadTextRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CaptureImagePayload.serializer(), "text", JsonObject(emptyMap()))
    }

    @Test
    fun captureAudioPayloadTextPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CaptureAudioPayload.serializer(), "text", value)

        assertEquals(value, encoded["text"])
    }

    @Test
    fun captureAudioPayloadTextRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CaptureAudioPayload.serializer(), "text", JsonObject(emptyMap()))
    }

    @Test
    fun captureDocumentPayloadTextPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CaptureDocumentPayload.serializer(), "text", value)

        assertEquals(value, encoded["text"])
    }

    @Test
    fun captureDocumentPayloadTextRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CaptureDocumentPayload.serializer(), "text", JsonObject(emptyMap()))
    }

    @Test
    fun captureContactsPayloadTextPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(CaptureContactsPayload.serializer(), "text", value)

        assertEquals(value, encoded["text"])
    }

    @Test
    fun captureContactsPayloadTextRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(CaptureContactsPayload.serializer(), "text", JsonObject(emptyMap()))
    }

    @Test
    fun observeToolStatNamePreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ObserveToolStat.serializer(), "name", value)

        assertEquals(value, encoded["name"])
    }

    @Test
    fun observeToolStatNameRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ObserveToolStat.serializer(), "name", JsonObject(emptyMap()))
    }

    @Test
    fun observeToolStatCallsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(ObserveToolStat.serializer(), "calls", value)

        assertEquals(value, encoded["calls"])
    }

    @Test
    fun observeToolStatCallsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ObserveToolStat.serializer(), "calls", JsonPrimitive("not-a-number"))
    }

    @Test
    fun observeToolStatErrorsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(ObserveToolStat.serializer(), "errors", value)

        assertEquals(value, encoded["errors"])
    }

    @Test
    fun observeToolStatErrorsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ObserveToolStat.serializer(), "errors", JsonPrimitive("not-a-number"))
    }

    @Test
    fun observeToolStatAvgMsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Long.MAX_VALUE)
        val encoded = roundTrip(ObserveToolStat.serializer(), "avgMs", value)

        assertEquals(value, encoded["avgMs"])
    }

    @Test
    fun observeToolStatAvgMsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ObserveToolStat.serializer(), "avgMs", JsonPrimitive("not-a-number"))
    }

    @Test
    fun observeBehaviorRunsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(ObserveBehavior.serializer(), "runs", value)

        assertEquals(value, encoded["runs"])
    }

    @Test
    fun observeBehaviorRunsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ObserveBehavior.serializer(), "runs", JsonPrimitive("not-a-number"))
    }

    @Test
    fun observeBehaviorProactiveRunsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(ObserveBehavior.serializer(), "proactiveRuns", value)

        assertEquals(value, encoded["proactiveRuns"])
    }

    @Test
    fun observeBehaviorProactiveRunsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ObserveBehavior.serializer(), "proactiveRuns", JsonPrimitive("not-a-number"))
    }

    @Test
    fun observeBehaviorCompactedRunsPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(ObserveBehavior.serializer(), "compactedRuns", value)

        assertEquals(value, encoded["compactedRuns"])
    }

    @Test
    fun observeBehaviorCompactedRunsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ObserveBehavior.serializer(), "compactedRuns", JsonPrimitive("not-a-number"))
    }

    @Test
    fun observeBehaviorToolsPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(ObserveBehavior.serializer(), "tools", value)
        val encodedValues = encoded.getValue("tools").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun observeBehaviorToolsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ObserveBehavior.serializer(), "tools", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun observeBehaviorBackgroundErrorsPreservesItsBoundaryValue() {
        val value = JsonObject(mapOf("최대" to JsonPrimitive(Int.MAX_VALUE), "zero" to JsonPrimitive(0)))
        val encoded = roundTrip(ObserveBehavior.serializer(), "backgroundErrors", value)

        assertEquals(value, encoded["backgroundErrors"])
    }

    @Test
    fun observeBehaviorBackgroundErrorsRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ObserveBehavior.serializer(), "backgroundErrors", JsonPrimitive("not-an-object"))
    }

    @Test
    fun observeLogLineLevelPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ObserveLogLine.serializer(), "level", value)

        assertEquals(value, encoded["level"])
    }

    @Test
    fun observeLogLineLevelRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ObserveLogLine.serializer(), "level", JsonObject(emptyMap()))
    }

    @Test
    fun observeLogLineMsgPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ObserveLogLine.serializer(), "msg", value)

        assertEquals(value, encoded["msg"])
    }

    @Test
    fun observeLogLineMsgRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ObserveLogLine.serializer(), "msg", JsonObject(emptyMap()))
    }

    @Test
    fun observeLogLineRunIdPreservesItsBoundaryValue() {
        val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
        val encoded = roundTrip(ObserveLogLine.serializer(), "runId", value)

        assertEquals(value, encoded["runId"])
    }

    @Test
    fun observeLogLineRunIdRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ObserveLogLine.serializer(), "runId", JsonObject(emptyMap()))
    }

    @Test
    fun observeLogsPayloadLinesPreservesItsBoundaryValue() {
        val value = buildJsonArray {
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
            add(JsonObject(emptyMap()))
        }
        val encoded = roundTrip(ObserveLogsPayload.serializer(), "lines", value)
        val encodedValues = encoded.getValue("lines").jsonArray

        assertEquals(3, encodedValues.size)
        encodedValues.forEach { assertIs<JsonObject>(it) }
    }

    @Test
    fun observeLogsPayloadLinesRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ObserveLogsPayload.serializer(), "lines", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
    }

    @Test
    fun observeLogsPayloadCountPreservesItsBoundaryValue() {
        val value = JsonPrimitive(Int.MAX_VALUE)
        val encoded = roundTrip(ObserveLogsPayload.serializer(), "count", value)

        assertEquals(value, encoded["count"])
    }

    @Test
    fun observeLogsPayloadCountRejectsAnIncompatibleWireShape() {
        rejectsWrongShape(ObserveLogsPayload.serializer(), "count", JsonPrimitive("not-a-number"))
    }
}
