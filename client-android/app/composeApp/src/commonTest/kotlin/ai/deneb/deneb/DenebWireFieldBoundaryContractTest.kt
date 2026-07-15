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

    private fun fieldBoundaryCases(): List<() -> Unit> = listOf(
        {
            val value = buildJsonArray {
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
            }
            val encoded = roundTrip(RecentPayload.serializer(), "sessions", value)
            val encodedValues = encoded.getValue("sessions").jsonArray

            assertEquals(3, encodedValues.size)
            encodedValues.forEach { assertIs<JsonObject>(it) }
        },
        {
            rejectsWrongShape(RecentPayload.serializer(), "sessions", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = buildJsonArray {
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
            }
            val encoded = roundTrip(TranscriptPayload.serializer(), "messages", value)
            val encodedValues = encoded.getValue("messages").jsonArray

            assertEquals(3, encodedValues.size)
            encodedValues.forEach { assertIs<JsonObject>(it) }
        },
        {
            rejectsWrongShape(TranscriptPayload.serializer(), "messages", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = buildJsonArray {
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
            }
            val encoded = roundTrip(WorkFeedPayload.serializer(), "items", value)
            val encodedValues = encoded.getValue("items").jsonArray

            assertEquals(3, encodedValues.size)
            encodedValues.forEach { assertIs<JsonObject>(it) }
        },
        {
            rejectsWrongShape(WorkFeedPayload.serializer(), "items", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = JsonPrimitive(true)
            val encoded = roundTrip(WorkFeedActionRunPayload.serializer(), "ok", value)

            assertEquals(value, encoded["ok"])
        },
        {
            rejectsWrongShape(WorkFeedActionRunPayload.serializer(), "ok", JsonPrimitive(1))
        },
        {
            val value = JsonObject(emptyMap())
            val encoded = roundTrip(WorkFeedActionRunPayload.serializer(), "item", value)

            assertIs<JsonObject>(encoded["item"])
        },
        {
            rejectsWrongShape(WorkFeedActionRunPayload.serializer(), "item", JsonPrimitive("not-an-object"))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(WorkFeedActionRunPayload.serializer(), "sessionKey", value)

            assertEquals(value, encoded["sessionKey"])
        },
        {
            rejectsWrongShape(WorkFeedActionRunPayload.serializer(), "sessionKey", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(WorkFeedActionRunPayload.serializer(), "prompt", value)

            assertEquals(value, encoded["prompt"])
        },
        {
            rejectsWrongShape(WorkFeedActionRunPayload.serializer(), "prompt", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(WorkFeedActionRunPayload.serializer(), "message", value)

            assertEquals(value, encoded["message"])
        },
        {
            rejectsWrongShape(WorkFeedActionRunPayload.serializer(), "message", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive(true)
            val encoded = roundTrip(WorkFeedActionRunPayload.serializer(), "removeFromFeed", value)

            assertEquals(value, encoded["removeFromFeed"])
        },
        {
            rejectsWrongShape(WorkFeedActionRunPayload.serializer(), "removeFromFeed", JsonPrimitive(1))
        },
        {
            val value = JsonPrimitive(true)
            val encoded = roundTrip(WorkFeedFeedbackPayload.serializer(), "ok", value)

            assertEquals(value, encoded["ok"])
        },
        {
            rejectsWrongShape(WorkFeedFeedbackPayload.serializer(), "ok", JsonPrimitive(1))
        },
        {
            val value = JsonObject(emptyMap())
            val encoded = roundTrip(WorkFeedFeedbackPayload.serializer(), "item", value)

            assertIs<JsonObject>(encoded["item"])
        },
        {
            rejectsWrongShape(WorkFeedFeedbackPayload.serializer(), "item", JsonPrimitive("not-an-object"))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(WorkFeedFeedbackPayload.serializer(), "text", value)

            assertEquals(value, encoded["text"])
        },
        {
            rejectsWrongShape(WorkFeedFeedbackPayload.serializer(), "text", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(WorkFeedFeedbackPayload.serializer(), "sessionKey", value)

            assertEquals(value, encoded["sessionKey"])
        },
        {
            rejectsWrongShape(WorkFeedFeedbackPayload.serializer(), "sessionKey", JsonObject(emptyMap()))
        },
        {
            val value = buildJsonArray {
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
            }
            val encoded = roundTrip(NativeSyncPayload.serializer(), "events", value)
            val encodedValues = encoded.getValue("events").jsonArray

            assertEquals(3, encodedValues.size)
            encodedValues.forEach { assertIs<JsonObject>(it) }
        },
        {
            rejectsWrongShape(NativeSyncPayload.serializer(), "events", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = JsonPrimitive(Long.MAX_VALUE)
            val encoded = roundTrip(NativeSyncPayload.serializer(), "cursor", value)

            assertEquals(value, encoded["cursor"])
        },
        {
            rejectsWrongShape(NativeSyncPayload.serializer(), "cursor", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Long.MAX_VALUE)
            val encoded = roundTrip(NativeSyncPayload.serializer(), "latestSeq", value)

            assertEquals(value, encoded["latestSeq"])
        },
        {
            rejectsWrongShape(NativeSyncPayload.serializer(), "latestSeq", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(true)
            val encoded = roundTrip(NativeSyncPayload.serializer(), "hasMore", value)

            assertEquals(value, encoded["hasMore"])
        },
        {
            rejectsWrongShape(NativeSyncPayload.serializer(), "hasMore", JsonPrimitive(1))
        },
        {
            val value = JsonPrimitive(Long.MAX_VALUE)
            val encoded = roundTrip(NativeSyncEvent.serializer(), "seq", value)

            assertEquals(value, encoded["seq"])
        },
        {
            rejectsWrongShape(NativeSyncEvent.serializer(), "seq", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(NativeSyncEvent.serializer(), "type", value)

            assertEquals(value, encoded["type"])
        },
        {
            rejectsWrongShape(NativeSyncEvent.serializer(), "type", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(NativeSyncEvent.serializer(), "entityId", value)

            assertEquals(value, encoded["entityId"])
        },
        {
            rejectsWrongShape(NativeSyncEvent.serializer(), "entityId", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(NativeSyncEvent.serializer(), "sessionKey", value)

            assertEquals(value, encoded["sessionKey"])
        },
        {
            rejectsWrongShape(NativeSyncEvent.serializer(), "sessionKey", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(NativeSyncEvent.serializer(), "workFeedItemId", value)

            assertEquals(value, encoded["workFeedItemId"])
        },
        {
            rejectsWrongShape(NativeSyncEvent.serializer(), "workFeedItemId", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive(Long.MAX_VALUE)
            val encoded = roundTrip(NativeSyncEvent.serializer(), "timestampMs", value)

            assertEquals(value, encoded["timestampMs"])
        },
        {
            rejectsWrongShape(NativeSyncEvent.serializer(), "timestampMs", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonObject(mapOf("nested" to JsonPrimitive("값"), "count" to JsonPrimitive(7)))
            val encoded = roundTrip(NativeSyncEvent.serializer(), "payload", value)

            assertEquals(value, encoded["payload"])
        },
        {
            rejectsWrongShape(NativeSyncEvent.serializer(), "payload", JsonPrimitive("not-an-object"))
        },
        {
            val value = JsonObject(emptyMap())
            val encoded = roundTrip(NativeSyncActionPayload.serializer(), "item", value)

            assertIs<JsonObject>(encoded["item"])
        },
        {
            rejectsWrongShape(NativeSyncActionPayload.serializer(), "item", JsonPrimitive("not-an-object"))
        },
        {
            val value = JsonPrimitive(true)
            val encoded = roundTrip(NativeSyncActionPayload.serializer(), "removeFromFeed", value)

            assertEquals(value, encoded["removeFromFeed"])
        },
        {
            rejectsWrongShape(NativeSyncActionPayload.serializer(), "removeFromFeed", JsonPrimitive(1))
        },
        {
            val value = buildJsonArray {
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
            }
            val encoded = roundTrip(MemoryListPayload.serializer(), "pages", value)
            val encodedValues = encoded.getValue("pages").jsonArray

            assertEquals(3, encodedValues.size)
            encodedValues.forEach { assertIs<JsonObject>(it) }
        },
        {
            rejectsWrongShape(MemoryListPayload.serializer(), "pages", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = buildJsonArray {
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
            }
            val encoded = roundTrip(DiaryRecentPayload.serializer(), "entries", value)
            val encodedValues = encoded.getValue("entries").jsonArray

            assertEquals(3, encodedValues.size)
            encodedValues.forEach { assertIs<JsonObject>(it) }
        },
        {
            rejectsWrongShape(DiaryRecentPayload.serializer(), "entries", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(DiaryRecentRow.serializer(), "file", value)

            assertEquals(value, encoded["file"])
        },
        {
            rejectsWrongShape(DiaryRecentRow.serializer(), "file", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(DiaryRecentRow.serializer(), "header", value)

            assertEquals(value, encoded["header"])
        },
        {
            rejectsWrongShape(DiaryRecentRow.serializer(), "header", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(DiaryRecentRow.serializer(), "content", value)

            assertEquals(value, encoded["content"])
        },
        {
            rejectsWrongShape(DiaryRecentRow.serializer(), "content", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive(Long.MAX_VALUE)
            val encoded = roundTrip(DiaryRecentRow.serializer(), "at", value)

            assertEquals(value, encoded["at"])
        },
        {
            rejectsWrongShape(DiaryRecentRow.serializer(), "at", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(true)
            val encoded = roundTrip(DeletePagesPayload.serializer(), "ok", value)

            assertEquals(value, encoded["ok"])
        },
        {
            rejectsWrongShape(DeletePagesPayload.serializer(), "ok", JsonPrimitive(1))
        },
        {
            val value = JsonPrimitive(Int.MAX_VALUE)
            val encoded = roundTrip(DeletePagesPayload.serializer(), "deleted", value)

            assertEquals(value, encoded["deleted"])
        },
        {
            rejectsWrongShape(DeletePagesPayload.serializer(), "deleted", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(true)
            val encoded = roundTrip(MovePagePayload.serializer(), "ok", value)

            assertEquals(value, encoded["ok"])
        },
        {
            rejectsWrongShape(MovePagePayload.serializer(), "ok", JsonPrimitive(1))
        },
        {
            val value = buildJsonArray {
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
            }
            val encoded = roundTrip(CategoriesPayload.serializer(), "categories", value)
            val encodedValues = encoded.getValue("categories").jsonArray

            assertEquals(3, encodedValues.size)
            encodedValues.forEach { assertIs<JsonObject>(it) }
        },
        {
            rejectsWrongShape(CategoriesPayload.serializer(), "categories", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = JsonPrimitive(Int.MAX_VALUE)
            val encoded = roundTrip(CategoriesPayload.serializer(), "totalPages", value)

            assertEquals(value, encoded["totalPages"])
        },
        {
            rejectsWrongShape(CategoriesPayload.serializer(), "totalPages", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Long.MAX_VALUE)
            val encoded = roundTrip(CategoriesPayload.serializer(), "totalBytes", value)

            assertEquals(value, encoded["totalBytes"])
        },
        {
            rejectsWrongShape(CategoriesPayload.serializer(), "totalBytes", JsonPrimitive("not-a-number"))
        },
        {
            val value = buildJsonArray {
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
            }
            val encoded = roundTrip(CronListPayload.serializer(), "jobs", value)
            val encodedValues = encoded.getValue("jobs").jsonArray

            assertEquals(3, encodedValues.size)
            encodedValues.forEach { assertIs<JsonObject>(it) }
        },
        {
            rejectsWrongShape(CronListPayload.serializer(), "jobs", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(ModelsPayload.serializer(), "current", value)

            assertEquals(value, encoded["current"])
        },
        {
            rejectsWrongShape(ModelsPayload.serializer(), "current", JsonObject(emptyMap()))
        },
        {
            val value = buildJsonArray {
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
            }
            val encoded = roundTrip(ModelsPayload.serializer(), "roles", value)
            val encodedValues = encoded.getValue("roles").jsonArray

            assertEquals(3, encodedValues.size)
            encodedValues.forEach { assertIs<JsonObject>(it) }
        },
        {
            rejectsWrongShape(ModelsPayload.serializer(), "roles", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = buildJsonArray {
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
            }
            val encoded = roundTrip(ModelsPayload.serializer(), "sections", value)
            val encodedValues = encoded.getValue("sections").jsonArray

            assertEquals(3, encodedValues.size)
            encodedValues.forEach { assertIs<JsonObject>(it) }
        },
        {
            rejectsWrongShape(ModelsPayload.serializer(), "sections", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = buildJsonArray {
                add(JsonPrimitive("첫 번째"))
                add(JsonPrimitive(""))
                add(JsonPrimitive("첫 번째"))
                add(JsonPrimitive("끝\n값"))
            }
            val encoded = roundTrip(ModelsPayload.serializer(), "advisories", value)

            assertEquals(value, encoded["advisories"])
        },
        {
            rejectsWrongShape(ModelsPayload.serializer(), "advisories", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = JsonPrimitive(true)
            val encoded = roundTrip(ModelsPayload.serializer(), "mainHasVision", value)

            assertEquals(value, encoded["mainHasVision"])
        },
        {
            rejectsWrongShape(ModelsPayload.serializer(), "mainHasVision", JsonPrimitive(1))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(ClientHelloPayload.serializer(), "version", value)

            assertEquals(value, encoded["version"])
        },
        {
            rejectsWrongShape(ClientHelloPayload.serializer(), "version", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive(Int.MAX_VALUE)
            val encoded = roundTrip(ClientHelloPayload.serializer(), "nativeApiVersion", value)

            assertEquals(value, encoded["nativeApiVersion"])
        },
        {
            rejectsWrongShape(ClientHelloPayload.serializer(), "nativeApiVersion", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(ClientHelloPayload.serializer(), "model", value)

            assertEquals(value, encoded["model"])
        },
        {
            rejectsWrongShape(ClientHelloPayload.serializer(), "model", JsonObject(emptyMap()))
        },
        {
            val value = JsonObject(mapOf("기능" to JsonPrimitive(true), "disabled" to JsonPrimitive(false)))
            val encoded = roundTrip(ClientHelloPayload.serializer(), "capabilities", value)

            assertEquals(value, encoded["capabilities"])
        },
        {
            rejectsWrongShape(ClientHelloPayload.serializer(), "capabilities", JsonPrimitive("not-an-object"))
        },
        {
            val value = JsonObject(mapOf("한글 키" to JsonPrimitive("  값  "), "empty" to JsonPrimitive("")))
            val encoded = roundTrip(ClientHelloPayload.serializer(), "endpoints", value)

            assertEquals(value, encoded["endpoints"])
        },
        {
            rejectsWrongShape(ClientHelloPayload.serializer(), "endpoints", JsonPrimitive("not-an-object"))
        },
        {
            val value = JsonPrimitive(Long.MAX_VALUE)
            val encoded = roundTrip(ClientHelloPayload.serializer(), "tsMs", value)

            assertEquals(value, encoded["tsMs"])
        },
        {
            rejectsWrongShape(ClientHelloPayload.serializer(), "tsMs", JsonPrimitive("not-a-number"))
        },
        {
            val value = buildJsonArray {
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
            }
            val encoded = roundTrip(MailListPayload.serializer(), "messages", value)
            val encodedValues = encoded.getValue("messages").jsonArray

            assertEquals(3, encodedValues.size)
            encodedValues.forEach { assertIs<JsonObject>(it) }
        },
        {
            rejectsWrongShape(MailListPayload.serializer(), "messages", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(MailListPayload.serializer(), "nextPageToken", value)

            assertEquals(value, encoded["nextPageToken"])
        },
        {
            rejectsWrongShape(MailListPayload.serializer(), "nextPageToken", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive(true)
            val encoded = roundTrip(OkPayload.serializer(), "ok", value)

            assertEquals(value, encoded["ok"])
        },
        {
            rejectsWrongShape(OkPayload.serializer(), "ok", JsonPrimitive(1))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(AskPayload.serializer(), "answer", value)

            assertEquals(value, encoded["answer"])
        },
        {
            rejectsWrongShape(AskPayload.serializer(), "answer", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(SenderContextPayload.serializer(), "sender", value)

            assertEquals(value, encoded["sender"])
        },
        {
            rejectsWrongShape(SenderContextPayload.serializer(), "sender", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(SenderContextPayload.serializer(), "email", value)

            assertEquals(value, encoded["email"])
        },
        {
            rejectsWrongShape(SenderContextPayload.serializer(), "email", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(SenderContextPayload.serializer(), "displayName", value)

            assertEquals(value, encoded["displayName"])
        },
        {
            rejectsWrongShape(SenderContextPayload.serializer(), "displayName", JsonObject(emptyMap()))
        },
        {
            val value = JsonObject(emptyMap())
            val encoded = roundTrip(SenderContextPayload.serializer(), "recent", value)

            assertIs<JsonObject>(encoded["recent"])
        },
        {
            rejectsWrongShape(SenderContextPayload.serializer(), "recent", JsonPrimitive("not-an-object"))
        },
        {
            val value = buildJsonArray {
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
            }
            val encoded = roundTrip(SenderContextPayload.serializer(), "wikiHits", value)
            val encodedValues = encoded.getValue("wikiHits").jsonArray

            assertEquals(3, encodedValues.size)
            encodedValues.forEach { assertIs<JsonObject>(it) }
        },
        {
            rejectsWrongShape(SenderContextPayload.serializer(), "wikiHits", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(SenderContextPayload.serializer(), "wikiFacts", value)

            assertEquals(value, encoded["wikiFacts"])
        },
        {
            rejectsWrongShape(SenderContextPayload.serializer(), "wikiFacts", JsonObject(emptyMap()))
        },
        {
            val value = buildJsonArray {
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
            }
            val encoded = roundTrip(CalListPayload.serializer(), "events", value)
            val encodedValues = encoded.getValue("events").jsonArray

            assertEquals(3, encodedValues.size)
            encodedValues.forEach { assertIs<JsonObject>(it) }
        },
        {
            rejectsWrongShape(CalListPayload.serializer(), "events", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = buildJsonArray {
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
            }
            val encoded = roundTrip(CalProposalsPayload.serializer(), "proposals", value)
            val encodedValues = encoded.getValue("proposals").jsonArray

            assertEquals(3, encodedValues.size)
            encodedValues.forEach { assertIs<JsonObject>(it) }
        },
        {
            rejectsWrongShape(CalProposalsPayload.serializer(), "proposals", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = buildJsonArray {
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
            }
            val encoded = roundTrip(TodoListPayload.serializer(), "todos", value)
            val encodedValues = encoded.getValue("todos").jsonArray

            assertEquals(3, encodedValues.size)
            encodedValues.forEach { assertIs<JsonObject>(it) }
        },
        {
            rejectsWrongShape(TodoListPayload.serializer(), "todos", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = buildJsonArray {
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
            }
            val encoded = roundTrip(PeopleListPayload.serializer(), "people", value)
            val encodedValues = encoded.getValue("people").jsonArray

            assertEquals(3, encodedValues.size)
            encodedValues.forEach { assertIs<JsonObject>(it) }
        },
        {
            rejectsWrongShape(PeopleListPayload.serializer(), "people", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = buildJsonArray {
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
            }
            val encoded = roundTrip(ContactsListPayload.serializer(), "contacts", value)
            val encodedValues = encoded.getValue("contacts").jsonArray

            assertEquals(3, encodedValues.size)
            encodedValues.forEach { assertIs<JsonObject>(it) }
        },
        {
            rejectsWrongShape(ContactsListPayload.serializer(), "contacts", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(WikiPagePayload.serializer(), "path", value)

            assertEquals(value, encoded["path"])
        },
        {
            rejectsWrongShape(WikiPagePayload.serializer(), "path", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(WikiPagePayload.serializer(), "title", value)

            assertEquals(value, encoded["title"])
        },
        {
            rejectsWrongShape(WikiPagePayload.serializer(), "title", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(WikiPagePayload.serializer(), "summary", value)

            assertEquals(value, encoded["summary"])
        },
        {
            rejectsWrongShape(WikiPagePayload.serializer(), "summary", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(WikiPagePayload.serializer(), "category", value)

            assertEquals(value, encoded["category"])
        },
        {
            rejectsWrongShape(WikiPagePayload.serializer(), "category", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(WikiPagePayload.serializer(), "code", value)

            assertEquals(value, encoded["code"])
        },
        {
            rejectsWrongShape(WikiPagePayload.serializer(), "code", JsonObject(emptyMap()))
        },
        {
            val value = buildJsonArray {
                add(JsonPrimitive("첫 번째"))
                add(JsonPrimitive(""))
                add(JsonPrimitive("첫 번째"))
                add(JsonPrimitive("끝\n값"))
            }
            val encoded = roundTrip(WikiPagePayload.serializer(), "tags", value)

            assertEquals(value, encoded["tags"])
        },
        {
            rejectsWrongShape(WikiPagePayload.serializer(), "tags", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = buildJsonArray {
                add(JsonPrimitive("첫 번째"))
                add(JsonPrimitive(""))
                add(JsonPrimitive("첫 번째"))
                add(JsonPrimitive("끝\n값"))
            }
            val encoded = roundTrip(WikiPagePayload.serializer(), "related", value)

            assertEquals(value, encoded["related"])
        },
        {
            rejectsWrongShape(WikiPagePayload.serializer(), "related", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(WikiPagePayload.serializer(), "updated", value)

            assertEquals(value, encoded["updated"])
        },
        {
            rejectsWrongShape(WikiPagePayload.serializer(), "updated", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(WikiPagePayload.serializer(), "body", value)

            assertEquals(value, encoded["body"])
        },
        {
            rejectsWrongShape(WikiPagePayload.serializer(), "body", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(CaptureImagePayload.serializer(), "text", value)

            assertEquals(value, encoded["text"])
        },
        {
            rejectsWrongShape(CaptureImagePayload.serializer(), "text", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(CaptureAudioPayload.serializer(), "text", value)

            assertEquals(value, encoded["text"])
        },
        {
            rejectsWrongShape(CaptureAudioPayload.serializer(), "text", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(CaptureDocumentPayload.serializer(), "text", value)

            assertEquals(value, encoded["text"])
        },
        {
            rejectsWrongShape(CaptureDocumentPayload.serializer(), "text", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(CaptureContactsPayload.serializer(), "text", value)

            assertEquals(value, encoded["text"])
        },
        {
            rejectsWrongShape(CaptureContactsPayload.serializer(), "text", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(ObserveToolStat.serializer(), "name", value)

            assertEquals(value, encoded["name"])
        },
        {
            rejectsWrongShape(ObserveToolStat.serializer(), "name", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive(Int.MAX_VALUE)
            val encoded = roundTrip(ObserveToolStat.serializer(), "calls", value)

            assertEquals(value, encoded["calls"])
        },
        {
            rejectsWrongShape(ObserveToolStat.serializer(), "calls", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Int.MAX_VALUE)
            val encoded = roundTrip(ObserveToolStat.serializer(), "errors", value)

            assertEquals(value, encoded["errors"])
        },
        {
            rejectsWrongShape(ObserveToolStat.serializer(), "errors", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Long.MAX_VALUE)
            val encoded = roundTrip(ObserveToolStat.serializer(), "avgMs", value)

            assertEquals(value, encoded["avgMs"])
        },
        {
            rejectsWrongShape(ObserveToolStat.serializer(), "avgMs", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Int.MAX_VALUE)
            val encoded = roundTrip(ObserveToolStat.serializer(), "repaired", value)

            assertEquals(value, encoded["repaired"])
        },
        {
            rejectsWrongShape(ObserveToolStat.serializer(), "repaired", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Int.MAX_VALUE)
            val encoded = roundTrip(ObserveToolStat.serializer(), "unknown", value)

            assertEquals(value, encoded["unknown"])
        },
        {
            rejectsWrongShape(ObserveToolStat.serializer(), "unknown", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Int.MAX_VALUE)
            val encoded = roundTrip(ObserveToolStat.serializer(), "blocked", value)

            assertEquals(value, encoded["blocked"])
        },
        {
            rejectsWrongShape(ObserveToolStat.serializer(), "blocked", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Int.MAX_VALUE)
            val encoded = roundTrip(ObserveToolStat.serializer(), "cacheHits", value)

            assertEquals(value, encoded["cacheHits"])
        },
        {
            rejectsWrongShape(ObserveToolStat.serializer(), "cacheHits", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Int.MAX_VALUE)
            val encoded = roundTrip(ObserveToolStat.serializer(), "truncated", value)

            assertEquals(value, encoded["truncated"])
        },
        {
            rejectsWrongShape(ObserveToolStat.serializer(), "truncated", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Int.MAX_VALUE)
            val encoded = roundTrip(ObserveBehavior.serializer(), "runs", value)

            assertEquals(value, encoded["runs"])
        },
        {
            rejectsWrongShape(ObserveBehavior.serializer(), "runs", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Int.MAX_VALUE)
            val encoded = roundTrip(ObserveBehavior.serializer(), "proactiveRuns", value)

            assertEquals(value, encoded["proactiveRuns"])
        },
        {
            rejectsWrongShape(ObserveBehavior.serializer(), "proactiveRuns", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Int.MAX_VALUE)
            val encoded = roundTrip(ObserveBehavior.serializer(), "compactedRuns", value)

            assertEquals(value, encoded["compactedRuns"])
        },
        {
            rejectsWrongShape(ObserveBehavior.serializer(), "compactedRuns", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Long.MAX_VALUE)
            val encoded = roundTrip(ObserveBehavior.serializer(), "totalInputTokens", value)

            assertEquals(value, encoded["totalInputTokens"])
        },
        {
            rejectsWrongShape(ObserveBehavior.serializer(), "totalInputTokens", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Long.MAX_VALUE)
            val encoded = roundTrip(ObserveBehavior.serializer(), "totalOutputTokens", value)

            assertEquals(value, encoded["totalOutputTokens"])
        },
        {
            rejectsWrongShape(ObserveBehavior.serializer(), "totalOutputTokens", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Long.MAX_VALUE)
            val encoded = roundTrip(ObserveBehavior.serializer(), "cacheReadTokens", value)

            assertEquals(value, encoded["cacheReadTokens"])
        },
        {
            rejectsWrongShape(ObserveBehavior.serializer(), "cacheReadTokens", JsonPrimitive("not-a-number"))
        },
        {
            val value = buildJsonArray {
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
            }
            val encoded = roundTrip(ObserveBehavior.serializer(), "tools", value)
            val encodedValues = encoded.getValue("tools").jsonArray

            assertEquals(3, encodedValues.size)
            encodedValues.forEach { assertIs<JsonObject>(it) }
        },
        {
            rejectsWrongShape(ObserveBehavior.serializer(), "tools", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = JsonObject(mapOf("delivered" to JsonPrimitive(Int.MAX_VALUE), "zero" to JsonPrimitive(0)))
            val encoded = roundTrip(ObserveBehavior.serializer(), "proactiveDecisions", value)

            assertEquals(value, encoded["proactiveDecisions"])
        },
        {
            rejectsWrongShape(ObserveBehavior.serializer(), "proactiveDecisions", JsonPrimitive("not-an-object"))
        },
        {
            val value = JsonObject(mapOf("gmailpoll" to JsonPrimitive(Int.MAX_VALUE), "zero" to JsonPrimitive(0)))
            val encoded = roundTrip(ObserveBehavior.serializer(), "backgroundJobs", value)

            assertEquals(value, encoded["backgroundJobs"])
        },
        {
            rejectsWrongShape(ObserveBehavior.serializer(), "backgroundJobs", JsonPrimitive("not-an-object"))
        },
        {
            val value = JsonObject(mapOf("최대" to JsonPrimitive(Int.MAX_VALUE), "zero" to JsonPrimitive(0)))
            val encoded = roundTrip(ObserveBehavior.serializer(), "backgroundErrors", value)

            assertEquals(value, encoded["backgroundErrors"])
        },
        {
            rejectsWrongShape(ObserveBehavior.serializer(), "backgroundErrors", JsonPrimitive("not-an-object"))
        },
        {
            val value = JsonPrimitive(Long.MAX_VALUE)
            val encoded = roundTrip(ObserveLogLine.serializer(), "ts", value)

            assertEquals(value, encoded["ts"])
        },
        {
            rejectsWrongShape(ObserveLogLine.serializer(), "ts", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(ObserveLogLine.serializer(), "level", value)

            assertEquals(value, encoded["level"])
        },
        {
            rejectsWrongShape(ObserveLogLine.serializer(), "level", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(ObserveLogLine.serializer(), "msg", value)

            assertEquals(value, encoded["msg"])
        },
        {
            rejectsWrongShape(ObserveLogLine.serializer(), "msg", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(ObserveLogLine.serializer(), "runId", value)

            assertEquals(value, encoded["runId"])
        },
        {
            rejectsWrongShape(ObserveLogLine.serializer(), "runId", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(ObserveLogLine.serializer(), "session", value)

            assertEquals(value, encoded["session"])
        },
        {
            rejectsWrongShape(ObserveLogLine.serializer(), "session", JsonObject(emptyMap()))
        },
        {
            val value = buildJsonArray {
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
            }
            val encoded = roundTrip(ObserveLogsPayload.serializer(), "lines", value)
            val encodedValues = encoded.getValue("lines").jsonArray

            assertEquals(3, encodedValues.size)
            encodedValues.forEach { assertIs<JsonObject>(it) }
        },
        {
            rejectsWrongShape(ObserveLogsPayload.serializer(), "lines", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
        {
            val value = JsonPrimitive(Int.MAX_VALUE)
            val encoded = roundTrip(ObserveLogsPayload.serializer(), "count", value)

            assertEquals(value, encoded["count"])
        },
        {
            rejectsWrongShape(ObserveLogsPayload.serializer(), "count", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
            val encoded = roundTrip(ObserveVllmPrefixCache.serializer(), "model", value)

            assertEquals(value, encoded["model"])
        },
        {
            rejectsWrongShape(ObserveVllmPrefixCache.serializer(), "model", JsonObject(emptyMap()))
        },
        {
            val value = JsonPrimitive(Long.MAX_VALUE)
            val encoded = roundTrip(ObserveVllmPrefixCache.serializer(), "queries", value)

            assertEquals(value, encoded["queries"])
        },
        {
            rejectsWrongShape(ObserveVllmPrefixCache.serializer(), "queries", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Long.MAX_VALUE)
            val encoded = roundTrip(ObserveVllmPrefixCache.serializer(), "hits", value)

            assertEquals(value, encoded["hits"])
        },
        {
            rejectsWrongShape(ObserveVllmPrefixCache.serializer(), "hits", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(37.5)
            val encoded = roundTrip(ObserveVllmPrefixCache.serializer(), "hitRatePct", value)

            assertEquals(value, encoded["hitRatePct"])
        },
        {
            rejectsWrongShape(ObserveVllmPrefixCache.serializer(), "hitRatePct", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(true)
            val encoded = roundTrip(ObserveHealth.serializer(), "captureEnabled", value)

            assertEquals(value, encoded["captureEnabled"])
        },
        {
            rejectsWrongShape(ObserveHealth.serializer(), "captureEnabled", JsonPrimitive(1))
        },
        {
            val value = JsonPrimitive(true)
            val encoded = roundTrip(ObserveHealth.serializer(), "agentLogEnabled", value)

            assertEquals(value, encoded["agentLogEnabled"])
        },
        {
            rejectsWrongShape(ObserveHealth.serializer(), "agentLogEnabled", JsonPrimitive(1))
        },
        {
            val value = JsonPrimitive(Int.MAX_VALUE)
            val encoded = roundTrip(ObserveHealth.serializer(), "ringCapacity", value)

            assertEquals(value, encoded["ringCapacity"])
        },
        {
            rejectsWrongShape(ObserveHealth.serializer(), "ringCapacity", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Int.MAX_VALUE)
            val encoded = roundTrip(ObserveHealth.serializer(), "ringUsed", value)

            assertEquals(value, encoded["ringUsed"])
        },
        {
            rejectsWrongShape(ObserveHealth.serializer(), "ringUsed", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Int.MAX_VALUE)
            val encoded = roundTrip(ObserveHealth.serializer(), "recentErrors", value)

            assertEquals(value, encoded["recentErrors"])
        },
        {
            rejectsWrongShape(ObserveHealth.serializer(), "recentErrors", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Int.MAX_VALUE)
            val encoded = roundTrip(ObserveHealth.serializer(), "runs24h", value)

            assertEquals(value, encoded["runs24h"])
        },
        {
            rejectsWrongShape(ObserveHealth.serializer(), "runs24h", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Int.MAX_VALUE)
            val encoded = roundTrip(ObserveHealth.serializer(), "proactiveRuns24h", value)

            assertEquals(value, encoded["proactiveRuns24h"])
        },
        {
            rejectsWrongShape(ObserveHealth.serializer(), "proactiveRuns24h", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Int.MAX_VALUE)
            val encoded = roundTrip(ObserveHealth.serializer(), "compactedRuns24h", value)

            assertEquals(value, encoded["compactedRuns24h"])
        },
        {
            rejectsWrongShape(ObserveHealth.serializer(), "compactedRuns24h", JsonPrimitive("not-a-number"))
        },
        {
            val value = JsonPrimitive(Int.MAX_VALUE)
            val encoded = roundTrip(ObserveHealth.serializer(), "backgroundErrors24h", value)

            assertEquals(value, encoded["backgroundErrors24h"])
        },
        {
            rejectsWrongShape(ObserveHealth.serializer(), "backgroundErrors24h", JsonPrimitive("not-a-number"))
        },
        {
            val value = buildJsonArray {
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
                add(JsonObject(emptyMap()))
            }
            val encoded = roundTrip(ObserveHealth.serializer(), "vllmPrefixCache", value)
            val encodedValues = encoded.getValue("vllmPrefixCache").jsonArray

            assertEquals(3, encodedValues.size)
            encodedValues.forEach { assertIs<JsonObject>(it) }
        },
        {
            rejectsWrongShape(ObserveHealth.serializer(), "vllmPrefixCache", JsonObject(mapOf("not" to JsonPrimitive("a-list"))))
        },
    )

    @Test
    fun wireFieldsPreserveBoundaryValuesAndRejectIncompatibleShapes() {
        fieldBoundaryCases().forEach { it() }
    }
}
