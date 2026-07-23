package ai.deneb.deneb

import io.ktor.http.ContentType
import io.ktor.http.HttpStatusCode
import io.ktor.serialization.JsonConvertException
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

/** Low-level chat POST/SSE transport contracts after extraction from the gateway facade. */
class GatewayChatTransportContractTest {
    @Test
    fun blockingChatReturnsMissingTokenReplyWithoutNetwork() = runTest {
        val transport = GatewayHttpHarness()

        val reply = sendGatewayChat(
            http = transport.httpClient,
            gatewayUrl = "https://gateway.example",
            clientToken = "",
            sessionKey = "client:main",
            message = "hello",
        )

        assertFalse(reply.ok)
        assertTrue(reply.text.contains("토큰"))
        assertEquals("", reply.model)
        assertFalse(reply.fellBack)
        assertTrue(transport.requests.isEmpty())
    }

    @Test
    fun blockingChatPostsTypedRequestAndMapsSuccessfulPayload() = runTest {
        val transport = GatewayHttpHarness()
        transport.enqueueJson(
            """{
                "ok":true,
                "payload":{
                    "text":"완료했습니다.",
                    "model":"provider/model",
                    "sessionKey":"client:main:returned",
                    "fellBack":true
                }
            }
            """.trimIndent(),
        )

        val reply = sendGatewayChat(
            http = transport.httpClient,
            gatewayUrl = "https://gateway.example/",
            clientToken = "chat-token",
            sessionKey = "client:main:target",
            message = "계약서를 검토해줘",
        )

        assertTrue(reply.ok)
        assertEquals("완료했습니다.", reply.text)
        assertEquals("provider/model", reply.model)
        assertTrue(reply.fellBack)
        val request = transport.singleRequest()
        assertEquals("POST", request.method.value)
        assertEquals("https://gateway.example/api/v1/miniapp/rpc", request.url)
        assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
        assertEquals("chat-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
        assertEquals("miniapp.chat.send", request.rpcMethod)
        assertEquals("계약서를 검토해줘", request.rpcParams?.get("message")?.jsonPrimitive?.content)
        assertEquals("client:main:target", request.rpcParams?.get("sessionKey")?.jsonPrimitive?.content)
        assertTrue(request.jsonBody?.get("id")?.jsonPrimitive?.content.orEmpty().isNotBlank())
    }

    @Test
    fun blockingChatOmitsNullSessionKeyForDefaultCompatiblePayload() = runTest {
        val transport = GatewayHttpHarness()
        transport.enqueueJson("""{"ok":true,"payload":{"text":"ok"}}""")

        val reply = sendGatewayChat(
            http = transport.httpClient,
            gatewayUrl = "https://gateway.example",
            clientToken = "token",
            sessionKey = null,
            message = "message",
        )

        assertEquals("ok", reply.text)
        assertFalse(transport.singleRequest().rpcParams?.containsKey("sessionKey") == true)
    }

    @Test
    fun blockingChatReturnsGatewayErrorReplyForRejectedEnvelope() = runTest {
        val transport = GatewayHttpHarness()
        transport.enqueueJson("""{"ok":false,"payload":{"text":"must not surface"}}""")

        val reply = sendGatewayChat(
            transport.httpClient,
            "https://gateway.example",
            "token",
            "client:main",
            "message",
        )

        assertFalse(reply.ok)
        assertEquals("⚠️ 게이트웨이 오류", reply.text)
    }

    @Test
    fun blockingChatReturnsGatewayErrorReplyForMissingPayload() = runTest {
        val transport = GatewayHttpHarness()
        transport.enqueueJson("""{"ok":true}""")

        val reply = sendGatewayChat(
            transport.httpClient,
            "https://gateway.example",
            "token",
            "client:main",
            "message",
        )

        assertFalse(reply.ok)
        assertEquals("⚠️ 게이트웨이 오류", reply.text)
    }

    @Test
    fun blockingChatThrowsForHttpFailureBeforeDecodingBody() = runTest {
        val transport = GatewayHttpHarness()
        transport.enqueueJson(
            body = """{"ok":true,"payload":{"text":"stale"}}""",
            status = HttpStatusCode.BadGateway,
        )

        val failure = assertFailsWith<IllegalStateException> {
            sendGatewayChat(
                transport.httpClient,
                "https://gateway.example",
                "token",
                "client:main",
                "message",
            )
        }

        assertEquals("chat HTTP 502", failure.message)
    }

    @Test
    fun blockingChatRejectsMalformedEnvelope() = runTest {
        val transport = GatewayHttpHarness()
        transport.enqueueJson("not-json")

        assertFailsWith<JsonConvertException> {
            sendGatewayChat(
                transport.httpClient,
                "https://gateway.example",
                "token",
                "client:main",
                "message",
            )
        }
    }

    @Test
    fun blockingChatPropagatesCancellation() = runTest {
        val transport = GatewayHttpHarness()
        transport.enqueueFailure(CancellationException("cancel blocking chat"))

        val failure = assertFailsWith<CancellationException> {
            sendGatewayChat(
                transport.httpClient,
                "https://gateway.example",
                "token",
                "client:main",
                "message",
            )
        }

        assertEquals("cancel blocking chat", failure.message)
    }

    @Test
    fun streamingChatReturnsMissingTokenReplyWithoutCallbacksOrNetwork() = runTest {
        val transport = GatewayHttpHarness()
        val deltas = mutableListOf<String>()
        val tools = mutableListOf<ToolEvent>()
        val thinking = mutableListOf<String>()

        val reply = streamGatewayChat(
            http = transport.httpClient,
            jsonCodec = GatewayHttpHarness.TEST_JSON,
            gatewayUrl = "https://gateway.example",
            clientToken = "",
            sessionKey = "client:main",
            message = "hello",
            onTool = tools::add,
            onThinking = thinking::add,
            onDelta = deltas::add,
        )

        assertFalse(reply.ok)
        assertTrue(reply.text.contains("토큰"))
        assertTrue(deltas.isEmpty())
        assertTrue(tools.isEmpty())
        assertTrue(thinking.isEmpty())
        assertTrue(transport.requests.isEmpty())
    }

    @Test
    fun streamingChatDispatchesDeltaThinkingToolAndDoneFramesInOrder() = runTest {
        val transport = GatewayHttpHarness()
        transport.enqueueSse(
            """
            : keepalive

            event: thinking
            data: {"preview":"계약 조항 비교 중"}

            event: tool
            data: {"state":"started","tool":"mail_search","toolUseId":"tool-1","detail":"검색","isError":false}

            event: delta
            data: {"delta":"첫째 "}

            event: delta
            data: {"delta":"둘째"}

            event: tool
            data: {"state":"completed","tool":"mail_search","toolUseId":"tool-1","detail":"2건","isError":false}

            event: done
            data: {"text":"첫째 둘째","model":"provider/model","fellBack":true}

            """.trimIndent(),
        )
        val deltas = mutableListOf<String>()
        val tools = mutableListOf<ToolEvent>()
        val thinking = mutableListOf<String>()

        val reply = streamGatewayChat(
            http = transport.httpClient,
            jsonCodec = GatewayHttpHarness.TEST_JSON,
            gatewayUrl = "https://gateway.example",
            clientToken = "stream-token",
            sessionKey = "client:main:stream",
            message = "분석",
            onTool = tools::add,
            onThinking = thinking::add,
            onDelta = deltas::add,
        )

        assertEquals(listOf("계약 조항 비교 중"), thinking)
        assertEquals(listOf("첫째 ", "둘째"), deltas)
        assertEquals(listOf("started", "completed"), tools.map { it.state })
        assertEquals(listOf("mail_search", "mail_search"), tools.map { it.tool })
        assertEquals(listOf("tool-1", "tool-1"), tools.map { it.toolUseId })
        assertEquals(listOf("검색", "2건"), tools.map { it.detail })
        assertTrue(tools.none { it.isError })
        assertEquals("첫째 둘째", reply.text)
        assertEquals("provider/model", reply.model)
        assertTrue(reply.fellBack)
        val request = transport.singleRequest()
        assertEquals("https://gateway.example/api/v1/miniapp/chat/stream", request.url)
        assertEquals("stream-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
        assertEquals("분석", request.jsonBody?.get("message")?.jsonPrimitive?.content)
        assertEquals("client:main:stream", request.jsonBody?.get("sessionKey")?.jsonPrimitive?.content)
    }

    @Test
    fun streamingChatIgnoresUnknownMalformedAndEmptyFrames() = runTest {
        val transport = GatewayHttpHarness()
        transport.enqueueSse(
            """
            event: future
            data: {"value":1}

            event: delta
            data: not-json

            event: delta
            data: {"delta":""}

            event: tool
            data: {"state":"started","tool":""}

            event: thinking
            data: not-json

            event: done
            data: {"text":"final"}

            """.trimIndent(),
        )
        val deltas = mutableListOf<String>()
        val tools = mutableListOf<ToolEvent>()
        val thinking = mutableListOf<String>()

        val reply = streamGatewayChat(
            transport.httpClient,
            GatewayHttpHarness.TEST_JSON,
            "https://gateway.example",
            "token",
            "client:main",
            "message",
            onTool = tools::add,
            onThinking = thinking::add,
            onDelta = deltas::add,
        )

        assertTrue(deltas.isEmpty())
        assertTrue(tools.isEmpty())
        assertEquals(listOf(""), thinking)
        assertEquals("final", reply.text)
    }

    @Test
    fun streamingChatUsesLastDoneFrameAsCanonicalResult() = runTest {
        val transport = GatewayHttpHarness()
        transport.enqueueSse(
            """
            event: done
            data: {"text":"intermediate","model":"model-a","fellBack":false}

            event: done
            data: {"text":"final","model":"model-b","fellBack":true}

            """.trimIndent(),
        )

        val reply = streamGatewayChat(
            transport.httpClient,
            GatewayHttpHarness.TEST_JSON,
            "https://gateway.example",
            "token",
            "client:main",
            "message",
            onDelta = {},
        )

        assertEquals("final", reply.text)
        assertEquals("model-b", reply.model)
        assertTrue(reply.fellBack)
    }

    @Test
    fun streamingChatFailsWhenStreamEndsWithoutTerminalEvent() = runTest {
        val transport = GatewayHttpHarness()
        transport.enqueueSse(
            """
            event: delta
            data: {"delta":"partial"}

            """.trimIndent(),
        )
        val deltas = mutableListOf<String>()

        val failure = assertFailsWith<IllegalStateException> {
            streamGatewayChat(
                transport.httpClient,
                GatewayHttpHarness.TEST_JSON,
                "https://gateway.example",
                "token",
                "client:main",
                "message",
                onDelta = deltas::add,
            )
        }

        assertEquals(listOf("partial"), deltas)
        assertEquals("chat stream ended before terminal event", failure.message)
    }

    @Test
    fun streamingChatThrowsGatewayErrorFrameMessage() = runTest {
        val transport = GatewayHttpHarness()
        transport.enqueueSse(
            """
            event: error
            data: {"error":"tool execution failed"}

            """.trimIndent(),
        )

        val failure = assertFailsWith<IllegalStateException> {
            streamGatewayChat(
                transport.httpClient,
                GatewayHttpHarness.TEST_JSON,
                "https://gateway.example",
                "token",
                "client:main",
                "message",
                onDelta = {},
            )
        }

        assertEquals("tool execution failed", failure.message)
    }

    @Test
    fun streamingChatUsesGenericMessageForMalformedErrorFrame() = runTest {
        val transport = GatewayHttpHarness()
        transport.enqueueSse(
            """
            event: error
            data: not-json

            """.trimIndent(),
        )

        val failure = assertFailsWith<IllegalStateException> {
            streamGatewayChat(
                transport.httpClient,
                GatewayHttpHarness.TEST_JSON,
                "https://gateway.example",
                "token",
                "client:main",
                "message",
                onDelta = {},
            )
        }

        assertEquals("gateway stream error", failure.message)
    }

    @Test
    fun streamingChatRejectsHttpFailureBeforeReadingEventBody() = runTest {
        val transport = GatewayHttpHarness()
        transport.enqueueSse(
            text = "event: done\ndata: {\"text\":\"stale\"}\n\n",
            status = HttpStatusCode.ServiceUnavailable,
        )

        val failure = assertFailsWith<IllegalStateException> {
            streamGatewayChat(
                transport.httpClient,
                GatewayHttpHarness.TEST_JSON,
                "https://gateway.example",
                "token",
                "client:main",
                "message",
                onDelta = {},
            )
        }

        assertEquals("stream HTTP 503", failure.message)
    }

    @Test
    fun streamingChatPropagatesTransportCancellation() = runTest {
        val transport = GatewayHttpHarness()
        transport.enqueueFailure(CancellationException("cancel stream"))

        val failure = assertFailsWith<CancellationException> {
            streamGatewayChat(
                transport.httpClient,
                GatewayHttpHarness.TEST_JSON,
                "https://gateway.example",
                "token",
                "client:main",
                "message",
                onDelta = {},
            )
        }

        assertEquals("cancel stream", failure.message)
    }

    @Test
    fun rpcTransportDtosPreserveNestedSendPayloadAndError() {
        val json = GatewayHttpHarness.TEST_JSON
        val request = RpcRequest(
            id = "request-1",
            method = "miniapp.chat.send",
            params = SendParams("한글 message", "client:main:one"),
        )
        val response = RpcResponse(
            ok = true,
            payload = SendPayload(
                text = "reply",
                model = "provider/model",
                sessionKey = "client:main:one",
                fellBack = true,
            ),
        )
        val rpcRequest = RpcReq(
            id = "request-2",
            method = "miniapp.test",
            params = buildJsonObject { put("value", "opaque") },
        )
        val result = RpcResult(
            ok = false,
            error = RpcError(code = "invalid", message = "잘못된 요청"),
        )

        assertEquals(request, json.decodeFromString(RpcRequest.serializer(), json.encodeToString(RpcRequest.serializer(), request)))
        assertEquals(response, json.decodeFromString(RpcResponse.serializer(), json.encodeToString(RpcResponse.serializer(), response)))
        assertEquals(rpcRequest, json.decodeFromString(RpcReq.serializer(), json.encodeToString(RpcReq.serializer(), rpcRequest)))
        assertEquals(result, json.decodeFromString(RpcResult.serializer(), json.encodeToString(RpcResult.serializer(), result)))
    }
}
