package ai.deneb.deneb

import ai.deneb.ui.chat.WorkFeedItem
import io.ktor.http.HttpStatusCode
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue
import kotlin.time.TimeSource

@OptIn(ExperimentalCoroutinesApi::class)
class WorkFeedNativeSyncStateMachineTest {

    private val json = Json { encodeDefaults = true }

    private fun item(
        id: String,
        priority: Int = 0,
        createdAtMs: Long = 0,
        status: String = "unread",
        title: String = "title-$id",
        summary: String = "summary-$id",
        body: String = "body-$id",
        sessionKey: String = "",
    ) = WorkFeedItem(
        id = id,
        priority = priority,
        createdAtMs = createdAtMs,
        status = status,
        title = title,
        summary = summary,
        body = body,
        sessionKey = sessionKey,
    )

    private fun workFeedPayload(vararg items: WorkFeedItem): String = json.encodeToString(WorkFeedPayload(items.toList()))

    private fun syncPayload(
        cursor: Long,
        hasMore: Boolean = false,
        truncated: Boolean = false,
        vararg events: NativeSyncEvent,
    ): String = json.encodeToString(
        NativeSyncPayload(
            events = events.toList(),
            cursor = cursor,
            latestSeq = cursor,
            hasMore = hasMore,
            truncated = truncated,
        ),
    )

    private fun event(
        type: String,
        seq: Long,
        item: WorkFeedItem? = null,
        sessionKey: String = "",
        removeFromFeed: Boolean = false,
    ): NativeSyncEvent {
        val payload = when {
            item == null -> null

            type == "workfeed.action.run" -> json.encodeToJsonElement(
                NativeSyncActionPayload.serializer(),
                NativeSyncActionPayload(item = item, removeFromFeed = removeFromFeed),
            ) as kotlinx.serialization.json.JsonObject

            else -> buildJsonObject {
                put("item", json.encodeToJsonElement(WorkFeedItem.serializer(), item))
            }
        }
        return NativeSyncEvent(
            seq = seq,
            type = type,
            entityId = item?.id.orEmpty(),
            sessionKey = sessionKey,
            timestampMs = seq,
            payload = payload,
        )
    }

    private fun GatewayClientFixture.prepareSuccessfulSync(feed: List<WorkFeedItem> = listOf(item("seed"))) {
        client._denebWorkFeed.value = feed
        client.lastHomeWarm = TimeSource.Monotonic.markNow()
        client.lastUsageForward = TimeSource.Monotonic.markNow()
        client.lastLocationForward = TimeSource.Monotonic.markNow()
        client.lastWikiMirrorRefresh = TimeSource.Monotonic.markNow()
        client.lastDiaryMirrorRefresh = TimeSource.Monotonic.markNow()
    }

    @Test
    fun defaultWorkFeedRefreshUsesRecentLimitTwenty() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(workFeedPayload(item("one")))

        assertTrue(f.client.refreshWorkFeed())

        val params = f.transport.singleRequest().requireRpc("miniapp.workfeed.list")
        assertEquals(20, params["limit"]?.jsonPrimitive?.content?.toInt())
        assertFalse(params.containsKey("sinceMs"))
        assertFalse(params.containsKey("beforeMs"))
    }

    @Test
    fun rangedRefreshUsesHundredRowLimitAndBothBounds() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(workFeedPayload())

        assertTrue(f.client.refreshWorkFeed(sinceMs = 10, beforeMs = 20, merge = true))

        val params = f.transport.singleRequest().requireRpc("miniapp.workfeed.list")
        assertEquals(100, params["limit"]?.jsonPrimitive?.content?.toInt())
        assertEquals(10L, params["sinceMs"]?.jsonPrimitive?.content?.toLong())
        assertEquals(20L, params["beforeMs"]?.jsonPrimitive?.content?.toLong())
    }

    @Test
    fun oneSidedRangeOmitsUnusedBoundary() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(workFeedPayload())

        f.client.refreshWorkFeed(beforeMs = 30, merge = true)

        val params = f.transport.singleRequest().rpcParams.orEmpty()
        assertFalse(params.containsKey("sinceMs"))
        assertEquals(30L, params["beforeMs"]?.jsonPrimitive?.content?.toLong())
    }

    @Test
    fun failedRefreshMarksFirstLoadCompleteWithoutErasingCachedFeed() = runTest {
        val f = gatewayClientFixture()
        val cached = item("cached")
        f.client._denebWorkFeed.value = listOf(cached)
        f.transport.enqueueJson("bad", status = HttpStatusCode.ServiceUnavailable)

        val result = f.client.refreshWorkFeed()

        assertFalse(result)
        assertTrue(f.client.workFeedLoaded.value)
        assertEquals(listOf(cached), f.client.denebWorkFeed.value)
    }

    @Test
    fun successfulRefreshFiltersBlankIdentityRows() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(workFeedPayload(item(""), item("valid"), item("   ")))

        f.client.refreshWorkFeed()

        assertEquals(listOf("valid"), f.client.denebWorkFeed.value.map { it.id })
    }

    @Test
    fun successfulNonRangedRefreshReplacesOldSnapshot() = runTest {
        val f = gatewayClientFixture()
        f.client._denebWorkFeed.value = listOf(item("old"))
        f.transport.enqueueRpc(workFeedPayload(item("new-one"), item("new-two")))

        f.client.refreshWorkFeed()

        assertEquals(listOf("new-one", "new-two"), f.client.denebWorkFeed.value.map { it.id })
        assertTrue(f.client.workFeedLoaded.value)
    }

    @Test
    fun rangedMergeReplacesOnlyItemsInsideRequestedWindow() = runTest {
        val f = gatewayClientFixture()
        f.client._denebWorkFeed.value = listOf(
            item("before", createdAtMs = 5),
            item("inside-old", createdAtMs = 15),
            item("after", createdAtMs = 25),
        )
        f.transport.enqueueRpc(workFeedPayload(item("inside-new", createdAtMs = 18)))

        f.client.refreshWorkFeed(sinceMs = 10, beforeMs = 20, merge = true)

        assertEquals(setOf("before", "inside-new", "after"), f.client.denebWorkFeed.value.map { it.id }.toSet())
        assertFalse(f.client.denebWorkFeed.value.any { it.id == "inside-old" })
    }

    @Test
    fun rangedMergeSortsPriorityThenTimestampThenId() = runTest {
        val f = gatewayClientFixture()
        f.client._denebWorkFeed.value = listOf(item("low", priority = 1, createdAtMs = 100))
        f.transport.enqueueRpc(
            workFeedPayload(
                item("b", priority = 5, createdAtMs = 10),
                item("a", priority = 5, createdAtMs = 10),
                item("newer", priority = 5, createdAtMs = 20),
            ),
        )

        f.client.refreshWorkFeed(sinceMs = 1, merge = true)

        assertEquals(listOf("newer", "b", "a"), f.client.denebWorkFeed.value.map { it.id })
    }

    @Test
    fun rangedMergeDeduplicatesRepeatedServerRowsByIdentity() = runTest {
        val f = gatewayClientFixture()
        f.client._denebWorkFeed.value = listOf(item("outside", createdAtMs = 1))
        f.transport.enqueueRpc(
            workFeedPayload(
                item("same", createdAtMs = 20, summary = "first"),
                item("same", createdAtMs = 21, summary = "newest"),
            ),
        )

        f.client.refreshWorkFeed(sinceMs = 10, merge = true)

        assertEquals(1, f.client.denebWorkFeed.value.count { it.id == "same" })
        assertEquals("newest", f.client.denebWorkFeed.value.first { it.id == "same" }.summary)
    }

    @Test
    fun fullRefreshDeduplicatesRepeatedServerRowsByIdentity() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            workFeedPayload(
                item("same", summary = "first"),
                item("same", summary = "last"),
            ),
        )

        f.client.refreshWorkFeed()

        assertEquals(listOf("same"), f.client.denebWorkFeed.value.map { it.id })
        assertEquals("last", f.client.denebWorkFeed.value.single().summary)
    }

    @Test
    fun workItemSessionKeyNormalizesCaseAndPunctuation() {
        val f = gatewayClientFixture()

        val key = f.client.workItemSessionKey("  Deal/Room 42! ")

        assertEquals("client:main:wf-deal-room-42", key)
    }

    @Test
    fun workItemSessionKeyCapsSlugAtFortyCharacters() {
        val f = gatewayClientFixture()

        val key = f.client.workItemSessionKey("A".repeat(80))

        assertEquals(40, key.substringAfter("client:main:wf-").length)
    }

    @Test
    fun workItemSessionKeyFallsBackToUniqueSessionForNonAsciiOnlyId() {
        val f = gatewayClientFixture()

        val first = f.client.workItemSessionKey("한글")
        val second = f.client.workItemSessionKey("한글")

        assertTrue(first.startsWith("client:main:wf-"))
        assertTrue(second.startsWith("client:main:wf-"))
        assertFalse(first == second)
    }

    @Test
    fun blankWorkFeedActionInputsShortCircuitWithoutNetwork() = runTest {
        val f = gatewayClientFixture()

        assertNull(f.client.runWorkFeedAction("", "open"))
        assertNull(f.client.runWorkFeedAction("item", "  "))

        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun workFeedActionSendsExactItemAndActionIds() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            json.encodeToString(WorkFeedActionRunPayload(prompt = "do it")),
        )

        val prompt = f.client.runWorkFeedAction("item-1", "approve", adoptSession = false)

        assertEquals("do it", prompt)
        val params = f.transport.singleRequest().requireRpc("miniapp.workfeed.action.run")
        assertEquals("item-1", params["itemId"]?.jsonPrimitive?.content)
        assertEquals("approve", params["actionId"]?.jsonPrimitive?.content)
    }

    @Test
    fun removingActionDropsCardFromFeed() = runTest {
        val f = gatewayClientFixture()
        f.client._denebWorkFeed.value = listOf(item("keep"), item("remove"))
        f.transport.enqueueRpc(
            json.encodeToString(
                WorkFeedActionRunPayload(
                    item = item("remove"),
                    prompt = "done",
                    removeFromFeed = true,
                ),
            ),
        )

        f.client.runWorkFeedAction("remove", "ack", adoptSession = false)

        assertEquals(listOf("keep"), f.client.denebWorkFeed.value.map { it.id })
    }

    @Test
    fun nonRemovingActionUpdatesMatchingCardInPlace() = runTest {
        val f = gatewayClientFixture()
        f.client._denebWorkFeed.value = listOf(item("one", summary = "old"), item("two"))
        f.transport.enqueueRpc(
            json.encodeToString(
                WorkFeedActionRunPayload(
                    item = item("one", summary = "updated"),
                    prompt = "done",
                ),
            ),
        )

        f.client.runWorkFeedAction("one", "refresh", adoptSession = false)

        assertEquals("updated", f.client.denebWorkFeed.value.first().summary)
        assertEquals("two", f.client.denebWorkFeed.value.last().id)
    }

    @Test
    fun blankActionPromptMapsToNull() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(json.encodeToString(WorkFeedActionRunPayload(prompt = "   ")))

        val prompt = f.client.runWorkFeedAction("one", "ack", adoptSession = false)

        assertNull(prompt)
    }

    @Test
    fun actionWithoutAdoptionNeverSwitchesOrLoadsTranscript() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            json.encodeToString(
                WorkFeedActionRunPayload(
                    sessionKey = "client:main:target",
                    prompt = "prompt",
                ),
            ),
        )

        f.client.runWorkFeedAction("one", "open", adoptSession = false)

        assertEquals("client:main", f.client.currentConversationId.value)
        assertEquals(listOf("miniapp.workfeed.action.run"), f.transport.requestMethods())
    }

    @Test
    fun actionAdoptsReturnedSessionAndLoadsItsTranscript() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            json.encodeToString(
                WorkFeedActionRunPayload(
                    sessionKey = "client:main:target",
                    prompt = "prompt",
                ),
            ),
        )
        f.transport.enqueueRpc("""{"messages":[{"role":"assistant","content":"target answer"}]}""")

        f.client.runWorkFeedAction("one", "continue")

        assertEquals("client:main:target", f.client.currentConversationId.value)
        assertEquals("target answer", f.client.chatHistory.value.single().content)
        assertEquals(
            listOf("miniapp.workfeed.action.run", "miniapp.sessions.transcript"),
            f.transport.requestMethods(),
        )
    }

    @Test
    fun actionFallsBackToItemSessionWhenEnvelopeSessionBlank() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            json.encodeToString(
                WorkFeedActionRunPayload(
                    item = item("one", sessionKey = "client:main:item-session"),
                    prompt = "prompt",
                ),
            ),
        )
        f.transport.enqueueRpc("""{"messages":[]}""")

        f.client.runWorkFeedAction("one", "continue")

        assertEquals("client:main:item-session", f.client.currentConversationId.value)
    }

    @Test
    fun blankAnswerInputsShortCircuitWithoutNetwork() = runTest {
        val f = gatewayClientFixture()

        assertNull(f.client.answerWorkFeedItem("", "answer"))
        assertNull(f.client.answerWorkFeedItem("one", " \n "))

        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun answerSendsTextAndRemovesSettledCard() = runTest {
        val f = gatewayClientFixture()
        f.client._denebWorkFeed.value = listOf(item("one"), item("two"))
        f.transport.enqueueRpc(
            json.encodeToString(
                WorkFeedActionRunPayload(
                    item = item("one"),
                    prompt = "deliver answer",
                    removeFromFeed = true,
                ),
            ),
        )

        val prompt = f.client.answerWorkFeedItem("one", "my answer")

        assertEquals("deliver answer", prompt)
        assertEquals(listOf("two"), f.client.denebWorkFeed.value.map { it.id })
        val params = f.transport.singleRequest().requireRpc("miniapp.workfeed.answer")
        assertEquals("my answer", params["answer"]?.jsonPrimitive?.content)
    }

    @Test
    fun feedbackUpdatesCardAndReturnsConfirmationText() = runTest {
        val f = gatewayClientFixture()
        f.client._denebWorkFeed.value = listOf(item("one", summary = "old"))
        f.transport.enqueueRpc(
            json.encodeToString(
                WorkFeedFeedbackPayload(
                    item = item("one", summary = "corrected"),
                    text = "wiki updated",
                ),
            ),
        )

        val result = f.client.sendWorkFeedFeedback("one", "wrong counterparty")

        assertEquals("wiki updated", result)
        assertEquals("corrected", f.client.denebWorkFeed.value.single().summary)
        val params = f.transport.singleRequest().requireRpc("miniapp.workfeed.feedback")
        assertEquals("wrong counterparty", params["feedback"]?.jsonPrimitive?.content)
    }

    @Test
    fun rewriteUpdatesCardAndReturnsResultText() = runTest {
        val f = gatewayClientFixture()
        f.client._denebWorkFeed.value = listOf(item("one", body = "old"))
        f.transport.enqueueRpc(
            json.encodeToString(
                WorkFeedFeedbackPayload(
                    item = item("one", body = "rewritten"),
                    text = "rewritten now",
                ),
            ),
        )

        val result = f.client.rewriteWorkFeedCard("one")

        assertEquals("rewritten now", result)
        assertEquals("rewritten", f.client.denebWorkFeed.value.single().body)
        f.transport.singleRequest().requireRpc("miniapp.workfeed.rewrite")
    }

    @Test
    fun feedbackAndRewriteRejectBlankIdentityInputs() = runTest {
        val f = gatewayClientFixture()

        assertNull(f.client.sendWorkFeedFeedback("", "feedback"))
        assertNull(f.client.sendWorkFeedFeedback("one", "  "))
        assertNull(f.client.rewriteWorkFeedCard("  "))

        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun markReadRejectsBlankAndAcceptsNonNullRpcPayload() = runTest {
        val f = gatewayClientFixture()
        assertFalse(f.client.markWorkFeedRead(""))
        f.transport.enqueueRpc("{}")

        assertTrue(f.client.markWorkFeedRead("one"))

        val params = f.transport.singleRequest().requireRpc("miniapp.workfeed.read")
        assertEquals("one", params["itemId"]?.jsonPrimitive?.content)
    }

    @Test
    fun ingestRequiresTextButPreservesTypeAndSource() = runTest {
        val f = gatewayClientFixture()
        assertFalse(f.client.ingestEvent("notification", "mail", "   "))
        f.transport.enqueueRpc("{}")

        assertTrue(f.client.ingestEvent("notification", "메일", "계약 도착"))

        val params = f.transport.singleRequest().requireRpc("miniapp.event.ingest")
        assertEquals("notification", params["type"]?.jsonPrimitive?.content)
        assertEquals("메일", params["source"]?.jsonPrimitive?.content)
        assertEquals("계약 도착", params["text"]?.jsonPrimitive?.content)
    }

    @Test
    fun successfulEmptySyncAdvancesAndPersistsCursor() = runTest {
        val f = gatewayClientFixture()
        f.prepareSuccessfulSync()
        f.transport.enqueueRpc(syncPayload(cursor = 7))

        assertTrue(f.client.syncNativeState())

        assertEquals(7L, f.client.nativeSyncCursor)
        assertEquals(7L, f.settings.settings.getLong(DenebGatewayClient.KEY_SYNC_CURSOR, -1))
        assertTrue(f.client.nativeSyncBaselined)
    }

    @Test
    fun nativeSyncStartsFromPersistedCursorAfterRestart() = runTest {
        val transport = GatewayHttpHarness()
        val f = gatewayClientFixture(transport = transport)
        f.settings.settings.putLong(DenebGatewayClient.KEY_SYNC_CURSOR, 41)
        val restarted = DenebGatewayClient(
            f.settings,
            ai.deneb.data.SmsDraftStore(f.settings),
            ai.deneb.sms.SmsSender(),
            transport.httpClient,
            MemoryWikiMirrorFiles(),
        )
        restarted._denebWorkFeed.value = listOf(item("seed"))
        restarted.lastHomeWarm = TimeSource.Monotonic.markNow()
        restarted.lastUsageForward = TimeSource.Monotonic.markNow()
        restarted.lastLocationForward = TimeSource.Monotonic.markNow()
        restarted.lastWikiMirrorRefresh = TimeSource.Monotonic.markNow()
        restarted.lastDiaryMirrorRefresh = TimeSource.Monotonic.markNow()
        transport.enqueueRpc(syncPayload(cursor = 42))

        restarted.syncNativeState()

        val params = transport.singleRequest().requireRpc("miniapp.sync.pull")
        assertEquals(41L, params["cursor"]?.jsonPrimitive?.content?.toLong())
    }

    @Test
    fun syncPullUsesFixedHundredEventPageSize() = runTest {
        val f = gatewayClientFixture()
        f.prepareSuccessfulSync()
        f.transport.enqueueRpc(syncPayload(cursor = 1))

        f.client.syncNativeState()

        val params = f.transport.singleRequest().requireRpc("miniapp.sync.pull")
        assertEquals(100, params["limit"]?.jsonPrimitive?.content?.toInt())
    }

    @Test
    fun hasMorePagesAdvanceCursorAcrossRequests() = runTest {
        val f = gatewayClientFixture()
        f.prepareSuccessfulSync()
        f.transport.enqueueRpc(syncPayload(cursor = 10, hasMore = true))
        f.transport.enqueueRpc(syncPayload(cursor = 20))

        f.client.syncNativeState()

        assertEquals(listOf(0L, 10L), f.transport.requests.map { it.rpcParams?.get("cursor")?.jsonPrimitive?.content?.toLong() })
        assertEquals(20L, f.client.nativeSyncCursor)
    }

    @Test
    fun hasMoreWithNonAdvancingCursorStopsToAvoidInfiniteLoop() = runTest {
        val f = gatewayClientFixture()
        f.prepareSuccessfulSync()
        f.transport.enqueueRpc(syncPayload(cursor = 0, hasMore = true))

        assertTrue(f.client.syncNativeState())

        assertEquals(1, f.transport.requests.size)
        assertEquals(0L, f.client.nativeSyncCursor)
    }

    @Test
    fun maliciousRegressingCursorCannotMoveStateBackward() = runTest {
        val f = gatewayClientFixture()
        f.prepareSuccessfulSync()
        f.client.nativeSyncCursor = 50
        f.settings.settings.putLong(DenebGatewayClient.KEY_SYNC_CURSOR, 50)
        f.transport.enqueueRpc(syncPayload(cursor = -100))

        f.client.syncNativeState()

        assertEquals(50L, f.client.nativeSyncCursor)
        assertEquals(50L, f.settings.settings.getLong(DenebGatewayClient.KEY_SYNC_CURSOR, -1))
    }

    @Test
    fun backlogBeyondFourPagesDrainsFullyInOneResume() = runTest {
        // Regression guard for the old 4-page cap (max 400 events): a long
        // background stretch must catch up in ONE resume, not silently stop.
        val f = gatewayClientFixture()
        f.prepareSuccessfulSync()
        (1L..6L).forEach { cursor -> f.transport.enqueueRpc(syncPayload(cursor, hasMore = cursor < 6L)) }

        assertTrue(f.client.syncNativeState())

        assertEquals(6, f.transport.requests.size)
        assertEquals(6L, f.client.nativeSyncCursor)
    }

    @Test
    fun maliciousEndlessPaginationIsCappedAtFortyPages() = runTest {
        val f = gatewayClientFixture()
        f.prepareSuccessfulSync()
        (1L..45L).forEach { cursor -> f.transport.enqueueRpc(syncPayload(cursor, hasMore = true)) }

        assertTrue(f.client.syncNativeState())

        assertEquals(40, f.transport.requests.size)
        assertEquals(40L, f.client.nativeSyncCursor)
    }

    @Test
    fun truncatedSyncResyncsWholesaleInsteadOfTrustingTheDelta() = runTest {
        // Server retention rotated past our cursor (truncated=true): the
        // retained tail still applies, but the pruned range is unrecoverable —
        // so the feed must be replaced from server truth and the home warm must
        // fire despite prepareSuccessfulSync arming its throttle (battery doc
        // §3.1, the last open gap behind the background-Doze handoff).
        val f = gatewayClientFixture()
        f.prepareSuccessfulSync(feed = listOf(item("stale")))
        f.transport.enqueueRpc(
            syncPayload(
                cursor = 9,
                truncated = true,
                events = arrayOf(event("workfeed.created", 9, item("retained"))),
            ),
        )
        f.transport.enqueueRpc(workFeedPayload(item("fresh-one"), item("fresh-two")))
        f.transport.enqueueRpc("{}") // calendar.list_upcoming — forced warm
        f.transport.enqueueRpc("{}") // mail.list_recent — forced warm
        f.transport.enqueueRpc("{}") // mail.native_status — refreshMail tail

        assertTrue(f.client.syncNativeState())

        assertEquals(9L, f.client.nativeSyncCursor)
        // The wholesale replace wins over both the retained-tail delta and the
        // stale pre-sync feed.
        assertEquals(listOf("fresh-one", "fresh-two"), f.client.denebWorkFeed.value.map { it.id })
        val methods = f.transport.requests.mapNotNull { it.rpcMethod }
        assertTrue(methods.contains("miniapp.workfeed.list"), "methods=$methods")
        assertTrue(methods.contains("miniapp.calendar.list_upcoming"), "methods=$methods")
        assertTrue(methods.contains("miniapp.mail.list_recent"), "methods=$methods")
    }

    @Test
    fun firstCatchUpCreatedEventUpdatesFeedWithoutNotification() = runTest {
        val f = gatewayClientFixture()
        f.prepareSuccessfulSync(feed = emptyList())
        f.transport.enqueueRpc(syncPayload(1, events = arrayOf(event("workfeed.created", 1, item("new")))))

        f.client.syncNativeState()

        assertEquals(listOf("new"), f.client.denebWorkFeed.value.map { it.id })
        assertTrue(f.client.nativeSyncBaselined)
    }

    @Test
    fun postBaselineUnreadCreatedEventEmitsOneNotification() = runTest {
        val f = gatewayClientFixture()
        f.prepareSuccessfulSync()
        f.client.nativeSyncBaselined = true
        f.transport.enqueueRpc(syncPayload(1, events = arrayOf(event("workfeed.created", 1, item("new", title = "Alert", summary = "Summary")))))
        val notification = async { f.client.proactiveNotifications.first() }
        runCurrent()

        f.client.syncNativeState()

        assertEquals("Alert", notification.await().title)
        assertEquals("Summary", notification.getCompleted().body)
        assertEquals("workfeed", notification.getCompleted().kind)
        assertEquals("new", notification.getCompleted().ref)
    }

    @Test
    fun updatedEventNeverEmitsButReplacesCard() = runTest {
        val f = gatewayClientFixture()
        f.prepareSuccessfulSync(feed = listOf(item("one", summary = "old")))
        f.client.nativeSyncBaselined = true
        f.transport.enqueueRpc(syncPayload(1, events = arrayOf(event("workfeed.updated", 1, item("one", summary = "new")))))

        f.client.syncNativeState()

        assertEquals("new", f.client.denebWorkFeed.value.single().summary)
    }

    @Test
    fun ackedAndSnoozedSyncItemsAreRemoved() = runTest {
        val f = gatewayClientFixture()
        f.prepareSuccessfulSync(feed = listOf(item("acked"), item("snoozed"), item("keep")))
        f.transport.enqueueRpc(
            syncPayload(
                2,
                events = arrayOf(
                    event("workfeed.updated", 1, item("acked", status = "acked")),
                    event("workfeed.updated", 2, item("snoozed", status = "snoozed")),
                ),
            ),
        )

        f.client.syncNativeState()

        assertEquals(listOf("keep"), f.client.denebWorkFeed.value.map { it.id })
    }

    @Test
    fun malformedAndUnknownSyncEventsAreIgnoredWhileCursorAdvances() = runTest {
        val f = gatewayClientFixture()
        f.prepareSuccessfulSync()
        f.transport.enqueueRpc(
            syncPayload(
                3,
                events = arrayOf(
                    NativeSyncEvent(seq = 1, type = "workfeed.created", payload = buildJsonObject { put("item", "wrong") }),
                    NativeSyncEvent(seq = 2, type = "future.event"),
                ),
            ),
        )

        assertTrue(f.client.syncNativeState())

        assertEquals(listOf("seed"), f.client.denebWorkFeed.value.map { it.id })
        assertEquals(3L, f.client.nativeSyncCursor)
    }

    @Test
    fun actionRunEventCanRemoveCard() = runTest {
        val f = gatewayClientFixture()
        f.prepareSuccessfulSync(feed = listOf(item("one"), item("two")))
        f.transport.enqueueRpc(
            syncPayload(
                1,
                events = arrayOf(event("workfeed.action.run", 1, item("one"), removeFromFeed = true)),
            ),
        )

        f.client.syncNativeState()

        assertEquals(listOf("two"), f.client.denebWorkFeed.value.map { it.id })
    }

    @Test
    fun currentSessionTranscriptEventReloadsCanonicalTranscript() = runTest {
        val f = gatewayClientFixture()
        f.prepareSuccessfulSync()
        f.transport.enqueueRpc(
            syncPayload(
                1,
                events = arrayOf(NativeSyncEvent(seq = 1, type = "transcript.appended", sessionKey = "client:main")),
            ),
        )
        f.transport.enqueueRpc("""{"messages":[{"role":"assistant","content":"canonical"}]}""")

        f.client.syncNativeState()

        assertEquals("canonical", f.client.chatHistory.value.single().content)
        assertEquals(
            listOf("miniapp.sync.pull", "miniapp.sessions.transcript"),
            f.transport.requestMethods(),
        )
    }

    @Test
    fun otherSessionTranscriptEventDoesNotClobberOpenConversation() = runTest {
        val f = gatewayClientFixture()
        f.prepareSuccessfulSync()
        f.client._chatHistory.value = listOf(ai.deneb.ui.chat.History(role = ai.deneb.ui.chat.History.Role.ASSISTANT, content = "open"))
        f.transport.enqueueRpc(
            syncPayload(
                1,
                events = arrayOf(NativeSyncEvent(seq = 1, type = "transcript.appended", sessionKey = "client:main:other")),
            ),
        )

        f.client.syncNativeState()

        assertEquals("open", f.client.chatHistory.value.single().content)
        assertEquals(listOf("miniapp.sync.pull"), f.transport.requestMethods())
    }

    @Test
    fun failedSyncFallsBackToFullWorkFeedRefresh() = runTest {
        val f = gatewayClientFixture()
        f.client.lastHomeWarm = TimeSource.Monotonic.markNow()
        f.transport.enqueueJson("bad")
        f.transport.enqueueRpc(workFeedPayload(item("healed")))

        val result = f.client.syncNativeState()

        assertTrue(result)
        assertEquals(listOf("healed"), f.client.denebWorkFeed.value.map { it.id })
        assertEquals(listOf("miniapp.sync.pull", "miniapp.workfeed.list"), f.transport.requestMethods())
    }

    @Test
    fun successfulSyncHealsUnexpectedlyEmptyFeed() = runTest {
        val f = gatewayClientFixture()
        f.prepareSuccessfulSync(feed = emptyList())
        f.transport.enqueueRpc(syncPayload(1))
        f.transport.enqueueRpc(workFeedPayload(item("healed")))

        assertTrue(f.client.syncNativeState())

        assertEquals(listOf("healed"), f.client.denebWorkFeed.value.map { it.id })
    }

    @Test
    fun credentialSwitchDuringPullDropsEventsAndReassertsCursorReset() = runTest {
        val f = gatewayClientFixture(token = "old")
        f.prepareSuccessfulSync()
        val gate = CompletableDeferred<Unit>()
        f.transport.enqueueRpc(
            payload = syncPayload(99, events = arrayOf(event("workfeed.created", 1, item("old-private")))),
            gate = gate,
        )
        val syncing = async { f.client.syncNativeState() }
        f.transport.awaitRequestCount(1)

        f.client.onCredentialsChanged("https://new.example", "new")
        gate.complete(Unit)

        assertFalse(syncing.await())
        assertEquals(0L, f.client.nativeSyncCursor)
        assertEquals(0L, f.settings.settings.getLong(DenebGatewayClient.KEY_SYNC_CURSOR, -1))
        assertTrue(f.client.denebWorkFeed.value.isEmpty())
        assertFalse(f.client.nativeSyncBaselined)
    }

    @Test
    fun cancellationDuringSyncPropagatesAndPreservesCursor() = runTest {
        val f = gatewayClientFixture()
        f.prepareSuccessfulSync()
        f.client.nativeSyncCursor = 12
        f.transport.enqueueFailure(CancellationException("cancel sync"))

        val failure = assertFailsWith<CancellationException> { f.client.syncNativeState() }

        assertEquals("cancel sync", failure.message)
        assertEquals(12L, f.client.nativeSyncCursor)
    }

    @Test
    fun concurrentSyncCallsAreSerializedByNativeSyncGate() = runTest {
        val f = gatewayClientFixture()
        f.prepareSuccessfulSync()
        val gate = CompletableDeferred<Unit>()
        f.transport.enqueueRpc(payload = syncPayload(1), gate = gate)
        f.transport.enqueueRpc(syncPayload(2))
        val first = async { f.client.syncNativeState() }
        val second = async { f.client.syncNativeState() }
        f.transport.awaitRequestCount(1)

        assertEquals(1, f.transport.requests.size)
        gate.complete(Unit)
        assertTrue(first.await())
        assertTrue(second.await())
        assertEquals(2L, f.client.nativeSyncCursor)
        assertEquals(2, f.transport.requests.size)
    }
}
