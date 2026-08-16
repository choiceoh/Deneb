package ai.deneb.deneb

import ai.deneb.contacts.ContactData
import ai.deneb.ui.chat.History
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlin.io.encoding.Base64
import kotlin.io.encoding.ExperimentalEncodingApi
import kotlin.test.Test
import kotlin.test.assertContentEquals
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/** Share-sheet capture contracts, including cancellation cleanup of optimistic rows. */
@OptIn(ExperimentalEncodingApi::class)
class GatewayCaptureStateContractTest {
    private fun GatewayClientFixture.allowBackgroundSyncs() {
        transport.fallback = { gatewayRpcReply() }
    }

    @Test
    fun captureImageRejectsEmptyBytesWithoutHistoryOrNetwork() = runTest {
        val f = gatewayClientFixture()

        val accepted = f.client.captureImage(byteArrayOf(), "image/jpeg", "caption")

        assertFalse(accepted)
        assertTrue(f.client.chatHistory.value.isEmpty())
        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun captureImageRejectsMissingTokenWithoutPublishingOptimisticRow() = runTest {
        val f = gatewayClientFixture(token = "")

        val accepted = f.client.captureImage(byteArrayOf(1, 2, 3), "image/png")

        assertFalse(accepted)
        assertTrue(f.client.chatHistory.value.isEmpty())
        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun captureImagePostsBase64MimeSessionAndTrimmedCaption() = runTest {
        val f = gatewayClientFixture()
        f.allowBackgroundSyncs()
        f.transport.enqueueRpc("""{"text":"영수증 합계는 42,000원입니다."}""")
        val bytes = byteArrayOf(0, 1, 2, 0x7f, 0x80.toByte(), 0xff.toByte())

        val accepted = f.client.captureImage(
            bytes = bytes,
            mimeType = "image/heic",
            caption = "  카카오톡에서 공유  ",
        )

        assertTrue(accepted)
        val request = f.transport.requests.first()
        val params = request.requireRpc("miniapp.capture.image")
        assertContentEquals(bytes, Base64.decode(params["image"]?.jsonPrimitive?.content.orEmpty()))
        assertEquals("image/heic", params["mimeType"]?.jsonPrimitive?.content)
        assertEquals("client:main", params["sessionKey"]?.jsonPrimitive?.content)
        assertEquals("카카오톡에서 공유", params["caption"]?.jsonPrimitive?.content)
        assertEquals(4, params.size)
        val history = f.client.chatHistory.value.take(2)
        assertEquals(listOf(History.Role.USER, History.Role.ASSISTANT), history.map { it.role })
        assertEquals("카카오톡에서 공유\n📷 이미지 OCR 분석 중…", history[0].content)
        assertEquals("영수증 합계는 42,000원입니다.", history[1].content)
    }

    @Test
    fun captureImageOmitsCaptionWhenOnlyWhitespaceProvided() = runTest {
        val f = gatewayClientFixture()
        f.allowBackgroundSyncs()
        f.transport.enqueueRpc("""{"text":"OCR result"}""")

        val accepted = f.client.captureImage(
            bytes = byteArrayOf(9),
            mimeType = "image/png",
            caption = " \n\t ",
        )

        assertTrue(accepted)
        val params = f.transport.requests.first().requireRpc("miniapp.capture.image")
        assertFalse(params.containsKey("caption"))
        assertEquals("📷 이미지 공유됨 (OCR 분석 중…)", f.client.chatHistory.value[0].content)
    }

    @Test
    fun captureImageSurfacesDeterministicFallbackForBlankGatewayText() = runTest {
        val f = gatewayClientFixture()
        f.allowBackgroundSyncs()
        f.transport.enqueueRpc("""{"text":"   "}""")

        val accepted = f.client.captureImage(byteArrayOf(1), "image/png")

        assertTrue(accepted)
        assertEquals(2, f.client.chatHistory.value.size)
        assertEquals(
            "이미지를 저장했습니다. 분석합니다.",
            f.client.chatHistory.value.last().content,
        )
    }

    @Test
    fun captureImageSurfacesSameFallbackForRejectedEnvelope() = runTest {
        val f = gatewayClientFixture()
        f.allowBackgroundSyncs()
        f.transport.enqueueRpc(payload = "{}", ok = false)

        val accepted = f.client.captureImage(byteArrayOf(1), "image/png")

        assertTrue(accepted)
        assertEquals(
            "이미지를 저장했습니다. 분석합니다.",
            f.client.chatHistory.value.last().content,
        )
    }

    @Test
    fun captureImageCancellationRemovesOnlyItsPendingHistoryRow() = runTest {
        val f = gatewayClientFixture()
        f.client._chatHistory.value = listOf(
            History(id = "existing", role = History.Role.ASSISTANT, content = "기존 응답"),
        )
        f.transport.enqueueFailure(CancellationException("cancel image"))

        val failure = assertFailsWith<CancellationException> {
            f.client.captureImage(byteArrayOf(1), "image/png", "share")
        }

        assertEquals("cancel image", failure.message)
        assertEquals(listOf("existing"), f.client.chatHistory.value.map { it.id })
        assertEquals(listOf("기존 응답"), f.client.chatHistory.value.map { it.content })
    }

    @Test
    fun captureAudioRejectsEmptyBytesWithoutHistoryOrNetwork() = runTest {
        val f = gatewayClientFixture()

        f.client.captureAudio(byteArrayOf(), "audio/m4a")

        assertTrue(f.client.chatHistory.value.isEmpty())
        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun captureAudioPostsBinaryMimeAndCurrentSession() = runTest {
        val f = gatewayClientFixture()
        f.allowBackgroundSyncs()
        f.client.switchSession("client:main:meeting")
        f.transport.enqueueRpc("""{"text":"화자 1: 회의를 시작합니다."}""")
        val bytes = "audio-bytes".encodeToByteArray()

        f.client.captureAudio(bytes, "audio/mp4")

        val params = f.transport.requests.first().requireRpc("miniapp.capture.audio")
        assertContentEquals(bytes, Base64.decode(params["audio"]?.jsonPrimitive?.content.orEmpty()))
        assertEquals("audio/mp4", params["mimeType"]?.jsonPrimitive?.content)
        assertEquals("client:main:meeting", params["sessionKey"]?.jsonPrimitive?.content)
        assertEquals(3, params.size)
        assertEquals("🎙️ 녹음 공유됨 (전사·회의록 분석 중…)", f.client.chatHistory.value[0].content)
        assertEquals("화자 1: 회의를 시작합니다.", f.client.chatHistory.value[1].content)
    }

    @Test
    fun captureAudioUsesFailureMessageForMissingTranscript() = runTest {
        val f = gatewayClientFixture()
        f.allowBackgroundSyncs()
        f.transport.enqueueRpc("{}")

        f.client.captureAudio(byteArrayOf(1, 2), "audio/wav")

        assertEquals(
            "녹음을 저장했습니다. 분석합니다.",
            f.client.chatHistory.value.last().content,
        )
    }

    @Test
    fun captureAudioCancellationRemovesPendingRowAndPropagates() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel audio"))

        val failure = assertFailsWith<CancellationException> {
            f.client.captureAudio(byteArrayOf(1), "audio/wav")
        }

        assertEquals("cancel audio", failure.message)
        assertTrue(f.client.chatHistory.value.isEmpty())
    }

    @Test
    fun captureDocumentRejectsEmptyBytesBeforeNormalizingFilename() = runTest {
        val f = gatewayClientFixture()

        val accepted = f.client.captureDocument(
            bytes = byteArrayOf(),
            filename = " ",
            mimeType = "application/pdf",
            caption = "analyze",
        )

        assertFalse(accepted)
        assertTrue(f.client.chatHistory.value.isEmpty())
        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun captureDocumentPostsExactFilenameButUsesTrimmedNameInHistory() = runTest {
        val f = gatewayClientFixture()
        f.allowBackgroundSyncs()
        f.transport.enqueueRpc("""{"text":"계약서 핵심 조항"}""")
        val bytes = byteArrayOf(37, 80, 68, 70)

        val accepted = f.client.captureDocument(
            bytes = bytes,
            filename = "  계약서.PDF  ",
            mimeType = "application/pdf",
            caption = "  위약금 확인  ",
        )

        assertTrue(accepted)
        val params = f.transport.requests.first().requireRpc("miniapp.capture.document")
        assertContentEquals(bytes, Base64.decode(params["document"]?.jsonPrimitive?.content.orEmpty()))
        assertEquals("  계약서.PDF  ", params["filename"]?.jsonPrimitive?.content)
        assertEquals("application/pdf", params["mimeType"]?.jsonPrimitive?.content)
        assertEquals("client:main", params["sessionKey"]?.jsonPrimitive?.content)
        assertEquals("위약금 확인", params["caption"]?.jsonPrimitive?.content)
        assertEquals(5, params.size)
        assertEquals("위약금 확인\n📄 계약서.PDF 분석 중…", f.client.chatHistory.value[0].content)
        assertEquals("계약서 핵심 조항", f.client.chatHistory.value[1].content)
    }

    @Test
    fun captureDocumentUsesGenericNameForBlankFilenameAndOmitsBlankCaption() = runTest {
        val f = gatewayClientFixture()
        f.allowBackgroundSyncs()
        f.transport.enqueueRpc("""{"text":"text"}""")

        val accepted = f.client.captureDocument(
            bytes = byteArrayOf(1),
            filename = " \t ",
            mimeType = "application/octet-stream",
            caption = " \n ",
        )

        assertTrue(accepted)
        val params = f.transport.requests.first().requireRpc("miniapp.capture.document")
        assertEquals(" \t ", params["filename"]?.jsonPrimitive?.content)
        assertFalse(params.containsKey("caption"))
        assertEquals("📄 문서 공유됨: 문서 (분석 중…)", f.client.chatHistory.value[0].content)
    }

    @Test
    fun captureDocumentUsesFailureMessageForMalformedPayload() = runTest {
        val f = gatewayClientFixture()
        f.allowBackgroundSyncs()
        f.transport.enqueueRpc("""{"text":{"wrong":true}}""")

        val accepted = f.client.captureDocument(byteArrayOf(1), "file.pdf", "application/pdf")

        assertTrue(accepted)
        assertEquals(
            "문서를 저장했습니다. 분석합니다.",
            f.client.chatHistory.value.last().content,
        )
    }

    @Test
    fun captureDocumentCancellationRemovesPendingRowAndPropagates() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel document"))

        val failure = assertFailsWith<CancellationException> {
            f.client.captureDocument(byteArrayOf(1), "file.pdf", "application/pdf")
        }

        assertEquals("cancel document", failure.message)
        assertTrue(f.client.chatHistory.value.isEmpty())
    }

    @Test
    fun captureContactsRejectsEmptyListWithoutHistoryOrNetwork() = runTest {
        val f = gatewayClientFixture()

        f.client.captureContacts(emptyList())

        assertTrue(f.client.chatHistory.value.isEmpty())
        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun captureContactsRejectsMissingTokenWithoutSerializingPrivateData() = runTest {
        val f = gatewayClientFixture(token = "")

        f.client.captureContacts(
            listOf(ContactData(name = "Private", phones = listOf("010"))),
        )

        assertTrue(f.client.chatHistory.value.isEmpty())
        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun captureContactsSerializesEveryChannelAndPreservesOrder() = runTest {
        val f = gatewayClientFixture()
        f.allowBackgroundSyncs()
        f.client.switchSession("client:main:contacts")
        f.transport.enqueueRpc("""{"text":"주소록 2명 중 1명을 갱신했습니다."}""")
        val contacts = listOf(
            ContactData(
                name = "홍길동",
                phones = listOf("010-1111-2222", "062-123-4567"),
                emails = listOf("hong@example.com"),
                org = "Deneb",
            ),
            ContactData(
                name = "Alice",
                phones = emptyList(),
                emails = listOf("alice@example.com", "a@work.example"),
                org = "A&B",
            ),
        )

        f.client.captureContacts(contacts)

        val params = f.transport.requests.first().requireRpc("miniapp.capture.contacts")
        assertEquals("client:main:contacts", params["sessionKey"]?.jsonPrimitive?.content)
        val encoded = params["contacts"]?.jsonArray.orEmpty()
        assertEquals(2, encoded.size)
        val first = encoded[0].jsonObject
        assertEquals("홍길동", first["name"]?.jsonPrimitive?.content)
        assertEquals(
            listOf("010-1111-2222", "062-123-4567"),
            first["phones"]?.jsonArray?.map { it.jsonPrimitive.content },
        )
        assertEquals(listOf("hong@example.com"), first["emails"]?.jsonArray?.map { it.jsonPrimitive.content })
        assertEquals("Deneb", first["org"]?.jsonPrimitive?.content)
        val second = encoded[1].jsonObject
        assertEquals("Alice", second["name"]?.jsonPrimitive?.content)
        assertTrue(second["phones"]?.jsonArray?.isEmpty() == true)
        assertEquals(
            listOf("alice@example.com", "a@work.example"),
            second["emails"]?.jsonArray?.map { it.jsonPrimitive.content },
        )
        assertEquals("A&B", second["org"]?.jsonPrimitive?.content)
        assertEquals("📇 주소록 2개 동기화 중…", f.client.chatHistory.value[0].content)
        assertEquals("주소록 2명 중 1명을 갱신했습니다.", f.client.chatHistory.value[1].content)
    }

    @Test
    fun captureContactsUsesFailureMessageForBlankGatewayText() = runTest {
        val f = gatewayClientFixture()
        f.allowBackgroundSyncs()
        f.transport.enqueueRpc("""{"text":""}""")

        f.client.captureContacts(listOf(ContactData(name = "One")))

        assertEquals("주소록 동기화에 실패했습니다.", f.client.chatHistory.value.last().content)
    }

    @Test
    fun captureContactsCancellationRemovesPendingRowAndPropagates() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel contacts"))

        val failure = assertFailsWith<CancellationException> {
            f.client.captureContacts(listOf(ContactData(name = "One")))
        }

        assertEquals("cancel contacts", failure.message)
        assertTrue(f.client.chatHistory.value.isEmpty())
    }
}
