package ai.deneb.deneb.generated

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
 * Non-default and malformed payload contracts for every generated gateway model.
 *
 * Descriptor tests protect missing/unknown fields; this suite exercises the other
 * half of compatibility: all present fields at once, non-empty collections,
 * non-null nested objects, large numeric values, Unicode-safe strings, and strict
 * rejection of a wrong JSON shape before it can reach native UI state.
 */
class MiniappWireValueContractTest {
    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
        encodeDefaults = true
    }

    @Test
    fun calendarAttendeeOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "email": "email-value",
                "displayName": "displayName-value",
                "responseStatus": "responseStatus-value",
                "self": true,
                "organizer": true
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(CalendarAttendeeOut.serializer(), input)
        val encoded = json.encodeToJsonElement(CalendarAttendeeOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["email"], encoded["email"])
        assertEquals(input["displayName"], encoded["displayName"])
        assertEquals(input["responseStatus"], encoded["responseStatus"])
        assertEquals(input["self"], encoded["self"])
        assertEquals(input["organizer"], encoded["organizer"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(CalendarAttendeeOut.serializer(), encoded),
        )
    }

    @Test
    fun calendarAttendeeOutRejectsWrongShapeForEmail() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                CalendarAttendeeOut.serializer(),
                json.parseToJsonElement("""{"email":{}}"""),
            )
        }
    }

    @Test
    fun calendarConferenceOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "solution": "solution-value",
                "uri": "uri-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(CalendarConferenceOut.serializer(), input)
        val encoded = json.encodeToJsonElement(CalendarConferenceOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["solution"], encoded["solution"])
        assertEquals(input["uri"], encoded["uri"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(CalendarConferenceOut.serializer(), encoded),
        )
    }

    @Test
    fun calendarConferenceOutRejectsWrongShapeForSolution() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                CalendarConferenceOut.serializer(),
                json.parseToJsonElement("""{"solution":{}}"""),
            )
        }
    }

    @Test
    fun calendarEventOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-value",
                "summary": "summary-value",
                "description": "description-value",
                "location": "location-value",
                "start": "start-value",
                "end": "end-value",
                "allDay": true,
                "status": "status-value",
                "htmlLink": "htmlLink-value",
                "local": true,
                "category": "category-value",
                "organizer": {},
                "attendees": [
                    {}
                ],
                "conference": {},
                "hasMeet": true
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(CalendarEventOut.serializer(), input)
        val encoded = json.encodeToJsonElement(CalendarEventOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["summary"], encoded["summary"])
        assertEquals(input["description"], encoded["description"])
        assertEquals(input["location"], encoded["location"])
        assertEquals(input["start"], encoded["start"])
        assertEquals(input["end"], encoded["end"])
        assertEquals(input["allDay"], encoded["allDay"])
        assertEquals(input["status"], encoded["status"])
        assertEquals(input["htmlLink"], encoded["htmlLink"])
        assertEquals(input["local"], encoded["local"])
        assertEquals(input["category"], encoded["category"])
        assertTrue(encoded["organizer"] is JsonObject)
        assertTrue(encoded["attendees"] is JsonArray)
        assertEquals(1, (encoded["attendees"] as JsonArray).size)
        assertTrue(encoded["conference"] is JsonObject)
        assertEquals(input["hasMeet"], encoded["hasMeet"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(CalendarEventOut.serializer(), encoded),
        )
    }

    @Test
    fun calendarEventOutRejectsWrongShapeForId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                CalendarEventOut.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun calendarProposalOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-value",
                "title": "title-value",
                "start": "start-value",
                "allDay": true,
                "kind": "kind-value",
                "sourceSubject": "sourceSubject-value",
                "sourceFrom": "sourceFrom-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(CalendarProposalOut.serializer(), input)
        val encoded = json.encodeToJsonElement(CalendarProposalOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["title"], encoded["title"])
        assertEquals(input["start"], encoded["start"])
        assertEquals(input["allDay"], encoded["allDay"])
        assertEquals(input["kind"], encoded["kind"])
        assertEquals(input["sourceSubject"], encoded["sourceSubject"])
        assertEquals(input["sourceFrom"], encoded["sourceFrom"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(CalendarProposalOut.serializer(), encoded),
        )
    }

    @Test
    fun calendarProposalOutRejectsWrongShapeForId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                CalendarProposalOut.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun contactRowPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "name": "name-value",
                "phones": [
                    "phonesItem-value"
                ],
                "emails": [
                    "emailsItem-value"
                ],
                "org": "org-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(ContactRow.serializer(), input)
        val encoded = json.encodeToJsonElement(ContactRow.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["name"], encoded["name"])
        assertEquals(input["phones"], encoded["phones"])
        assertEquals(input["emails"], encoded["emails"])
        assertEquals(input["org"], encoded["org"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(ContactRow.serializer(), encoded),
        )
    }

    @Test
    fun contactRowRejectsWrongShapeForName() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                ContactRow.serializer(),
                json.parseToJsonElement("""{"name":{}}"""),
            )
        }
    }

    @Test
    fun dashboardItemPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "title": "title-value",
                "subtitle": "subtitle-value",
                "source": "source-value",
                "refType": "refType-value",
                "refId": "refId-value",
                "whenMs": 7000000000
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(DashboardItem.serializer(), input)
        val encoded = json.encodeToJsonElement(DashboardItem.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["title"], encoded["title"])
        assertEquals(input["subtitle"], encoded["subtitle"])
        assertEquals(input["source"], encoded["source"])
        assertEquals(input["refType"], encoded["refType"])
        assertEquals(input["refId"], encoded["refId"])
        assertEquals(input["whenMs"], encoded["whenMs"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(DashboardItem.serializer(), encoded),
        )
    }

    @Test
    fun dashboardItemRejectsWrongShapeForTitle() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                DashboardItem.serializer(),
                json.parseToJsonElement("""{"title":{}}"""),
            )
        }
    }

    @Test
    fun dashboardOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "lanes": [
                    {}
                ]
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(DashboardOut.serializer(), input)
        val encoded = json.encodeToJsonElement(DashboardOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["lanes"] is JsonArray)
        assertEquals(1, (encoded["lanes"] as JsonArray).size)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(DashboardOut.serializer(), encoded),
        )
    }

    @Test
    fun dashboardOutRejectsWrongShapeForLanes() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                DashboardOut.serializer(),
                json.parseToJsonElement("""{"lanes":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun filesEntryOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "tag": "tag-value",
                "name": "name-value",
                "pathDisplay": "pathDisplay-value",
                "pathLower": "pathLower-value",
                "id": "id-value",
                "size": 7000000000,
                "serverModified": "serverModified-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(FilesEntryOut.serializer(), input)
        val encoded = json.encodeToJsonElement(FilesEntryOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["tag"], encoded["tag"])
        assertEquals(input["name"], encoded["name"])
        assertEquals(input["pathDisplay"], encoded["pathDisplay"])
        assertEquals(input["pathLower"], encoded["pathLower"])
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["size"], encoded["size"])
        assertEquals(input["serverModified"], encoded["serverModified"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(FilesEntryOut.serializer(), encoded),
        )
    }

    @Test
    fun filesEntryOutRejectsWrongShapeForTag() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                FilesEntryOut.serializer(),
                json.parseToJsonElement("""{"tag":{}}"""),
            )
        }
    }

    @Test
    fun filesListOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "entries": [
                    {}
                ],
                "path": "path-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(FilesListOut.serializer(), input)
        val encoded = json.encodeToJsonElement(FilesListOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["entries"] is JsonArray)
        assertEquals(1, (encoded["entries"] as JsonArray).size)
        assertEquals(input["path"], encoded["path"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(FilesListOut.serializer(), encoded),
        )
    }

    @Test
    fun filesListOutRejectsWrongShapeForEntries() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                FilesListOut.serializer(),
                json.parseToJsonElement("""{"entries":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun filesShareOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "url": "url-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(FilesShareOut.serializer(), input)
        val encoded = json.encodeToJsonElement(FilesShareOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["url"], encoded["url"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(FilesShareOut.serializer(), encoded),
        )
    }

    @Test
    fun filesShareOutRejectsWrongShapeForUrl() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                FilesShareOut.serializer(),
                json.parseToJsonElement("""{"url":{}}"""),
            )
        }
    }

    @Test
    fun filesUploadOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "entry": {}
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(FilesUploadOut.serializer(), input)
        val encoded = json.encodeToJsonElement(FilesUploadOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["entry"] is JsonObject)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(FilesUploadOut.serializer(), encoded),
        )
    }

    @Test
    fun filesUploadOutRejectsWrongShapeForEntry() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                FilesUploadOut.serializer(),
                json.parseToJsonElement("""{"entry":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun laneOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "key": "key-value",
                "name": "name-value",
                "items": [
                    {}
                ]
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(LaneOut.serializer(), input)
        val encoded = json.encodeToJsonElement(LaneOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["key"], encoded["key"])
        assertEquals(input["name"], encoded["name"])
        assertTrue(encoded["items"] is JsonArray)
        assertEquals(1, (encoded["items"] as JsonArray).size)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(LaneOut.serializer(), encoded),
        )
    }

    @Test
    fun laneOutRejectsWrongShapeForKey() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                LaneOut.serializer(),
                json.parseToJsonElement("""{"key":{}}"""),
            )
        }
    }

    @Test
    fun mailAnalysisOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-value",
                "subject": "subject-value",
                "from": "from-value",
                "date": "date-value",
                "analysis": "analysis-value",
                "relatedProjects": [
                    {}
                ],
                "durationMs": 7000000000,
                "cached": true,
                "createdAt": "createdAt-value",
                "analysisStatus": "analysisStatus-value",
                "analysisQuality": "analysisQuality-value",
                "feedStatus": "feedStatus-value",
                "calendarProposalCount": 7,
                "todoCount": 7,
                "workStateHint": "workStateHint-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(MailAnalysisOut.serializer(), input)
        val encoded = json.encodeToJsonElement(MailAnalysisOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["subject"], encoded["subject"])
        assertEquals(input["from"], encoded["from"])
        assertEquals(input["date"], encoded["date"])
        assertEquals(input["analysis"], encoded["analysis"])
        assertTrue(encoded["relatedProjects"] is JsonArray)
        assertEquals(1, (encoded["relatedProjects"] as JsonArray).size)
        assertEquals(input["durationMs"], encoded["durationMs"])
        assertEquals(input["cached"], encoded["cached"])
        assertEquals(input["createdAt"], encoded["createdAt"])
        assertEquals(input["analysisStatus"], encoded["analysisStatus"])
        assertEquals(input["analysisQuality"], encoded["analysisQuality"])
        assertEquals(input["feedStatus"], encoded["feedStatus"])
        assertEquals(input["calendarProposalCount"], encoded["calendarProposalCount"])
        assertEquals(input["todoCount"], encoded["todoCount"])
        assertEquals(input["workStateHint"], encoded["workStateHint"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(MailAnalysisOut.serializer(), encoded),
        )
    }

    @Test
    fun mailAnalysisOutRejectsWrongShapeForId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                MailAnalysisOut.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun mailAttachmentOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-value",
                "filename": "filename-value",
                "mimeType": "mimeType-value",
                "size": 7,
                "truncated": true
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(MailAttachmentOut.serializer(), input)
        val encoded = json.encodeToJsonElement(MailAttachmentOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["filename"], encoded["filename"])
        assertEquals(input["mimeType"], encoded["mimeType"])
        assertEquals(input["size"], encoded["size"])
        assertEquals(input["truncated"], encoded["truncated"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(MailAttachmentOut.serializer(), encoded),
        )
    }

    @Test
    fun mailAttachmentOutRejectsWrongShapeForId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                MailAttachmentOut.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun mailMessageOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-value",
                "threadId": "threadId-value",
                "from": "from-value",
                "to": "to-value",
                "cc": "cc-value",
                "subject": "subject-value",
                "date": "date-value",
                "isUnread": true,
                "body": "body-value",
                "bodyTotal": 7,
                "rawBody": "rawBody-value",
                "rawBodyTotal": 7,
                "bodyCleaned": true,
                "bodyHiddenBlockCount": 7,
                "bodyHiddenLineCount": 7,
                "labels": [
                    "labelsItem-value"
                ],
                "attachments": [
                    {}
                ],
                "analysisStatus": "analysisStatus-value",
                "analysisQuality": "analysisQuality-value",
                "feedStatus": "feedStatus-value",
                "calendarProposalCount": 7,
                "todoCount": 7,
                "workStateHint": "workStateHint-value",
                "relatedProjects": [
                    {}
                ]
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(MailMessageOut.serializer(), input)
        val encoded = json.encodeToJsonElement(MailMessageOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["threadId"], encoded["threadId"])
        assertEquals(input["from"], encoded["from"])
        assertEquals(input["to"], encoded["to"])
        assertEquals(input["cc"], encoded["cc"])
        assertEquals(input["subject"], encoded["subject"])
        assertEquals(input["date"], encoded["date"])
        assertEquals(input["isUnread"], encoded["isUnread"])
        assertEquals(input["body"], encoded["body"])
        assertEquals(input["bodyTotal"], encoded["bodyTotal"])
        assertEquals(input["rawBody"], encoded["rawBody"])
        assertEquals(input["rawBodyTotal"], encoded["rawBodyTotal"])
        assertEquals(input["bodyCleaned"], encoded["bodyCleaned"])
        assertEquals(input["bodyHiddenBlockCount"], encoded["bodyHiddenBlockCount"])
        assertEquals(input["bodyHiddenLineCount"], encoded["bodyHiddenLineCount"])
        assertEquals(input["labels"], encoded["labels"])
        assertTrue(encoded["attachments"] is JsonArray)
        assertEquals(1, (encoded["attachments"] as JsonArray).size)
        assertEquals(input["analysisStatus"], encoded["analysisStatus"])
        assertEquals(input["analysisQuality"], encoded["analysisQuality"])
        assertEquals(input["feedStatus"], encoded["feedStatus"])
        assertEquals(input["calendarProposalCount"], encoded["calendarProposalCount"])
        assertEquals(input["todoCount"], encoded["todoCount"])
        assertEquals(input["workStateHint"], encoded["workStateHint"])
        assertTrue(encoded["relatedProjects"] is JsonArray)
        assertEquals(1, (encoded["relatedProjects"] as JsonArray).size)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(MailMessageOut.serializer(), encoded),
        )
    }

    @Test
    fun mailMessageOutRejectsWrongShapeForId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                MailMessageOut.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun mailNativeMailboxOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "name": "name-value",
                "total": 7,
                "unread": 7,
                "locallyRead": 7,
                "locallyArchived": 7,
                "locallyTrashed": 7,
                "latestUid": "latestUid-value",
                "attachmentCapable": true
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(MailNativeMailboxOut.serializer(), input)
        val encoded = json.encodeToJsonElement(MailNativeMailboxOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["name"], encoded["name"])
        assertEquals(input["total"], encoded["total"])
        assertEquals(input["unread"], encoded["unread"])
        assertEquals(input["locallyRead"], encoded["locallyRead"])
        assertEquals(input["locallyArchived"], encoded["locallyArchived"])
        assertEquals(input["locallyTrashed"], encoded["locallyTrashed"])
        assertEquals(input["latestUid"], encoded["latestUid"])
        assertEquals(input["attachmentCapable"], encoded["attachmentCapable"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(MailNativeMailboxOut.serializer(), encoded),
        )
    }

    @Test
    fun mailNativeMailboxOutRejectsWrongShapeForName() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                MailNativeMailboxOut.serializer(),
                json.parseToJsonElement("""{"name":{}}"""),
            )
        }
    }

    @Test
    fun mailNativeOverlayOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "messages": 7,
                "read": 7,
                "archived": 7,
                "trashed": 7
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(MailNativeOverlayOut.serializer(), input)
        val encoded = json.encodeToJsonElement(MailNativeOverlayOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["messages"], encoded["messages"])
        assertEquals(input["read"], encoded["read"])
        assertEquals(input["archived"], encoded["archived"])
        assertEquals(input["trashed"], encoded["trashed"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(MailNativeOverlayOut.serializer(), encoded),
        )
    }

    @Test
    fun mailNativeOverlayOutRejectsWrongShapeForMessages() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                MailNativeOverlayOut.serializer(),
                json.parseToJsonElement("""{"messages":{}}"""),
            )
        }
    }

    @Test
    fun mailNativePipelineOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "messages": 7,
                "analyzed": 7,
                "analyzing": 7,
                "failed": 7,
                "feedCreated": 7,
                "feedMissing": 7,
                "calendarCandidates": 7,
                "todoCandidates": 7,
                "updatedAt": "updatedAt-value",
                "error": "error-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(MailNativePipelineOut.serializer(), input)
        val encoded = json.encodeToJsonElement(MailNativePipelineOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["messages"], encoded["messages"])
        assertEquals(input["analyzed"], encoded["analyzed"])
        assertEquals(input["analyzing"], encoded["analyzing"])
        assertEquals(input["failed"], encoded["failed"])
        assertEquals(input["feedCreated"], encoded["feedCreated"])
        assertEquals(input["feedMissing"], encoded["feedMissing"])
        assertEquals(input["calendarCandidates"], encoded["calendarCandidates"])
        assertEquals(input["todoCandidates"], encoded["todoCandidates"])
        assertEquals(input["updatedAt"], encoded["updatedAt"])
        assertEquals(input["error"], encoded["error"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(MailNativePipelineOut.serializer(), encoded),
        )
    }

    @Test
    fun mailNativePipelineOutRejectsWrongShapeForMessages() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                MailNativePipelineOut.serializer(),
                json.parseToJsonElement("""{"messages":{}}"""),
            )
        }
    }

    @Test
    fun mailNativeStatusOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "source": "source-value",
                "available": true,
                "offlineCapable": true,
                "mailboxes": [
                    {}
                ],
                "overlay": {},
                "pipeline": {},
                "generatedAt": "generatedAt-value",
                "error": "error-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(MailNativeStatusOut.serializer(), input)
        val encoded = json.encodeToJsonElement(MailNativeStatusOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["source"], encoded["source"])
        assertEquals(input["available"], encoded["available"])
        assertEquals(input["offlineCapable"], encoded["offlineCapable"])
        assertTrue(encoded["mailboxes"] is JsonArray)
        assertEquals(1, (encoded["mailboxes"] as JsonArray).size)
        assertTrue(encoded["overlay"] is JsonObject)
        assertTrue(encoded["pipeline"] is JsonObject)
        assertEquals(input["generatedAt"], encoded["generatedAt"])
        assertEquals(input["error"], encoded["error"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(MailNativeStatusOut.serializer(), encoded),
        )
    }

    @Test
    fun mailNativeStatusOutRejectsWrongShapeForSource() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                MailNativeStatusOut.serializer(),
                json.parseToJsonElement("""{"source":{}}"""),
            )
        }
    }

    @Test
    fun mailRowOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-value",
                "threadId": "threadId-value",
                "from": "from-value",
                "subject": "subject-value",
                "snippet": "snippet-value",
                "date": "date-value",
                "isUnread": true,
                "labels": [
                    "labelsItem-value"
                ],
                "mailbox": "mailbox-value",
                "hasAttachment": true,
                "attachmentCount": 7,
                "priority": "priority-value",
                "priorityHint": "priorityHint-value",
                "analysisStatus": "analysisStatus-value",
                "analysisQuality": "analysisQuality-value",
                "feedStatus": "feedStatus-value",
                "calendarProposalCount": 7,
                "todoCount": 7,
                "workStateHint": "workStateHint-value",
                "relatedProjects": [
                    {}
                ]
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(MailRowOut.serializer(), input)
        val encoded = json.encodeToJsonElement(MailRowOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["threadId"], encoded["threadId"])
        assertEquals(input["from"], encoded["from"])
        assertEquals(input["subject"], encoded["subject"])
        assertEquals(input["snippet"], encoded["snippet"])
        assertEquals(input["date"], encoded["date"])
        assertEquals(input["isUnread"], encoded["isUnread"])
        assertEquals(input["labels"], encoded["labels"])
        assertEquals(input["mailbox"], encoded["mailbox"])
        assertEquals(input["hasAttachment"], encoded["hasAttachment"])
        assertEquals(input["attachmentCount"], encoded["attachmentCount"])
        assertEquals(input["priority"], encoded["priority"])
        assertEquals(input["priorityHint"], encoded["priorityHint"])
        assertEquals(input["analysisStatus"], encoded["analysisStatus"])
        assertEquals(input["analysisQuality"], encoded["analysisQuality"])
        assertEquals(input["feedStatus"], encoded["feedStatus"])
        assertEquals(input["calendarProposalCount"], encoded["calendarProposalCount"])
        assertEquals(input["todoCount"], encoded["todoCount"])
        assertEquals(input["workStateHint"], encoded["workStateHint"])
        assertTrue(encoded["relatedProjects"] is JsonArray)
        assertEquals(1, (encoded["relatedProjects"] as JsonArray).size)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(MailRowOut.serializer(), encoded),
        )
    }

    @Test
    fun mailRowOutRejectsWrongShapeForId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                MailRowOut.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun marketQuotePreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "symbol": "symbol-value",
                "label": "label-value",
                "price": 1.25,
                "prevClose": 1.25,
                "changePct": 1.25,
                "currency": "currency-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(MarketQuote.serializer(), input)
        val encoded = json.encodeToJsonElement(MarketQuote.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["symbol"], encoded["symbol"])
        assertEquals(input["label"], encoded["label"])
        assertEquals(input["price"], encoded["price"])
        assertEquals(input["prevClose"], encoded["prevClose"])
        assertEquals(input["changePct"], encoded["changePct"])
        assertEquals(input["currency"], encoded["currency"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(MarketQuote.serializer(), encoded),
        )
    }

    @Test
    fun marketQuoteRejectsWrongShapeForSymbol() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                MarketQuote.serializer(),
                json.parseToJsonElement("""{"symbol":{}}"""),
            )
        }
    }

    @Test
    fun marketSummaryPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "quotes": [
                    {}
                ],
                "asOf": 7000000000,
                "stale": true
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(MarketSummary.serializer(), input)
        val encoded = json.encodeToJsonElement(MarketSummary.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["quotes"] is JsonArray)
        assertEquals(1, (encoded["quotes"] as JsonArray).size)
        assertEquals(input["asOf"], encoded["asOf"])
        assertEquals(input["stale"], encoded["stale"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(MarketSummary.serializer(), encoded),
        )
    }

    @Test
    fun marketSummaryRejectsWrongShapeForQuotes() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                MarketSummary.serializer(),
                json.parseToJsonElement("""{"quotes":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun memberOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "name": "name-value",
                "rank": "rank-value",
                "position": "position-value",
                "phones": [
                    "phonesItem-value"
                ],
                "emails": [
                    "emailsItem-value"
                ],
                "personPath": "personPath-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(MemberOut.serializer(), input)
        val encoded = json.encodeToJsonElement(MemberOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["name"], encoded["name"])
        assertEquals(input["rank"], encoded["rank"])
        assertEquals(input["position"], encoded["position"])
        assertEquals(input["phones"], encoded["phones"])
        assertEquals(input["emails"], encoded["emails"])
        assertEquals(input["personPath"], encoded["personPath"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(MemberOut.serializer(), encoded),
        )
    }

    @Test
    fun memberOutRejectsWrongShapeForName() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                MemberOut.serializer(),
                json.parseToJsonElement("""{"name":{}}"""),
            )
        }
    }

    @Test
    fun memoryCategoryRowPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "name": "name-value",
                "pageCount": 7
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(MemoryCategoryRow.serializer(), input)
        val encoded = json.encodeToJsonElement(MemoryCategoryRow.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["name"], encoded["name"])
        assertEquals(input["pageCount"], encoded["pageCount"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(MemoryCategoryRow.serializer(), encoded),
        )
    }

    @Test
    fun memoryCategoryRowRejectsWrongShapeForName() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                MemoryCategoryRow.serializer(),
                json.parseToJsonElement("""{"name":{}}"""),
            )
        }
    }

    @Test
    fun memoryPageRowPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "path": "path-value",
                "title": "title-value",
                "summary": "summary-value",
                "updated": "updated-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(MemoryPageRow.serializer(), input)
        val encoded = json.encodeToJsonElement(MemoryPageRow.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["path"], encoded["path"])
        assertEquals(input["title"], encoded["title"])
        assertEquals(input["summary"], encoded["summary"])
        assertEquals(input["updated"], encoded["updated"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(MemoryPageRow.serializer(), encoded),
        )
    }

    @Test
    fun memoryPageRowRejectsWrongShapeForPath() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                MemoryPageRow.serializer(),
                json.parseToJsonElement("""{"path":{}}"""),
            )
        }
    }

    @Test
    fun miniappCronDetailPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-value",
                "name": "name-value",
                "enabled": true,
                "agentId": "agentId-value",
                "sessionTarget": "sessionTarget-value",
                "schedule": "schedule-value",
                "scheduleSpec": "scheduleSpec-value",
                "scheduleKind": "scheduleKind-value",
                "timezone": "timezone-value",
                "cronExpr": "cronExpr-value",
                "staggerMs": 7000000000,
                "payloadKind": "payloadKind-value",
                "prompt": "prompt-value",
                "model": "model-value",
                "thinking": "thinking-value",
                "timeoutSeconds": 7,
                "lightContext": true,
                "retryCount": 7,
                "deliveryChannel": "deliveryChannel-value",
                "deliveryTo": "deliveryTo-value",
                "deliveryThreadId": "deliveryThreadId-value",
                "failureAlertAfter": 7,
                "nextRunAtMs": 7000000000,
                "lastSessionKey": "lastSessionKey-value",
                "lastDeliveryStatus": "lastDeliveryStatus-value",
                "lastError": "lastError-value",
                "consecutiveErrors": 7,
                "autoDisabledAtMs": 7000000000,
                "createdAtMs": 7000000000,
                "updatedAtMs": 7000000000
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(MiniappCronDetail.serializer(), input)
        val encoded = json.encodeToJsonElement(MiniappCronDetail.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["name"], encoded["name"])
        assertEquals(input["enabled"], encoded["enabled"])
        assertEquals(input["agentId"], encoded["agentId"])
        assertEquals(input["sessionTarget"], encoded["sessionTarget"])
        assertEquals(input["schedule"], encoded["schedule"])
        assertEquals(input["scheduleSpec"], encoded["scheduleSpec"])
        assertEquals(input["scheduleKind"], encoded["scheduleKind"])
        assertEquals(input["timezone"], encoded["timezone"])
        assertEquals(input["cronExpr"], encoded["cronExpr"])
        assertEquals(input["staggerMs"], encoded["staggerMs"])
        assertEquals(input["payloadKind"], encoded["payloadKind"])
        assertEquals(input["prompt"], encoded["prompt"])
        assertEquals(input["model"], encoded["model"])
        assertEquals(input["thinking"], encoded["thinking"])
        assertEquals(input["timeoutSeconds"], encoded["timeoutSeconds"])
        assertEquals(input["lightContext"], encoded["lightContext"])
        assertEquals(input["retryCount"], encoded["retryCount"])
        assertEquals(input["deliveryChannel"], encoded["deliveryChannel"])
        assertEquals(input["deliveryTo"], encoded["deliveryTo"])
        assertEquals(input["deliveryThreadId"], encoded["deliveryThreadId"])
        assertEquals(input["failureAlertAfter"], encoded["failureAlertAfter"])
        assertEquals(input["nextRunAtMs"], encoded["nextRunAtMs"])
        assertEquals(input["lastSessionKey"], encoded["lastSessionKey"])
        assertEquals(input["lastDeliveryStatus"], encoded["lastDeliveryStatus"])
        assertEquals(input["lastError"], encoded["lastError"])
        assertEquals(input["consecutiveErrors"], encoded["consecutiveErrors"])
        assertEquals(input["autoDisabledAtMs"], encoded["autoDisabledAtMs"])
        assertEquals(input["createdAtMs"], encoded["createdAtMs"])
        assertEquals(input["updatedAtMs"], encoded["updatedAtMs"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(MiniappCronDetail.serializer(), encoded),
        )
    }

    @Test
    fun miniappCronDetailRejectsWrongShapeForId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                MiniappCronDetail.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun miniappCronRowPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-value",
                "name": "name-value",
                "enabled": true,
                "schedule": "schedule-value",
                "payloadKind": "payloadKind-value",
                "payloadPreview": "payloadPreview-value",
                "nextRunAtMs": 7000000000,
                "consecutiveErrors": 7,
                "autoDisabledAtMs": 7000000000,
                "lastError": "lastError-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(MiniappCronRow.serializer(), input)
        val encoded = json.encodeToJsonElement(MiniappCronRow.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["name"], encoded["name"])
        assertEquals(input["enabled"], encoded["enabled"])
        assertEquals(input["schedule"], encoded["schedule"])
        assertEquals(input["payloadKind"], encoded["payloadKind"])
        assertEquals(input["payloadPreview"], encoded["payloadPreview"])
        assertEquals(input["nextRunAtMs"], encoded["nextRunAtMs"])
        assertEquals(input["consecutiveErrors"], encoded["consecutiveErrors"])
        assertEquals(input["autoDisabledAtMs"], encoded["autoDisabledAtMs"])
        assertEquals(input["lastError"], encoded["lastError"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(MiniappCronRow.serializer(), encoded),
        )
    }

    @Test
    fun miniappCronRowRejectsWrongShapeForId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                MiniappCronRow.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun modelAddResultPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "ok": true,
                "id": "id-value",
                "provider": "provider-value",
                "endpoint": "endpoint-value",
                "model": "model-value",
                "added": true
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(ModelAddResult.serializer(), input)
        val encoded = json.encodeToJsonElement(ModelAddResult.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["ok"], encoded["ok"])
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["provider"], encoded["provider"])
        assertEquals(input["endpoint"], encoded["endpoint"])
        assertEquals(input["model"], encoded["model"])
        assertEquals(input["added"], encoded["added"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(ModelAddResult.serializer(), encoded),
        )
    }

    @Test
    fun modelAddResultRejectsWrongShapeForOk() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                ModelAddResult.serializer(),
                json.parseToJsonElement("""{"ok":{}}"""),
            )
        }
    }

    @Test
    fun modelDeleteResultPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "ok": true,
                "id": "id-value",
                "removed": true,
                "clearedRoles": [
                    "clearedRolesItem-value"
                ],
                "current": "current-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(ModelDeleteResult.serializer(), input)
        val encoded = json.encodeToJsonElement(ModelDeleteResult.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["ok"], encoded["ok"])
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["removed"], encoded["removed"])
        assertEquals(input["clearedRoles"], encoded["clearedRoles"])
        assertEquals(input["current"], encoded["current"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(ModelDeleteResult.serializer(), encoded),
        )
    }

    @Test
    fun modelDeleteResultRejectsWrongShapeForOk() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                ModelDeleteResult.serializer(),
                json.parseToJsonElement("""{"ok":{}}"""),
            )
        }
    }

    @Test
    fun modelOptionPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-value",
                "label": "label-value",
                "provider": "provider-value",
                "display": "display-value",
                "health": "health-value",
                "current": true,
                "custom": true,
                "deletable": true,
                "unhealthy": true,
                "note": "note-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(ModelOption.serializer(), input)
        val encoded = json.encodeToJsonElement(ModelOption.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["label"], encoded["label"])
        assertEquals(input["provider"], encoded["provider"])
        assertEquals(input["display"], encoded["display"])
        assertEquals(input["health"], encoded["health"])
        assertEquals(input["current"], encoded["current"])
        assertEquals(input["custom"], encoded["custom"])
        assertEquals(input["deletable"], encoded["deletable"])
        assertEquals(input["unhealthy"], encoded["unhealthy"])
        assertEquals(input["note"], encoded["note"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(ModelOption.serializer(), encoded),
        )
    }

    @Test
    fun modelOptionRejectsWrongShapeForId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                ModelOption.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun modelSectionPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "title": "title-value",
                "models": [
                    {}
                ]
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(ModelSection.serializer(), input)
        val encoded = json.encodeToJsonElement(ModelSection.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["title"], encoded["title"])
        assertTrue(encoded["models"] is JsonArray)
        assertEquals(1, (encoded["models"] as JsonArray).size)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(ModelSection.serializer(), encoded),
        )
    }

    @Test
    fun modelSectionRejectsWrongShapeForTitle() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                ModelSection.serializer(),
                json.parseToJsonElement("""{"title":{}}"""),
            )
        }
    }

    @Test
    fun modelsListResultPreservesEveryPresentWireField() {
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

        val decoded = json.decodeFromJsonElement(ModelsListResult.serializer(), input)
        val encoded = json.encodeToJsonElement(ModelsListResult.serializer(), decoded).jsonObject

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
            json.decodeFromJsonElement(ModelsListResult.serializer(), encoded),
        )
    }

    @Test
    fun modelsListResultRejectsWrongShapeForCurrent() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                ModelsListResult.serializer(),
                json.parseToJsonElement("""{"current":{}}"""),
            )
        }
    }

    @Test
    fun notebookListOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "notebooks": [
                    {}
                ]
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(NotebookListOut.serializer(), input)
        val encoded = json.encodeToJsonElement(NotebookListOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["notebooks"] is JsonArray)
        assertEquals(1, (encoded["notebooks"] as JsonArray).size)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(NotebookListOut.serializer(), encoded),
        )
    }

    @Test
    fun notebookListOutRejectsWrongShapeForNotebooks() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                NotebookListOut.serializer(),
                json.parseToJsonElement("""{"notebooks":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun notebookOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-value",
                "name": "name-value",
                "description": "description-value",
                "dealRef": "dealRef-value",
                "mode": "mode-value",
                "sources": [
                    {}
                ],
                "updated": 7000000000
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(NotebookOut.serializer(), input)
        val encoded = json.encodeToJsonElement(NotebookOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["name"], encoded["name"])
        assertEquals(input["description"], encoded["description"])
        assertEquals(input["dealRef"], encoded["dealRef"])
        assertEquals(input["mode"], encoded["mode"])
        assertTrue(encoded["sources"] is JsonArray)
        assertEquals(1, (encoded["sources"] as JsonArray).size)
        assertEquals(input["updated"], encoded["updated"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(NotebookOut.serializer(), encoded),
        )
    }

    @Test
    fun notebookOutRejectsWrongShapeForId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                NotebookOut.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun notebookSourceOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "cite": "cite-value",
                "kind": "kind-value",
                "ref": "ref-value",
                "title": "title-value",
                "text": "text-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(NotebookSourceOut.serializer(), input)
        val encoded = json.encodeToJsonElement(NotebookSourceOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["cite"], encoded["cite"])
        assertEquals(input["kind"], encoded["kind"])
        assertEquals(input["ref"], encoded["ref"])
        assertEquals(input["title"], encoded["title"])
        assertEquals(input["text"], encoded["text"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(NotebookSourceOut.serializer(), encoded),
        )
    }

    @Test
    fun notebookSourceOutRejectsWrongShapeForCite() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                NotebookSourceOut.serializer(),
                json.parseToJsonElement("""{"cite":{}}"""),
            )
        }
    }

    @Test
    fun notebookSummaryOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-value",
                "name": "name-value",
                "description": "description-value",
                "dealRef": "dealRef-value",
                "projectRefs": [
                    "projectRefsItem-value"
                ],
                "sourceCount": 7,
                "updated": 7000000000
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(NotebookSummaryOut.serializer(), input)
        val encoded = json.encodeToJsonElement(NotebookSummaryOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["name"], encoded["name"])
        assertEquals(input["description"], encoded["description"])
        assertEquals(input["dealRef"], encoded["dealRef"])
        assertEquals(input["projectRefs"], encoded["projectRefs"])
        assertEquals(input["sourceCount"], encoded["sourceCount"])
        assertEquals(input["updated"], encoded["updated"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(NotebookSummaryOut.serializer(), encoded),
        )
    }

    @Test
    fun notebookSummaryOutRejectsWrongShapeForId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                NotebookSummaryOut.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun orgNodeOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-value",
                "name": "name-value",
                "type": "type-value",
                "parentId": "parentId-value",
                "lane": "lane-value",
                "members": [
                    {}
                ],
                "keywords": [
                    "keywordsItem-value"
                ],
                "companies": [
                    "companiesItem-value"
                ]
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(OrgNodeOut.serializer(), input)
        val encoded = json.encodeToJsonElement(OrgNodeOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["name"], encoded["name"])
        assertEquals(input["type"], encoded["type"])
        assertEquals(input["parentId"], encoded["parentId"])
        assertEquals(input["lane"], encoded["lane"])
        assertTrue(encoded["members"] is JsonArray)
        assertEquals(1, (encoded["members"] as JsonArray).size)
        assertEquals(input["keywords"], encoded["keywords"])
        assertEquals(input["companies"], encoded["companies"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(OrgNodeOut.serializer(), encoded),
        )
    }

    @Test
    fun orgNodeOutRejectsWrongShapeForId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                OrgNodeOut.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun orgSaveOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "saved": true,
                "nodeCount": 7,
                "hasLanes": true
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(OrgSaveOut.serializer(), input)
        val encoded = json.encodeToJsonElement(OrgSaveOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["saved"], encoded["saved"])
        assertEquals(input["nodeCount"], encoded["nodeCount"])
        assertEquals(input["hasLanes"], encoded["hasLanes"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(OrgSaveOut.serializer(), encoded),
        )
    }

    @Test
    fun orgSaveOutRejectsWrongShapeForSaved() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                OrgSaveOut.serializer(),
                json.parseToJsonElement("""{"saved":{}}"""),
            )
        }
    }

    @Test
    fun orgTreeOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "nodes": [
                    {}
                ]
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(OrgTreeOut.serializer(), input)
        val encoded = json.encodeToJsonElement(OrgTreeOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["nodes"] is JsonArray)
        assertEquals(1, (encoded["nodes"] as JsonArray).size)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(OrgTreeOut.serializer(), encoded),
        )
    }

    @Test
    fun orgTreeOutRejectsWrongShapeForNodes() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                OrgTreeOut.serializer(),
                json.parseToJsonElement("""{"nodes":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun personRowPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "email": "email-value",
                "name": "name-value",
                "messageCount": 7,
                "lastSeen": "lastSeen-value",
                "lastSubject": "lastSubject-value",
                "wikiPath": "wikiPath-value",
                "wikiSummary": "wikiSummary-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(PersonRow.serializer(), input)
        val encoded = json.encodeToJsonElement(PersonRow.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["email"], encoded["email"])
        assertEquals(input["name"], encoded["name"])
        assertEquals(input["messageCount"], encoded["messageCount"])
        assertEquals(input["lastSeen"], encoded["lastSeen"])
        assertEquals(input["lastSubject"], encoded["lastSubject"])
        assertEquals(input["wikiPath"], encoded["wikiPath"])
        assertEquals(input["wikiSummary"], encoded["wikiSummary"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(PersonRow.serializer(), encoded),
        )
    }

    @Test
    fun personRowRejectsWrongShapeForEmail() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                PersonRow.serializer(),
                json.parseToJsonElement("""{"email":{}}"""),
            )
        }
    }

    @Test
    fun projectDigestRowPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "project": "project-value",
                "headline": "headline-value",
                "bullets": [
                    "bulletsItem-value"
                ],
                "due": "due-value",
                "updatedAtMs": 7000000000,
                "path": "path-value",
                "code": "code-value",
                "client": "client-value",
                "refs": [
                    "refsItem-value"
                ]
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(ProjectDigestRow.serializer(), input)
        val encoded = json.encodeToJsonElement(ProjectDigestRow.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["project"], encoded["project"])
        assertEquals(input["headline"], encoded["headline"])
        assertEquals(input["bullets"], encoded["bullets"])
        assertEquals(input["due"], encoded["due"])
        assertEquals(input["updatedAtMs"], encoded["updatedAtMs"])
        assertEquals(input["path"], encoded["path"])
        assertEquals(input["code"], encoded["code"])
        assertEquals(input["client"], encoded["client"])
        assertEquals(input["refs"], encoded["refs"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(ProjectDigestRow.serializer(), encoded),
        )
    }

    @Test
    fun projectDigestRowRejectsWrongShapeForProject() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                ProjectDigestRow.serializer(),
                json.parseToJsonElement("""{"project":{}}"""),
            )
        }
    }

    @Test
    fun projectDigestsOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "digests": [
                    {}
                ]
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(ProjectDigestsOut.serializer(), input)
        val encoded = json.encodeToJsonElement(ProjectDigestsOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["digests"] is JsonArray)
        assertEquals(1, (encoded["digests"] as JsonArray).size)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(ProjectDigestsOut.serializer(), encoded),
        )
    }

    @Test
    fun projectDigestsOutRejectsWrongShapeForDigests() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                ProjectDigestsOut.serializer(),
                json.parseToJsonElement("""{"digests":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun projectLinkedOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "mail": [
                    "mailItem-value"
                ],
                "calendar": [
                    "calendarItem-value"
                ],
                "todo": [
                    "todoItem-value"
                ],
                "workfeed": [
                    "workfeedItem-value"
                ],
                "notebook": [
                    "notebookItem-value"
                ]
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(ProjectLinkedOut.serializer(), input)
        val encoded = json.encodeToJsonElement(ProjectLinkedOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["mail"], encoded["mail"])
        assertEquals(input["calendar"], encoded["calendar"])
        assertEquals(input["todo"], encoded["todo"])
        assertEquals(input["workfeed"], encoded["workfeed"])
        assertEquals(input["notebook"], encoded["notebook"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(ProjectLinkedOut.serializer(), encoded),
        )
    }

    @Test
    fun projectLinkedOutRejectsWrongShapeForMail() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                ProjectLinkedOut.serializer(),
                json.parseToJsonElement("""{"mail":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun projectRefPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "path": "path-value",
                "title": "title-value",
                "summary": "summary-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(ProjectRef.serializer(), input)
        val encoded = json.encodeToJsonElement(ProjectRef.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["path"], encoded["path"])
        assertEquals(input["title"], encoded["title"])
        assertEquals(input["summary"], encoded["summary"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(ProjectRef.serializer(), encoded),
        )
    }

    @Test
    fun projectRefRejectsWrongShapeForPath() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                ProjectRef.serializer(),
                json.parseToJsonElement("""{"path":{}}"""),
            )
        }
    }

    @Test
    fun promptDetailOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-value",
                "title": "title-value",
                "description": "description-value",
                "category": "category-value",
                "text": "text-value",
                "defaultText": "defaultText-value",
                "editable": true,
                "overridden": true,
                "updatedAtMs": 7000000000
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(PromptDetailOut.serializer(), input)
        val encoded = json.encodeToJsonElement(PromptDetailOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["title"], encoded["title"])
        assertEquals(input["description"], encoded["description"])
        assertEquals(input["category"], encoded["category"])
        assertEquals(input["text"], encoded["text"])
        assertEquals(input["defaultText"], encoded["defaultText"])
        assertEquals(input["editable"], encoded["editable"])
        assertEquals(input["overridden"], encoded["overridden"])
        assertEquals(input["updatedAtMs"], encoded["updatedAtMs"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(PromptDetailOut.serializer(), encoded),
        )
    }

    @Test
    fun promptDetailOutRejectsWrongShapeForId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                PromptDetailOut.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun promptListResponsePreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "prompts": [
                    {}
                ],
                "count": 7
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(PromptListResponse.serializer(), input)
        val encoded = json.encodeToJsonElement(PromptListResponse.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["prompts"] is JsonArray)
        assertEquals(1, (encoded["prompts"] as JsonArray).size)
        assertEquals(input["count"], encoded["count"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(PromptListResponse.serializer(), encoded),
        )
    }

    @Test
    fun promptListResponseRejectsWrongShapeForPrompts() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                PromptListResponse.serializer(),
                json.parseToJsonElement("""{"prompts":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun promptRowPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-value",
                "title": "title-value",
                "description": "description-value",
                "category": "category-value",
                "editable": true,
                "overridden": true,
                "updatedAtMs": 7000000000
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(PromptRow.serializer(), input)
        val encoded = json.encodeToJsonElement(PromptRow.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["title"], encoded["title"])
        assertEquals(input["description"], encoded["description"])
        assertEquals(input["category"], encoded["category"])
        assertEquals(input["editable"], encoded["editable"])
        assertEquals(input["overridden"], encoded["overridden"])
        assertEquals(input["updatedAtMs"], encoded["updatedAtMs"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(PromptRow.serializer(), encoded),
        )
    }

    @Test
    fun promptRowRejectsWrongShapeForId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                PromptRow.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun promptTunerReportPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "ran": true,
                "changed": true,
                "reason": "reason-value",
                "error": "error-value",
                "leafSummaries": 7,
                "minSummaries": 7,
                "proposed": [
                    "proposedItem-value"
                ],
                "added": [
                    "addedItem-value"
                ],
                "beforeCount": 7,
                "afterCount": 7
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(PromptTunerReport.serializer(), input)
        val encoded = json.encodeToJsonElement(PromptTunerReport.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["ran"], encoded["ran"])
        assertEquals(input["changed"], encoded["changed"])
        assertEquals(input["reason"], encoded["reason"])
        assertEquals(input["error"], encoded["error"])
        assertEquals(input["leafSummaries"], encoded["leafSummaries"])
        assertEquals(input["minSummaries"], encoded["minSummaries"])
        assertEquals(input["proposed"], encoded["proposed"])
        assertEquals(input["added"], encoded["added"])
        assertEquals(input["beforeCount"], encoded["beforeCount"])
        assertEquals(input["afterCount"], encoded["afterCount"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(PromptTunerReport.serializer(), encoded),
        )
    }

    @Test
    fun promptTunerReportRejectsWrongShapeForRan() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                PromptTunerReport.serializer(),
                json.parseToJsonElement("""{"ran":{}}"""),
            )
        }
    }

    @Test
    fun promptTunerRunResponsePreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "target": "target-value",
                "report": {}
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(PromptTunerRunResponse.serializer(), input)
        val encoded = json.encodeToJsonElement(PromptTunerRunResponse.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["target"], encoded["target"])
        assertTrue(encoded["report"] is JsonObject)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(PromptTunerRunResponse.serializer(), encoded),
        )
    }

    @Test
    fun promptTunerRunResponseRejectsWrongShapeForTarget() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                PromptTunerRunResponse.serializer(),
                json.parseToJsonElement("""{"target":{}}"""),
            )
        }
    }

    @Test
    fun propusLifecycleSummaryPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "system": "system-value",
                "state": "state-value",
                "total": 7,
                "genesis": 7,
                "evolved": 7,
                "review": 7,
                "rejected": 7,
                "rolledBack": 7,
                "attention": 7,
                "latestAt": 7000000000,
                "latestType": "latestType-value",
                "latestSkill": "latestSkill-value",
                "doctrineVersion": "doctrineVersion-value",
                "doctrine": "doctrine-value",
                "sourcePapers": [
                    "sourcePapersItem-value"
                ],
                "filteredSources": [
                    "filteredSourcesItem-value"
                ],
                "principles": [
                    "principlesItem-value"
                ],
                "qualityGates": [
                    "qualityGatesItem-value"
                ],
                "nextActions": [
                    "nextActionsItem-value"
                ],
                "coverageState": "coverageState-value",
                "coverageGaps": [
                    "coverageGapsItem-value"
                ],
                "nextCue": "nextCue-value",
                "qualityGate": "qualityGate-value",
                "attentionCue": "attentionCue-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(PropusLifecycleSummary.serializer(), input)
        val encoded = json.encodeToJsonElement(PropusLifecycleSummary.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["system"], encoded["system"])
        assertEquals(input["state"], encoded["state"])
        assertEquals(input["total"], encoded["total"])
        assertEquals(input["genesis"], encoded["genesis"])
        assertEquals(input["evolved"], encoded["evolved"])
        assertEquals(input["review"], encoded["review"])
        assertEquals(input["rejected"], encoded["rejected"])
        assertEquals(input["rolledBack"], encoded["rolledBack"])
        assertEquals(input["attention"], encoded["attention"])
        assertEquals(input["latestAt"], encoded["latestAt"])
        assertEquals(input["latestType"], encoded["latestType"])
        assertEquals(input["latestSkill"], encoded["latestSkill"])
        assertEquals(input["doctrineVersion"], encoded["doctrineVersion"])
        assertEquals(input["doctrine"], encoded["doctrine"])
        assertEquals(input["sourcePapers"], encoded["sourcePapers"])
        assertEquals(input["filteredSources"], encoded["filteredSources"])
        assertEquals(input["principles"], encoded["principles"])
        assertEquals(input["qualityGates"], encoded["qualityGates"])
        assertEquals(input["nextActions"], encoded["nextActions"])
        assertEquals(input["coverageState"], encoded["coverageState"])
        assertEquals(input["coverageGaps"], encoded["coverageGaps"])
        assertEquals(input["nextCue"], encoded["nextCue"])
        assertEquals(input["qualityGate"], encoded["qualityGate"])
        assertEquals(input["attentionCue"], encoded["attentionCue"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(PropusLifecycleSummary.serializer(), encoded),
        )
    }

    @Test
    fun propusLifecycleSummaryRejectsWrongShapeForSystem() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                PropusLifecycleSummary.serializer(),
                json.parseToJsonElement("""{"system":{}}"""),
            )
        }
    }

    @Test
    fun qATurnPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "q": "q-value",
                "a": "a-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(QATurn.serializer(), input)
        val encoded = json.encodeToJsonElement(QATurn.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["q"], encoded["q"])
        assertEquals(input["a"], encoded["a"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(QATurn.serializer(), encoded),
        )
    }

    @Test
    fun qATurnRejectsWrongShapeForQ() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                QATurn.serializer(),
                json.parseToJsonElement("""{"q":{}}"""),
            )
        }
    }

    @Test
    fun roleModelPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "role": "role-value",
                "model": "model-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(RoleModel.serializer(), input)
        val encoded = json.encodeToJsonElement(RoleModel.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["role"], encoded["role"])
        assertEquals(input["model"], encoded["model"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(RoleModel.serializer(), encoded),
        )
    }

    @Test
    fun roleModelRejectsWrongShapeForRole() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                RoleModel.serializer(),
                json.parseToJsonElement("""{"role":{}}"""),
            )
        }
    }

    @Test
    fun searchAllResultPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "wiki": [
                    {}
                ],
                "diary": [
                    {}
                ],
                "people": [
                    {}
                ]
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(SearchAllResult.serializer(), input)
        val encoded = json.encodeToJsonElement(SearchAllResult.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["wiki"] is JsonArray)
        assertEquals(1, (encoded["wiki"] as JsonArray).size)
        assertTrue(encoded["diary"] is JsonArray)
        assertEquals(1, (encoded["diary"] as JsonArray).size)
        assertTrue(encoded["people"] is JsonArray)
        assertEquals(1, (encoded["people"] as JsonArray).size)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(SearchAllResult.serializer(), encoded),
        )
    }

    @Test
    fun searchAllResultRejectsWrongShapeForWiki() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                SearchAllResult.serializer(),
                json.parseToJsonElement("""{"wiki":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun searchDiaryHitPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "file": "file-value",
                "header": "header-value",
                "content": "content-value",
                "at": 7000000000,
                "score": 1.25
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(SearchDiaryHit.serializer(), input)
        val encoded = json.encodeToJsonElement(SearchDiaryHit.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["file"], encoded["file"])
        assertEquals(input["header"], encoded["header"])
        assertEquals(input["content"], encoded["content"])
        assertEquals(input["at"], encoded["at"])
        assertEquals(input["score"], encoded["score"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(SearchDiaryHit.serializer(), encoded),
        )
    }

    @Test
    fun searchDiaryHitRejectsWrongShapeForFile() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                SearchDiaryHit.serializer(),
                json.parseToJsonElement("""{"file":{}}"""),
            )
        }
    }

    @Test
    fun searchWikiHitPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "path": "path-value",
                "title": "title-value",
                "summary": "summary-value",
                "category": "category-value",
                "snippet": "snippet-value",
                "score": 1.25
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(SearchWikiHit.serializer(), input)
        val encoded = json.encodeToJsonElement(SearchWikiHit.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["path"], encoded["path"])
        assertEquals(input["title"], encoded["title"])
        assertEquals(input["summary"], encoded["summary"])
        assertEquals(input["category"], encoded["category"])
        assertEquals(input["snippet"], encoded["snippet"])
        assertEquals(input["score"], encoded["score"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(SearchWikiHit.serializer(), encoded),
        )
    }

    @Test
    fun searchWikiHitRejectsWrongShapeForPath() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                SearchWikiHit.serializer(),
                json.parseToJsonElement("""{"path":{}}"""),
            )
        }
    }

    @Test
    fun selfCorrectionCandidatePreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-value",
                "status": "status-value",
                "scope": "scope-value",
                "skillName": "skillName-value",
                "sessionKey": "sessionKey-value",
                "title": "title-value",
                "candidate": "candidate-value",
                "evidence": "evidence-value",
                "reason": "reason-value",
                "targetFiles": [
                    "targetFilesItem-value"
                ],
                "proposedChange": "proposedChange-value",
                "risk": "risk-value",
                "source": "source-value",
                "reviewer": "reviewer-value",
                "reviewNote": "reviewNote-value",
                "evidenceKinds": [
                    "evidenceKindsItem-value"
                ],
                "reviewActions": [
                    "reviewActionsItem-value"
                ],
                "createdAt": 7000000000,
                "updatedAt": 7000000000
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(SelfCorrectionCandidate.serializer(), input)
        val encoded = json.encodeToJsonElement(SelfCorrectionCandidate.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["status"], encoded["status"])
        assertEquals(input["scope"], encoded["scope"])
        assertEquals(input["skillName"], encoded["skillName"])
        assertEquals(input["sessionKey"], encoded["sessionKey"])
        assertEquals(input["title"], encoded["title"])
        assertEquals(input["candidate"], encoded["candidate"])
        assertEquals(input["evidence"], encoded["evidence"])
        assertEquals(input["reason"], encoded["reason"])
        assertEquals(input["targetFiles"], encoded["targetFiles"])
        assertEquals(input["proposedChange"], encoded["proposedChange"])
        assertEquals(input["risk"], encoded["risk"])
        assertEquals(input["source"], encoded["source"])
        assertEquals(input["reviewer"], encoded["reviewer"])
        assertEquals(input["reviewNote"], encoded["reviewNote"])
        assertEquals(input["evidenceKinds"], encoded["evidenceKinds"])
        assertEquals(input["reviewActions"], encoded["reviewActions"])
        assertEquals(input["createdAt"], encoded["createdAt"])
        assertEquals(input["updatedAt"], encoded["updatedAt"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(SelfCorrectionCandidate.serializer(), encoded),
        )
    }

    @Test
    fun selfCorrectionCandidateRejectsWrongShapeForId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                SelfCorrectionCandidate.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun selfImprovementCodingFunnelPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "lastCaptureAt": 7000000000,
                "lastReviewAt": 7000000000,
                "rejections7d": 7,
                "promotableRejections7d": 7,
                "lastRejectionAt": 7000000000,
                "lastNudgeAt": 7000000000
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(SelfImprovementCodingFunnel.serializer(), input)
        val encoded = json.encodeToJsonElement(SelfImprovementCodingFunnel.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["lastCaptureAt"], encoded["lastCaptureAt"])
        assertEquals(input["lastReviewAt"], encoded["lastReviewAt"])
        assertEquals(input["rejections7d"], encoded["rejections7d"])
        assertEquals(input["promotableRejections7d"], encoded["promotableRejections7d"])
        assertEquals(input["lastRejectionAt"], encoded["lastRejectionAt"])
        assertEquals(input["lastNudgeAt"], encoded["lastNudgeAt"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(SelfImprovementCodingFunnel.serializer(), encoded),
        )
    }

    @Test
    fun selfImprovementCodingFunnelRejectsWrongShapeForLastCaptureAt() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                SelfImprovementCodingFunnel.serializer(),
                json.parseToJsonElement("""{"lastCaptureAt":{}}"""),
            )
        }
    }

    @Test
    fun selfImprovementCodingListResponsePreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "candidates": [
                    {}
                ],
                "count": 7,
                "statusCounts": [
                    {}
                ],
                "funnel": {}
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(SelfImprovementCodingListResponse.serializer(), input)
        val encoded = json.encodeToJsonElement(SelfImprovementCodingListResponse.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["candidates"] is JsonArray)
        assertEquals(1, (encoded["candidates"] as JsonArray).size)
        assertEquals(input["count"], encoded["count"])
        assertTrue(encoded["statusCounts"] is JsonArray)
        assertEquals(1, (encoded["statusCounts"] as JsonArray).size)
        assertTrue(encoded["funnel"] is JsonObject)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(SelfImprovementCodingListResponse.serializer(), encoded),
        )
    }

    @Test
    fun selfImprovementCodingListResponseRejectsWrongShapeForCandidates() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                SelfImprovementCodingListResponse.serializer(),
                json.parseToJsonElement("""{"candidates":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun selfImprovementCodingStatusCountPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "status": "status-value",
                "count": 7
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(SelfImprovementCodingStatusCount.serializer(), input)
        val encoded = json.encodeToJsonElement(SelfImprovementCodingStatusCount.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["status"], encoded["status"])
        assertEquals(input["count"], encoded["count"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(SelfImprovementCodingStatusCount.serializer(), encoded),
        )
    }

    @Test
    fun selfImprovementCodingStatusCountRejectsWrongShapeForStatus() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                SelfImprovementCodingStatusCount.serializer(),
                json.parseToJsonElement("""{"status":{}}"""),
            )
        }
    }

    @Test
    fun senderRecentOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "count": 7,
                "lastReceivedAt": "lastReceivedAt-value",
                "windowDays": 7,
                "truncated": true
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(SenderRecentOut.serializer(), input)
        val encoded = json.encodeToJsonElement(SenderRecentOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["count"], encoded["count"])
        assertEquals(input["lastReceivedAt"], encoded["lastReceivedAt"])
        assertEquals(input["windowDays"], encoded["windowDays"])
        assertEquals(input["truncated"], encoded["truncated"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(SenderRecentOut.serializer(), encoded),
        )
    }

    @Test
    fun senderRecentOutRejectsWrongShapeForCount() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                SenderRecentOut.serializer(),
                json.parseToJsonElement("""{"count":{}}"""),
            )
        }
    }

    @Test
    fun senderWikiHitOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "path": "path-value",
                "title": "title-value",
                "summary": "summary-value",
                "category": "category-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(SenderWikiHitOut.serializer(), input)
        val encoded = json.encodeToJsonElement(SenderWikiHitOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["path"], encoded["path"])
        assertEquals(input["title"], encoded["title"])
        assertEquals(input["summary"], encoded["summary"])
        assertEquals(input["category"], encoded["category"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(SenderWikiHitOut.serializer(), encoded),
        )
    }

    @Test
    fun senderWikiHitOutRejectsWrongShapeForPath() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                SenderWikiHitOut.serializer(),
                json.parseToJsonElement("""{"path":{}}"""),
            )
        }
    }

    @Test
    fun sessionRowOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "key": "key-value",
                "kind": "kind-value",
                "status": "status-value",
                "channel": "channel-value",
                "model": "model-value",
                "label": "label-value",
                "updatedAtMs": 7000000000,
                "startedAtMs": 7000000000,
                "runtimeMs": 7000000000,
                "totalTokens": 7000000000
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(SessionRowOut.serializer(), input)
        val encoded = json.encodeToJsonElement(SessionRowOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["key"], encoded["key"])
        assertEquals(input["kind"], encoded["kind"])
        assertEquals(input["status"], encoded["status"])
        assertEquals(input["channel"], encoded["channel"])
        assertEquals(input["model"], encoded["model"])
        assertEquals(input["label"], encoded["label"])
        assertEquals(input["updatedAtMs"], encoded["updatedAtMs"])
        assertEquals(input["startedAtMs"], encoded["startedAtMs"])
        assertEquals(input["runtimeMs"], encoded["runtimeMs"])
        assertEquals(input["totalTokens"], encoded["totalTokens"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(SessionRowOut.serializer(), encoded),
        )
    }

    @Test
    fun sessionRowOutRejectsWrongShapeForKey() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                SessionRowOut.serializer(),
                json.parseToJsonElement("""{"key":{}}"""),
            )
        }
    }

    @Test
    fun skillDetailResponsePreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "skill": {},
                "body": "body-value",
                "bodyTruncated": true,
                "path": "path-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(SkillDetailResponse.serializer(), input)
        val encoded = json.encodeToJsonElement(SkillDetailResponse.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["skill"] is JsonObject)
        assertEquals(input["body"], encoded["body"])
        assertEquals(input["bodyTruncated"], encoded["bodyTruncated"])
        assertEquals(input["path"], encoded["path"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(SkillDetailResponse.serializer(), encoded),
        )
    }

    @Test
    fun skillDetailResponseRejectsWrongShapeForSkill() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                SkillDetailResponse.serializer(),
                json.parseToJsonElement("""{"skill":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun skillLifecycleEventPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "type": "type-value",
                "skillName": "skillName-value",
                "at": 7000000000,
                "version": "version-value",
                "detail": "detail-value",
                "route": "route-value",
                "evidence": "evidence-value",
                "targetSignature": "targetSignature-value",
                "editedSurface": "editedSurface-value",
                "expectedBehaviorChange": "expectedBehaviorChange-value",
                "regressionRisk": "regressionRisk-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(SkillLifecycleEvent.serializer(), input)
        val encoded = json.encodeToJsonElement(SkillLifecycleEvent.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["type"], encoded["type"])
        assertEquals(input["skillName"], encoded["skillName"])
        assertEquals(input["at"], encoded["at"])
        assertEquals(input["version"], encoded["version"])
        assertEquals(input["detail"], encoded["detail"])
        assertEquals(input["route"], encoded["route"])
        assertEquals(input["evidence"], encoded["evidence"])
        assertEquals(input["targetSignature"], encoded["targetSignature"])
        assertEquals(input["editedSurface"], encoded["editedSurface"])
        assertEquals(input["expectedBehaviorChange"], encoded["expectedBehaviorChange"])
        assertEquals(input["regressionRisk"], encoded["regressionRisk"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(SkillLifecycleEvent.serializer(), encoded),
        )
    }

    @Test
    fun skillLifecycleEventRejectsWrongShapeForType() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                SkillLifecycleEvent.serializer(),
                json.parseToJsonElement("""{"type":{}}"""),
            )
        }
    }

    @Test
    fun skillRowPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "name": "name-value",
                "description": "description-value",
                "category": "category-value",
                "homepage": "homepage-value",
                "tags": [
                    "tagsItem-value"
                ],
                "relatedSkills": [
                    "relatedSkillsItem-value"
                ],
                "source": "source-value",
                "version": "version-value",
                "origin": "origin-value",
                "createdAt": 7000000000,
                "evolveCount": 7,
                "lastEvolvedAt": 7000000000,
                "totalUses": 7,
                "lastUsedAt": 7000000000,
                "curatorState": "curatorState-value",
                "editable": true,
                "deletable": true,
                "dependencySummary": [
                    "dependencySummaryItem-value"
                ],
                "installSummary": [
                    "installSummaryItem-value"
                ]
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(SkillRow.serializer(), input)
        val encoded = json.encodeToJsonElement(SkillRow.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["name"], encoded["name"])
        assertEquals(input["description"], encoded["description"])
        assertEquals(input["category"], encoded["category"])
        assertEquals(input["homepage"], encoded["homepage"])
        assertEquals(input["tags"], encoded["tags"])
        assertEquals(input["relatedSkills"], encoded["relatedSkills"])
        assertEquals(input["source"], encoded["source"])
        assertEquals(input["version"], encoded["version"])
        assertEquals(input["origin"], encoded["origin"])
        assertEquals(input["createdAt"], encoded["createdAt"])
        assertEquals(input["evolveCount"], encoded["evolveCount"])
        assertEquals(input["lastEvolvedAt"], encoded["lastEvolvedAt"])
        assertEquals(input["totalUses"], encoded["totalUses"])
        assertEquals(input["lastUsedAt"], encoded["lastUsedAt"])
        assertEquals(input["curatorState"], encoded["curatorState"])
        assertEquals(input["editable"], encoded["editable"])
        assertEquals(input["deletable"], encoded["deletable"])
        assertEquals(input["dependencySummary"], encoded["dependencySummary"])
        assertEquals(input["installSummary"], encoded["installSummary"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(SkillRow.serializer(), encoded),
        )
    }

    @Test
    fun skillRowRejectsWrongShapeForName() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                SkillRow.serializer(),
                json.parseToJsonElement("""{"name":{}}"""),
            )
        }
    }

    @Test
    fun skillsLifecycleResponsePreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "events": [
                    {}
                ],
                "count": 7,
                "summary": {}
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(SkillsLifecycleResponse.serializer(), input)
        val encoded = json.encodeToJsonElement(SkillsLifecycleResponse.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["events"] is JsonArray)
        assertEquals(1, (encoded["events"] as JsonArray).size)
        assertEquals(input["count"], encoded["count"])
        assertTrue(encoded["summary"] is JsonObject)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(SkillsLifecycleResponse.serializer(), encoded),
        )
    }

    @Test
    fun skillsLifecycleResponseRejectsWrongShapeForEvents() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                SkillsLifecycleResponse.serializer(),
                json.parseToJsonElement("""{"events":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun skillsListResponsePreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "skills": [
                    {}
                ],
                "count": 7
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(SkillsListResponse.serializer(), input)
        val encoded = json.encodeToJsonElement(SkillsListResponse.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertTrue(encoded["skills"] is JsonArray)
        assertEquals(1, (encoded["skills"] as JsonArray).size)
        assertEquals(input["count"], encoded["count"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(SkillsListResponse.serializer(), encoded),
        )
    }

    @Test
    fun skillsListResponseRejectsWrongShapeForSkills() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                SkillsListResponse.serializer(),
                json.parseToJsonElement("""{"skills":"wrong-shape"}"""),
            )
        }
    }

    @Test
    fun todoOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-value",
                "title": "title-value",
                "note": "note-value",
                "due": "due-value",
                "dueAllDay": true,
                "done": true,
                "doneAt": "doneAt-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(TodoOut.serializer(), input)
        val encoded = json.encodeToJsonElement(TodoOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["title"], encoded["title"])
        assertEquals(input["note"], encoded["note"])
        assertEquals(input["due"], encoded["due"])
        assertEquals(input["dueAllDay"], encoded["dueAllDay"])
        assertEquals(input["done"], encoded["done"])
        assertEquals(input["doneAt"], encoded["doneAt"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(TodoOut.serializer(), encoded),
        )
    }

    @Test
    fun todoOutRejectsWrongShapeForId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                TodoOut.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun topicDocOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "key": "key-value",
                "name": "name-value",
                "content": "content-value",
                "size": 7000000000,
                "modified": "modified-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(TopicDocOut.serializer(), input)
        val encoded = json.encodeToJsonElement(TopicDocOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["key"], encoded["key"])
        assertEquals(input["name"], encoded["name"])
        assertEquals(input["content"], encoded["content"])
        assertEquals(input["size"], encoded["size"])
        assertEquals(input["modified"], encoded["modified"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(TopicDocOut.serializer(), encoded),
        )
    }

    @Test
    fun topicDocOutRejectsWrongShapeForKey() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                TopicDocOut.serializer(),
                json.parseToJsonElement("""{"key":{}}"""),
            )
        }
    }

    @Test
    fun topicDocWriteOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "key": "key-value",
                "name": "name-value",
                "size": 7000000000,
                "modified": "modified-value",
                "applied": true
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(TopicDocWriteOut.serializer(), input)
        val encoded = json.encodeToJsonElement(TopicDocWriteOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["key"], encoded["key"])
        assertEquals(input["name"], encoded["name"])
        assertEquals(input["size"], encoded["size"])
        assertEquals(input["modified"], encoded["modified"])
        assertEquals(input["applied"], encoded["applied"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(TopicDocWriteOut.serializer(), encoded),
        )
    }

    @Test
    fun topicDocWriteOutRejectsWrongShapeForKey() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                TopicDocWriteOut.serializer(),
                json.parseToJsonElement("""{"key":{}}"""),
            )
        }
    }

    @Test
    fun transcriptAttachmentOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "type": "type-value",
                "mimeType": "mimeType-value",
                "url": "url-value",
                "data": "data-value",
                "name": "name-value",
                "size": 7000000000
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(TranscriptAttachmentOut.serializer(), input)
        val encoded = json.encodeToJsonElement(TranscriptAttachmentOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["type"], encoded["type"])
        assertEquals(input["mimeType"], encoded["mimeType"])
        assertEquals(input["url"], encoded["url"])
        assertEquals(input["data"], encoded["data"])
        assertEquals(input["name"], encoded["name"])
        assertEquals(input["size"], encoded["size"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(TranscriptAttachmentOut.serializer(), encoded),
        )
    }

    @Test
    fun transcriptAttachmentOutRejectsWrongShapeForType() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                TranscriptAttachmentOut.serializer(),
                json.parseToJsonElement("""{"type":{}}"""),
            )
        }
    }

    @Test
    fun transcriptMsgOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-value",
                "role": "role-value",
                "content": "content-value",
                "attachments": [
                    {}
                ],
                "timestampMs": 7000000000
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(TranscriptMsgOut.serializer(), input)
        val encoded = json.encodeToJsonElement(TranscriptMsgOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["role"], encoded["role"])
        assertEquals(input["content"], encoded["content"])
        assertTrue(encoded["attachments"] is JsonArray)
        assertEquals(1, (encoded["attachments"] as JsonArray).size)
        assertEquals(input["timestampMs"], encoded["timestampMs"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(TranscriptMsgOut.serializer(), encoded),
        )
    }

    @Test
    fun transcriptMsgOutRejectsWrongShapeForId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                TranscriptMsgOut.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun wormholeModelOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "name": "name-value",
                "protocol": "protocol-value",
                "local": true,
                "thinking": true,
                "source": "source-value",
                "keyHealth": "keyHealth-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(WormholeModelOut.serializer(), input)
        val encoded = json.encodeToJsonElement(WormholeModelOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["name"], encoded["name"])
        assertEquals(input["protocol"], encoded["protocol"])
        assertEquals(input["local"], encoded["local"])
        assertEquals(input["thinking"], encoded["thinking"])
        assertEquals(input["source"], encoded["source"])
        assertEquals(input["keyHealth"], encoded["keyHealth"])

        assertEquals(
            decoded,
            json.decodeFromJsonElement(WormholeModelOut.serializer(), encoded),
        )
    }

    @Test
    fun wormholeModelOutRejectsWrongShapeForName() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                WormholeModelOut.serializer(),
                json.parseToJsonElement("""{"name":{}}"""),
            )
        }
    }

    @Test
    fun wormholeStatusOutPreservesEveryPresentWireField() {
        val input = json.parseToJsonElement(
            """{
                "reachable": true,
                "listen": "listen-value",
                "localOnly": true,
                "effortRouting": true,
                "auto": [
                    "autoItem-value"
                ],
                "models": [
                    {}
                ]
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(WormholeStatusOut.serializer(), input)
        val encoded = json.encodeToJsonElement(WormholeStatusOut.serializer(), decoded).jsonObject

        assertEquals(input.keys, encoded.keys)
        assertEquals(input["reachable"], encoded["reachable"])
        assertEquals(input["listen"], encoded["listen"])
        assertEquals(input["localOnly"], encoded["localOnly"])
        assertEquals(input["effortRouting"], encoded["effortRouting"])
        assertEquals(input["auto"], encoded["auto"])
        assertTrue(encoded["models"] is JsonArray)
        assertEquals(1, (encoded["models"] as JsonArray).size)

        assertEquals(
            decoded,
            json.decodeFromJsonElement(WormholeStatusOut.serializer(), encoded),
        )
    }

    @Test
    fun wormholeStatusOutRejectsWrongShapeForReachable() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                WormholeStatusOut.serializer(),
                json.parseToJsonElement("""{"reachable":{}}"""),
            )
        }
    }
}
