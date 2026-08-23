package ai.deneb.deneb

import io.ktor.http.ContentType
import io.ktor.http.HttpStatusCode
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * Runtime endpoint contracts for work-feed, observation, session, and detached-chat flows.
 *
 * These calls run while the app is backgrounded as well as foregrounded, so exact
 * parameters, auth fencing, cancellation, and HTTP failure handling are all part
 * of the runtime-health contract rather than UI-only behavior.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class GatewayRuntimeEndpointContractTest {
    @Test
    fun refreshWorkFeedRangeSendsExactRuntimeContract() = runTest {
        val f = gatewayClientFixture(token = "runtime-token")
        f.transport.enqueueRpc(payload = "{}", ok = false)

        f.client.refreshWorkFeed(sinceMs = 100L, beforeMs = 500L, merge = true)

        val request = f.transport.singleRequest()
        val params = request.requireRpc("miniapp.workfeed.list")
        assertEquals("POST", request.method.value)
        assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
        assertEquals("runtime-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
        assertEquals(setOf("limit", "sinceMs", "beforeMs"), params.keys)
        assertEquals(100, params["limit"]?.jsonPrimitive?.content?.toInt())
        assertEquals(100L, params["sinceMs"]?.jsonPrimitive?.content?.toLong())
        assertEquals(500L, params["beforeMs"]?.jsonPrimitive?.content?.toLong())
    }

    @Test
    fun refreshWorkFeedRangeSkipsNetworkWithoutCredentials() = runTest {
        val f = gatewayClientFixture(token = "")

        f.client.refreshWorkFeed(sinceMs = 100L, beforeMs = 500L, merge = true)

        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun refreshWorkFeedRangePropagatesCancellationWithoutPublishingFallbackData() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel refreshWorkFeedRange"))

        val failure = assertFailsWith<CancellationException> {
            f.client.refreshWorkFeed(sinceMs = 100L, beforeMs = 500L, merge = true)
        }

        assertEquals("cancel refreshWorkFeedRange", failure.message)
        assertEquals(1, f.transport.requests.size)
        assertEquals("miniapp.workfeed.list", f.transport.singleRequest().rpcMethod)
    }

    @Test
    fun translateSegmentsSendsExactRuntimeContract() = runTest {
        val f = gatewayClientFixture(token = "runtime-token")
        f.transport.enqueueRpc(payload = "{}", ok = false)

        f.client.translateSegments(listOf("first", "둘째"), targetLang = "ja")

        val request = f.transport.singleRequest()
        val params = request.requireRpc("miniapp.web.translate")
        assertEquals("POST", request.method.value)
        assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
        assertEquals("runtime-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
        assertEquals(setOf("segments", "targetLang"), params.keys)
        assertEquals(listOf("first", "둘째"), params["segments"]?.jsonArray?.map { it.jsonPrimitive.content })
        assertEquals("ja", params["targetLang"]?.jsonPrimitive?.content)
    }

    @Test
    fun translateSegmentsSkipsNetworkWithoutCredentials() = runTest {
        val f = gatewayClientFixture(token = "")

        f.client.translateSegments(listOf("first", "둘째"), targetLang = "ja")

        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun translateSegmentsPropagatesCancellationWithoutPublishingFallbackData() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel translateSegments"))

        val failure = assertFailsWith<CancellationException> {
            f.client.translateSegments(listOf("first", "둘째"), targetLang = "ja")
        }

        assertEquals("cancel translateSegments", failure.message)
        assertEquals(1, f.transport.requests.size)
        assertEquals("miniapp.web.translate", f.transport.singleRequest().rpcMethod)
    }

    @Test
    fun observeBehaviorSendsExactRuntimeContract() = runTest {
        val f = gatewayClientFixture(token = "runtime-token")
        f.transport.enqueueRpc(payload = "{}", ok = false)

        f.client.observeBehavior(days = 31)

        val request = f.transport.singleRequest()
        val params = request.requireRpc("miniapp.observe.behavior")
        assertEquals("POST", request.method.value)
        assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
        assertEquals("runtime-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
        assertEquals(setOf("days"), params.keys)
        assertEquals(31, params["days"]?.jsonPrimitive?.content?.toInt())
    }

    @Test
    fun observeBehaviorSkipsNetworkWithoutCredentials() = runTest {
        val f = gatewayClientFixture(token = "")

        f.client.observeBehavior(days = 31)

        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun observeBehaviorPropagatesCancellationWithoutPublishingFallbackData() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel observeBehavior"))

        val failure = assertFailsWith<CancellationException> {
            f.client.observeBehavior(days = 31)
        }

        assertEquals("cancel observeBehavior", failure.message)
        assertEquals(1, f.transport.requests.size)
        assertEquals("miniapp.observe.behavior", f.transport.singleRequest().rpcMethod)
    }

    @Test
    fun observeLogsSendsExactRuntimeContract() = runTest {
        val f = gatewayClientFixture(token = "runtime-token")
        f.transport.enqueueRpc(payload = "{}", ok = false)

        f.client.observeLogs(level = "warn", limit = 77, days = 14)

        val request = f.transport.singleRequest()
        val params = request.requireRpc("miniapp.observe.logs")
        assertEquals("POST", request.method.value)
        assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
        assertEquals("runtime-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
        assertEquals(setOf("level", "limit", "days"), params.keys)
        assertEquals("warn", params["level"]?.jsonPrimitive?.content)
        assertEquals(77, params["limit"]?.jsonPrimitive?.content?.toInt())
        assertEquals(14, params["days"]?.jsonPrimitive?.content?.toInt())
    }

    @Test
    fun observeLogsSkipsNetworkWithoutCredentials() = runTest {
        val f = gatewayClientFixture(token = "")

        f.client.observeLogs(level = "warn", limit = 77, days = 14)

        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun observeLogsPropagatesCancellationWithoutPublishingFallbackData() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel observeLogs"))

        val failure = assertFailsWith<CancellationException> {
            f.client.observeLogs(level = "warn", limit = 77, days = 14)
        }

        assertEquals("cancel observeLogs", failure.message)
        assertEquals(1, f.transport.requests.size)
        assertEquals("miniapp.observe.logs", f.transport.singleRequest().rpcMethod)
    }

    @Test
    fun observeHealthSendsExactRuntimeContract() = runTest {
        val f = gatewayClientFixture(token = "runtime-token")
        f.transport.enqueueRpc(payload = "{}", ok = false)

        f.client.observeHealth()

        val request = f.transport.singleRequest()
        val params = request.requireRpc("miniapp.observe.health")
        assertEquals("POST", request.method.value)
        assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
        assertEquals("runtime-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
        assertEquals(emptySet(), params.keys)
    }

    @Test
    fun observeHealthSkipsNetworkWithoutCredentials() = runTest {
        val f = gatewayClientFixture(token = "")

        f.client.observeHealth()

        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun observeHealthPropagatesCancellationWithoutPublishingFallbackData() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel observeHealth"))

        val failure = assertFailsWith<CancellationException> {
            f.client.observeHealth()
        }

        assertEquals("cancel observeHealth", failure.message)
        assertEquals(1, f.transport.requests.size)
        assertEquals("miniapp.observe.health", f.transport.singleRequest().rpcMethod)
    }

    @Test
    fun runWorkFeedActionSendsExactRuntimeContract() = runTest {
        val f = gatewayClientFixture(token = "runtime-token")
        f.transport.enqueueRpc(payload = "{}", ok = false)

        f.client.runWorkFeedAction("item-7", "approve", adoptSession = false)

        val request = f.transport.singleRequest()
        val params = request.requireRpc("miniapp.workfeed.action.run")
        assertEquals("POST", request.method.value)
        assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
        assertEquals("runtime-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
        assertEquals(setOf("itemId", "actionId"), params.keys)
        assertEquals("item-7", params["itemId"]?.jsonPrimitive?.content)
        assertEquals("approve", params["actionId"]?.jsonPrimitive?.content)
    }

    @Test
    fun runWorkFeedRejectActionSendsTrimmedComment() = runTest {
        val f = gatewayClientFixture(token = "runtime-token")
        f.transport.enqueueRpc(payload = "{}", ok = false)

        f.client.runWorkFeedAction(
            "item-7",
            "approval:reject",
            comment = "  예산 재검토  ",
            adoptSession = false,
        )

        val params = f.transport.singleRequest().requireRpc("miniapp.workfeed.action.run")
        assertEquals(setOf("itemId", "actionId", "comment"), params.keys)
        assertEquals("예산 재검토", params["comment"]?.jsonPrimitive?.content)
    }

    @Test
    fun runWorkFeedActionDropsCommentUnlessNonBlankReject() = runTest {
        val f = gatewayClientFixture(token = "runtime-token")
        repeat(3) { f.transport.enqueueRpc(payload = "{}", ok = false) }

        f.client.runWorkFeedAction("item-1", "approval:approve", comment = "drop", adoptSession = false)
        f.client.runWorkFeedAction("item-2", "ack", comment = "drop", adoptSession = false)
        f.client.runWorkFeedAction("item-3", "approval:reject", comment = "  ", adoptSession = false)

        f.transport.requests.forEach { request ->
            assertFalse(request.requireRpc("miniapp.workfeed.action.run").containsKey("comment"))
        }
    }

    @Test
    fun runWorkFeedActionSkipsNetworkWithoutCredentials() = runTest {
        val f = gatewayClientFixture(token = "")

        f.client.runWorkFeedAction("item-7", "approve", adoptSession = false)

        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun runWorkFeedActionPropagatesCancellationWithoutPublishingFallbackData() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel runWorkFeedAction"))

        val failure = assertFailsWith<CancellationException> {
            f.client.runWorkFeedAction("item-7", "approve", adoptSession = false)
        }

        assertEquals("cancel runWorkFeedAction", failure.message)
        assertEquals(1, f.transport.requests.size)
        assertEquals("miniapp.workfeed.action.run", f.transport.singleRequest().rpcMethod)
    }

    @Test
    fun answerWorkFeedItemSendsExactRuntimeContract() = runTest {
        val f = gatewayClientFixture(token = "runtime-token")
        f.transport.enqueueRpc(payload = "{}", ok = false)

        f.client.answerWorkFeedItem("item-8", "직접 답변")

        val request = f.transport.singleRequest()
        val params = request.requireRpc("miniapp.workfeed.answer")
        assertEquals("POST", request.method.value)
        assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
        assertEquals("runtime-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
        assertEquals(setOf("itemId", "answer"), params.keys)
        assertEquals("item-8", params["itemId"]?.jsonPrimitive?.content)
        assertEquals("직접 답변", params["answer"]?.jsonPrimitive?.content)
    }

    @Test
    fun answerWorkFeedItemSkipsNetworkWithoutCredentials() = runTest {
        val f = gatewayClientFixture(token = "")

        f.client.answerWorkFeedItem("item-8", "직접 답변")

        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun answerWorkFeedItemPropagatesCancellationWithoutPublishingFallbackData() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel answerWorkFeedItem"))

        val failure = assertFailsWith<CancellationException> {
            f.client.answerWorkFeedItem("item-8", "직접 답변")
        }

        assertEquals("cancel answerWorkFeedItem", failure.message)
        assertEquals(1, f.transport.requests.size)
        assertEquals("miniapp.workfeed.answer", f.transport.singleRequest().rpcMethod)
    }

    @Test
    fun sendWorkFeedFeedbackSendsExactRuntimeContract() = runTest {
        val f = gatewayClientFixture(token = "runtime-token")
        f.transport.enqueueRpc(payload = "{}", ok = false)

        f.client.sendWorkFeedFeedback("item-9", "고객명이 잘못됨")

        val request = f.transport.singleRequest()
        val params = request.requireRpc("miniapp.workfeed.feedback")
        assertEquals("POST", request.method.value)
        assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
        assertEquals("runtime-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
        assertEquals(setOf("itemId", "feedback"), params.keys)
        assertEquals("item-9", params["itemId"]?.jsonPrimitive?.content)
        assertEquals("고객명이 잘못됨", params["feedback"]?.jsonPrimitive?.content)
    }

    @Test
    fun sendWorkFeedFeedbackSkipsNetworkWithoutCredentials() = runTest {
        val f = gatewayClientFixture(token = "")

        f.client.sendWorkFeedFeedback("item-9", "고객명이 잘못됨")

        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun sendWorkFeedFeedbackPropagatesCancellationWithoutPublishingFallbackData() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel sendWorkFeedFeedback"))

        val failure = assertFailsWith<CancellationException> {
            f.client.sendWorkFeedFeedback("item-9", "고객명이 잘못됨")
        }

        assertEquals("cancel sendWorkFeedFeedback", failure.message)
        assertEquals(1, f.transport.requests.size)
        assertEquals("miniapp.workfeed.feedback", f.transport.singleRequest().rpcMethod)
    }

    @Test
    fun rewriteWorkFeedCardSendsExactRuntimeContract() = runTest {
        val f = gatewayClientFixture(token = "runtime-token")
        f.transport.enqueueRpc(payload = "{}", ok = false)

        f.client.rewriteWorkFeedCard("item-10")

        val request = f.transport.singleRequest()
        val params = request.requireRpc("miniapp.workfeed.rewrite")
        assertEquals("POST", request.method.value)
        assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
        assertEquals("runtime-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
        assertEquals(setOf("itemId"), params.keys)
        assertEquals("item-10", params["itemId"]?.jsonPrimitive?.content)
    }

    @Test
    fun rewriteWorkFeedCardSkipsNetworkWithoutCredentials() = runTest {
        val f = gatewayClientFixture(token = "")

        f.client.rewriteWorkFeedCard("item-10")

        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun rewriteWorkFeedCardPropagatesCancellationWithoutPublishingFallbackData() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel rewriteWorkFeedCard"))

        val failure = assertFailsWith<CancellationException> {
            f.client.rewriteWorkFeedCard("item-10")
        }

        assertEquals("cancel rewriteWorkFeedCard", failure.message)
        assertEquals(1, f.transport.requests.size)
        assertEquals("miniapp.workfeed.rewrite", f.transport.singleRequest().rpcMethod)
    }

    @Test
    fun markWorkFeedReadSendsExactRuntimeContract() = runTest {
        val f = gatewayClientFixture(token = "runtime-token")
        f.transport.enqueueRpc(payload = "{}", ok = false)

        f.client.markWorkFeedRead("item-11")

        val request = f.transport.singleRequest()
        val params = request.requireRpc("miniapp.workfeed.read")
        assertEquals("POST", request.method.value)
        assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
        assertEquals("runtime-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
        assertEquals(setOf("itemId"), params.keys)
        assertEquals("item-11", params["itemId"]?.jsonPrimitive?.content)
    }

    @Test
    fun markWorkFeedReadSkipsNetworkWithoutCredentials() = runTest {
        val f = gatewayClientFixture(token = "")

        f.client.markWorkFeedRead("item-11")

        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun markWorkFeedReadPropagatesCancellationWithoutPublishingFallbackData() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel markWorkFeedRead"))

        val failure = assertFailsWith<CancellationException> {
            f.client.markWorkFeedRead("item-11")
        }

        assertEquals("cancel markWorkFeedRead", failure.message)
        assertEquals(1, f.transport.requests.size)
        assertEquals("miniapp.workfeed.read", f.transport.singleRequest().rpcMethod)
    }

    @Test
    fun ingestEventSendsExactRuntimeContract() = runTest {
        val f = gatewayClientFixture(token = "runtime-token")
        f.transport.enqueueRpc(payload = "{}", ok = false)

        f.client.ingestEvent("notification", "com.example", "결제 승인 42,000원")

        val request = f.transport.singleRequest()
        val params = request.requireRpc("miniapp.event.ingest")
        assertEquals("POST", request.method.value)
        assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
        assertEquals("runtime-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
        assertEquals(setOf("type", "source", "text"), params.keys)
        assertEquals("notification", params["type"]?.jsonPrimitive?.content)
        assertEquals("com.example", params["source"]?.jsonPrimitive?.content)
        assertEquals("결제 승인 42,000원", params["text"]?.jsonPrimitive?.content)
    }

    @Test
    fun ingestEventSkipsNetworkWithoutCredentials() = runTest {
        val f = gatewayClientFixture(token = "")

        f.client.ingestEvent("notification", "com.example", "결제 승인 42,000원")

        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun ingestEventPropagatesCancellationWithoutPublishingFallbackData() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel ingestEvent"))

        val failure = assertFailsWith<CancellationException> {
            f.client.ingestEvent("notification", "com.example", "결제 승인 42,000원")
        }

        assertEquals("cancel ingestEvent", failure.message)
        assertEquals(1, f.transport.requests.size)
        assertEquals("miniapp.event.ingest", f.transport.singleRequest().rpcMethod)
    }

    @Test
    fun fetchRecentSessionsSendsExactRuntimeContract() = runTest {
        val f = gatewayClientFixture(token = "runtime-token")
        f.transport.enqueueRpc(payload = "{}", ok = false)

        f.client.fetchRecentSessions()

        val request = f.transport.singleRequest()
        val params = request.requireRpc("miniapp.sessions.recent")
        assertEquals("POST", request.method.value)
        assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
        assertEquals("runtime-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
        assertEquals(setOf("limit"), params.keys)
        assertEquals(50, params["limit"]?.jsonPrimitive?.content?.toInt())
        assertEquals(null, params["channel"])
    }

    @Test
    fun fetchRecentSessionsSkipsNetworkWithoutCredentials() = runTest {
        val f = gatewayClientFixture(token = "")

        f.client.fetchRecentSessions()

        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun fetchRecentSessionsPropagatesCancellationWithoutPublishingFallbackData() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel fetchRecentSessions"))

        val failure = assertFailsWith<CancellationException> {
            f.client.fetchRecentSessions()
        }

        assertEquals("cancel fetchRecentSessions", failure.message)
        assertEquals(1, f.transport.requests.size)
        assertEquals("miniapp.sessions.recent", f.transport.singleRequest().rpcMethod)
    }

    @Test
    fun fetchTranscriptSendsExactRuntimeContract() = runTest {
        val f = gatewayClientFixture(token = "runtime-token")
        f.transport.enqueueRpc(payload = "{}", ok = false)

        f.client.fetchTranscript("client:main:opaque /?#")

        val request = f.transport.singleRequest()
        val params = request.requireRpc("miniapp.sessions.transcript")
        assertEquals("POST", request.method.value)
        assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
        assertEquals("runtime-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
        assertEquals(setOf("sessionKey", "limit"), params.keys)
        assertEquals("client:main:opaque /?#", params["sessionKey"]?.jsonPrimitive?.content)
        assertEquals(200, params["limit"]?.jsonPrimitive?.content?.toInt())
    }

    @Test
    fun fetchTranscriptSkipsNetworkWithoutCredentials() = runTest {
        val f = gatewayClientFixture(token = "")

        f.client.fetchTranscript("client:main:opaque /?#")

        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun fetchTranscriptPropagatesCancellationWithoutPublishingFallbackData() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel fetchTranscript"))

        val failure = assertFailsWith<CancellationException> {
            f.client.fetchTranscript("client:main:opaque /?#")
        }

        assertEquals("cancel fetchTranscript", failure.message)
        assertEquals(1, f.transport.requests.size)
        assertEquals("miniapp.sessions.transcript", f.transport.singleRequest().rpcMethod)
    }

    @Test
    fun translateSegmentsReturnsEmptyWithoutNetworkForEmptyInput() = runTest {
        val f = gatewayClientFixture()

        val translated = f.client.translateSegments(emptyList(), targetLang = "ko")

        assertEquals(emptyList(), translated)
        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun workFeedCommandsRejectBlankIdentityOrContentBeforeNetwork() = runTest {
        val f = gatewayClientFixture()

        assertNull(f.client.runWorkFeedAction("", "approve"))
        assertNull(f.client.runWorkFeedAction("item", " "))
        assertNull(f.client.answerWorkFeedItem("", "answer"))
        assertNull(f.client.answerWorkFeedItem("item", " "))
        assertNull(f.client.sendWorkFeedFeedback("", "feedback"))
        assertNull(f.client.sendWorkFeedFeedback("item", " "))
        assertNull(f.client.rewriteWorkFeedCard(" "))
        assertFalse(f.client.markWorkFeedRead(" "))
        assertFalse(f.client.ingestEvent("type", "source", " "))

        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun observeLogsOmitsNonPositiveDaysButKeepsLevelAndLimit() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(payload = "{}", ok = false)

        f.client.observeLogs(level = "error", limit = 1, days = 0)

        val params = f.transport.singleRequest().requireRpc("miniapp.observe.logs")
        assertEquals(setOf("level", "limit"), params.keys)
        assertEquals("error", params["level"]?.jsonPrimitive?.content)
        assertEquals(1, params["limit"]?.jsonPrimitive?.content?.toInt())
    }

    @Test
    fun detachedChatPostsExplicitSessionAndReturnsAcceptedStatus() = runTest {
        val f = gatewayClientFixture(token = "detached-token")
        f.transport.enqueueJson("""{"ok":true,"payload":{"text":"done"}}""")

        val accepted = f.client.sendDetachedChat(
            message = "운전 중 음성 답장",
            targetSessionKey = "client:main:auto",
        )

        assertTrue(accepted)
        val request = f.transport.singleRequest()
        val params = request.requireRpc("miniapp.chat.send")
        assertEquals("POST", request.method.value)
        assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
        assertEquals("detached-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
        assertEquals("운전 중 음성 답장", params["message"]?.jsonPrimitive?.content)
        assertEquals("client:main:auto", params["sessionKey"]?.jsonPrimitive?.content)
        assertTrue(request.jsonBody?.get("id")?.jsonPrimitive?.content.orEmpty().isNotBlank())
    }

    @Test
    fun detachedChatUsesMainSessionByDefault() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("""{"ok":true}""")

        val accepted = f.client.sendDetachedChat("default target")

        assertTrue(accepted)
        assertEquals(
            "client:main",
            f.transport.singleRequest().rpcParams?.get("sessionKey")?.jsonPrimitive?.content,
        )
    }

    @Test
    fun detachedChatRejectsGatewayEnvelopeFailure() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("""{"ok":false,"payload":{"text":"must not accept"}}""")

        val accepted = f.client.sendDetachedChat("message")

        assertFalse(accepted)
    }

    @Test
    fun detachedChatRejectsHttpFailureEvenWhenBodySaysOk() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson(
            body = """{"ok":true,"payload":{"text":"stale"}}""",
            status = HttpStatusCode.BadGateway,
        )

        val accepted = f.client.sendDetachedChat("message")

        assertFalse(accepted)
    }

    @Test
    fun detachedChatRejectsMalformedResponseWithoutThrowing() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("not-json")

        val accepted = f.client.sendDetachedChat("message")

        assertFalse(accepted)
    }

    @Test
    fun detachedChatSkipsBlankMessageAndMissingToken() = runTest {
        val connected = gatewayClientFixture()
        val disconnected = gatewayClientFixture(token = "")

        assertFalse(connected.client.sendDetachedChat("   "))
        assertFalse(disconnected.client.sendDetachedChat("message"))

        assertTrue(connected.transport.requests.isEmpty())
        assertTrue(disconnected.transport.requests.isEmpty())
    }

    @Test
    fun detachedChatPropagatesCancellation() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel detached"))

        val failure = assertFailsWith<CancellationException> {
            f.client.sendDetachedChat("message")
        }

        assertEquals("cancel detached", failure.message)
    }

    @Test
    fun detachedChatDropsResponseAfterCredentialSwitch() = runTest {
        val f = gatewayClientFixture(token = "old-token", url = "https://old.example")
        val gate = CompletableDeferred<Unit>()
        f.transport.enqueueJson(
            body = """{"ok":true,"payload":{"text":"old account"}}""",
            gate = gate,
        )
        val pending = async { f.client.sendDetachedChat("message") }
        runCurrent()

        f.client.onCredentialsChanged("https://new.example", "new-token")
        gate.complete(Unit)

        assertFalse(pending.await())
        val request = f.transport.singleRequest()
        assertEquals("https://old.example/api/v1/miniapp/rpc", request.url)
        assertEquals("old-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
    }
}
