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

    @Test
    fun recentPayloadPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["sessions"] is JsonArray)
        assertEquals(1, (encoded["sessions"] as JsonArray).size)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(RecentPayload.serializer(), encoded),
        )
    }

    @Test
    fun recentPayloadRejectsWrongShapeForSessions() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                RecentPayload.serializer(),
                json.parseToJsonElement("""{"sessions":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun transcriptPayloadPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["messages"] is JsonArray)
        assertEquals(1, (encoded["messages"] as JsonArray).size)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(TranscriptPayload.serializer(), encoded),
        )
    }

    @Test
    fun transcriptPayloadRejectsWrongShapeForMessages() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                TranscriptPayload.serializer(),
                json.parseToJsonElement("""{"messages":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun workFeedPayloadPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["items"] is JsonArray)
        assertEquals(1, (encoded["items"] as JsonArray).size)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(WorkFeedPayload.serializer(), encoded),
        )
    }

    @Test
    fun workFeedPayloadRejectsWrongShapeForItems() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                WorkFeedPayload.serializer(),
                json.parseToJsonElement("""{"items":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun workFeedActionRunPayloadPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
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
    }

    @Test
    fun workFeedActionRunPayloadRejectsWrongShapeForOk() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                WorkFeedActionRunPayload.serializer(),
                json.parseToJsonElement("""{"ok":{}}"""),
            )
        }
    }

    @Test
    fun workFeedFeedbackPayloadPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["ok"], encoded["ok"])
        assertTrue(encoded["item"] is JsonObject)
        assertEquals(input["text"], encoded["text"])
        assertEquals(input["sessionKey"], encoded["sessionKey"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(WorkFeedFeedbackPayload.serializer(), encoded),
        )
    }

    @Test
    fun workFeedFeedbackPayloadRejectsWrongShapeForOk() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                WorkFeedFeedbackPayload.serializer(),
                json.parseToJsonElement("""{"ok":{}}"""),
            )
        }
    }

    @Test
    fun nativeSyncPayloadPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["events"] is JsonArray)
        assertEquals(1, (encoded["events"] as JsonArray).size)
        assertEquals(input["cursor"], encoded["cursor"])
        assertEquals(input["latestSeq"], encoded["latestSeq"])
        assertEquals(input["hasMore"], encoded["hasMore"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(NativeSyncPayload.serializer(), encoded),
        )
    }

    @Test
    fun nativeSyncPayloadRejectsWrongShapeForEvents() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                NativeSyncPayload.serializer(),
                json.parseToJsonElement("""{"events":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun nativeSyncEventPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
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
    }

    @Test
    fun nativeSyncEventRejectsWrongShapeForSeq() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                NativeSyncEvent.serializer(),
                json.parseToJsonElement("""{"seq":{}}"""),
            )
        }
    }

    @Test
    fun nativeSyncActionPayloadPreservesEveryPresentEnvelopeField() {
        val input = json.parseToJsonElement(
            """{
                "item": {},
                "removeFromFeed": true
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(NativeSyncActionPayload.serializer(), input)
        val encoded = json.encodeToJsonElement(NativeSyncActionPayload.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["item"] is JsonObject)
        assertEquals(input["removeFromFeed"], encoded["removeFromFeed"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(NativeSyncActionPayload.serializer(), encoded),
        )
    }

    @Test
    fun nativeSyncActionPayloadRejectsWrongShapeForItem() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                NativeSyncActionPayload.serializer(),
                json.parseToJsonElement("""{"item":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun memoryListPayloadPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["pages"] is JsonArray)
        assertEquals(1, (encoded["pages"] as JsonArray).size)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(MemoryListPayload.serializer(), encoded),
        )
    }

    @Test
    fun memoryListPayloadRejectsWrongShapeForPages() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                MemoryListPayload.serializer(),
                json.parseToJsonElement("""{"pages":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun diaryRecentPayloadPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["entries"] is JsonArray)
        assertEquals(1, (encoded["entries"] as JsonArray).size)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(DiaryRecentPayload.serializer(), encoded),
        )
    }

    @Test
    fun diaryRecentPayloadRejectsWrongShapeForEntries() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                DiaryRecentPayload.serializer(),
                json.parseToJsonElement("""{"entries":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun diaryRecentRowPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["file"], encoded["file"])
        assertEquals(input["header"], encoded["header"])
        assertEquals(input["content"], encoded["content"])
        assertEquals(input["at"], encoded["at"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(DiaryRecentRow.serializer(), encoded),
        )
    }

    @Test
    fun diaryRecentRowRejectsWrongShapeForFile() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                DiaryRecentRow.serializer(),
                json.parseToJsonElement("""{"file":{}}"""),
            )
        }
    }

    @Test
    fun deletePagesPayloadPreservesEveryPresentEnvelopeField() {
        val input = json.parseToJsonElement(
            """{
                "ok": true,
                "deleted": 7
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(DeletePagesPayload.serializer(), input)
        val encoded = json.encodeToJsonElement(DeletePagesPayload.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["ok"], encoded["ok"])
        assertEquals(input["deleted"], encoded["deleted"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(DeletePagesPayload.serializer(), encoded),
        )
    }

    @Test
    fun deletePagesPayloadRejectsWrongShapeForOk() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                DeletePagesPayload.serializer(),
                json.parseToJsonElement("""{"ok":{}}"""),
            )
        }
    }

    @Test
    fun movePagePayloadPreservesEveryPresentEnvelopeField() {
        val input = json.parseToJsonElement(
            """{
                "ok": true
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(MovePagePayload.serializer(), input)
        val encoded = json.encodeToJsonElement(MovePagePayload.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["ok"], encoded["ok"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(MovePagePayload.serializer(), encoded),
        )
    }

    @Test
    fun movePagePayloadRejectsWrongShapeForOk() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                MovePagePayload.serializer(),
                json.parseToJsonElement("""{"ok":{}}"""),
            )
        }
    }

    @Test
    fun categoriesPayloadPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["categories"] is JsonArray)
        assertEquals(1, (encoded["categories"] as JsonArray).size)
        assertEquals(input["totalPages"], encoded["totalPages"])
        assertEquals(input["totalBytes"], encoded["totalBytes"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(CategoriesPayload.serializer(), encoded),
        )
    }

    @Test
    fun categoriesPayloadRejectsWrongShapeForCategories() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                CategoriesPayload.serializer(),
                json.parseToJsonElement("""{"categories":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun cronListPayloadPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["jobs"] is JsonArray)
        assertEquals(1, (encoded["jobs"] as JsonArray).size)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(CronListPayload.serializer(), encoded),
        )
    }

    @Test
    fun cronListPayloadRejectsWrongShapeForJobs() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                CronListPayload.serializer(),
                json.parseToJsonElement("""{"jobs":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun modelsPayloadPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
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
    }

    @Test
    fun modelsPayloadRejectsWrongShapeForCurrent() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                ModelsPayload.serializer(),
                json.parseToJsonElement("""{"current":{}}"""),
            )
        }
    }

    @Test
    fun clientHelloPayloadPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
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
    }

    @Test
    fun clientHelloPayloadRejectsWrongShapeForVersion() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                ClientHelloPayload.serializer(),
                json.parseToJsonElement("""{"version":{}}"""),
            )
        }
    }

    @Test
    fun mailListPayloadPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["messages"] is JsonArray)
        assertEquals(1, (encoded["messages"] as JsonArray).size)
        assertEquals(input["nextPageToken"], encoded["nextPageToken"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(MailListPayload.serializer(), encoded),
        )
    }

    @Test
    fun mailListPayloadRejectsWrongShapeForMessages() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                MailListPayload.serializer(),
                json.parseToJsonElement("""{"messages":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun okPayloadPreservesEveryPresentEnvelopeField() {
        val input = json.parseToJsonElement(
            """{
                "ok": true
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(OkPayload.serializer(), input)
        val encoded = json.encodeToJsonElement(OkPayload.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["ok"], encoded["ok"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(OkPayload.serializer(), encoded),
        )
    }

    @Test
    fun okPayloadRejectsWrongShapeForOk() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                OkPayload.serializer(),
                json.parseToJsonElement("""{"ok":{}}"""),
            )
        }
    }

    @Test
    fun askPayloadPreservesEveryPresentEnvelopeField() {
        val input = json.parseToJsonElement(
            """{
                "answer": "answer-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(AskPayload.serializer(), input)
        val encoded = json.encodeToJsonElement(AskPayload.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["answer"], encoded["answer"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(AskPayload.serializer(), encoded),
        )
    }

    @Test
    fun askPayloadRejectsWrongShapeForAnswer() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                AskPayload.serializer(),
                json.parseToJsonElement("""{"answer":{}}"""),
            )
        }
    }

    @Test
    fun senderContextPayloadPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
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
    }

    @Test
    fun senderContextPayloadRejectsWrongShapeForSender() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                SenderContextPayload.serializer(),
                json.parseToJsonElement("""{"sender":{}}"""),
            )
        }
    }

    @Test
    fun calListPayloadPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["events"] is JsonArray)
        assertEquals(1, (encoded["events"] as JsonArray).size)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(CalListPayload.serializer(), encoded),
        )
    }

    @Test
    fun calListPayloadRejectsWrongShapeForEvents() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                CalListPayload.serializer(),
                json.parseToJsonElement("""{"events":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun calProposalsPayloadPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["proposals"] is JsonArray)
        assertEquals(1, (encoded["proposals"] as JsonArray).size)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(CalProposalsPayload.serializer(), encoded),
        )
    }

    @Test
    fun calProposalsPayloadRejectsWrongShapeForProposals() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                CalProposalsPayload.serializer(),
                json.parseToJsonElement("""{"proposals":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun todoListPayloadPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["todos"] is JsonArray)
        assertEquals(1, (encoded["todos"] as JsonArray).size)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(TodoListPayload.serializer(), encoded),
        )
    }

    @Test
    fun todoListPayloadRejectsWrongShapeForTodos() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                TodoListPayload.serializer(),
                json.parseToJsonElement("""{"todos":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun peopleListPayloadPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["people"] is JsonArray)
        assertEquals(1, (encoded["people"] as JsonArray).size)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(PeopleListPayload.serializer(), encoded),
        )
    }

    @Test
    fun peopleListPayloadRejectsWrongShapeForPeople() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                PeopleListPayload.serializer(),
                json.parseToJsonElement("""{"people":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun contactsListPayloadPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["contacts"] is JsonArray)
        assertEquals(1, (encoded["contacts"] as JsonArray).size)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(ContactsListPayload.serializer(), encoded),
        )
    }

    @Test
    fun contactsListPayloadRejectsWrongShapeForContacts() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                ContactsListPayload.serializer(),
                json.parseToJsonElement("""{"contacts":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun wikiPagePayloadPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
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
    }

    @Test
    fun wikiPagePayloadRejectsWrongShapeForPath() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                WikiPagePayload.serializer(),
                json.parseToJsonElement("""{"path":{}}"""),
            )
        }
    }

    @Test
    fun captureImagePayloadPreservesEveryPresentEnvelopeField() {
        val input = json.parseToJsonElement(
            """{
                "text": "text-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(CaptureImagePayload.serializer(), input)
        val encoded = json.encodeToJsonElement(CaptureImagePayload.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["text"], encoded["text"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(CaptureImagePayload.serializer(), encoded),
        )
    }

    @Test
    fun captureImagePayloadRejectsWrongShapeForText() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                CaptureImagePayload.serializer(),
                json.parseToJsonElement("""{"text":{}}"""),
            )
        }
    }

    @Test
    fun captureAudioPayloadPreservesEveryPresentEnvelopeField() {
        val input = json.parseToJsonElement(
            """{
                "text": "text-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(CaptureAudioPayload.serializer(), input)
        val encoded = json.encodeToJsonElement(CaptureAudioPayload.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["text"], encoded["text"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(CaptureAudioPayload.serializer(), encoded),
        )
    }

    @Test
    fun captureAudioPayloadRejectsWrongShapeForText() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                CaptureAudioPayload.serializer(),
                json.parseToJsonElement("""{"text":{}}"""),
            )
        }
    }

    @Test
    fun captureDocumentPayloadPreservesEveryPresentEnvelopeField() {
        val input = json.parseToJsonElement(
            """{
                "text": "text-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(CaptureDocumentPayload.serializer(), input)
        val encoded = json.encodeToJsonElement(CaptureDocumentPayload.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["text"], encoded["text"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(CaptureDocumentPayload.serializer(), encoded),
        )
    }

    @Test
    fun captureDocumentPayloadRejectsWrongShapeForText() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                CaptureDocumentPayload.serializer(),
                json.parseToJsonElement("""{"text":{}}"""),
            )
        }
    }

    @Test
    fun captureContactsPayloadPreservesEveryPresentEnvelopeField() {
        val input = json.parseToJsonElement(
            """{
                "text": "text-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(CaptureContactsPayload.serializer(), input)
        val encoded = json.encodeToJsonElement(CaptureContactsPayload.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["text"], encoded["text"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(CaptureContactsPayload.serializer(), encoded),
        )
    }

    @Test
    fun captureContactsPayloadRejectsWrongShapeForText() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                CaptureContactsPayload.serializer(),
                json.parseToJsonElement("""{"text":{}}"""),
            )
        }
    }

    @Test
    fun observeToolStatPreservesEveryPresentEnvelopeField() {
        val input = json.parseToJsonElement(
            """{
                "name": "name-value",
                "calls": 7,
                "errors": 7,
                "avgMs": 7000000000
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(ObserveToolStat.serializer(), input)
        val encoded = json.encodeToJsonElement(ObserveToolStat.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["name"], encoded["name"])
        assertEquals(input["calls"], encoded["calls"])
        assertEquals(input["errors"], encoded["errors"])
        assertEquals(input["avgMs"], encoded["avgMs"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(ObserveToolStat.serializer(), encoded),
        )
    }

    @Test
    fun observeToolStatRejectsWrongShapeForName() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                ObserveToolStat.serializer(),
                json.parseToJsonElement("""{"name":{}}"""),
            )
        }
    }

    @Test
    fun observeBehaviorPreservesEveryPresentEnvelopeField() {
        val input = json.parseToJsonElement(
            """{
                "runs": 7,
                "proactiveRuns": 7,
                "compactedRuns": 7,
                "tools": [
                    {}
                ],
                "backgroundErrors": {
                    "sample-key": 7
                }
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(ObserveBehavior.serializer(), input)
        val encoded = json.encodeToJsonElement(ObserveBehavior.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["runs"], encoded["runs"])
        assertEquals(input["proactiveRuns"], encoded["proactiveRuns"])
        assertEquals(input["compactedRuns"], encoded["compactedRuns"])
        assertTrue(encoded["tools"] is JsonArray)
        assertEquals(1, (encoded["tools"] as JsonArray).size)
        assertEquals(input["backgroundErrors"], encoded["backgroundErrors"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(ObserveBehavior.serializer(), encoded),
        )
    }

    @Test
    fun observeBehaviorRejectsWrongShapeForRuns() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                ObserveBehavior.serializer(),
                json.parseToJsonElement("""{"runs":{}}"""),
            )
        }
    }

    @Test
    fun observeLogLinePreservesEveryPresentEnvelopeField() {
        val input = json.parseToJsonElement(
            """{
                "level": "level-value",
                "msg": "msg-value",
                "runId": "runId-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(ObserveLogLine.serializer(), input)
        val encoded = json.encodeToJsonElement(ObserveLogLine.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["level"], encoded["level"])
        assertEquals(input["msg"], encoded["msg"])
        assertEquals(input["runId"], encoded["runId"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(ObserveLogLine.serializer(), encoded),
        )
    }

    @Test
    fun observeLogLineRejectsWrongShapeForLevel() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                ObserveLogLine.serializer(),
                json.parseToJsonElement("""{"level":{}}"""),
            )
        }
    }

    @Test
    fun observeLogsPayloadPreservesEveryPresentEnvelopeField() {
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

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["lines"] is JsonArray)
        assertEquals(1, (encoded["lines"] as JsonArray).size)
        assertEquals(input["count"], encoded["count"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(ObserveLogsPayload.serializer(), encoded),
        )
    }

    @Test
    fun observeLogsPayloadRejectsWrongShapeForLines() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                ObserveLogsPayload.serializer(),
                json.parseToJsonElement("""{"lines":"wrong-shape"}"""),
            )
        }
    }
}
