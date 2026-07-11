package ai.deneb.deneb

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
 * Compatibility contracts for the hand-written mini-app envelopes.
 *
 * These envelopes sit between transport and domain mapping, so all fields must
 * remain optional while gateway versions roll independently from the native app.
 */
@OptIn(ExperimentalSerializationApi::class)
class DenebWireDescriptorContractTest {
    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
    }

    private fun SerialDescriptor.names(): List<String> = (0 until elementsCount).map(::getElementName)

    @Test
    fun recentPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = RecentPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("RecentPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(RecentPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun transcriptPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = TranscriptPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("TranscriptPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(TranscriptPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun workFeedPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = WorkFeedPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("WorkFeedPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(WorkFeedPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun workFeedActionRunPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = WorkFeedActionRunPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("WorkFeedActionRunPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(WorkFeedActionRunPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun workFeedFeedbackPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = WorkFeedFeedbackPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("WorkFeedFeedbackPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(WorkFeedFeedbackPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun nativeSyncPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = NativeSyncPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("NativeSyncPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(NativeSyncPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun nativeSyncEventKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = NativeSyncEvent.serializer()
        val descriptor = serializer.descriptor

        assertEquals("NativeSyncEvent", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(NativeSyncEvent(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun nativeSyncActionPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = NativeSyncActionPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("NativeSyncActionPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(NativeSyncActionPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun memoryListPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = MemoryListPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("MemoryListPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(MemoryListPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun diaryRecentPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = DiaryRecentPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("DiaryRecentPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(DiaryRecentPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun diaryRecentRowKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = DiaryRecentRow.serializer()
        val descriptor = serializer.descriptor

        assertEquals("DiaryRecentRow", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(DiaryRecentRow(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun deletePagesPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = DeletePagesPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("DeletePagesPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(DeletePagesPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun movePagePayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = MovePagePayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("MovePagePayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(MovePagePayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun categoriesPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = CategoriesPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("CategoriesPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(CategoriesPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun cronListPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = CronListPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("CronListPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(CronListPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun modelsPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = ModelsPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("ModelsPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(ModelsPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun clientHelloPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = ClientHelloPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("ClientHelloPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(ClientHelloPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun mailListPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = MailListPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("MailListPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(MailListPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun okPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = OkPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("OkPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(OkPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun askPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = AskPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("AskPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(AskPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun senderContextPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = SenderContextPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("SenderContextPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(SenderContextPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun calListPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = CalListPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("CalListPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(CalListPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun calProposalsPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = CalProposalsPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("CalProposalsPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(CalProposalsPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun todoListPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = TodoListPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("TodoListPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(TodoListPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun peopleListPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = PeopleListPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("PeopleListPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(PeopleListPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun contactsListPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = ContactsListPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("ContactsListPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(ContactsListPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun wikiPagePayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = WikiPagePayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("WikiPagePayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(WikiPagePayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun captureImagePayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = CaptureImagePayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("CaptureImagePayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(CaptureImagePayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun captureAudioPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = CaptureAudioPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("CaptureAudioPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(CaptureAudioPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun captureDocumentPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = CaptureDocumentPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("CaptureDocumentPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(CaptureDocumentPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun captureContactsPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = CaptureContactsPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("CaptureContactsPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(CaptureContactsPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun observeToolStatKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = ObserveToolStat.serializer()
        val descriptor = serializer.descriptor

        assertEquals("ObserveToolStat", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(ObserveToolStat(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun observeBehaviorKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = ObserveBehavior.serializer()
        val descriptor = serializer.descriptor

        assertEquals("ObserveBehavior", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(ObserveBehavior(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun observeLogLineKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = ObserveLogLine.serializer()
        val descriptor = serializer.descriptor

        assertEquals("ObserveLogLine", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(ObserveLogLine(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun observeLogsPayloadKeepsSafeDefaultsAndUnknownFieldTolerance() {
        val serializer = ObserveLogsPayload.serializer()
        val descriptor = serializer.descriptor

        assertEquals("ObserveLogsPayload", descriptor.serialName.substringAfterLast('.'))
        assertTrue(descriptor.elementsCount > 0)
        assertEquals(descriptor.elementsCount, descriptor.names().distinct().size)
        assertTrue((0 until descriptor.elementsCount).all(descriptor::isElementOptional))

        val empty = json.decodeFromString(serializer, "{}")
        assertEquals(ObserveLogsPayload(), empty)

        val future = json.decodeFromString(
            serializer,
            """{"futureEnvelopeVersion":99,"future":{"nested":[true,1,null]}}""",
        )
        assertEquals(empty, future)

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject)
        assertTrue(encoded.jsonObject.isEmpty())
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded))
    }
}
