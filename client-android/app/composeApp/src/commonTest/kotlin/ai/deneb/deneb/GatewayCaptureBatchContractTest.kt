package ai.deneb.deneb

import ai.deneb.ui.chat.History
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlin.io.encoding.Base64
import kotlin.io.encoding.ExperimentalEncodingApi
import kotlin.test.Test
import kotlin.test.assertContentEquals
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/** Batch capture contract: N files → ONE miniapp.capture.batch turn, not N turns. */
@OptIn(ExperimentalEncodingApi::class)
class GatewayCaptureBatchContractTest {
    private fun GatewayClientFixture.allowBackgroundSyncs() {
        transport.fallback = { gatewayRpcReply() }
    }

    @Test
    fun captureBatchRejectsEmptyListWithoutHistoryOrNetwork() = runTest {
        val f = gatewayClientFixture()

        val accepted = f.client.captureBatch(emptyList())

        assertFalse(accepted)
        assertTrue(f.client.chatHistory.value.isEmpty())
        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun captureBatchRejectsMissingTokenWithoutPublishingOptimisticRow() = runTest {
        val f = gatewayClientFixture(token = "")

        val accepted = f.client.captureBatch(listOf(DenebAttachment(byteArrayOf(1), "a.pdf", "application/pdf")))

        assertFalse(accepted)
        assertTrue(f.client.chatHistory.value.isEmpty())
        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun captureBatchPostsEveryFileInOneTurnWithCaption() = runTest {
        val f = gatewayClientFixture()
        f.allowBackgroundSyncs()
        f.transport.enqueueRpc("""{"text":"세 파일 교차 검토 완료"}""")
        val a = byteArrayOf(0, 1, 2)
        val b = byteArrayOf(0x7f, 0x80.toByte(), 0xff.toByte())
        val c = byteArrayOf(9)

        val accepted = f.client.captureBatch(
            files = listOf(
                DenebAttachment(a, "contract.pdf", "application/pdf"),
                DenebAttachment(b, "quote.png", "image/png"),
                DenebAttachment(c, "memo.mp3", "audio/mpeg"),
            ),
            caption = "  세 파일 비교  ",
        )

        assertTrue(accepted)
        // Three files, ONE capture turn carrying all of them — a per-file regression
        // would post three single-file requests instead of one three-file batch.
        val params = f.transport.requests.first().requireRpc("miniapp.capture.batch")
        val files = params["files"]?.jsonArray
        assertEquals(3, files?.size)
        val first = files!![0].jsonObject
        assertContentEquals(a, Base64.decode(first["data"]?.jsonPrimitive?.content.orEmpty()))
        assertEquals("application/pdf", first["mimeType"]?.jsonPrimitive?.content)
        assertEquals("contract.pdf", first["filename"]?.jsonPrimitive?.content)
        assertEquals("client:main", params["sessionKey"]?.jsonPrimitive?.content)
        assertEquals("세 파일 비교", params["caption"]?.jsonPrimitive?.content)

        val history = f.client.chatHistory.value.take(2)
        assertEquals(listOf(History.Role.USER, History.Role.ASSISTANT), history.map { it.role })
        assertEquals("📎 첨부 3개\n• contract.pdf\n• quote.png\n• memo.mp3\n\n세 파일 비교", history[0].content)
        assertEquals("세 파일 교차 검토 완료", history[1].content)
    }

    @Test
    fun captureBatchDropsEmptyByteFilesFromTheBatch() = runTest {
        val f = gatewayClientFixture()
        f.allowBackgroundSyncs()
        f.transport.enqueueRpc("""{"text":"ok"}""")

        val accepted = f.client.captureBatch(
            files = listOf(
                DenebAttachment(byteArrayOf(1), "good.pdf", "application/pdf"),
                DenebAttachment(byteArrayOf(), "empty.pdf", "application/pdf"),
            ),
        )

        assertTrue(accepted)
        val params = f.transport.requests.first().requireRpc("miniapp.capture.batch")
        assertEquals(1, params["files"]?.jsonArray?.size)
        assertFalse(params.containsKey("caption"))
        assertEquals("📎 첨부 1개\n• good.pdf", f.client.chatHistory.value[0].content)
    }

    @Test
    fun captureBatchRejectsWhenEveryFileIsEmpty() = runTest {
        val f = gatewayClientFixture()

        val accepted = f.client.captureBatch(listOf(DenebAttachment(byteArrayOf(), "empty.pdf", "application/pdf")))

        assertFalse(accepted)
        assertTrue(f.client.chatHistory.value.isEmpty())
        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun captureBatchSurfacesDeterministicFallbackForBlankGatewayText() = runTest {
        val f = gatewayClientFixture()
        f.allowBackgroundSyncs()
        f.transport.enqueueRpc("""{"text":"   "}""")

        val accepted = f.client.captureBatch(listOf(DenebAttachment(byteArrayOf(1), "a.pdf", "application/pdf")))

        assertTrue(accepted)
        assertEquals(
            "첨부에서 내용을 추출하지 못했거나 분석에 실패했습니다.",
            f.client.chatHistory.value[1].content,
        )
    }
}
