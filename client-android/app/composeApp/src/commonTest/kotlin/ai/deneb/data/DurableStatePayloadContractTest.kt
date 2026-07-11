package ai.deneb.data

import ai.deneb.TerminalLine
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.put
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

/**
 * Durable-state compatibility suite for every persisted assistant record.
 *
 * The app may reopen data written months earlier after multiple gateway/client
 * upgrades. These tests exercise complete non-default snapshots, nested messages,
 * attachments, terminal transcripts, enum evolution, unknown future fields, and
 * malformed storage so migrations never silently turn durable state into defaults.
 */
class DurableStatePayloadContractTest {
    private val json = Json {
        ignoreUnknownKeys = true
        coerceInputValues = true
        encodeDefaults = true
        classDiscriminator = "type"
    }

    @Test
    fun attachmentPreservesCompleteNonDefaultSnapshot() {
        val input = json.parseToJsonElement(
            """{
                "data": "data-한글-value",
                "mimeType": "mimeType-한글-value",
                "fileName": "fileName-한글-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(Attachment.serializer(), input)
        val encoded = json.encodeToJsonElement(Attachment.serializer(), decoded).jsonObject

        assertEquals(setOf("data", "mimeType", "fileName"), encoded.keys)
        assertEquals(input["data"], encoded["data"])
        assertEquals(input["mimeType"], encoded["mimeType"])
        assertEquals(input["fileName"], encoded["fileName"])
        assertEquals(decoded, json.decodeFromJsonElement(Attachment.serializer(), encoded))
    }

    @Test
    fun attachmentIgnoresUnknownFutureStorageFields() {
        val baseline = json.parseToJsonElement(
            """{
                "data": "data-한글-value",
                "mimeType": "mimeType-한글-value",
                "fileName": "fileName-한글-value"
            }
            """.trimIndent(),
        ).jsonObject
        val future = JsonObject(
            baseline + (
                "futureStorage" to buildJsonObject {
                    put("version", 99)
                    put("source", "future-client")
                }
                ),
        )

        val expected = json.decodeFromJsonElement(Attachment.serializer(), baseline)
        val actual = json.decodeFromJsonElement(Attachment.serializer(), future)

        assertEquals(expected, actual)
    }

    @Test
    fun attachmentRejectsMalformedDataWithoutPartialDecode() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                Attachment.serializer(),
                json.parseToJsonElement("""{"data":{}}"""),
            )
        }
    }

    @Test
    fun conversationPreservesCompleteNonDefaultSnapshot() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-한글-value",
                "messages": [
                    {
                        "id": "id-한글-value",
                        "role": "role-한글-value",
                        "content": "content-한글-value",
                        "attachments": [
                            {
                                "data": "data-한글-value",
                                "mimeType": "mimeType-한글-value",
                                "fileName": "fileName-한글-value"
                            }
                        ],
                        "uiSubmission": {
                            "sourceContent": "sourceContent-한글-value",
                            "values": {
                                "key": "valuesValue-한글-value"
                            },
                            "pressedEvent": "pressedEvent-한글-value"
                        },
                        "isThinking": true,
                        "data": "data-한글-value",
                        "fileName": "fileName-한글-value"
                    }
                ],
                "createdAt": 7000000000,
                "updatedAt": 7000000000,
                "title": "title-한글-value",
                "type": "type-한글-value",
                "shellTranscript": [
                    {
                        "type": "command",
                        "text": "echo 건강도"
                    }
                ]
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(Conversation.serializer(), input)
        val encoded = json.encodeToJsonElement(Conversation.serializer(), decoded).jsonObject

        assertEquals(setOf("id", "messages", "createdAt", "updatedAt", "title", "type", "shellTranscript"), encoded.keys)
        assertEquals(input["id"], encoded["id"])
        assertTrue(encoded["messages"] is JsonArray)
        assertEquals(1, (encoded["messages"] as JsonArray).size)
        assertEquals(input["createdAt"], encoded["createdAt"])
        assertEquals(input["updatedAt"], encoded["updatedAt"])
        assertEquals(input["title"], encoded["title"])
        assertEquals(input["type"], encoded["type"])
        assertTrue(encoded["shellTranscript"] is JsonArray)
        assertEquals(1, (encoded["shellTranscript"] as JsonArray).size)
        assertEquals(decoded, json.decodeFromJsonElement(Conversation.serializer(), encoded))
    }

    @Test
    fun conversationIgnoresUnknownFutureStorageFields() {
        val baseline = json.parseToJsonElement(
            """{
                "id": "id-한글-value",
                "messages": [
                    {
                        "id": "id-한글-value",
                        "role": "role-한글-value",
                        "content": "content-한글-value",
                        "attachments": [
                            {
                                "data": "data-한글-value",
                                "mimeType": "mimeType-한글-value",
                                "fileName": "fileName-한글-value"
                            }
                        ],
                        "uiSubmission": {
                            "sourceContent": "sourceContent-한글-value",
                            "values": {
                                "key": "valuesValue-한글-value"
                            },
                            "pressedEvent": "pressedEvent-한글-value"
                        },
                        "isThinking": true,
                        "data": "data-한글-value",
                        "fileName": "fileName-한글-value"
                    }
                ],
                "createdAt": 7000000000,
                "updatedAt": 7000000000,
                "title": "title-한글-value",
                "type": "type-한글-value",
                "shellTranscript": [
                    {
                        "type": "command",
                        "text": "echo 건강도"
                    }
                ]
            }
            """.trimIndent(),
        ).jsonObject
        val future = JsonObject(
            baseline + (
                "futureStorage" to buildJsonObject {
                    put("version", 99)
                    put("source", "future-client")
                }
                ),
        )

        val expected = json.decodeFromJsonElement(Conversation.serializer(), baseline)
        val actual = json.decodeFromJsonElement(Conversation.serializer(), future)

        assertEquals(expected, actual)
    }

    @Test
    fun conversationRejectsMalformedIdWithoutPartialDecode() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                Conversation.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun messagePreservesCompleteNonDefaultSnapshot() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-한글-value",
                "role": "role-한글-value",
                "content": "content-한글-value",
                "attachments": [
                    {
                        "data": "data-한글-value",
                        "mimeType": "mimeType-한글-value",
                        "fileName": "fileName-한글-value"
                    }
                ],
                "uiSubmission": {
                    "sourceContent": "sourceContent-한글-value",
                    "values": {
                        "key": "valuesValue-한글-value"
                    },
                    "pressedEvent": "pressedEvent-한글-value"
                },
                "isThinking": true,
                "mimeType": "application/legacy",
                "data": "data-한글-value",
                "fileName": "fileName-한글-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(Conversation.Message.serializer(), input)
        val encoded = json.encodeToJsonElement(Conversation.Message.serializer(), decoded).jsonObject

        assertEquals(
            setOf("id", "role", "content", "attachments", "uiSubmission", "isThinking", "mimeType", "data", "fileName"),
            encoded.keys,
        )
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["role"], encoded["role"])
        assertEquals(input["content"], encoded["content"])
        assertTrue(encoded["attachments"] is JsonArray)
        assertEquals(1, (encoded["attachments"] as JsonArray).size)
        assertTrue(encoded["uiSubmission"] is JsonObject)
        assertEquals(input["isThinking"], encoded["isThinking"])
        assertEquals(input["mimeType"], encoded["mimeType"])
        assertEquals(input["data"], encoded["data"])
        assertEquals(input["fileName"], encoded["fileName"])
        assertEquals(decoded, json.decodeFromJsonElement(Conversation.Message.serializer(), encoded))
    }

    @Test
    fun messageIgnoresUnknownFutureStorageFields() {
        val baseline = json.parseToJsonElement(
            """{
                "id": "id-한글-value",
                "role": "role-한글-value",
                "content": "content-한글-value",
                "attachments": [
                    {
                        "data": "data-한글-value",
                        "mimeType": "mimeType-한글-value",
                        "fileName": "fileName-한글-value"
                    }
                ],
                "uiSubmission": {
                    "sourceContent": "sourceContent-한글-value",
                    "values": {
                        "key": "valuesValue-한글-value"
                    },
                    "pressedEvent": "pressedEvent-한글-value"
                },
                "isThinking": true,
                "data": "data-한글-value",
                "fileName": "fileName-한글-value"
            }
            """.trimIndent(),
        ).jsonObject
        val future = JsonObject(
            baseline + (
                "futureStorage" to buildJsonObject {
                    put("version", 99)
                    put("source", "future-client")
                }
                ),
        )

        val expected = json.decodeFromJsonElement(Conversation.Message.serializer(), baseline)
        val actual = json.decodeFromJsonElement(Conversation.Message.serializer(), future)

        assertEquals(expected, actual)
    }

    @Test
    fun messageRejectsMalformedIdWithoutPartialDecode() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                Conversation.Message.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun uiSubmissionPreservesCompleteNonDefaultSnapshot() {
        val input = json.parseToJsonElement(
            """{
                "sourceContent": "sourceContent-한글-value",
                "values": {
                    "key": "valuesValue-한글-value"
                },
                "pressedEvent": "pressedEvent-한글-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(UiSubmission.serializer(), input)
        val encoded = json.encodeToJsonElement(UiSubmission.serializer(), decoded).jsonObject

        assertEquals(setOf("sourceContent", "values", "pressedEvent"), encoded.keys)
        assertEquals(input["sourceContent"], encoded["sourceContent"])
        assertTrue(encoded["values"] is JsonObject)
        assertTrue("key" in (encoded["values"] as JsonObject))
        assertEquals(input["pressedEvent"], encoded["pressedEvent"])
        assertEquals(decoded, json.decodeFromJsonElement(UiSubmission.serializer(), encoded))
    }

    @Test
    fun uiSubmissionIgnoresUnknownFutureStorageFields() {
        val baseline = json.parseToJsonElement(
            """{
                "sourceContent": "sourceContent-한글-value",
                "values": {
                    "key": "valuesValue-한글-value"
                },
                "pressedEvent": "pressedEvent-한글-value"
            }
            """.trimIndent(),
        ).jsonObject
        val future = JsonObject(
            baseline + (
                "futureStorage" to buildJsonObject {
                    put("version", 99)
                    put("source", "future-client")
                }
                ),
        )

        val expected = json.decodeFromJsonElement(UiSubmission.serializer(), baseline)
        val actual = json.decodeFromJsonElement(UiSubmission.serializer(), future)

        assertEquals(expected, actual)
    }

    @Test
    fun uiSubmissionRejectsMalformedSourceContentWithoutPartialDecode() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                UiSubmission.serializer(),
                json.parseToJsonElement("""{"sourceContent":{}}"""),
            )
        }
    }

    @Test
    fun conversationsDataPreservesCompleteNonDefaultSnapshot() {
        val input = json.parseToJsonElement(
            """{
                "version": 17,
                "conversations": [
                    {
                        "id": "id-한글-value",
                        "messages": [
                            {
                                "id": "id-한글-value",
                                "role": "role-한글-value",
                                "content": "content-한글-value",
                                "attachments": [
                                    {
                                        "data": "data-한글-value",
                                        "mimeType": "mimeType-한글-value",
                                        "fileName": "fileName-한글-value"
                                    }
                                ],
                                "uiSubmission": {
                                    "sourceContent": "sourceContent-한글-value",
                                    "values": {
                                        "key": "valuesValue-한글-value"
                                    },
                                    "pressedEvent": "pressedEvent-한글-value"
                                },
                                "isThinking": true,
                                "data": "data-한글-value",
                                "fileName": "fileName-한글-value"
                            }
                        ],
                        "createdAt": 7000000000,
                        "updatedAt": 7000000000,
                        "title": "title-한글-value",
                        "type": "type-한글-value",
                        "shellTranscript": [
                            {
                                "type": "command",
                                "text": "echo 건강도"
                            }
                        ]
                    }
                ]
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(ConversationsData.serializer(), input)
        val encoded = json.encodeToJsonElement(ConversationsData.serializer(), decoded).jsonObject

        assertEquals(setOf("version", "conversations"), encoded.keys)
        assertEquals(input["version"], encoded["version"])
        assertTrue(encoded["conversations"] is JsonArray)
        assertEquals(1, (encoded["conversations"] as JsonArray).size)
        assertEquals(decoded, json.decodeFromJsonElement(ConversationsData.serializer(), encoded))
    }

    @Test
    fun conversationsDataIgnoresUnknownFutureStorageFields() {
        val baseline = json.parseToJsonElement(
            """{
                "version": 17,
                "conversations": [
                    {
                        "id": "id-한글-value",
                        "messages": [
                            {
                                "id": "id-한글-value",
                                "role": "role-한글-value",
                                "content": "content-한글-value",
                                "attachments": [
                                    {
                                        "data": "data-한글-value",
                                        "mimeType": "mimeType-한글-value",
                                        "fileName": "fileName-한글-value"
                                    }
                                ],
                                "uiSubmission": {
                                    "sourceContent": "sourceContent-한글-value",
                                    "values": {
                                        "key": "valuesValue-한글-value"
                                    },
                                    "pressedEvent": "pressedEvent-한글-value"
                                },
                                "isThinking": true,
                                "data": "data-한글-value",
                                "fileName": "fileName-한글-value"
                            }
                        ],
                        "createdAt": 7000000000,
                        "updatedAt": 7000000000,
                        "title": "title-한글-value",
                        "type": "type-한글-value",
                        "shellTranscript": [
                            {
                                "type": "command",
                                "text": "echo 건강도"
                            }
                        ]
                    }
                ]
            }
            """.trimIndent(),
        ).jsonObject
        val future = JsonObject(
            baseline + (
                "futureStorage" to buildJsonObject {
                    put("version", 99)
                    put("source", "future-client")
                }
                ),
        )

        val expected = json.decodeFromJsonElement(ConversationsData.serializer(), baseline)
        val actual = json.decodeFromJsonElement(ConversationsData.serializer(), future)

        assertEquals(expected, actual)
    }

    @Test
    fun conversationsDataRejectsMalformedVersionWithoutPartialDecode() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                ConversationsData.serializer(),
                json.parseToJsonElement("""{"version":{}}"""),
            )
        }
    }

    @Test
    fun emailAccountPreservesCompleteNonDefaultSnapshot() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-한글-value",
                "email": "email-한글-value",
                "displayName": "displayName-한글-value",
                "imapHost": "imapHost-한글-value",
                "imapPort": 17,
                "smtpHost": "smtpHost-한글-value",
                "smtpPort": 17,
                "username": "username-한글-value",
                "useStartTls": true
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(EmailAccount.serializer(), input)
        val encoded = json.encodeToJsonElement(EmailAccount.serializer(), decoded).jsonObject

        assertEquals(setOf("id", "email", "displayName", "imapHost", "imapPort", "smtpHost", "smtpPort", "username", "useStartTls"), encoded.keys)
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["email"], encoded["email"])
        assertEquals(input["displayName"], encoded["displayName"])
        assertEquals(input["imapHost"], encoded["imapHost"])
        assertEquals(input["imapPort"], encoded["imapPort"])
        assertEquals(input["smtpHost"], encoded["smtpHost"])
        assertEquals(input["smtpPort"], encoded["smtpPort"])
        assertEquals(input["username"], encoded["username"])
        assertEquals(input["useStartTls"], encoded["useStartTls"])
        assertEquals(decoded, json.decodeFromJsonElement(EmailAccount.serializer(), encoded))
    }

    @Test
    fun emailAccountIgnoresUnknownFutureStorageFields() {
        val baseline = json.parseToJsonElement(
            """{
                "id": "id-한글-value",
                "email": "email-한글-value",
                "displayName": "displayName-한글-value",
                "imapHost": "imapHost-한글-value",
                "imapPort": 17,
                "smtpHost": "smtpHost-한글-value",
                "smtpPort": 17,
                "username": "username-한글-value",
                "useStartTls": true
            }
            """.trimIndent(),
        ).jsonObject
        val future = JsonObject(
            baseline + (
                "futureStorage" to buildJsonObject {
                    put("version", 99)
                    put("source", "future-client")
                }
                ),
        )

        val expected = json.decodeFromJsonElement(EmailAccount.serializer(), baseline)
        val actual = json.decodeFromJsonElement(EmailAccount.serializer(), future)

        assertEquals(expected, actual)
    }

    @Test
    fun emailAccountRejectsMalformedIdWithoutPartialDecode() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                EmailAccount.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun emailMessagePreservesCompleteNonDefaultSnapshot() {
        val input = json.parseToJsonElement(
            """{
                "uid": 7000000000,
                "accountId": "accountId-한글-value",
                "from": "from-한글-value",
                "to": "to-한글-value",
                "subject": "subject-한글-value",
                "date": "date-한글-value",
                "preview": "preview-한글-value",
                "body": "body-한글-value",
                "bodyHtml": "bodyHtml-한글-value",
                "messageId": "messageId-한글-value",
                "isRead": true,
                "listUnsubscribe": "listUnsubscribe-한글-value",
                "listUnsubscribePost": "listUnsubscribePost-한글-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(EmailMessage.serializer(), input)
        val encoded = json.encodeToJsonElement(EmailMessage.serializer(), decoded).jsonObject

        assertEquals(setOf("uid", "accountId", "from", "to", "subject", "date", "preview", "body", "bodyHtml", "messageId", "isRead", "listUnsubscribe", "listUnsubscribePost"), encoded.keys)
        assertEquals(input["uid"], encoded["uid"])
        assertEquals(input["accountId"], encoded["accountId"])
        assertEquals(input["from"], encoded["from"])
        assertEquals(input["to"], encoded["to"])
        assertEquals(input["subject"], encoded["subject"])
        assertEquals(input["date"], encoded["date"])
        assertEquals(input["preview"], encoded["preview"])
        assertEquals(input["body"], encoded["body"])
        assertEquals(input["bodyHtml"], encoded["bodyHtml"])
        assertEquals(input["messageId"], encoded["messageId"])
        assertEquals(input["isRead"], encoded["isRead"])
        assertEquals(input["listUnsubscribe"], encoded["listUnsubscribe"])
        assertEquals(input["listUnsubscribePost"], encoded["listUnsubscribePost"])
        assertEquals(decoded, json.decodeFromJsonElement(EmailMessage.serializer(), encoded))
    }

    @Test
    fun emailMessageIgnoresUnknownFutureStorageFields() {
        val baseline = json.parseToJsonElement(
            """{
                "uid": 7000000000,
                "accountId": "accountId-한글-value",
                "from": "from-한글-value",
                "to": "to-한글-value",
                "subject": "subject-한글-value",
                "date": "date-한글-value",
                "preview": "preview-한글-value",
                "body": "body-한글-value",
                "bodyHtml": "bodyHtml-한글-value",
                "messageId": "messageId-한글-value",
                "isRead": true,
                "listUnsubscribe": "listUnsubscribe-한글-value",
                "listUnsubscribePost": "listUnsubscribePost-한글-value"
            }
            """.trimIndent(),
        ).jsonObject
        val future = JsonObject(
            baseline + (
                "futureStorage" to buildJsonObject {
                    put("version", 99)
                    put("source", "future-client")
                }
                ),
        )

        val expected = json.decodeFromJsonElement(EmailMessage.serializer(), baseline)
        val actual = json.decodeFromJsonElement(EmailMessage.serializer(), future)

        assertEquals(expected, actual)
    }

    @Test
    fun emailMessageRejectsMalformedUidWithoutPartialDecode() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                EmailMessage.serializer(),
                json.parseToJsonElement("""{"uid":{}}"""),
            )
        }
    }

    @Test
    fun emailSyncStatePreservesCompleteNonDefaultSnapshot() {
        val input = json.parseToJsonElement(
            """{
                "accountId": "accountId-한글-value",
                "lastSeenUid": 7000000000,
                "lastSyncEpochMs": 7000000000,
                "unreadCount": 17,
                "lastAttemptEpochMs": 7000000000,
                "lastError": "lastError-한글-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(EmailSyncState.serializer(), input)
        val encoded = json.encodeToJsonElement(EmailSyncState.serializer(), decoded).jsonObject

        assertEquals(setOf("accountId", "lastSeenUid", "lastSyncEpochMs", "unreadCount", "lastAttemptEpochMs", "lastError"), encoded.keys)
        assertEquals(input["accountId"], encoded["accountId"])
        assertEquals(input["lastSeenUid"], encoded["lastSeenUid"])
        assertEquals(input["lastSyncEpochMs"], encoded["lastSyncEpochMs"])
        assertEquals(input["unreadCount"], encoded["unreadCount"])
        assertEquals(input["lastAttemptEpochMs"], encoded["lastAttemptEpochMs"])
        assertEquals(input["lastError"], encoded["lastError"])
        assertEquals(decoded, json.decodeFromJsonElement(EmailSyncState.serializer(), encoded))
    }

    @Test
    fun emailSyncStateIgnoresUnknownFutureStorageFields() {
        val baseline = json.parseToJsonElement(
            """{
                "accountId": "accountId-한글-value",
                "lastSeenUid": 7000000000,
                "lastSyncEpochMs": 7000000000,
                "unreadCount": 17,
                "lastAttemptEpochMs": 7000000000,
                "lastError": "lastError-한글-value"
            }
            """.trimIndent(),
        ).jsonObject
        val future = JsonObject(
            baseline + (
                "futureStorage" to buildJsonObject {
                    put("version", 99)
                    put("source", "future-client")
                }
                ),
        )

        val expected = json.decodeFromJsonElement(EmailSyncState.serializer(), baseline)
        val actual = json.decodeFromJsonElement(EmailSyncState.serializer(), future)

        assertEquals(expected, actual)
    }

    @Test
    fun emailSyncStateRejectsMalformedAccountIdWithoutPartialDecode() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                EmailSyncState.serializer(),
                json.parseToJsonElement("""{"accountId":{}}"""),
            )
        }
    }

    @Test
    fun heartbeatLogEntryPreservesCompleteNonDefaultSnapshot() {
        val input = json.parseToJsonElement(
            """{
                "timestampEpochMs": 7000000000,
                "success": true,
                "error": "error-한글-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(HeartbeatLogEntry.serializer(), input)
        val encoded = json.encodeToJsonElement(HeartbeatLogEntry.serializer(), decoded).jsonObject

        assertEquals(setOf("timestampEpochMs", "success", "error"), encoded.keys)
        assertEquals(input["timestampEpochMs"], encoded["timestampEpochMs"])
        assertEquals(input["success"], encoded["success"])
        assertEquals(input["error"], encoded["error"])
        assertEquals(decoded, json.decodeFromJsonElement(HeartbeatLogEntry.serializer(), encoded))
    }

    @Test
    fun heartbeatLogEntryIgnoresUnknownFutureStorageFields() {
        val baseline = json.parseToJsonElement(
            """{
                "timestampEpochMs": 7000000000,
                "success": true,
                "error": "error-한글-value"
            }
            """.trimIndent(),
        ).jsonObject
        val future = JsonObject(
            baseline + (
                "futureStorage" to buildJsonObject {
                    put("version", 99)
                    put("source", "future-client")
                }
                ),
        )

        val expected = json.decodeFromJsonElement(HeartbeatLogEntry.serializer(), baseline)
        val actual = json.decodeFromJsonElement(HeartbeatLogEntry.serializer(), future)

        assertEquals(expected, actual)
    }

    @Test
    fun heartbeatLogEntryRejectsMalformedTimestampEpochMsWithoutPartialDecode() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                HeartbeatLogEntry.serializer(),
                json.parseToJsonElement("""{"timestampEpochMs":{}}"""),
            )
        }
    }

    @Test
    fun heartbeatConfigPreservesCompleteNonDefaultSnapshot() {
        val input = json.parseToJsonElement(
            """{
                "enabled": true,
                "intervalMinutes": 17,
                "activeHoursStart": 17,
                "activeHoursEnd": 17,
                "lastHeartbeatEpochMs": 7000000000,
                "heartbeatInstanceId": "heartbeatInstanceId-한글-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(HeartbeatConfig.serializer(), input)
        val encoded = json.encodeToJsonElement(HeartbeatConfig.serializer(), decoded).jsonObject

        assertEquals(setOf("enabled", "intervalMinutes", "activeHoursStart", "activeHoursEnd", "lastHeartbeatEpochMs", "heartbeatInstanceId"), encoded.keys)
        assertEquals(input["enabled"], encoded["enabled"])
        assertEquals(input["intervalMinutes"], encoded["intervalMinutes"])
        assertEquals(input["activeHoursStart"], encoded["activeHoursStart"])
        assertEquals(input["activeHoursEnd"], encoded["activeHoursEnd"])
        assertEquals(input["lastHeartbeatEpochMs"], encoded["lastHeartbeatEpochMs"])
        assertEquals(input["heartbeatInstanceId"], encoded["heartbeatInstanceId"])
        assertEquals(decoded, json.decodeFromJsonElement(HeartbeatConfig.serializer(), encoded))
    }

    @Test
    fun heartbeatConfigIgnoresUnknownFutureStorageFields() {
        val baseline = json.parseToJsonElement(
            """{
                "enabled": true,
                "intervalMinutes": 17,
                "activeHoursStart": 17,
                "activeHoursEnd": 17,
                "lastHeartbeatEpochMs": 7000000000,
                "heartbeatInstanceId": "heartbeatInstanceId-한글-value"
            }
            """.trimIndent(),
        ).jsonObject
        val future = JsonObject(
            baseline + (
                "futureStorage" to buildJsonObject {
                    put("version", 99)
                    put("source", "future-client")
                }
                ),
        )

        val expected = json.decodeFromJsonElement(HeartbeatConfig.serializer(), baseline)
        val actual = json.decodeFromJsonElement(HeartbeatConfig.serializer(), future)

        assertEquals(expected, actual)
    }

    @Test
    fun heartbeatConfigRejectsMalformedEnabledWithoutPartialDecode() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                HeartbeatConfig.serializer(),
                json.parseToJsonElement("""{"enabled":{}}"""),
            )
        }
    }

    @Test
    fun memoryEntryPreservesCompleteNonDefaultSnapshot() {
        val input = json.parseToJsonElement(
            """{
                "key": "key-한글-value",
                "content": "content-한글-value",
                "createdAt": 7000000000,
                "updatedAt": 7000000000,
                "category": "LEARNING",
                "hitCount": 17,
                "source": "source-한글-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(MemoryEntry.serializer(), input)
        val encoded = json.encodeToJsonElement(MemoryEntry.serializer(), decoded).jsonObject

        assertEquals(setOf("key", "content", "createdAt", "updatedAt", "category", "hitCount", "source"), encoded.keys)
        assertEquals(input["key"], encoded["key"])
        assertEquals(input["content"], encoded["content"])
        assertEquals(input["createdAt"], encoded["createdAt"])
        assertEquals(input["updatedAt"], encoded["updatedAt"])
        assertEquals(input["category"], encoded["category"])
        assertEquals(input["hitCount"], encoded["hitCount"])
        assertEquals(input["source"], encoded["source"])
        assertEquals(decoded, json.decodeFromJsonElement(MemoryEntry.serializer(), encoded))
    }

    @Test
    fun memoryEntryIgnoresUnknownFutureStorageFields() {
        val baseline = json.parseToJsonElement(
            """{
                "key": "key-한글-value",
                "content": "content-한글-value",
                "createdAt": 7000000000,
                "updatedAt": 7000000000,
                "category": "LEARNING",
                "hitCount": 17,
                "source": "source-한글-value"
            }
            """.trimIndent(),
        ).jsonObject
        val future = JsonObject(
            baseline + (
                "futureStorage" to buildJsonObject {
                    put("version", 99)
                    put("source", "future-client")
                }
                ),
        )

        val expected = json.decodeFromJsonElement(MemoryEntry.serializer(), baseline)
        val actual = json.decodeFromJsonElement(MemoryEntry.serializer(), future)

        assertEquals(expected, actual)
    }

    @Test
    fun memoryEntryRejectsMalformedKeyWithoutPartialDecode() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                MemoryEntry.serializer(),
                json.parseToJsonElement("""{"key":{}}"""),
            )
        }
    }

    @Test
    fun scheduledTaskPreservesCompleteNonDefaultSnapshot() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-한글-value",
                "description": "description-한글-value",
                "prompt": "prompt-한글-value",
                "scheduledAtEpochMs": 7000000000,
                "createdAtEpochMs": 7000000000,
                "cron": "cron-한글-value",
                "trigger": "CRON",
                "status": "COMPLETED",
                "lastResult": "lastResult-한글-value",
                "consecutiveFailures": 17,
                "recentExecutions": [
                    {
                        "timestampEpochMs": 6999999999,
                        "success": false,
                        "message": "delivery failed"
                    }
                ]
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(ScheduledTask.serializer(), input)
        val encoded = json.encodeToJsonElement(ScheduledTask.serializer(), decoded).jsonObject

        assertEquals(
            setOf(
                "id",
                "description",
                "prompt",
                "scheduledAtEpochMs",
                "createdAtEpochMs",
                "cron",
                "trigger",
                "status",
                "lastResult",
                "consecutiveFailures",
                "recentExecutions",
            ),
            encoded.keys,
        )
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["description"], encoded["description"])
        assertEquals(input["prompt"], encoded["prompt"])
        assertEquals(input["scheduledAtEpochMs"], encoded["scheduledAtEpochMs"])
        assertEquals(input["createdAtEpochMs"], encoded["createdAtEpochMs"])
        assertEquals(input["cron"], encoded["cron"])
        assertEquals(input["trigger"], encoded["trigger"])
        assertEquals(input["status"], encoded["status"])
        assertEquals(input["lastResult"], encoded["lastResult"])
        assertEquals(input["consecutiveFailures"], encoded["consecutiveFailures"])
        assertEquals(input["recentExecutions"], encoded["recentExecutions"])
        assertEquals(decoded, json.decodeFromJsonElement(ScheduledTask.serializer(), encoded))
    }

    @Test
    fun scheduledTaskIgnoresUnknownFutureStorageFields() {
        val baseline = json.parseToJsonElement(
            """{
                "id": "id-한글-value",
                "description": "description-한글-value",
                "prompt": "prompt-한글-value",
                "scheduledAtEpochMs": 7000000000,
                "createdAtEpochMs": 7000000000,
                "cron": "cron-한글-value",
                "status": "COMPLETED",
                "lastResult": "lastResult-한글-value",
                "consecutiveFailures": 17
            }
            """.trimIndent(),
        ).jsonObject
        val future = JsonObject(
            baseline + (
                "futureStorage" to buildJsonObject {
                    put("version", 99)
                    put("source", "future-client")
                }
                ),
        )

        val expected = json.decodeFromJsonElement(ScheduledTask.serializer(), baseline)
        val actual = json.decodeFromJsonElement(ScheduledTask.serializer(), future)

        assertEquals(expected, actual)
    }

    @Test
    fun scheduledTaskRejectsMalformedIdWithoutPartialDecode() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                ScheduledTask.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun taskExecutionLogEntryPreservesCompleteNonDefaultSnapshot() {
        val input = json.parseToJsonElement(
            """{
                "timestampEpochMs": 7000000000,
                "success": true,
                "message": "message-한글-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(TaskExecutionLogEntry.serializer(), input)
        val encoded = json.encodeToJsonElement(TaskExecutionLogEntry.serializer(), decoded).jsonObject

        assertEquals(setOf("timestampEpochMs", "success", "message"), encoded.keys)
        assertEquals(input["timestampEpochMs"], encoded["timestampEpochMs"])
        assertEquals(input["success"], encoded["success"])
        assertEquals(input["message"], encoded["message"])
        assertEquals(decoded, json.decodeFromJsonElement(TaskExecutionLogEntry.serializer(), encoded))
    }

    @Test
    fun taskExecutionLogEntryIgnoresUnknownFutureStorageFields() {
        val baseline = json.parseToJsonElement(
            """{
                "timestampEpochMs": 7000000000,
                "success": true,
                "message": "message-한글-value"
            }
            """.trimIndent(),
        ).jsonObject
        val future = JsonObject(
            baseline + (
                "futureStorage" to buildJsonObject {
                    put("version", 99)
                    put("source", "future-client")
                }
                ),
        )

        val expected = json.decodeFromJsonElement(TaskExecutionLogEntry.serializer(), baseline)
        val actual = json.decodeFromJsonElement(TaskExecutionLogEntry.serializer(), future)

        assertEquals(expected, actual)
    }

    @Test
    fun taskExecutionLogEntryRejectsMalformedTimestampEpochMsWithoutPartialDecode() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                TaskExecutionLogEntry.serializer(),
                json.parseToJsonElement("""{"timestampEpochMs":{}}"""),
            )
        }
    }

    @Test
    fun smsMessagePreservesCompleteNonDefaultSnapshot() {
        val input = json.parseToJsonElement(
            """{
                "id": 7000000000,
                "address": "address-한글-value",
                "date": 7000000000,
                "preview": "preview-한글-value",
                "body": "body-한글-value",
                "read": true
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(SmsMessage.serializer(), input)
        val encoded = json.encodeToJsonElement(SmsMessage.serializer(), decoded).jsonObject

        assertEquals(setOf("id", "address", "date", "preview", "body", "read"), encoded.keys)
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["address"], encoded["address"])
        assertEquals(input["date"], encoded["date"])
        assertEquals(input["preview"], encoded["preview"])
        assertEquals(input["body"], encoded["body"])
        assertEquals(input["read"], encoded["read"])
        assertEquals(decoded, json.decodeFromJsonElement(SmsMessage.serializer(), encoded))
    }

    @Test
    fun smsMessageIgnoresUnknownFutureStorageFields() {
        val baseline = json.parseToJsonElement(
            """{
                "id": 7000000000,
                "address": "address-한글-value",
                "date": 7000000000,
                "preview": "preview-한글-value",
                "body": "body-한글-value",
                "read": true
            }
            """.trimIndent(),
        ).jsonObject
        val future = JsonObject(
            baseline + (
                "futureStorage" to buildJsonObject {
                    put("version", 99)
                    put("source", "future-client")
                }
                ),
        )

        val expected = json.decodeFromJsonElement(SmsMessage.serializer(), baseline)
        val actual = json.decodeFromJsonElement(SmsMessage.serializer(), future)

        assertEquals(expected, actual)
    }

    @Test
    fun smsMessageRejectsMalformedIdWithoutPartialDecode() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                SmsMessage.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun smsSyncStatePreservesCompleteNonDefaultSnapshot() {
        val input = json.parseToJsonElement(
            """{
                "lastSeenId": 7000000000,
                "lastSyncEpochMs": 7000000000,
                "lastAttemptEpochMs": 7000000000,
                "unreadCount": 17,
                "lastError": "lastError-한글-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(SmsSyncState.serializer(), input)
        val encoded = json.encodeToJsonElement(SmsSyncState.serializer(), decoded).jsonObject

        assertEquals(setOf("lastSeenId", "lastSyncEpochMs", "lastAttemptEpochMs", "unreadCount", "lastError"), encoded.keys)
        assertEquals(input["lastSeenId"], encoded["lastSeenId"])
        assertEquals(input["lastSyncEpochMs"], encoded["lastSyncEpochMs"])
        assertEquals(input["lastAttemptEpochMs"], encoded["lastAttemptEpochMs"])
        assertEquals(input["unreadCount"], encoded["unreadCount"])
        assertEquals(input["lastError"], encoded["lastError"])
        assertEquals(decoded, json.decodeFromJsonElement(SmsSyncState.serializer(), encoded))
    }

    @Test
    fun smsSyncStateIgnoresUnknownFutureStorageFields() {
        val baseline = json.parseToJsonElement(
            """{
                "lastSeenId": 7000000000,
                "lastSyncEpochMs": 7000000000,
                "lastAttemptEpochMs": 7000000000,
                "unreadCount": 17,
                "lastError": "lastError-한글-value"
            }
            """.trimIndent(),
        ).jsonObject
        val future = JsonObject(
            baseline + (
                "futureStorage" to buildJsonObject {
                    put("version", 99)
                    put("source", "future-client")
                }
                ),
        )

        val expected = json.decodeFromJsonElement(SmsSyncState.serializer(), baseline)
        val actual = json.decodeFromJsonElement(SmsSyncState.serializer(), future)

        assertEquals(expected, actual)
    }

    @Test
    fun smsSyncStateRejectsMalformedLastSeenIdWithoutPartialDecode() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                SmsSyncState.serializer(),
                json.parseToJsonElement("""{"lastSeenId":{}}"""),
            )
        }
    }

    @Test
    fun smsDraftPreservesCompleteNonDefaultSnapshot() {
        val input = json.parseToJsonElement(
            """{
                "id": "id-한글-value",
                "address": "address-한글-value",
                "body": "body-한글-value",
                "createdAtEpochMs": 7000000000,
                "inReplyToSmsId": 7000000000,
                "status": "FAILED",
                "lastError": "lastError-한글-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(SmsDraft.serializer(), input)
        val encoded = json.encodeToJsonElement(SmsDraft.serializer(), decoded).jsonObject

        assertEquals(setOf("id", "address", "body", "createdAtEpochMs", "inReplyToSmsId", "status", "lastError"), encoded.keys)
        assertEquals(input["id"], encoded["id"])
        assertEquals(input["address"], encoded["address"])
        assertEquals(input["body"], encoded["body"])
        assertEquals(input["createdAtEpochMs"], encoded["createdAtEpochMs"])
        assertEquals(input["inReplyToSmsId"], encoded["inReplyToSmsId"])
        assertEquals(input["status"], encoded["status"])
        assertEquals(input["lastError"], encoded["lastError"])
        assertEquals(decoded, json.decodeFromJsonElement(SmsDraft.serializer(), encoded))
    }

    @Test
    fun smsDraftIgnoresUnknownFutureStorageFields() {
        val baseline = json.parseToJsonElement(
            """{
                "id": "id-한글-value",
                "address": "address-한글-value",
                "body": "body-한글-value",
                "createdAtEpochMs": 7000000000,
                "inReplyToSmsId": 7000000000,
                "status": "FAILED",
                "lastError": "lastError-한글-value"
            }
            """.trimIndent(),
        ).jsonObject
        val future = JsonObject(
            baseline + (
                "futureStorage" to buildJsonObject {
                    put("version", 99)
                    put("source", "future-client")
                }
                ),
        )

        val expected = json.decodeFromJsonElement(SmsDraft.serializer(), baseline)
        val actual = json.decodeFromJsonElement(SmsDraft.serializer(), future)

        assertEquals(expected, actual)
    }

    @Test
    fun smsDraftRejectsMalformedIdWithoutPartialDecode() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                SmsDraft.serializer(),
                json.parseToJsonElement("""{"id":{}}"""),
            )
        }
    }

    @Test
    fun commandPreservesCompleteNonDefaultSnapshot() {
        val input = json.parseToJsonElement(
            """{
                "text": "text-한글-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(TerminalLine.Command.serializer(), input)
        val encoded = json.encodeToJsonElement(TerminalLine.Command.serializer(), decoded).jsonObject

        assertEquals(setOf("text"), encoded.keys)
        assertEquals(input["text"], encoded["text"])
        assertEquals(decoded, json.decodeFromJsonElement(TerminalLine.Command.serializer(), encoded))
    }

    @Test
    fun commandIgnoresUnknownFutureStorageFields() {
        val baseline = json.parseToJsonElement(
            """{
                "text": "text-한글-value"
            }
            """.trimIndent(),
        ).jsonObject
        val future = JsonObject(
            baseline + (
                "futureStorage" to buildJsonObject {
                    put("version", 99)
                    put("source", "future-client")
                }
                ),
        )

        val expected = json.decodeFromJsonElement(TerminalLine.Command.serializer(), baseline)
        val actual = json.decodeFromJsonElement(TerminalLine.Command.serializer(), future)

        assertEquals(expected, actual)
    }

    @Test
    fun commandRejectsMalformedTextWithoutPartialDecode() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                TerminalLine.Command.serializer(),
                json.parseToJsonElement("""{"text":{}}"""),
            )
        }
    }

    @Test
    fun outputPreservesCompleteNonDefaultSnapshot() {
        val input = json.parseToJsonElement(
            """{
                "text": "text-한글-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(TerminalLine.Output.serializer(), input)
        val encoded = json.encodeToJsonElement(TerminalLine.Output.serializer(), decoded).jsonObject

        assertEquals(setOf("text"), encoded.keys)
        assertEquals(input["text"], encoded["text"])
        assertEquals(decoded, json.decodeFromJsonElement(TerminalLine.Output.serializer(), encoded))
    }

    @Test
    fun outputIgnoresUnknownFutureStorageFields() {
        val baseline = json.parseToJsonElement(
            """{
                "text": "text-한글-value"
            }
            """.trimIndent(),
        ).jsonObject
        val future = JsonObject(
            baseline + (
                "futureStorage" to buildJsonObject {
                    put("version", 99)
                    put("source", "future-client")
                }
                ),
        )

        val expected = json.decodeFromJsonElement(TerminalLine.Output.serializer(), baseline)
        val actual = json.decodeFromJsonElement(TerminalLine.Output.serializer(), future)

        assertEquals(expected, actual)
    }

    @Test
    fun outputRejectsMalformedTextWithoutPartialDecode() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                TerminalLine.Output.serializer(),
                json.parseToJsonElement("""{"text":{}}"""),
            )
        }
    }

    @Test
    fun errorPreservesCompleteNonDefaultSnapshot() {
        val input = json.parseToJsonElement(
            """{
                "text": "text-한글-value"
            }
            """.trimIndent(),
        ).jsonObject

        val decoded = json.decodeFromJsonElement(TerminalLine.Error.serializer(), input)
        val encoded = json.encodeToJsonElement(TerminalLine.Error.serializer(), decoded).jsonObject

        assertEquals(setOf("text"), encoded.keys)
        assertEquals(input["text"], encoded["text"])
        assertEquals(decoded, json.decodeFromJsonElement(TerminalLine.Error.serializer(), encoded))
    }

    @Test
    fun errorIgnoresUnknownFutureStorageFields() {
        val baseline = json.parseToJsonElement(
            """{
                "text": "text-한글-value"
            }
            """.trimIndent(),
        ).jsonObject
        val future = JsonObject(
            baseline + (
                "futureStorage" to buildJsonObject {
                    put("version", 99)
                    put("source", "future-client")
                }
                ),
        )

        val expected = json.decodeFromJsonElement(TerminalLine.Error.serializer(), baseline)
        val actual = json.decodeFromJsonElement(TerminalLine.Error.serializer(), future)

        assertEquals(expected, actual)
    }

    @Test
    fun errorRejectsMalformedTextWithoutPartialDecode() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                TerminalLine.Error.serializer(),
                json.parseToJsonElement("""{"text":{}}"""),
            )
        }
    }
}
