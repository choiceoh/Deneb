package ai.deneb.deneb

import ai.deneb.deneb.generated.SessionRowOut
import ai.deneb.ui.chat.History
import ai.deneb.ui.chat.WorkFeedItem
import io.ktor.http.HttpStatusCode
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNotEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

class SessionTranscriptStateMachineTest {

    private val json = Json { encodeDefaults = true }

    private fun sessionsPayload(vararg sessions: SessionRowOut, total: Int? = null): String = buildJsonObject {
        put(
            "sessions",
            buildJsonArray {
                sessions.forEach { add(json.encodeToJsonElement(SessionRowOut.serializer(), it)) }
            },
        )
        if (total != null) put("total", total)
    }.toString()

    private fun transcriptPayload(vararg messages: String): String = """{"messages":[${messages.joinToString(",")}] }"""

    private fun message(
        role: String,
        content: String,
        id: String = "wire-id",
        timestampMs: Long = 0,
        attachments: String = "[]",
    ): String = """{
        "id":${json.encodeToString(id)},
        "role":${json.encodeToString(role)},
        "content":${json.encodeToString(content)},
        "attachments":$attachments,
        "timestampMs":$timestampMs
    }
    """.trimIndent()

    private fun answered(sent: String = "question", answer: String = "answer"): String = transcriptPayload(
        message("user", sent, id = "u"),
        message("assistant", answer, id = "a"),
    )

    private fun GatewayHttpHarness.enqueueTranscript(payload: String) {
        enqueueRpc(payload)
    }

    @Test
    fun failedRecentSessionsRpcReturnsNullInsteadOfSyntheticHome() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("malformed")

        val result = f.client.fetchRecentSessions()

        assertNull(result)
    }

    @Test
    fun emptyRecentSessionsCreatesSingleHomeConversation() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(sessionsPayload())

        val result = f.client.fetchRecentSessions()?.conversations.orEmpty()

        assertEquals(1, result.size)
        assertEquals("client:main", result.single().id)
        assertEquals("업무", result.single().title)
        assertTrue(result.single().updatedAt > 0)
    }

    @Test
    fun existingHomeSessionIsNotDuplicated() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            sessionsPayload(SessionRowOut(key = "client:main", updatedAtMs = 9)),
        )

        val result = f.client.fetchRecentSessions()?.conversations.orEmpty()

        assertEquals(listOf("client:main"), result.map { it.id })
        assertEquals(9, result.single().updatedAt)
    }

    @Test
    fun blankSessionKeysAreDroppedBeforeHomeFallback() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            sessionsPayload(
                SessionRowOut(key = "", label = "secret", updatedAtMs = 10),
                SessionRowOut(key = "client:main:valid", updatedAtMs = 20),
            ),
        )

        val result = f.client.fetchRecentSessions()?.conversations.orEmpty()

        assertEquals(listOf("client:main", "client:main:valid"), result.map { it.id })
        assertFalse(result.any { it.title == "secret" })
    }

    @Test
    fun homeIsMovedToFrontWithoutReorderingOtherSessions() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            sessionsPayload(
                SessionRowOut(key = "client:main:first", updatedAtMs = 30),
                SessionRowOut(key = "client:main", updatedAtMs = 20),
                SessionRowOut(key = "system:heartbeat", updatedAtMs = 10),
            ),
        )

        val result = f.client.fetchRecentSessions()?.conversations.orEmpty()

        assertEquals(
            listOf("client:main", "client:main:first", "system:heartbeat"),
            result.map { it.id },
        )
    }

    @Test
    fun explicitServerLabelWinsForEverySessionKind() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            sessionsPayload(
                SessionRowOut(key = "cron:morning:1", label = "Custom cron", updatedAtMs = 1),
                SessionRowOut(key = "chat:abc", label = "Custom chat", updatedAtMs = 2),
            ),
        )

        val result = f.client.fetchRecentSessions()?.conversations.orEmpty().associateBy { it.id }

        assertEquals("Custom cron", result["cron:morning:1"]?.title)
        assertEquals("Custom chat", result["chat:abc"]?.title)
    }

    @Test
    fun unlabeledHomeKeepsBusinessTitle() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(sessionsPayload(SessionRowOut(key = "client:main", updatedAtMs = 1)))

        val result = f.client.fetchRecentSessions()?.conversations.orEmpty()

        assertEquals("업무", result.single().title)
    }

    @Test
    fun legacyChatSessionUsesReadableShortId() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            sessionsPayload(SessionRowOut(key = "chat:123456789abcdef", updatedAtMs = 1)),
        )

        val title = f.client.fetchRecentSessions()?.conversations.orEmpty().last().title

        assertEquals("대화 · 12345678", title)
    }

    @Test
    fun cronEmailAnalysisGetsKoreanLabel() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(sessionsPayload(SessionRowOut(key = "cron:email-analysis-full:1")))

        val title = f.client.fetchRecentSessions()?.conversations.orEmpty().last().title

        assertEquals("예약 · 메일 분석", title)
    }

    @Test
    fun cronKnownJobsGetStableKoreanLabels() = runTest {
        val cases = mapOf(
            "cron:morning-letter:1" to "예약 · 모닝레터",
            "cron:weekly-report:1" to "예약 · 주간 보고",
            "cron:evening-letter:1" to "예약 · 저녁 정리",
            "cron:heartbeat:1" to "예약 · 하트비트",
        )
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            sessionsPayload(*cases.keys.map { SessionRowOut(key = it) }.toTypedArray()),
        )

        val result = f.client.fetchRecentSessions()?.conversations.orEmpty().associateBy { it.id }

        cases.forEach { (key, title) -> assertEquals(title, result[key]?.title) }
    }

    @Test
    fun unknownCronJobIsDehyphenatedInsteadOfOpaqueTimestamp() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(sessionsPayload(SessionRowOut(key = "cron:deal-watch-daily:178")))

        val title = f.client.fetchRecentSessions()?.conversations.orEmpty().last().title

        assertEquals("예약 · deal watch daily", title)
    }

    @Test
    fun systemAndBootSessionsGetReadableTitles() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            sessionsPayload(
                SessionRowOut(key = "system:heartbeat"),
                SessionRowOut(key = "boot:178"),
            ),
        )

        val result = f.client.fetchRecentSessions()?.conversations.orEmpty().associateBy { it.id }

        assertEquals("시스템 · 하트비트", result["system:heartbeat"]?.title)
        assertEquals("시스템 부팅", result["boot:178"]?.title)
    }

    @Test
    fun ordinaryClientSessionUsesEightCharacterSuffix() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            sessionsPayload(SessionRowOut(key = "client:main:abcdefghijk", updatedAtMs = 1)),
        )

        val title = f.client.fetchRecentSessions()?.conversations.orEmpty().last().title

        assertEquals("내 대화 · abcdefgh", title)
    }

    @Test
    fun positiveStartedAtBecomesConversationCreatedAt() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            sessionsPayload(SessionRowOut(key = "client:main:one", startedAtMs = 10, updatedAtMs = 20)),
        )

        val conversation = f.client.fetchRecentSessions()?.conversations.orEmpty().last()

        assertEquals(10, conversation.createdAt)
        assertEquals(20, conversation.updatedAt)
    }

    @Test
    fun nonPositiveStartedAtFallsBackToUpdatedAt() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            sessionsPayload(
                SessionRowOut(key = "client:main:zero", startedAtMs = 0, updatedAtMs = 20),
                SessionRowOut(key = "client:main:negative", startedAtMs = -1, updatedAtMs = 30),
            ),
        )

        val result = f.client.fetchRecentSessions()?.conversations.orEmpty().associateBy { it.id }

        assertEquals(20, result["client:main:zero"]?.createdAt)
        assertEquals(30, result["client:main:negative"]?.createdAt)
    }

    @Test
    fun workFeedSideSessionUsesMatchingCardTitle() = runTest {
        val f = gatewayClientFixture()
        val item = WorkFeedItem(id = "Deal 42", title = "계약 검토", status = "unread")
        f.client._denebWorkFeed.value = listOf(item)
        val key = f.client.workItemSessionKey(item.id)
        f.transport.enqueueRpc(sessionsPayload(SessionRowOut(key = key)))

        val title = f.client.fetchRecentSessions()?.conversations.orEmpty().last().title

        assertEquals("업무 · 계약 검토", title)
    }

    @Test
    fun missingWorkFeedCardUsesGenericMemoTitle() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(sessionsPayload(SessionRowOut(key = "client:main:wf-gone")))

        val title = f.client.fetchRecentSessions()?.conversations.orEmpty().last().title

        assertEquals("업무 메모", title)
    }

    @Test
    fun workFeedSessionTitleIsTrimmedAndCapped() = runTest {
        val f = gatewayClientFixture()
        val rawTitle = "  " + "가".repeat(60) + "  "
        val item = WorkFeedItem(id = "long", title = rawTitle)
        f.client._denebWorkFeed.value = listOf(item)
        f.transport.enqueueRpc(sessionsPayload(SessionRowOut(key = f.client.workItemSessionKey(item.id))))

        val title = f.client.fetchRecentSessions()?.conversations.orEmpty().last().title

        assertEquals("업무 · ${"가".repeat(40)}", title)
    }

    @Test
    fun recentSessionsRequestUsesHardLimitFifty() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(sessionsPayload())

        f.client.fetchRecentSessions()

        val params = f.transport.singleRequest().requireRpc("miniapp.sessions.recent")
        assertEquals(50, params["limit"]?.jsonPrimitive?.content?.toInt())
    }

    @Test
    fun fetchTranscriptSendsSessionKeyAndHardLimit() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueTranscript(transcriptPayload())

        f.client.fetchTranscript("client:main:one")

        val params = f.transport.singleRequest().requireRpc("miniapp.sessions.transcript")
        assertEquals("client:main:one", params["sessionKey"]?.jsonPrimitive?.content)
        assertEquals(200, params["limit"]?.jsonPrimitive?.content?.toInt())
    }

    @Test
    fun fetchTranscriptMapsRolesCaseInsensitively() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueTranscript(
            transcriptPayload(
                message("USER", "question"),
                message("Assistant", "answer"),
            ),
        )

        val result = f.client.fetchTranscript("client:main").orEmpty()

        assertEquals(listOf(History.Role.USER, History.Role.ASSISTANT), result.map { it.role })
        assertEquals(listOf("question", "answer"), result.map { it.content })
    }

    @Test
    fun fetchTranscriptDropsUnknownAndBlankRoles() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueTranscript(
            transcriptPayload(
                message("tool", "internal"),
                message("", "missing"),
                message("assistant", "visible"),
            ),
        )

        val result = f.client.fetchTranscript("client:main").orEmpty()

        assertEquals(listOf("visible"), result.map { it.content })
    }

    @Test
    fun fetchTranscriptDropsBlankTextWithoutAttachments() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueTranscript(
            transcriptPayload(
                message("user", "   "),
                message("assistant", "answer"),
            ),
        )

        val result = f.client.fetchTranscript("client:main").orEmpty()

        assertEquals(1, result.size)
        assertEquals("answer", result.single().content)
    }

    @Test
    fun fetchTranscriptKeepsImageOnlyMessageWithValidAttachment() = runTest {
        val f = gatewayClientFixture()
        val attachments = """[{"mimeType":"image/png","data":"YWJj","name":"chart.png"}]"""
        f.transport.enqueueTranscript(transcriptPayload(message("assistant", "", attachments = attachments)))

        val result = f.client.fetchTranscript("client:main").orEmpty()

        assertEquals(1, result.size)
        assertEquals("", result.single().content)
        assertEquals("image/png", result.single().attachments.single().mimeType)
        assertEquals("chart.png", result.single().attachments.single().fileName)
    }

    @Test
    fun invalidAttachmentsAreFilteredIndividually() = runTest {
        val f = gatewayClientFixture()
        val attachments = """[
            {"mimeType":"","data":"abc","name":"missing-mime"},
            {"mimeType":"text/plain","data":"","name":"missing-data"},
            {"mimeType":"text/plain","data":"dmFsaWQ=","name":"ok.txt"}
        ]
        """.trimIndent()
        f.transport.enqueueTranscript(transcriptPayload(message("assistant", "body", attachments = attachments)))

        val result = f.client.fetchTranscript("client:main").orEmpty()

        assertEquals(1, result.single().attachments.size)
        assertEquals("ok.txt", result.single().attachments.single().fileName)
    }

    @Test
    fun blankAttachmentNameMapsToNullFilename() = runTest {
        val f = gatewayClientFixture()
        val attachments = """[{"mimeType":"text/plain","data":"YWJj","name":""}]"""
        f.transport.enqueueTranscript(transcriptPayload(message("assistant", "body", attachments = attachments)))

        val result = f.client.fetchTranscript("client:main").orEmpty()

        assertNull(result.single().attachments.single().fileName)
    }

    @Test
    fun transcriptTimestampIsPreservedExactly() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueTranscript(transcriptPayload(message("assistant", "answer", timestampMs = Long.MAX_VALUE)))

        val result = f.client.fetchTranscript("client:main").orEmpty()

        assertEquals(Long.MAX_VALUE, result.single().timestampMs)
    }

    @Test
    fun transcriptTransportFailureReturnsNullNotEmpty() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("bad", status = HttpStatusCode.ServiceUnavailable)

        val result = f.client.fetchTranscript("client:main")

        assertNull(result)
    }

    @Test
    fun transcriptCancellationPropagates() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel transcript"))

        val failure = assertFailsWith<CancellationException> {
            f.client.fetchTranscript("client:main")
        }

        assertEquals("cancel transcript", failure.message)
    }

    @Test
    fun authoritativeTranscriptInstallsViewAndCache() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueTranscript(answered())

        f.client.loadTranscriptGuarded("client:main")

        assertEquals(listOf("question", "answer"), f.client.chatHistory.value.map { it.content })
        assertEquals(listOf("question", "answer"), f.client.loadCachedTranscript("client:main")?.map { it.content })
    }

    @Test
    fun unusableCachedTranscriptIsRemovedAfterFirstMiss() {
        val f = gatewayClientFixture()
        f.settings.putCachedTranscript("client:main", "not-json")

        assertNull(f.client.loadCachedTranscript("client:main"))
        assertNull(f.settings.getCachedTranscript("client:main"))
    }

    @Test
    fun cachedTranscriptFromAnotherOwnerIsRemovedWithoutRendering() {
        val prior = gatewayClientFixture(token = "prior-token")
        prior.client.storeCachedTranscript(
            "client:main",
            listOf(History(role = History.Role.ASSISTANT, content = "prior private answer")),
        )
        val current = gatewayClientFixture(token = "current-token", settings = prior.settings)

        assertNull(current.client.loadCachedTranscript("client:main"))
        assertNull(current.settings.getCachedTranscript("client:main"))
    }

    @Test
    fun failedInPlaceReloadKeepsLiveView() = runTest {
        val f = gatewayClientFixture()
        val live = History(id = "live", role = History.Role.ASSISTANT, content = "live answer")
        f.client._chatHistory.value = listOf(live)
        f.transport.enqueueJson("bad")

        f.client.loadTranscriptGuarded("client:main")

        assertEquals(listOf(live), f.client.chatHistory.value)
    }

    @Test
    fun replacingConversationClearsPriorRowsWhenNoCacheAndFetchFails() = runTest {
        val f = gatewayClientFixture()
        f.client._chatHistory.value = listOf(History(role = History.Role.ASSISTANT, content = "prior"))
        f.transport.enqueueJson("bad")

        f.client.loadTranscriptGuarded("client:main:new", replacing = true)

        assertTrue(f.client.chatHistory.value.isEmpty())
    }

    @Test
    fun replacingConversationRendersItsCacheBeforeNetworkCompletes() = runTest {
        val f = gatewayClientFixture()
        val cached = listOf(History(role = History.Role.ASSISTANT, content = "cached target"))
        f.client.storeCachedTranscript("client:main:new", cached)
        f.client._chatHistory.value = listOf(History(role = History.Role.ASSISTANT, content = "prior"))
        val gate = CompletableDeferred<Unit>()
        f.transport.enqueueRpc(answered(answer = "network target"), gate = gate)
        val loading = async { f.client.loadTranscriptGuarded("client:main:new", replacing = true) }
        f.transport.awaitRequestCount(1)

        assertEquals(listOf("cached target"), f.client.chatHistory.value.map { it.content })
        gate.complete(Unit)
        loading.await()
        assertEquals(listOf("question", "network target"), f.client.chatHistory.value.map { it.content })
    }

    @Test
    fun inPlaceReloadUsesCacheOnlyWhenViewIsEmpty() = runTest {
        val f = gatewayClientFixture()
        f.client.storeCachedTranscript(
            "client:main",
            listOf(History(role = History.Role.ASSISTANT, content = "cached")),
        )
        val gate = CompletableDeferred<Unit>()
        f.transport.enqueueRpc(answered(answer = "network"), gate = gate)
        val loading = async { f.client.loadTranscriptGuarded("client:main") }
        f.transport.awaitRequestCount(1)

        assertEquals(listOf("cached"), f.client.chatHistory.value.map { it.content })
        gate.complete(Unit)
        loading.await()
    }

    @Test
    fun inPlaceReloadNeverFlashesCacheOverLiveView() = runTest {
        val f = gatewayClientFixture()
        f.client.storeCachedTranscript(
            "client:main",
            listOf(History(role = History.Role.ASSISTANT, content = "cached")),
        )
        f.client._chatHistory.value = listOf(History(role = History.Role.ASSISTANT, content = "live"))
        val gate = CompletableDeferred<Unit>()
        f.transport.enqueueRpc(answered(answer = "network"), gate = gate)
        val loading = async { f.client.loadTranscriptGuarded("client:main") }
        f.transport.awaitRequestCount(1)

        assertEquals(listOf("live"), f.client.chatHistory.value.map { it.content })
        gate.complete(Unit)
        loading.await()
        assertEquals(listOf("question", "network"), f.client.chatHistory.value.map { it.content })
    }

    @Test
    fun optimisticHistoryEpochChangeFencesLateTranscript() = runTest {
        val f = gatewayClientFixture()
        val gate = CompletableDeferred<Unit>()
        f.transport.enqueueRpc(answered(answer = "stale"), gate = gate)
        val loading = async { f.client.loadTranscriptGuarded("client:main") }
        f.transport.awaitRequestCount(1)
        val optimistic = History(role = History.Role.USER, content = "new send")
        f.client.historyGate.lock()
        try {
            f.client.historyEpoch++
            f.client._chatHistory.value = listOf(optimistic)
        } finally {
            f.client.historyGate.unlock()
        }

        gate.complete(Unit)
        loading.await()

        assertEquals(listOf(optimistic), f.client.chatHistory.value)
        assertNull(f.client.loadCachedTranscript("client:main"))
    }

    @Test
    fun credentialSwitchFencesLateTranscriptAndCacheWrite() = runTest {
        val f = gatewayClientFixture(token = "old")
        val gate = CompletableDeferred<Unit>()
        f.transport.enqueueRpc(answered(answer = "old private answer"), gate = gate)
        val loading = async { f.client.loadTranscriptGuarded("client:main") }
        f.transport.awaitRequestCount(1)

        f.client.onCredentialsChanged("https://new.example", "new")
        gate.complete(Unit)
        loading.await()

        assertTrue(f.client.chatHistory.value.isEmpty())
        assertNull(f.client.loadCachedTranscript("client:main"))
    }

    @Test
    fun authoritativeEmptyTranscriptClearsViewAndEvictsCache() = runTest {
        val f = gatewayClientFixture()
        f.client.storeCachedTranscript(
            "client:main",
            listOf(History(role = History.Role.ASSISTANT, content = "stale")),
        )
        f.client._chatHistory.value = listOf(History(role = History.Role.ASSISTANT, content = "live"))
        f.transport.enqueueTranscript(transcriptPayload())

        f.client.loadTranscriptGuarded("client:main")

        assertTrue(f.client.chatHistory.value.isEmpty())
        assertNull(f.client.loadCachedTranscript("client:main"))
    }

    @Test
    fun recoveryRequiresSameAnsweredTailTwiceBeforeAccepting() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueTranscript(answered(answer = "first"))
        f.transport.enqueueTranscript(answered(answer = "second"))
        f.transport.enqueueTranscript(answered(answer = "second"))

        val recovered = f.client.recoverTurnFromTranscript("client:main", "question")

        assertEquals("second", (recovered as TurnRecoveryResult.Recovered).reply.text)
        assertEquals(3, f.transport.requests.size)
        assertEquals(listOf("question", "second"), f.client.chatHistory.value.map { it.content })
    }

    @Test
    fun recoveryReturnsNotArrivedAfterTwoConfirmedNotArrivedPolls() = runTest {
        val f = gatewayClientFixture()
        val unrelated = transcriptPayload(message("user", "different"))
        f.transport.enqueueTranscript(unrelated)
        f.transport.enqueueTranscript(unrelated)

        val recovered = f.client.recoverTurnFromTranscript("client:main", "question")

        assertEquals(TurnRecoveryResult.NotArrived, recovered)
        assertEquals(2, f.transport.requests.size)
        assertTrue(f.client.chatHistory.value.isEmpty())
    }

    @Test
    fun stillRunningPollResetsPriorAnswerCandidate() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueTranscript(answered(answer = "intermediate"))
        f.transport.enqueueTranscript(transcriptPayload(message("user", "question")))
        f.transport.enqueueTranscript(answered(answer = "final"))
        f.transport.enqueueTranscript(answered(answer = "final"))

        val recovered = f.client.recoverTurnFromTranscript("client:main", "question")

        assertEquals("final", (recovered as TurnRecoveryResult.Recovered).reply.text)
        assertEquals(4, f.transport.requests.size)
    }

    @Test
    fun confirmedRunningTurnPollsPastShortBudgetToRecoverLateAnswer() = runTest {
        val f = gatewayClientFixture()
        // Poll cadence 3s; the short budget is 90s = 30 polls. Keep the turn "still
        // running" well past that, then answer — a tool-heavy turn (multiple
        // wiki/mail lookups) routinely runs minutes. The stubbed clock advances one
        // poll interval per probe so the real wall-clock budget is exercised
        // (TimeSource.Monotonic ignores runTest's virtual time). Under the old fixed
        // 90s budget this recovered nothing and froze on the streamed preamble.
        val shortBudgetPolls = (
            DenebGatewayClient.STREAM_RECOVERY_BUDGET_MS /
                DenebGatewayClient.STREAM_RECOVERY_POLL_MS
            ).toInt()
        repeat(shortBudgetPolls + 5) {
            f.transport.enqueueTranscript(transcriptPayload(message("user", "question")))
        }
        f.transport.enqueueTranscript(answered(answer = "late answer"))
        f.transport.enqueueTranscript(answered(answer = "late answer"))

        var poll = 0
        val recovered = f.client.recoverTurnFromTranscript("client:main", "question") {
            poll++ * DenebGatewayClient.STREAM_RECOVERY_POLL_MS
        }

        assertEquals("late answer", (recovered as TurnRecoveryResult.Recovered).reply.text)
        assertTrue(f.transport.requests.size > shortBudgetPolls)
    }

    @Test
    fun recoveryReturnsGiveUpWhenStillRunningPastExtendedBudget() = runTest {
        val f = gatewayClientFixture()
        val maxPolls = (
            DenebGatewayClient.STREAM_RECOVERY_MAX_MS /
                DenebGatewayClient.STREAM_RECOVERY_POLL_MS
            ).toInt()
        repeat(maxPolls + 1) {
            f.transport.enqueueTranscript(transcriptPayload(message("user", "question")))
        }
        var poll = 0
        val recovered = f.client.recoverTurnFromTranscript("client:main", "question") {
            poll++ * DenebGatewayClient.STREAM_RECOVERY_POLL_MS
        }

        assertEquals(TurnRecoveryResult.GiveUp, recovered)
    }

    @Test
    fun recoveryAbandonsWhenUserSwitchesConversation() = runTest {
        val f = gatewayClientFixture()
        val gate = CompletableDeferred<Unit>()
        f.transport.enqueueRpc(answered(), gate = gate)
        val recovery = async { f.client.recoverTurnFromTranscript("client:main", "question") }
        f.transport.awaitRequestCount(1)

        f.client.switchSession("client:main:other")
        gate.complete(Unit)

        assertEquals(TurnRecoveryResult.GiveUp, recovery.await())
        assertTrue(f.client.chatHistory.value.isEmpty())
    }

    @Test
    fun recoveryPropagatesCancellationWithoutInstallingCandidate() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel recovery"))

        val failure = assertFailsWith<CancellationException> {
            f.client.recoverTurnFromTranscript("client:main", "question")
        }

        assertEquals("cancel recovery", failure.message)
        assertTrue(f.client.chatHistory.value.isEmpty())
    }

    @Test
    fun successfulRecoveryIncrementsHistoryEpochAndPersistsTranscript() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueTranscript(answered(answer = "stable"))
        f.transport.enqueueTranscript(answered(answer = "stable"))
        val before = f.client.historyEpoch

        val recovered = f.client.recoverTurnFromTranscript("client:main", "question")

        assertTrue((recovered as TurnRecoveryResult.Recovered).reply.ok)
        assertEquals(before + 1, f.client.historyEpoch)
        assertEquals("stable", f.client.loadCachedTranscript("client:main")?.last()?.content)
    }

    @Test
    fun recoveryRequestAlwaysPinsOriginalSessionKeyAndLimit() = runTest {
        val f = gatewayClientFixture()
        f.client.switchSession("client:main:pinned")
        f.transport.enqueueTranscript(answered())
        f.transport.enqueueTranscript(answered())

        f.client.recoverTurnFromTranscript("client:main:pinned", "question")

        f.transport.requests.forEach { request ->
            val params = request.requireRpc("miniapp.sessions.transcript")
            assertEquals("client:main:pinned", params["sessionKey"]?.jsonPrimitive?.content)
            assertEquals(200, params["limit"]?.jsonPrimitive?.content?.toInt())
        }
    }

    @Test
    fun wireMessageIdsDoNotCollideWhenServerRepeatsThem() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueTranscript(
            transcriptPayload(
                message("user", "one", id = "duplicate"),
                message("assistant", "two", id = "duplicate"),
            ),
        )

        val result = f.client.fetchTranscript("client:main").orEmpty()

        assertEquals(2, result.size)
        assertNotEquals(result[0].id, result[1].id)
    }

    // Conversations are no longer garbage-collected server-side (#4353), so the
    // drawer list outgrows one page. Without `total` + offset the phone stopped at
    // the first page and every older conversation was unreachable.
    @Test
    fun recentSessionsReportsWhetherMorePagesRemain() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            sessionsPayload(SessionRowOut(key = "client:main", updatedAtMs = 9), total = 12),
        )

        val first = f.client.fetchRecentSessions()

        assertEquals(12, first?.total)
        assertEquals(1, first?.serverRows)
        // serverRows, not the rendered list, is the next offset: the 업무 home row is
        // synthesized locally when a page lacks it.
        assertEquals(listOf("client:main"), first?.conversations?.map { it.id })
    }

    @Test
    fun laterPagesAppendVerbatimWithoutRepeatingTheHomeRow() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            sessionsPayload(SessionRowOut(key = "client:main:older", updatedAtMs = 3), total = 12),
        )

        val page = f.client.fetchRecentSessions(offset = 1)

        // No synthesized home on a later page — that would duplicate the row the
        // first page already showed and shift every subsequent offset.
        assertEquals(listOf("client:main:older"), page?.conversations?.map { it.id })
        assertEquals(1, page?.serverRows)
    }

    // An older gateway has no `total`; the drawer must stop rather than page into
    // nothing.
    @Test
    fun missingTotalFallsBackToTheRowsReceived() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(sessionsPayload(SessionRowOut(key = "client:main", updatedAtMs = 9)))

        val page = f.client.fetchRecentSessions()

        assertEquals(1, page?.total)
        assertEquals(1, page?.serverRows)
    }
}
