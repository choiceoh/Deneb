package ai.deneb.deneb.generated

import kotlinx.serialization.ExperimentalSerializationApi
import kotlinx.serialization.descriptors.SerialDescriptor
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.jsonObject
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * Exhaustive compatibility guards for generated Kotlin wire models.
 *
 * The generator makes every gateway field optional so an older client can read a
 * newer payload and a newer client can read an older payload. These tests pin that
 * contract for every generated type: descriptor names/order, optionality, safe
 * empty defaults, unknown-field tolerance, and lossless default round trips.
 */
@OptIn(ExperimentalSerializationApi::class)
class MiniappWireDescriptorContractTest {
    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
    }

    private fun SerialDescriptor.elementNames(): List<String> = (0 until elementsCount).map(::getElementName)

    @Test
    fun calendarAttendeeOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = CalendarAttendeeOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("CalendarAttendeeOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("email", "displayName", "responseStatus", "self", "organizer"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(CalendarAttendeeOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun calendarConferenceOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = CalendarConferenceOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("CalendarConferenceOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("solution", "uri"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(CalendarConferenceOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun calendarEventOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = CalendarEventOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("CalendarEventOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("id", "summary", "description", "location", "start", "end", "allDay", "status", "htmlLink", "local", "category", "organizer", "attendees", "conference", "hasMeet"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(CalendarEventOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun calendarProposalOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = CalendarProposalOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("CalendarProposalOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("id", "title", "start", "allDay", "kind", "sourceSubject", "sourceFrom"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(CalendarProposalOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun contactRowDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = ContactRow.serializer()
        val descriptor = serializer.descriptor

        assertEquals("ContactRow", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("name", "phones", "emails", "org"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(ContactRow(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun dashboardItemDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = DashboardItem.serializer()
        val descriptor = serializer.descriptor

        assertEquals("DashboardItem", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("title", "subtitle", "source", "refType", "refId", "whenMs"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(DashboardItem(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun dashboardOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = DashboardOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("DashboardOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("lanes"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(DashboardOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun filesEntryOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = FilesEntryOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("FilesEntryOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("tag", "name", "pathDisplay", "pathLower", "id", "size", "serverModified"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(FilesEntryOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun filesListOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = FilesListOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("FilesListOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("entries", "path"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(FilesListOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun filesShareOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = FilesShareOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("FilesShareOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("url"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(FilesShareOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun filesUploadOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = FilesUploadOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("FilesUploadOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("entry"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(FilesUploadOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun laneOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = LaneOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("LaneOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("key", "name", "items"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(LaneOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun mailAnalysisOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = MailAnalysisOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("MailAnalysisOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("id", "subject", "from", "date", "analysis", "relatedProjects", "durationMs", "cached", "createdAt", "analysisStatus", "analysisQuality", "feedStatus", "calendarProposalCount", "todoCount", "workStateHint"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(MailAnalysisOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun mailAttachmentOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = MailAttachmentOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("MailAttachmentOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("id", "filename", "mimeType", "size", "truncated"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(MailAttachmentOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun mailMessageOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = MailMessageOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("MailMessageOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("id", "threadId", "from", "to", "cc", "subject", "date", "isUnread", "body", "bodyTotal", "rawBody", "rawBodyTotal", "bodyCleaned", "bodyHiddenBlockCount", "bodyHiddenLineCount", "labels", "attachments", "analysisStatus", "analysisQuality", "feedStatus", "calendarProposalCount", "todoCount", "workStateHint", "relatedProjects"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(MailMessageOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun mailNativeMailboxOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = MailNativeMailboxOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("MailNativeMailboxOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("name", "total", "unread", "locallyRead", "locallyArchived", "locallyTrashed", "latestUid", "attachmentCapable"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(MailNativeMailboxOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun mailNativeOverlayOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = MailNativeOverlayOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("MailNativeOverlayOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("messages", "read", "archived", "trashed"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(MailNativeOverlayOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun mailNativePipelineOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = MailNativePipelineOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("MailNativePipelineOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("messages", "analyzed", "analyzing", "failed", "feedCreated", "feedMissing", "calendarCandidates", "todoCandidates", "updatedAt", "error"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(MailNativePipelineOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun mailNativeStatusOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = MailNativeStatusOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("MailNativeStatusOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("source", "available", "offlineCapable", "mailboxes", "overlay", "pipeline", "generatedAt", "error"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(MailNativeStatusOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun mailRowOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = MailRowOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("MailRowOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("id", "threadId", "from", "subject", "snippet", "date", "isUnread", "labels", "mailbox", "hasAttachment", "attachmentCount", "priority", "priorityHint", "analysisStatus", "analysisQuality", "feedStatus", "calendarProposalCount", "todoCount", "workStateHint", "relatedProjects"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(MailRowOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun marketQuoteDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = MarketQuote.serializer()
        val descriptor = serializer.descriptor

        assertEquals("MarketQuote", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("symbol", "label", "price", "prevClose", "changePct", "currency"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(MarketQuote(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun marketSummaryDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = MarketSummary.serializer()
        val descriptor = serializer.descriptor

        assertEquals("MarketSummary", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("quotes", "asOf", "stale"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(MarketSummary(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun memberOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = MemberOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("MemberOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("name", "rank", "position", "phones", "emails", "personPath"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(MemberOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun memoryCategoryRowDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = MemoryCategoryRow.serializer()
        val descriptor = serializer.descriptor

        assertEquals("MemoryCategoryRow", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("name", "pageCount"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(MemoryCategoryRow(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun memoryPageRowDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = MemoryPageRow.serializer()
        val descriptor = serializer.descriptor

        assertEquals("MemoryPageRow", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("path", "title", "summary", "updated"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(MemoryPageRow(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun miniappCronDetailDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = MiniappCronDetail.serializer()
        val descriptor = serializer.descriptor

        assertEquals("MiniappCronDetail", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("id", "name", "enabled", "agentId", "sessionTarget", "schedule", "scheduleSpec", "scheduleKind", "timezone", "cronExpr", "staggerMs", "payloadKind", "prompt", "model", "thinking", "timeoutSeconds", "lightContext", "retryCount", "deliveryChannel", "deliveryTo", "deliveryThreadId", "failureAlertAfter", "nextRunAtMs", "lastSessionKey", "lastDeliveryStatus", "lastError", "consecutiveErrors", "autoDisabledAtMs", "createdAtMs", "updatedAtMs"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(MiniappCronDetail(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun miniappCronRowDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = MiniappCronRow.serializer()
        val descriptor = serializer.descriptor

        assertEquals("MiniappCronRow", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("id", "name", "enabled", "schedule", "payloadKind", "payloadPreview", "nextRunAtMs", "consecutiveErrors", "autoDisabledAtMs", "lastError"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(MiniappCronRow(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun modelAddResultDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = ModelAddResult.serializer()
        val descriptor = serializer.descriptor

        assertEquals("ModelAddResult", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("ok", "id", "provider", "endpoint", "model", "added"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(ModelAddResult(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun modelDeleteResultDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = ModelDeleteResult.serializer()
        val descriptor = serializer.descriptor

        assertEquals("ModelDeleteResult", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("ok", "id", "removed", "clearedRoles", "current"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(ModelDeleteResult(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun modelOptionDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = ModelOption.serializer()
        val descriptor = serializer.descriptor

        assertEquals("ModelOption", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("id", "label", "provider", "display", "health", "current", "custom", "deletable", "unhealthy", "note"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(ModelOption(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun modelSectionDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = ModelSection.serializer()
        val descriptor = serializer.descriptor

        assertEquals("ModelSection", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("title", "models"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(ModelSection(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun modelsListResultDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = ModelsListResult.serializer()
        val descriptor = serializer.descriptor

        assertEquals("ModelsListResult", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("current", "roles", "sections", "advisories", "mainHasVision"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(ModelsListResult(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun notebookListOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = NotebookListOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("NotebookListOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("notebooks"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(NotebookListOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun notebookOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = NotebookOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("NotebookOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("id", "name", "description", "dealRef", "mode", "sources", "updated"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(NotebookOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun notebookSourceOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = NotebookSourceOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("NotebookSourceOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("cite", "kind", "ref", "title", "text"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(NotebookSourceOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun notebookSummaryOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = NotebookSummaryOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("NotebookSummaryOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("id", "name", "description", "dealRef", "projectRefs", "sourceCount", "updated"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(NotebookSummaryOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun orgNodeOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = OrgNodeOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("OrgNodeOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("id", "name", "type", "parentId", "lane", "members", "keywords", "companies"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(OrgNodeOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun orgSaveOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = OrgSaveOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("OrgSaveOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("saved", "nodeCount", "hasLanes"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(OrgSaveOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun orgTreeOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = OrgTreeOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("OrgTreeOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("nodes"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(OrgTreeOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun personRowDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = PersonRow.serializer()
        val descriptor = serializer.descriptor

        assertEquals("PersonRow", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("email", "name", "messageCount", "lastSeen", "lastSubject", "wikiPath", "wikiSummary"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(PersonRow(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun projectDigestRowDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = ProjectDigestRow.serializer()
        val descriptor = serializer.descriptor

        assertEquals("ProjectDigestRow", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("project", "headline", "bullets", "due", "updatedAtMs", "path", "code", "client", "refs"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(ProjectDigestRow(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun projectDigestsOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = ProjectDigestsOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("ProjectDigestsOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("digests"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(ProjectDigestsOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun projectLinkedOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = ProjectLinkedOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("ProjectLinkedOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("mail", "calendar", "todo", "workfeed", "notebook"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(ProjectLinkedOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun projectRefDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = ProjectRef.serializer()
        val descriptor = serializer.descriptor

        assertEquals("ProjectRef", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("path", "title", "summary"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(ProjectRef(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun promptDetailOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = PromptDetailOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("PromptDetailOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("id", "title", "description", "category", "text", "defaultText", "editable", "overridden", "updatedAtMs"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(PromptDetailOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun promptListResponseDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = PromptListResponse.serializer()
        val descriptor = serializer.descriptor

        assertEquals("PromptListResponse", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("prompts", "count"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(PromptListResponse(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun promptRowDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = PromptRow.serializer()
        val descriptor = serializer.descriptor

        assertEquals("PromptRow", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("id", "title", "description", "category", "editable", "overridden", "updatedAtMs"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(PromptRow(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun promptTunerReportDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = PromptTunerReport.serializer()
        val descriptor = serializer.descriptor

        assertEquals("PromptTunerReport", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("ran", "changed", "reason", "error", "leafSummaries", "minSummaries", "proposed", "added", "beforeCount", "afterCount"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(PromptTunerReport(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun promptTunerRunResponseDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = PromptTunerRunResponse.serializer()
        val descriptor = serializer.descriptor

        assertEquals("PromptTunerRunResponse", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("target", "report"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(PromptTunerRunResponse(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun propusLifecycleSummaryDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = PropusLifecycleSummary.serializer()
        val descriptor = serializer.descriptor

        assertEquals("PropusLifecycleSummary", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("system", "state", "total", "genesis", "evolved", "review", "rejected", "rolledBack", "attention", "latestAt", "latestType", "latestSkill", "doctrineVersion", "doctrine", "sourcePapers", "filteredSources", "principles", "qualityGates", "nextActions", "coverageState", "coverageGaps", "nextCue", "qualityGate", "attentionCue"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(PropusLifecycleSummary(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun qATurnDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = QATurn.serializer()
        val descriptor = serializer.descriptor

        assertEquals("QATurn", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("q", "a"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(QATurn(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun roleModelDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = RoleModel.serializer()
        val descriptor = serializer.descriptor

        assertEquals("RoleModel", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("role", "model"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(RoleModel(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun searchAllResultDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = SearchAllResult.serializer()
        val descriptor = serializer.descriptor

        assertEquals("SearchAllResult", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("wiki", "diary", "people"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(SearchAllResult(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun searchDiaryHitDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = SearchDiaryHit.serializer()
        val descriptor = serializer.descriptor

        assertEquals("SearchDiaryHit", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("file", "header", "content", "at", "score"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(SearchDiaryHit(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun searchWikiHitDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = SearchWikiHit.serializer()
        val descriptor = serializer.descriptor

        assertEquals("SearchWikiHit", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("path", "title", "summary", "category", "snippet", "score"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(SearchWikiHit(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun selfCorrectionCandidateDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = SelfCorrectionCandidate.serializer()
        val descriptor = serializer.descriptor

        assertEquals("SelfCorrectionCandidate", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("id", "status", "scope", "skillName", "sessionKey", "title", "candidate", "evidence", "reason", "targetFiles", "proposedChange", "risk", "source", "reviewer", "reviewNote", "evidenceKinds", "reviewActions", "createdAt", "updatedAt"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(SelfCorrectionCandidate(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun selfImprovementCodingFunnelDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = SelfImprovementCodingFunnel.serializer()
        val descriptor = serializer.descriptor

        assertEquals("SelfImprovementCodingFunnel", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("lastCaptureAt", "lastReviewAt", "rejections7d", "promotableRejections7d", "lastRejectionAt", "lastNudgeAt"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(SelfImprovementCodingFunnel(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun selfImprovementCodingListResponseDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = SelfImprovementCodingListResponse.serializer()
        val descriptor = serializer.descriptor

        assertEquals("SelfImprovementCodingListResponse", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("candidates", "count", "statusCounts", "funnel"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(SelfImprovementCodingListResponse(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun selfImprovementCodingStatusCountDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = SelfImprovementCodingStatusCount.serializer()
        val descriptor = serializer.descriptor

        assertEquals("SelfImprovementCodingStatusCount", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("status", "count"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(SelfImprovementCodingStatusCount(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun senderRecentOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = SenderRecentOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("SenderRecentOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("count", "lastReceivedAt", "windowDays", "truncated"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(SenderRecentOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun senderWikiHitOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = SenderWikiHitOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("SenderWikiHitOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("path", "title", "summary", "category"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(SenderWikiHitOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun sessionRowOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = SessionRowOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("SessionRowOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("key", "kind", "status", "channel", "model", "label", "updatedAtMs", "startedAtMs", "runtimeMs", "totalTokens"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(SessionRowOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun skillDetailResponseDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = SkillDetailResponse.serializer()
        val descriptor = serializer.descriptor

        assertEquals("SkillDetailResponse", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("skill", "body", "bodyTruncated", "path"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(SkillDetailResponse(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun skillLifecycleEventDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = SkillLifecycleEvent.serializer()
        val descriptor = serializer.descriptor

        assertEquals("SkillLifecycleEvent", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("type", "skillName", "at", "version", "detail", "route", "evidence", "targetSignature", "editedSurface", "expectedBehaviorChange", "regressionRisk"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(SkillLifecycleEvent(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun skillRowDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = SkillRow.serializer()
        val descriptor = serializer.descriptor

        assertEquals("SkillRow", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("name", "description", "category", "homepage", "tags", "relatedSkills", "source", "version", "origin", "createdAt", "evolveCount", "lastEvolvedAt", "totalUses", "lastUsedAt", "curatorState", "editable", "deletable", "dependencySummary", "installSummary"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(SkillRow(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun skillsLifecycleResponseDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = SkillsLifecycleResponse.serializer()
        val descriptor = serializer.descriptor

        assertEquals("SkillsLifecycleResponse", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("events", "count", "summary"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(SkillsLifecycleResponse(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun skillsListResponseDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = SkillsListResponse.serializer()
        val descriptor = serializer.descriptor

        assertEquals("SkillsListResponse", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("skills", "count"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(SkillsListResponse(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun todoOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = TodoOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("TodoOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("id", "title", "note", "due", "dueAllDay", "done", "doneAt"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(TodoOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun topicDocOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = TopicDocOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("TopicDocOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("key", "name", "content", "size", "modified"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(TopicDocOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun topicDocWriteOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = TopicDocWriteOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("TopicDocWriteOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("key", "name", "size", "modified", "applied"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(TopicDocWriteOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun transcriptAttachmentOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = TranscriptAttachmentOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("TranscriptAttachmentOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("type", "mimeType", "url", "data", "name", "size"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(TranscriptAttachmentOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun transcriptMsgOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = TranscriptMsgOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("TranscriptMsgOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("id", "role", "content", "attachments", "timestampMs"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(TranscriptMsgOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun wormholeModelOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = WormholeModelOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("WormholeModelOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("name", "protocol", "local", "thinking", "source", "keyHealth"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(WormholeModelOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun wormholeStatusOutDefaultAndDescriptorRemainBackwardCompatible() {
        val serializer = WormholeStatusOut.serializer()
        val descriptor = serializer.descriptor

        assertEquals("WormholeStatusOut", descriptor.serialName.substringAfterLast('.'))
        assertEquals(listOf("reachable", "listen", "localOnly", "effortRouting", "auto", "models"), descriptor.elementNames())
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(WormholeStatusOut(), empty)

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }
}
