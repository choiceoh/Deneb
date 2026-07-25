package ai.deneb.deneb

import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.jsonObject
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

/**
 * Present-field and malformed-shape coverage for the hand-written RPC envelopes.
 *
 * These types are maintained beside the client instead of generated, so this suite
 * ensures new fields keep backward-compatible defaults while non-default gateway
 * data, nested actions/events, maps, and large cursors survive decoding intact.
 */
class DenebWireValueContractTest {
    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
        encodeDefaults = true
    }

    /**
     * Every key present on the wire must survive the round-trip.
     *
     * The encoded output may carry ADDITIONAL keys: `encodeDefaults = true` emits
     * every field, so adding a new field with a default — the backward-compatible
     * way to extend an envelope, and exactly what this suite exists to protect —
     * legitimately widens the output. Asserting exact key equality made the suite
     * fail on its own stated contract the first time that happened
     * (`TranscriptPayload.turnRunning`, #4188).
     *
     * The load-bearing half is kept: with `ignoreUnknownKeys = true` a wire field
     * the Kotlin type forgot to declare is dropped on decode, and that still fails.
     */
    private fun assertKeysPreserved(input: JsonObject, encoded: JsonObject) {
        val dropped = input.keys - encoded.keys
        assertTrue(dropped.isEmpty(), "round-trip dropped wire keys: $dropped")
    }

    private fun wireValueContractCases(): List<() -> Unit> = listOf(
        {
            val input = json.parseToJsonElement(
                """{
                    "sessions": [
                        {}
                    ]
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(RecentPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(RecentPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertTrue(encoded["sessions"] is JsonArray)
            assertEquals(1, (encoded["sessions"] as JsonArray).size)

            assertEquals(
                decoded,
                json.decodeFromJsonElement(RecentPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    RecentPayload.serializer(),
                    json.parseToJsonElement("""{"sessions":"wrong-shape"}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "messages": [
                        {}
                    ]
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(TranscriptPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(TranscriptPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertTrue(encoded["messages"] is JsonArray)
            assertEquals(1, (encoded["messages"] as JsonArray).size)

            assertEquals(
                decoded,
                json.decodeFromJsonElement(TranscriptPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    TranscriptPayload.serializer(),
                    json.parseToJsonElement("""{"messages":"wrong-shape"}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "items": [
                        {}
                    ]
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(WorkFeedPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(WorkFeedPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertTrue(encoded["items"] is JsonArray)
            assertEquals(1, (encoded["items"] as JsonArray).size)

            assertEquals(
                decoded,
                json.decodeFromJsonElement(WorkFeedPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    WorkFeedPayload.serializer(),
                    json.parseToJsonElement("""{"items":"wrong-shape"}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "ok": true,
                    "item": {},
                    "sessionKey": "sessionKey-value",
                    "prompt": "prompt-value",
                    "message": "message-value",
                    "removeFromFeed": true
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(WorkFeedActionRunPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(WorkFeedActionRunPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertEquals(input["ok"], encoded["ok"])
            assertTrue(encoded["item"] is JsonObject)
            assertEquals(input["sessionKey"], encoded["sessionKey"])
            assertEquals(input["prompt"], encoded["prompt"])
            assertEquals(input["message"], encoded["message"])
            assertEquals(input["removeFromFeed"], encoded["removeFromFeed"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(WorkFeedActionRunPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    WorkFeedActionRunPayload.serializer(),
                    json.parseToJsonElement("""{"ok":{}}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "ok": true,
                    "item": {},
                    "text": "text-value",
                    "sessionKey": "sessionKey-value"
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(WorkFeedFeedbackPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(WorkFeedFeedbackPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertEquals(input["ok"], encoded["ok"])
            assertTrue(encoded["item"] is JsonObject)
            assertEquals(input["text"], encoded["text"])
            assertEquals(input["sessionKey"], encoded["sessionKey"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(WorkFeedFeedbackPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    WorkFeedFeedbackPayload.serializer(),
                    json.parseToJsonElement("""{"ok":{}}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "events": [
                        {}
                    ],
                    "cursor": 7000000000,
                    "latestSeq": 7000000000,
                    "hasMore": true
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(NativeSyncPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(NativeSyncPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertTrue(encoded["events"] is JsonArray)
            assertEquals(1, (encoded["events"] as JsonArray).size)
            assertEquals(input["cursor"], encoded["cursor"])
            assertEquals(input["latestSeq"], encoded["latestSeq"])
            assertEquals(input["hasMore"], encoded["hasMore"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(NativeSyncPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    NativeSyncPayload.serializer(),
                    json.parseToJsonElement("""{"events":"wrong-shape"}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "seq": 7000000000,
                    "type": "type-value",
                    "entityId": "entityId-value",
                    "sessionKey": "sessionKey-value",
                    "workFeedItemId": "workFeedItemId-value",
                    "timestampMs": 7000000000,
                    "payload": {
                        "sample": "value"
                    }
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(NativeSyncEvent.serializer(), input)
            val encoded = json.encodeToJsonElement(NativeSyncEvent.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertEquals(input["seq"], encoded["seq"])
            assertEquals(input["type"], encoded["type"])
            assertEquals(input["entityId"], encoded["entityId"])
            assertEquals(input["sessionKey"], encoded["sessionKey"])
            assertEquals(input["workFeedItemId"], encoded["workFeedItemId"])
            assertEquals(input["timestampMs"], encoded["timestampMs"])
            assertTrue(encoded["payload"] is JsonObject)

            assertEquals(
                decoded,
                json.decodeFromJsonElement(NativeSyncEvent.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    NativeSyncEvent.serializer(),
                    json.parseToJsonElement("""{"seq":{}}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "item": {},
                    "removeFromFeed": true
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(NativeSyncActionPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(NativeSyncActionPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertTrue(encoded["item"] is JsonObject)
            assertEquals(input["removeFromFeed"], encoded["removeFromFeed"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(NativeSyncActionPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    NativeSyncActionPayload.serializer(),
                    json.parseToJsonElement("""{"item":"wrong-shape"}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "pages": [
                        {}
                    ]
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(MemoryListPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(MemoryListPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertTrue(encoded["pages"] is JsonArray)
            assertEquals(1, (encoded["pages"] as JsonArray).size)

            assertEquals(
                decoded,
                json.decodeFromJsonElement(MemoryListPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    MemoryListPayload.serializer(),
                    json.parseToJsonElement("""{"pages":"wrong-shape"}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "entries": [
                        {}
                    ]
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(DiaryRecentPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(DiaryRecentPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertTrue(encoded["entries"] is JsonArray)
            assertEquals(1, (encoded["entries"] as JsonArray).size)

            assertEquals(
                decoded,
                json.decodeFromJsonElement(DiaryRecentPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    DiaryRecentPayload.serializer(),
                    json.parseToJsonElement("""{"entries":"wrong-shape"}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "file": "file-value",
                    "header": "header-value",
                    "content": "content-value",
                    "at": 7000000000
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(DiaryRecentRow.serializer(), input)
            val encoded = json.encodeToJsonElement(DiaryRecentRow.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertEquals(input["file"], encoded["file"])
            assertEquals(input["header"], encoded["header"])
            assertEquals(input["content"], encoded["content"])
            assertEquals(input["at"], encoded["at"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(DiaryRecentRow.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    DiaryRecentRow.serializer(),
                    json.parseToJsonElement("""{"file":{}}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "ok": true,
                    "deleted": 7
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(DeletePagesPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(DeletePagesPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertEquals(input["ok"], encoded["ok"])
            assertEquals(input["deleted"], encoded["deleted"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(DeletePagesPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    DeletePagesPayload.serializer(),
                    json.parseToJsonElement("""{"ok":{}}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "ok": true
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(MovePagePayload.serializer(), input)
            val encoded = json.encodeToJsonElement(MovePagePayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertEquals(input["ok"], encoded["ok"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(MovePagePayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    MovePagePayload.serializer(),
                    json.parseToJsonElement("""{"ok":{}}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "categories": [
                        {}
                    ],
                    "totalPages": 7,
                    "totalBytes": 7000000000
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(CategoriesPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(CategoriesPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertTrue(encoded["categories"] is JsonArray)
            assertEquals(1, (encoded["categories"] as JsonArray).size)
            assertEquals(input["totalPages"], encoded["totalPages"])
            assertEquals(input["totalBytes"], encoded["totalBytes"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(CategoriesPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    CategoriesPayload.serializer(),
                    json.parseToJsonElement("""{"categories":"wrong-shape"}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "jobs": [
                        {}
                    ]
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(CronListPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(CronListPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertTrue(encoded["jobs"] is JsonArray)
            assertEquals(1, (encoded["jobs"] as JsonArray).size)

            assertEquals(
                decoded,
                json.decodeFromJsonElement(CronListPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    CronListPayload.serializer(),
                    json.parseToJsonElement("""{"jobs":"wrong-shape"}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "current": "current-value",
                    "roles": [
                        {}
                    ],
                    "sections": [
                        {}
                    ],
                    "advisories": [
                        "advisoriesItem-value"
                    ],
                    "mainHasVision": true
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(ModelsPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(ModelsPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertEquals(input["current"], encoded["current"])
            assertTrue(encoded["roles"] is JsonArray)
            assertEquals(1, (encoded["roles"] as JsonArray).size)
            assertTrue(encoded["sections"] is JsonArray)
            assertEquals(1, (encoded["sections"] as JsonArray).size)
            assertEquals(input["advisories"], encoded["advisories"])
            assertEquals(input["mainHasVision"], encoded["mainHasVision"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(ModelsPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    ModelsPayload.serializer(),
                    json.parseToJsonElement("""{"current":{}}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "version": "version-value",
                    "nativeApiVersion": 7,
                    "model": "model-value",
                    "capabilities": {
                        "sample-key": true
                    },
                    "endpoints": {
                        "sample-key": "endpointsValue-value"
                    },
                    "tsMs": 7000000000
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(ClientHelloPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(ClientHelloPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertEquals(input["version"], encoded["version"])
            assertEquals(input["nativeApiVersion"], encoded["nativeApiVersion"])
            assertEquals(input["model"], encoded["model"])
            assertEquals(input["capabilities"], encoded["capabilities"])
            assertEquals(input["endpoints"], encoded["endpoints"])
            assertEquals(input["tsMs"], encoded["tsMs"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(ClientHelloPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    ClientHelloPayload.serializer(),
                    json.parseToJsonElement("""{"version":{}}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "messages": [
                        {}
                    ],
                    "nextPageToken": "nextPageToken-value"
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(MailListPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(MailListPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertTrue(encoded["messages"] is JsonArray)
            assertEquals(1, (encoded["messages"] as JsonArray).size)
            assertEquals(input["nextPageToken"], encoded["nextPageToken"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(MailListPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    MailListPayload.serializer(),
                    json.parseToJsonElement("""{"messages":"wrong-shape"}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "ok": true
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(OkPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(OkPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertEquals(input["ok"], encoded["ok"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(OkPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    OkPayload.serializer(),
                    json.parseToJsonElement("""{"ok":{}}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "answer": "answer-value"
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(AskPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(AskPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertEquals(input["answer"], encoded["answer"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(AskPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    AskPayload.serializer(),
                    json.parseToJsonElement("""{"answer":{}}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "sender": "sender-value",
                    "email": "email-value",
                    "displayName": "displayName-value",
                    "recent": {},
                    "wikiHits": [
                        {}
                    ],
                    "wikiFacts": "wikiFacts-value"
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(SenderContextPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(SenderContextPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertEquals(input["sender"], encoded["sender"])
            assertEquals(input["email"], encoded["email"])
            assertEquals(input["displayName"], encoded["displayName"])
            assertTrue(encoded["recent"] is JsonObject)
            assertTrue(encoded["wikiHits"] is JsonArray)
            assertEquals(1, (encoded["wikiHits"] as JsonArray).size)
            assertEquals(input["wikiFacts"], encoded["wikiFacts"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(SenderContextPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    SenderContextPayload.serializer(),
                    json.parseToJsonElement("""{"sender":{}}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "events": [
                        {}
                    ]
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(CalListPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(CalListPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertTrue(encoded["events"] is JsonArray)
            assertEquals(1, (encoded["events"] as JsonArray).size)

            assertEquals(
                decoded,
                json.decodeFromJsonElement(CalListPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    CalListPayload.serializer(),
                    json.parseToJsonElement("""{"events":"wrong-shape"}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "proposals": [
                        {}
                    ]
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(CalProposalsPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(CalProposalsPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertTrue(encoded["proposals"] is JsonArray)
            assertEquals(1, (encoded["proposals"] as JsonArray).size)

            assertEquals(
                decoded,
                json.decodeFromJsonElement(CalProposalsPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    CalProposalsPayload.serializer(),
                    json.parseToJsonElement("""{"proposals":"wrong-shape"}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "todos": [
                        {}
                    ]
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(TodoListPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(TodoListPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertTrue(encoded["todos"] is JsonArray)
            assertEquals(1, (encoded["todos"] as JsonArray).size)

            assertEquals(
                decoded,
                json.decodeFromJsonElement(TodoListPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    TodoListPayload.serializer(),
                    json.parseToJsonElement("""{"todos":"wrong-shape"}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "people": [
                        {}
                    ]
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(PeopleListPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(PeopleListPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertTrue(encoded["people"] is JsonArray)
            assertEquals(1, (encoded["people"] as JsonArray).size)

            assertEquals(
                decoded,
                json.decodeFromJsonElement(PeopleListPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    PeopleListPayload.serializer(),
                    json.parseToJsonElement("""{"people":"wrong-shape"}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "contacts": [
                        {}
                    ]
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(ContactsListPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(ContactsListPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertTrue(encoded["contacts"] is JsonArray)
            assertEquals(1, (encoded["contacts"] as JsonArray).size)

            assertEquals(
                decoded,
                json.decodeFromJsonElement(ContactsListPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    ContactsListPayload.serializer(),
                    json.parseToJsonElement("""{"contacts":"wrong-shape"}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "path": "path-value",
                    "title": "title-value",
                    "summary": "summary-value",
                    "category": "category-value",
                    "code": "code-value",
                    "tags": [
                        "tagsItem-value"
                    ],
                    "related": [
                        "relatedItem-value"
                    ],
                    "updated": "updated-value",
                    "body": "body-value"
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(WikiPagePayload.serializer(), input)
            val encoded = json.encodeToJsonElement(WikiPagePayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertEquals(input["path"], encoded["path"])
            assertEquals(input["title"], encoded["title"])
            assertEquals(input["summary"], encoded["summary"])
            assertEquals(input["category"], encoded["category"])
            assertEquals(input["code"], encoded["code"])
            assertEquals(input["tags"], encoded["tags"])
            assertEquals(input["related"], encoded["related"])
            assertEquals(input["updated"], encoded["updated"])
            assertEquals(input["body"], encoded["body"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(WikiPagePayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    WikiPagePayload.serializer(),
                    json.parseToJsonElement("""{"path":{}}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "text": "text-value"
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(CaptureImagePayload.serializer(), input)
            val encoded = json.encodeToJsonElement(CaptureImagePayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertEquals(input["text"], encoded["text"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(CaptureImagePayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    CaptureImagePayload.serializer(),
                    json.parseToJsonElement("""{"text":{}}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "text": "text-value"
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(CaptureAudioPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(CaptureAudioPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertEquals(input["text"], encoded["text"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(CaptureAudioPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    CaptureAudioPayload.serializer(),
                    json.parseToJsonElement("""{"text":{}}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "text": "text-value"
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(CaptureDocumentPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(CaptureDocumentPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertEquals(input["text"], encoded["text"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(CaptureDocumentPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    CaptureDocumentPayload.serializer(),
                    json.parseToJsonElement("""{"text":{}}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "text": "text-value"
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(CaptureContactsPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(CaptureContactsPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertEquals(input["text"], encoded["text"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(CaptureContactsPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    CaptureContactsPayload.serializer(),
                    json.parseToJsonElement("""{"text":{}}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "name": "name-value",
                    "calls": 7,
                    "errors": 7,
                    "avgMs": 7000000000,
                    "repaired": 7,
                    "unknown": 7,
                    "blocked": 7,
                    "cacheHits": 7,
                    "truncated": 7
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(ObserveToolStat.serializer(), input)
            val encoded = json.encodeToJsonElement(ObserveToolStat.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertEquals(input["name"], encoded["name"])
            assertEquals(input["calls"], encoded["calls"])
            assertEquals(input["errors"], encoded["errors"])
            assertEquals(input["avgMs"], encoded["avgMs"])
            assertEquals(input["repaired"], encoded["repaired"])
            assertEquals(input["unknown"], encoded["unknown"])
            assertEquals(input["blocked"], encoded["blocked"])
            assertEquals(input["cacheHits"], encoded["cacheHits"])
            assertEquals(input["truncated"], encoded["truncated"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(ObserveToolStat.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    ObserveToolStat.serializer(),
                    json.parseToJsonElement("""{"name":{}}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "runs": 7,
                    "proactiveRuns": 7,
                    "compactedRuns": 7,
                    "totalInputTokens": 7000000000,
                    "totalOutputTokens": 7000000000,
                    "cacheReadTokens": 7000000000,
                    "tools": [
                        {}
                    ],
                    "proactiveDecisions": {
                        "delivered": 7
                    },
                    "backgroundJobs": {
                        "gmailpoll": 7
                    },
                    "backgroundErrors": {
                        "sample-key": 7
                    }
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(ObserveBehavior.serializer(), input)
            val encoded = json.encodeToJsonElement(ObserveBehavior.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertEquals(input["runs"], encoded["runs"])
            assertEquals(input["proactiveRuns"], encoded["proactiveRuns"])
            assertEquals(input["compactedRuns"], encoded["compactedRuns"])
            assertEquals(input["totalInputTokens"], encoded["totalInputTokens"])
            assertEquals(input["totalOutputTokens"], encoded["totalOutputTokens"])
            assertEquals(input["cacheReadTokens"], encoded["cacheReadTokens"])
            assertTrue(encoded["tools"] is JsonArray)
            assertEquals(1, (encoded["tools"] as JsonArray).size)
            assertEquals(input["proactiveDecisions"], encoded["proactiveDecisions"])
            assertEquals(input["backgroundJobs"], encoded["backgroundJobs"])
            assertEquals(input["backgroundErrors"], encoded["backgroundErrors"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(ObserveBehavior.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    ObserveBehavior.serializer(),
                    json.parseToJsonElement("""{"runs":{}}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "ts": 7000000000,
                    "level": "level-value",
                    "msg": "msg-value",
                    "runId": "runId-value",
                    "session": "session-value"
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(ObserveLogLine.serializer(), input)
            val encoded = json.encodeToJsonElement(ObserveLogLine.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertEquals(input["ts"], encoded["ts"])
            assertEquals(input["level"], encoded["level"])
            assertEquals(input["msg"], encoded["msg"])
            assertEquals(input["runId"], encoded["runId"])
            assertEquals(input["session"], encoded["session"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(ObserveLogLine.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    ObserveLogLine.serializer(),
                    json.parseToJsonElement("""{"level":{}}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "lines": [
                        {}
                    ],
                    "count": 7
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(ObserveLogsPayload.serializer(), input)
            val encoded = json.encodeToJsonElement(ObserveLogsPayload.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertTrue(encoded["lines"] is JsonArray)
            assertEquals(1, (encoded["lines"] as JsonArray).size)
            assertEquals(input["count"], encoded["count"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(ObserveLogsPayload.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    ObserveLogsPayload.serializer(),
                    json.parseToJsonElement("""{"lines":"wrong-shape"}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "model": "model-value",
                    "queries": 7000000000,
                    "hits": 7000000000,
                    "hitRatePct": 37.5
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(ObserveVllmPrefixCache.serializer(), input)
            val encoded = json.encodeToJsonElement(ObserveVllmPrefixCache.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertEquals(input["model"], encoded["model"])
            assertEquals(input["queries"], encoded["queries"])
            assertEquals(input["hits"], encoded["hits"])
            assertEquals(input["hitRatePct"], encoded["hitRatePct"])

            assertEquals(
                decoded,
                json.decodeFromJsonElement(ObserveVllmPrefixCache.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    ObserveVllmPrefixCache.serializer(),
                    json.parseToJsonElement("""{"model":{}}"""),
                )
            }
        },
        {
            val input = json.parseToJsonElement(
                """{
                    "captureEnabled": true,
                    "agentLogEnabled": true,
                    "ringCapacity": 7,
                    "ringUsed": 7,
                    "recentErrors": 7,
                    "runs24h": 7,
                    "proactiveRuns24h": 7,
                    "compactedRuns24h": 7,
                    "backgroundErrors24h": 7,
                    "vllmPrefixCache": [
                        {}
                    ]
                }
                """.trimIndent(),
            ).jsonObject

            val decoded = json.decodeFromJsonElement(ObserveHealth.serializer(), input)
            val encoded = json.encodeToJsonElement(ObserveHealth.serializer(), decoded).jsonObject

            assertKeysPreserved(input, encoded)
            assertEquals(input["captureEnabled"], encoded["captureEnabled"])
            assertEquals(input["agentLogEnabled"], encoded["agentLogEnabled"])
            assertEquals(input["ringCapacity"], encoded["ringCapacity"])
            assertEquals(input["ringUsed"], encoded["ringUsed"])
            assertEquals(input["recentErrors"], encoded["recentErrors"])
            assertEquals(input["runs24h"], encoded["runs24h"])
            assertEquals(input["proactiveRuns24h"], encoded["proactiveRuns24h"])
            assertEquals(input["compactedRuns24h"], encoded["compactedRuns24h"])
            assertEquals(input["backgroundErrors24h"], encoded["backgroundErrors24h"])
            assertTrue(encoded["vllmPrefixCache"] is JsonArray)
            assertEquals(1, (encoded["vllmPrefixCache"] as JsonArray).size)

            assertEquals(
                decoded,
                json.decodeFromJsonElement(ObserveHealth.serializer(), encoded),
            )
        },
        {
            assertFailsWith<SerializationException> {
                json.decodeFromJsonElement(
                    ObserveHealth.serializer(),
                    json.parseToJsonElement("""{"captureEnabled":1}"""),
                )
            }
        },
    )

    @Test
    fun wirePayloadsPreserveFieldsAndRejectWrongShapes() {
        wireValueContractCases().forEach { it() }
    }
}
