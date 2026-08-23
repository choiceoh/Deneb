package ai.deneb.deneb

import ai.deneb.data.UiSubmission
import ai.deneb.ui.chat.History
import io.ktor.http.HttpStatusCode
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

@OptIn(ExperimentalCoroutinesApi::class)
class GatewayAskStreamingStateMachineTest {

    private val json = Json { encodeDefaults = true }

    @BeforeTest
    fun installMainDispatcher() {
        Dispatchers.setMain(UnconfinedTestDispatcher())
    }

    @AfterTest
    fun resetMainDispatcher() {
        Dispatchers.resetMain()
    }

    private fun frame(event: String, data: String): String = "event: $event\ndata: $data\n\n"

    private fun delta(text: String): String = frame(
        "delta",
        buildJsonObject { put("delta", text) }.toString(),
    )

    private fun done(
        text: String,
        model: String = "",
        fellBack: Boolean = false,
    ): String = frame(
        "done",
        buildJsonObject {
            put("text", text)
            put("model", model)
            put("fellBack", fellBack)
        }.toString(),
    )

    private fun error(message: String): String = frame(
        "error",
        buildJsonObject { put("error", message) }.toString(),
    )

    // A stream that ends without its terminal frame — what a dropped mobile socket
    // over a still-running server turn looks like. Distinct from an `event: error`
    // frame, which is the gateway telling us the turn itself failed: that one has
    // no detached run to recover, so only THIS shape may trigger recovery.
    private fun droppedStream(partial: String = ""): String = partial

    private fun tool(
        state: String,
        name: String = "mail_search",
        id: String = "tool-1",
        detail: String = "",
        isError: Boolean = false,
    ): String = frame(
        "tool",
        json.encodeToString(
            ToolEvent(
                state = state,
                tool = name,
                toolUseId = id,
                detail = detail,
                isError = isError,
            ),
        ),
    )

    private fun thinking(preview: String): String = frame(
        "thinking",
        buildJsonObject { put("preview", preview) }.toString(),
    )

    private fun notArrivedTranscript(): String = """{"messages":[{"role":"user","content":"different"}]}"""

    private fun answeredTranscript(question: String, answer: String): String = """{"messages":[
            {"role":"user","content":${json.encodeToString(question)}},
            {"role":"assistant","content":${json.encodeToString(answer)}}
        ]}
    """.trimIndent()

    private fun blockingReply(
        text: String,
        model: String = "",
        fellBack: Boolean = false,
        ok: Boolean = true,
    ): String = buildJsonObject {
        put("ok", ok)
        put(
            "payload",
            buildJsonObject {
                put("text", text)
                put("model", model)
                put("fellBack", fellBack)
            },
        )
    }.toString()

    private fun List<History>.users() = filter { it.role == History.Role.USER }

    private fun List<History>.assistants() = filter { it.role == History.Role.ASSISTANT }

    @Test
    fun nullQuestionWithoutUiSubmissionIsSuccessfulNoOp() = runTest {
        val f = gatewayClientFixture()

        val result = f.client.ask(null, emptyList(), null)

        assertTrue(result)
        assertTrue(f.client.chatHistory.value.isEmpty())
        assertTrue(f.transport.requests.isEmpty())
        assertFalse(f.client.askActive)
    }

    @Test
    fun whitespaceQuestionWithoutUiSubmissionIsSuccessfulNoOp() = runTest {
        val f = gatewayClientFixture()

        val result = f.client.ask(" \n\t ", emptyList(), null)

        assertTrue(result)
        assertTrue(f.client.chatHistory.value.isEmpty())
        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun questionIsTrimmedForVisibleAndSentText() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(delta("answer") + done("answer"))

        f.client.ask("  question  ", emptyList(), null)

        assertEquals("question", f.client.chatHistory.value.users().single().content)
        val body = f.transport.singleRequest().jsonBody.orEmpty()
        assertEquals("question", body["message"]?.jsonPrimitive?.content)
    }

    @Test
    fun streamingRequestUsesCurrentSessionAndDedicatedEndpoint() = runTest {
        val f = gatewayClientFixture()
        f.client.switchSession("client:main:topic")
        f.transport.enqueueSse(done("answer"))

        f.client.ask("question", emptyList(), null)

        val request = f.transport.singleRequest()
        assertEquals("https://gateway.example/api/v1/miniapp/chat/stream", request.url)
        assertEquals("client:main:topic", request.jsonBody?.get("sessionKey")?.jsonPrimitive?.content)
        assertEquals("test-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
        assertEquals("text/event-stream", request.header("Accept"))
    }

    @Test
    fun missingTokenProducesVisibleFailureWithoutNetwork() = runTest {
        val f = gatewayClientFixture(token = "")

        val result = f.client.ask("question", emptyList(), null)

        assertFalse(result)
        assertTrue(f.transport.requests.isEmpty())
        assertEquals("question", f.client.chatHistory.value.users().single().content)
        assertTrue(f.client.chatHistory.value.assistants().single().content.contains("토큰"))
        assertFalse(f.client.askActive)
    }

    @Test
    fun deltaAndDoneProduceOneCanonicalAssistantBubble() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(delta("Hello ") + delta("world") + done("Hello world"))

        val result = f.client.ask("question", emptyList(), null)

        assertTrue(result)
        assertEquals(listOf(History.Role.USER, History.Role.ASSISTANT), f.client.chatHistory.value.map { it.role })
        assertEquals("Hello world", f.client.chatHistory.value.assistants().single().content)
        assertFalse(f.client.askActive)
    }

    @Test
    fun terminalCanonicalTextWinsWhenSimilarLength() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(delta("draft answer") + done("canonical answer"))

        f.client.ask("question", emptyList(), null)

        assertEquals("canonical answer", f.client.chatHistory.value.assistants().single().content)
    }

    @Test
    fun meaningfullyLongerStreamedBodyWinsOverShortWrapUp() = runTest {
        val f = gatewayClientFixture()
        val streamed = "Detailed body: " + "x".repeat(120)
        f.transport.enqueueSse(delta(streamed) + done("Done."))

        f.client.ask("question", emptyList(), null)

        assertEquals(streamed, f.client.chatHistory.value.assistants().single().content)
    }

    @Test
    fun streamedTextIsUsedWhenDoneTextIsBlank() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(delta("stream only") + done(""))

        f.client.ask("question", emptyList(), null)

        assertEquals("stream only", f.client.chatHistory.value.assistants().single().content)
    }

    @Test
    fun emptySuccessfulStreamBecomesExplicitEmptyResponseWarning() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(done(""))

        val result = f.client.ask("question", emptyList(), null)

        assertTrue(result)
        assertEquals("⚠️ 빈 응답", f.client.chatHistory.value.assistants().single().content)
    }

    @Test
    fun fallbackModelBadgeIsAttachedOnlyWhenServerSaysFallback() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(done("answer", model = "fallback/model", fellBack = true))

        f.client.ask("question", emptyList(), null)

        assertEquals("fallback/model", f.client.chatHistory.value.assistants().single().fallbackServiceName)
    }

    @Test
    fun normalModelNeverGetsFallbackBadge() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(done("answer", model = "main/model", fellBack = false))

        f.client.ask("question", emptyList(), null)

        assertNull(f.client.chatHistory.value.assistants().single().fallbackServiceName)
    }

    @Test
    fun blankFallbackModelNeverGetsEmptyBadge() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(done("answer", model = "", fellBack = true))

        f.client.ask("question", emptyList(), null)

        assertNull(f.client.chatHistory.value.assistants().single().fallbackServiceName)
    }

    @Test
    fun keepaliveCommentsAndUnknownEventsAreIgnored() = runTest {
        val f = gatewayClientFixture()
        val stream = ": keepalive\n\n" + frame("future", "{\"value\":1}") + done("answer")
        f.transport.enqueueSse(stream)

        assertTrue(f.client.ask("question", emptyList(), null))

        assertEquals("answer", f.client.chatHistory.value.assistants().single().content)
    }

    @Test
    fun malformedDeltaIsIgnoredWithoutFailingTurn() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(frame("delta", "not-json") + done("canonical"))

        val result = f.client.ask("question", emptyList(), null)

        assertTrue(result)
        assertEquals("canonical", f.client.chatHistory.value.assistants().single().content)
    }

    @Test
    fun emptyDeltaIsIgnoredWithoutCreatingExtraRows() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(delta("") + done("answer"))

        f.client.ask("question", emptyList(), null)

        assertEquals(2, f.client.chatHistory.value.size)
        assertEquals("answer", f.client.chatHistory.value.assistants().single().content)
    }

    @Test
    fun thinkingAndToolRowsAreClearedAtSuccessfulTurnEnd() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(
            thinking("planning") +
                tool("started", detail = "inbox") +
                tool("completed") +
                done("answer"),
        )

        f.client.ask("question", emptyList(), null)

        assertFalse(f.client.chatHistory.value.any { it.role == History.Role.TOOL_EXECUTING })
        assertEquals(2, f.client.chatHistory.value.size)
    }

    @Test
    fun matchedToolLifecycleLeavesCompactFootprint() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(tool("started") + tool("completed") + done("answer"))

        f.client.ask("question", emptyList(), null)

        assertEquals(ToolStatusLabels.trailLabel("mail_search"), f.client.chatHistory.value.assistants().single().toolFootprint)
    }

    @Test
    fun failedToolLifecycleLeavesWarningFootprint() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(
            tool("started", name = "web_search") +
                tool("completed", name = "web_search", isError = true) +
                done("answer"),
        )

        f.client.ask("question", emptyList(), null)

        assertTrue(f.client.chatHistory.value.assistants().single().toolFootprint.orEmpty().endsWith("⚠"))
    }

    @Test
    fun malformedAndBlankToolFramesDoNotPolluteFootprint() = runTest {
        val f = gatewayClientFixture()
        val malformed = frame("tool", "not-json")
        val blank = frame("tool", "{}")
        f.transport.enqueueSse(malformed + blank + done("answer"))

        f.client.ask("question", emptyList(), null)

        assertNull(f.client.chatHistory.value.assistants().single().toolFootprint)
    }

    @Test
    fun uiSubmissionSendsStructuredCallbackButShowsFriendlySourceText() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(done("accepted"))
        val submission = UiSubmission(
            sourceContent = "Approve this deal?",
            pressedEvent = "approve",
            values = linkedMapOf("deal" to "D-42", "risk" to "low"),
        )

        f.client.ask("Friendly approval", emptyList(), submission)

        assertEquals("Friendly approval", f.client.chatHistory.value.users().single().content)
        val sent = f.transport.singleRequest().jsonBody?.get("message")?.jsonPrimitive?.content.orEmpty()
        assertEquals("[deneb-ui] event=approve values={deal=D-42, risk=low}", sent)
    }

    @Test
    fun uiSubmissionWithoutDisplayQuestionDoesNotAddEmptyUserBubble() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(done("accepted"))
        val submission = UiSubmission(sourceContent = "card", pressedEvent = "submit")

        f.client.ask(null, emptyList(), submission)

        assertTrue(f.client.chatHistory.value.users().isEmpty())
        assertEquals(1, f.client.chatHistory.value.assistants().size)
        assertEquals("[deneb-ui] event=submit", f.transport.singleRequest().jsonBody?.get("message")?.jsonPrimitive?.content)
    }

    @Test
    fun streamErrorRecoversCanonicalTranscriptWithoutBlockingResend() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(droppedStream())
        f.transport.enqueueRpc(answeredTranscript("question", "canonical"))
        f.transport.enqueueRpc(answeredTranscript("question", "canonical"))

        val result = f.client.ask("question", emptyList(), null)

        assertTrue(result)
        assertEquals(listOf("question", "canonical"), f.client.chatHistory.value.map { it.content })
        assertEquals(
            listOf(null, "miniapp.sessions.transcript", "miniapp.sessions.transcript"),
            f.transport.requestMethods(),
        )
    }

    @Test
    fun emptyStreamFailureResendsOnlyAfterTwoNotArrivedPolls() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(droppedStream())
        f.transport.enqueueRpc(notArrivedTranscript())
        f.transport.enqueueRpc(notArrivedTranscript())
        f.transport.enqueueJson(blockingReply("blocking answer"))

        val result = f.client.ask("question", emptyList(), null)

        assertTrue(result)
        assertEquals("blocking answer", f.client.chatHistory.value.assistants().single().content)
        assertEquals(4, f.transport.requests.size)
        val resend = f.transport.lastRequest().jsonBody.orEmpty()
        assertEquals("miniapp.chat.send", resend["method"]?.jsonPrimitive?.content)
    }

    @Test
    fun partialStreamFailureNeverResendsAndReportsFailure() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(delta("partial answer") + droppedStream())
        f.transport.enqueueRpc(notArrivedTranscript())
        f.transport.enqueueRpc(notArrivedTranscript())

        val result = f.client.ask("question", emptyList(), null)

        assertFalse(result)
        assertEquals("partial answer", f.client.chatHistory.value.assistants().single().content)
        assertEquals(3, f.transport.requests.size)
        assertFalse(f.transport.requests.any { it.rpcMethod == "miniapp.chat.send" })
    }

    @Test
    fun gatewayErrorFrameShowsItsMessageWithoutRecoveringOrResending() {
        // `event: error` is the gateway saying the turn failed — the turn does not
        // exist to be recovered, and its message IS the outcome. It used to be
        // swallowed as a transport drop: the text was discarded and the user got a
        // recovery failure (or a duplicate send) instead of the reason.
        runTest {
            val f = gatewayClientFixture()
            f.transport.enqueueSse(error("모델 사용량을 초과했습니다"))

            val result = f.client.ask("question", emptyList(), null)

            assertFalse(result)
            assertEquals("⚠️ 모델 사용량을 초과했습니다", f.client.chatHistory.value.assistants().single().content)
            // One request: the stream. No transcript polls, no blocking resend.
            assertEquals(1, f.transport.requests.size)
        }
    }

    @Test
    fun connectFailureFailsImmediatelyInsteadOfPollingForATurnThatNeverStarted() {
        // Offline. The transcript poll recovery depends on is guaranteed to fail the
        // same way, so it used to sit on "답변 이어받는 중…" for the full 90s budget
        // and then report that it could not resume — instead of "no connection".
        runTest {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(IllegalStateException("connect refused"))

            val result = f.client.ask("question", emptyList(), null)

            assertFalse(result)
            val bubble = f.client.chatHistory.value.assistants().single().content
            assertTrue(bubble.contains("연결하지 못했습니다"), bubble)
            assertEquals(1, f.transport.requests.size)
        }
    }

    @Test
    fun rejectedTokenFailsImmediatelyAndPointsAtSettings() {
        runTest {
            val f = gatewayClientFixture()
            f.transport.enqueueSse("unauthorized", status = HttpStatusCode.Unauthorized)

            val result = f.client.ask("question", emptyList(), null)

            assertFalse(result)
            val bubble = f.client.chatHistory.value.assistants().single().content
            assertTrue(bubble.contains("토큰"), bubble)
            assertEquals(1, f.transport.requests.size)
        }
    }

    @Test
    fun streamHttpFailureUsesSafeBlockingFallback() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse("unavailable", status = HttpStatusCode.NotFound)
        f.transport.enqueueRpc(notArrivedTranscript())
        f.transport.enqueueRpc(notArrivedTranscript())
        f.transport.enqueueJson(blockingReply("fallback"))

        val result = f.client.ask("question", emptyList(), null)

        assertTrue(result)
        assertEquals("fallback", f.client.chatHistory.value.assistants().single().content)
    }

    @Test
    fun blockingFallbackRejectsSuccessfulLookingBodyOnHttpError() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(droppedStream())
        f.transport.enqueueRpc(notArrivedTranscript())
        f.transport.enqueueRpc(notArrivedTranscript())
        f.transport.enqueueJson(blockingReply("must not accept"), status = HttpStatusCode.InternalServerError)

        val result = f.client.ask("question", emptyList(), null)

        assertFalse(result)
        val bubble = f.client.chatHistory.value.assistants().single().content
        // The status still identifies the failure, but in Korean — the raw
        // "chat HTTP 500" is a log line, not something to show the reader.
        assertTrue(bubble.contains("500"), bubble)
        assertFalse(bubble.contains("must not accept"), bubble)
        assertFalse(bubble.contains("HTTP"), bubble)
    }

    @Test
    fun blockingFallbackGatewayNotOkBecomesFailureBubble() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(droppedStream())
        f.transport.enqueueRpc(notArrivedTranscript())
        f.transport.enqueueRpc(notArrivedTranscript())
        f.transport.enqueueJson(blockingReply("ignored", ok = false))

        val result = f.client.ask("question", emptyList(), null)

        assertFalse(result)
        assertEquals("⚠️ 게이트웨이 오류", f.client.chatHistory.value.assistants().single().content)
    }

    @Test
    fun blockingFallbackTransportFailureNamesTheConnection() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(droppedStream())
        f.transport.enqueueRpc(notArrivedTranscript())
        f.transport.enqueueRpc(notArrivedTranscript())
        f.transport.enqueueFailure(IllegalStateException("offline"))

        val result = f.client.ask("question", emptyList(), null)

        assertFalse(result)
        // No response ever arrived, so the bubble names the connection rather than
        // echoing the exception's own English text.
        val bubble = f.client.chatHistory.value.assistants().single().content
        assertTrue(bubble.contains("연결하지 못했습니다"), bubble)
        assertFalse(bubble.contains("offline"), bubble)
    }

    @Test
    fun cancellationWhileWaitingForStreamDropsEmptyPlaceholder() = runTest {
        val f = gatewayClientFixture()
        val gate = CompletableDeferred<Unit>()
        f.transport.enqueueSse(done("late"), gate = gate)
        val asking = async { f.client.ask("question", emptyList(), null) }
        f.transport.awaitRequestCount(1)

        asking.cancelAndJoin()

        assertEquals(listOf("question"), f.client.chatHistory.value.map { it.content })
        assertFalse(f.client.askActive)
    }

    @Test
    fun blockingFallbackCancellationPropagatesInsteadOfBecomingErrorBubble() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(droppedStream())
        f.transport.enqueueRpc(notArrivedTranscript())
        f.transport.enqueueRpc(notArrivedTranscript())
        f.transport.enqueueFailure(CancellationException("cancel resend"))

        val failure = assertFailsWith<CancellationException> {
            f.client.ask("question", emptyList(), null)
        }

        assertEquals("cancel resend", failure.message)
        assertEquals(listOf("question"), f.client.chatHistory.value.map { it.content })
        assertFalse(f.client.askActive)
    }

    @Test
    fun progressRowsAreClearedWhenStreamErrorsAndRecoveryFails() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(tool("started") + error("reset"))
        f.transport.enqueueRpc(notArrivedTranscript())
        f.transport.enqueueRpc(notArrivedTranscript())
        f.transport.enqueueJson(blockingReply("fallback"))

        f.client.ask("question", emptyList(), null)

        assertFalse(f.client.chatHistory.value.any { it.role == History.Role.TOOL_EXECUTING })
    }

    @Test
    fun askAppendsToExistingHistoryWithoutReplacingEarlierRows() = runTest {
        val f = gatewayClientFixture()
        val earlier = History(id = "earlier", role = History.Role.ASSISTANT, content = "earlier answer")
        f.client._chatHistory.value = listOf(earlier)
        f.transport.enqueueSse(done("new answer"))

        f.client.ask("new question", emptyList(), null)

        assertEquals(earlier, f.client.chatHistory.value.first())
        assertEquals(listOf("earlier answer", "new question", "new answer"), f.client.chatHistory.value.map { it.content })
    }

    @Test
    fun eachAskUsesUniqueOptimisticAssistantIdentity() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueSse(done("first"))
        f.transport.enqueueSse(done("second"))

        f.client.ask("one", emptyList(), null)
        f.client.ask("two", emptyList(), null)

        val ids = f.client.chatHistory.value.assistants().map { it.id }
        assertEquals(2, ids.distinct().size)
    }
}
