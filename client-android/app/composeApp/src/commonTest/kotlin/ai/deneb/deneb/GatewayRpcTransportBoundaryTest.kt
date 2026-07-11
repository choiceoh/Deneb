package ai.deneb.deneb

import io.ktor.http.ContentType
import io.ktor.http.HttpStatusCode
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

@OptIn(ExperimentalCoroutinesApi::class)
class GatewayRpcTransportBoundaryTest {

    @Test
    fun callRpcDoesNotSendWithoutClientToken() = runTest {
        val f = gatewayClientFixture(token = "")

        val result = f.client.callRpc<JsonObject>("miniapp.test", buildJsonObject {})

        assertNull(result)
        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun callRpcPostsToTrimmedGatewayRpcEndpoint() = runTest {
        val f = gatewayClientFixture(url = "https://gateway.example///")
        f.transport.enqueueRpc("""{"value":"ok"}""")

        f.client.callRpc<JsonObject>("miniapp.test", buildJsonObject {})

        assertEquals("https://gateway.example/api/v1/miniapp/rpc", f.transport.singleRequest().url)
    }

    @Test
    fun callRpcUsesPostAndJsonContentType() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc()

        f.client.callRpc<JsonObject>("miniapp.test", buildJsonObject {})

        val request = f.transport.singleRequest()
        assertEquals("POST", request.method.value)
        assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
    }

    @Test
    fun callRpcSendsClientTokenHeaderExactlyOnce() = runTest {
        val f = gatewayClientFixture(token = "secret-token")
        f.transport.enqueueRpc()

        f.client.callRpc<JsonObject>("miniapp.test", buildJsonObject {})

        val request = f.transport.singleRequest()
        assertEquals("secret-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
        assertEquals(1, request.headers.getAll(DenebGatewayClient.CLIENT_TOKEN_HEADER)?.size)
    }

    @Test
    fun callRpcSerializesMethodAndParametersWithoutLoss() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc()
        val params = buildJsonObject {
            put("cursor", Long.MAX_VALUE)
            put("query", "한글 search")
            put("enabled", true)
        }

        f.client.callRpc<JsonObject>("miniapp.search.all", params)

        val request = f.transport.singleRequest()
        assertEquals("miniapp.search.all", request.rpcMethod)
        assertEquals(Long.MAX_VALUE, request.rpcParams?.get("cursor")?.jsonPrimitive?.content?.toLong())
        assertEquals("한글 search", request.rpcParams?.get("query")?.jsonPrimitive?.content)
        assertEquals(true, request.rpcParams?.get("enabled")?.jsonPrimitive?.content?.toBoolean())
    }

    @Test
    fun callRpcGeneratesNonBlankRequestId() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc()

        f.client.callRpc<JsonObject>("miniapp.test", buildJsonObject {})

        val id = f.transport.singleRequest().jsonBody?.get("id")?.jsonPrimitive?.content.orEmpty()
        assertTrue(id.isNotBlank())
        assertTrue('-' in id)
    }

    @Test
    fun sequentialRpcCallsUseDifferentRequestIds() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc()
        f.transport.enqueueRpc()

        f.client.callRpc<JsonObject>("miniapp.one", buildJsonObject {})
        f.client.callRpc<JsonObject>("miniapp.two", buildJsonObject {})

        val ids = f.transport.requests.map { it.jsonBody?.get("id")?.jsonPrimitive?.content }
        assertEquals(2, ids.distinct().size)
    }

    @Test
    fun callRpcReturnsTypedPayloadOnSuccessfulEnvelope() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"name":"Deneb","count":7}""")

        val payload = f.client.callRpc<JsonObject>("miniapp.test", buildJsonObject {})

        assertEquals("Deneb", payload?.get("name")?.jsonPrimitive?.content)
        assertEquals(7, payload?.get("count")?.jsonPrimitive?.content?.toInt())
    }

    @Test
    fun callRpcIgnoresUnknownEnvelopeAndPayloadFields() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson(
            """{"ok":true,"trace":"ignored","payload":{"known":"yes","future":{"x":1}}}""",
        )

        val payload = f.client.callRpc<JsonObject>("miniapp.test", buildJsonObject {})

        assertEquals("yes", payload?.get("known")?.jsonPrimitive?.content)
        assertTrue(payload?.containsKey("future") == true)
    }

    @Test
    fun callRpcReturnsNullForExplicitNullPayload() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("""{"ok":true,"payload":null}""")

        val payload = f.client.callRpc<JsonObject>("miniapp.test", buildJsonObject {})

        assertNull(payload)
    }

    @Test
    fun callRpcReturnsNullForMissingPayload() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("""{"ok":true}""")

        val payload = f.client.callRpc<JsonObject>("miniapp.test", buildJsonObject {})

        assertNull(payload)
    }

    @Test
    fun callRpcRejectsPayloadWhenEnvelopeSaysNotOk() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(payload = """{"stale":"must-not-use"}""", ok = false)

        val payload = f.client.callRpc<JsonObject>("miniapp.test", buildJsonObject {})

        assertNull(payload)
    }

    @Test
    fun callRpcRejectsSuccessfulLookingPayloadOnHttpError() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            payload = """{"stale":"must-not-use"}""",
            status = HttpStatusCode.InternalServerError,
        )

        val payload = f.client.callRpc<JsonObject>("miniapp.test", buildJsonObject {})

        assertNull(payload)
    }

    @Test
    fun callRpcReturnsNullForMalformedJson() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("not-json")

        val payload = f.client.callRpc<JsonObject>("miniapp.test", buildJsonObject {})

        assertNull(payload)
    }

    @Test
    fun callRpcReturnsNullWhenPayloadHasWrongShape() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("""{"ok":true,"payload":[1,2,3]}""")

        val payload = f.client.callRpc<JsonObject>("miniapp.test", buildJsonObject {})

        assertNull(payload)
    }

    @Test
    fun callRpcReturnsNullOnTransportFailure() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(IllegalStateException("connection reset"))

        val payload = f.client.callRpc<JsonObject>("miniapp.test", buildJsonObject {})

        assertNull(payload)
        assertEquals(1, f.transport.requests.size)
    }

    @Test
    fun callRpcPropagatesCancellationInsteadOfConvertingItToNull() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel rpc"))

        val failure = assertFailsWith<CancellationException> {
            f.client.callRpc<JsonObject>("miniapp.test", buildJsonObject {})
        }

        assertEquals("cancel rpc", failure.message)
    }

    @Test
    fun credentialEpochFencesResponseThatStartedBeforeGatewaySwitch() = runTest {
        val f = gatewayClientFixture(token = "old-token", url = "https://old.example")
        val gate = CompletableDeferred<Unit>()
        f.transport.enqueueRpc(payload = """{"private":"old"}""", gate = gate)
        val pending = async {
            f.client.callRpc<JsonObject>("miniapp.test", buildJsonObject {})
        }
        runCurrent()

        f.client.onCredentialsChanged("https://new.example", "new-token")
        gate.complete(Unit)

        assertNull(pending.await())
        assertEquals("https://old.example/api/v1/miniapp/rpc", f.transport.singleRequest().url)
        assertEquals("old-token", f.transport.singleRequest().header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
    }

    @Test
    fun credentialEpochFencesEvenWhenSameUrlAndTokenAreWrittenAgain() = runTest {
        val f = gatewayClientFixture(token = "same-token", url = "https://same.example")
        val gate = CompletableDeferred<Unit>()
        f.transport.enqueueRpc(payload = """{"private":"old epoch"}""", gate = gate)
        val pending = async {
            f.client.callRpc<JsonObject>("miniapp.test", buildJsonObject {})
        }
        runCurrent()

        f.client.onCredentialsChanged("https://same.example", "same-token")
        gate.complete(Unit)

        assertNull(pending.await())
        assertEquals(1, f.client.credEpoch)
    }

    @Test
    fun freshRpcAfterCredentialSwitchUsesNewEndpointAndToken() = runTest {
        val f = gatewayClientFixture(token = "old", url = "https://old.example")
        f.client.onCredentialsChanged("https://new.example/", "new")
        f.transport.enqueueRpc("""{"fresh":true}""")

        val payload = f.client.callRpc<JsonObject>("miniapp.test", buildJsonObject {})

        assertEquals(true, payload?.get("fresh")?.jsonPrimitive?.content?.toBoolean())
        assertEquals("https://new.example/api/v1/miniapp/rpc", f.transport.singleRequest().url)
        assertEquals("new", f.transport.singleRequest().header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
    }

    @Test
    fun rpcWriteReturnsConnectionMessageWithoutTokenAndSkipsNetwork() = runTest {
        val f = gatewayClientFixture(token = "")

        val error = f.client.rpcWrite("miniapp.write", buildJsonObject {})

        assertEquals("게이트웨이에 연결되어 있지 않습니다.", error)
        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun rpcWriteReturnsNullOnAcceptedWrite() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)

        val error = f.client.rpcWrite("miniapp.write", buildJsonObject { put("id", "one") })

        assertNull(error)
        assertEquals("miniapp.write", f.transport.singleRequest().rpcMethod)
        assertEquals("one", f.transport.singleRequest().rpcParams?.get("id")?.jsonPrimitive?.content)
    }

    @Test
    fun rpcWriteReturnsGatewayValidationMessage() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = false, message = "schedule is invalid")

        val error = f.client.rpcWrite("miniapp.write", buildJsonObject {})

        assertEquals("schedule is invalid", error)
    }

    @Test
    fun rpcWriteUsesGenericMessageForBlankGatewayError() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = false, message = "")

        val error = f.client.rpcWrite("miniapp.write", buildJsonObject {})

        assertEquals("요청을 처리하지 못했습니다.", error)
    }

    @Test
    fun rpcWriteUsesGenericMessageForMalformedResponse() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("not-json")

        val error = f.client.rpcWrite("miniapp.write", buildJsonObject {})

        assertEquals("요청을 처리하지 못했습니다.", error)
    }

    @Test
    fun rpcWriteUsesGenericMessageForTransportFailure() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(IllegalStateException("offline"))

        val error = f.client.rpcWrite("miniapp.write", buildJsonObject {})

        assertEquals("요청을 처리하지 못했습니다.", error)
    }

    @Test
    fun rpcWriteRejectsOkBodyDeliveredWithHttpFailure() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true, status = HttpStatusCode.ServiceUnavailable)

        val error = f.client.rpcWrite("miniapp.write", buildJsonObject {})

        assertEquals("요청을 처리하지 못했습니다.", error)
    }

    @Test
    fun rpcWritePropagatesCancellation() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel write"))

        val failure = assertFailsWith<CancellationException> {
            f.client.rpcWrite("miniapp.write", buildJsonObject {})
        }

        assertEquals("cancel write", failure.message)
    }

    @Test
    fun rpcWriteSendsCurrentTokenAndExactParams() = runTest {
        val f = gatewayClientFixture(token = "write-token")
        f.transport.enqueueWrite(ok = true)
        val params = buildJsonObject {
            put("name", "업무")
            put("enabled", false)
        }

        f.client.rpcWrite("miniapp.write", params)

        val request = f.transport.singleRequest()
        assertEquals("write-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
        assertEquals("업무", request.rpcParams?.get("name")?.jsonPrimitive?.content)
        assertEquals(false, request.rpcParams?.get("enabled")?.jsonPrimitive?.content?.toBoolean())
    }

    @Test
    fun switchSessionUpdatesFlowAndDurableLastSessionTogether() {
        val f = gatewayClientFixture()

        f.client.switchSession("client:main:next")

        assertEquals("client:main:next", f.client.currentConversationId.value)
        assertEquals("client:main:next", f.settings.lastSession())
    }

    @Test
    fun newSessionKeyUsesExplicitClientMainNamespace() {
        val f = gatewayClientFixture()

        val first = f.client.newSessionKey()
        val second = f.client.newSessionKey()

        assertTrue(first.startsWith("client:main:"))
        assertTrue(second.startsWith("client:main:"))
        assertFalse(first.substringAfter("client:main:").contains(':'))
        assertFalse(second.substringAfter("client:main:").contains(':'))
        assertFalse(first == second)
    }
}
