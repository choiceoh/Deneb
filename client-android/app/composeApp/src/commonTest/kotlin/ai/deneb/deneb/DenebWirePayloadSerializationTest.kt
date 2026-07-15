package ai.deneb.deneb

import kotlinx.serialization.SerializationException
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class DenebWirePayloadSerializationTest {

    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
        explicitNulls = false
    }

    @Test
    fun emptyListEnvelopesDecodeToEmptyCollections() {
        assertEquals(emptyList(), json.decodeFromString<RecentPayload>("{}").sessions)
        assertEquals(emptyList(), json.decodeFromString<TranscriptPayload>("{}").messages)
        assertEquals(emptyList(), json.decodeFromString<WorkFeedPayload>("{}").items)
        assertEquals(emptyList(), json.decodeFromString<MemoryListPayload>("{}").pages)
        assertEquals(emptyList(), json.decodeFromString<DiaryRecentPayload>("{}").entries)
        assertEquals(emptyList(), json.decodeFromString<CronListPayload>("{}").jobs)
    }

    @Test
    fun nativeSyncDefaultsRepresentAnEmptyCompletedPage() {
        val payload = json.decodeFromString<NativeSyncPayload>("{}")

        assertEquals(emptyList(), payload.events)
        assertEquals(0L, payload.cursor)
        assertEquals(0L, payload.latestSeq)
        assertFalse(payload.hasMore)
    }

    @Test
    fun nativeSyncDecodesCursorAndNestedEventPayload() {
        val payload = json.decodeFromString<NativeSyncPayload>(
            """
            {
              "cursor": 41,
              "latestSeq": 99,
              "hasMore": true,
              "events": [{
                "seq": 42,
                "type": "workfeed.updated",
                "entityId": "entity-1",
                "sessionKey": "client:main:ops",
                "workFeedItemId": "feed-1",
                "timestampMs": 1750000000123,
                "payload": {"status":"done","count":3}
              }]
            }
            """.trimIndent(),
        )

        assertEquals(41L, payload.cursor)
        assertEquals(99L, payload.latestSeq)
        assertTrue(payload.hasMore)
        val event = payload.events.single()
        assertEquals(42L, event.seq)
        assertEquals("workfeed.updated", event.type)
        val eventPayload = event.payload ?: error("event payload missing")
        assertEquals("done", (eventPayload["status"] as JsonPrimitive).content)
        assertEquals(3, (eventPayload["count"] as JsonPrimitive).content.toInt())
    }

    @Test
    fun nativeSyncEventAllowsMissingAndNullPayload() {
        assertNull(json.decodeFromString<NativeSyncEvent>("{}").payload)
        assertNull(json.decodeFromString<NativeSyncEvent>("""{"payload":null}""").payload)
    }

    @Test
    fun unknownNativeSyncFieldsAreIgnoredAtBothLevels() {
        val payload = json.decodeFromString<NativeSyncPayload>(
            """{"futurePage":"x","events":[{"seq":1,"futureEvent":{"x":true}}]}""",
        )

        assertEquals(1L, payload.events.single().seq)
    }

    @Test
    fun wrongPrimitiveTypeInEventFailsInsteadOfStringifyingObject() {
        assertFailsWith<SerializationException> {
            json.decodeFromString<NativeSyncEvent>("""{"type":{"nested":true}}""")
        }
    }

    @Test
    fun workFeedActionRunDefaultsAreSafeForFailedOrLegacyResponses() {
        val payload = json.decodeFromString<WorkFeedActionRunPayload>("{}")

        assertFalse(payload.ok)
        assertEquals("", payload.item.id)
        assertEquals("", payload.sessionKey)
        assertEquals("", payload.prompt)
        assertEquals("", payload.message)
        assertFalse(payload.removeFromFeed)
    }

    @Test
    fun workFeedActionRunDecodesNestedActionsAndRemovalFlag() {
        val payload = json.decodeFromString<WorkFeedActionRunPayload>(
            """
            {
              "ok":true,
              "sessionKey":"client:main",
              "prompt":"archive it",
              "message":"archived",
              "removeFromFeed":true,
              "item":{
                "id":"f1","source":"proactive","title":"Report","priority":3,
                "actions":[{"id":"archive","kind":"archive","label":"보관"}]
              }
            }
            """.trimIndent(),
        )

        assertTrue(payload.ok)
        assertTrue(payload.removeFromFeed)
        assertEquals("f1", payload.item.id)
        assertEquals("archive", payload.item.actions.single().id)
    }

    @Test
    fun workFeedFeedbackRetainsAgentReportAndSession() {
        val payload = json.decodeFromString<WorkFeedFeedbackPayload>(
            """{"ok":true,"text":"wiki updated","sessionKey":"client:main:deal","item":{"id":"f2","title":"Corrected"}}""",
        )

        assertTrue(payload.ok)
        assertEquals("wiki updated", payload.text)
        assertEquals("client:main:deal", payload.sessionKey)
        assertEquals("Corrected", payload.item.title)
    }

    @Test
    fun nativeSyncActionDefaultsDoNotRemoveCard() {
        val payload = json.decodeFromString<NativeSyncActionPayload>("{}")

        assertEquals("", payload.item.id)
        assertFalse(payload.removeFromFeed)
    }

    @Test
    fun diaryRowsPreserveUnicodeMultilineAndExtremeTimestamp() {
        val original = DiaryRecentPayload(
            entries = listOf(
                DiaryRecentRow(
                    file = "memory/2026-07-11.md",
                    header = "오전 기록",
                    content = "첫 줄\n둘째 줄 🚀",
                    at = Long.MAX_VALUE,
                ),
            ),
        )

        val decoded = json.decodeFromString<DiaryRecentPayload>(json.encodeToString(original))

        assertEquals(original, decoded)
    }

    @Test
    fun deleteAndMovePayloadDefaultsAreConservative() {
        assertEquals(DeletePagesPayload(ok = false, deleted = 0), json.decodeFromString<DeletePagesPayload>("{}"))
        assertEquals(MovePagePayload(ok = false), json.decodeFromString<MovePagePayload>("{}"))
    }

    @Test
    fun deletePayloadPreservesNegativeServerCountForDiagnostics() {
        val payload = json.decodeFromString<DeletePagesPayload>("""{"ok":true,"deleted":-1}""")

        assertTrue(payload.ok)
        assertEquals(-1, payload.deleted)
    }

    @Test
    fun categoryEnvelopeDecodesTotalsAndRows() {
        val payload = json.decodeFromString<CategoriesPayload>(
            """{"categories":[{"name":"projects","pageCount":7}],"totalPages":9,"totalBytes":123456789}""",
        )

        assertEquals("projects", payload.categories.single().name)
        assertEquals(7, payload.categories.single().pageCount)
        assertEquals(9, payload.totalPages)
        assertEquals(123_456_789L, payload.totalBytes)
    }

    @Test
    fun modelEnvelopeDecodesRolesSectionsAdvisoriesAndVisionFlag() {
        val payload = json.decodeFromString<ModelsPayload>(
            """
            {
              "current":"main-model",
              "roles":[{"role":"vision","model":"vision-model"}],
              "sections":[{"title":"Local","models":[{"id":"main-model","label":"Main","current":true}]}],
              "advisories":["fallback active"],
              "mainHasVision":true
            }
            """.trimIndent(),
        )

        assertEquals("main-model", payload.current)
        assertEquals("vision", payload.roles.single().role)
        assertEquals("Local", payload.sections.single().title)
        assertEquals(listOf("fallback active"), payload.advisories)
        assertTrue(payload.mainHasVision)
    }

    @Test
    fun omittedVisionCapabilityDefaultsFalse() {
        assertFalse(json.decodeFromString<ModelsPayload>("{}").mainHasVision)
        assertFalse(json.decodeFromString<ModelsPayload>("""{"mainHasVision":null}""").mainHasVision)
    }

    @Test
    fun clientHelloDecodesBooleanCapabilitiesAndEndpointMap() {
        val payload = json.decodeFromString<ClientHelloPayload>(
            """
            {
              "version":"2026.7","nativeApiVersion":4,"model":"gpt",
              "capabilities":{"mail":true,"sms":false},
              "endpoints":{"events":"/api/events"},"tsMs":1750000000000
            }
            """.trimIndent(),
        )

        assertEquals(4, payload.nativeApiVersion)
        assertEquals(true, payload.capabilities["mail"])
        assertEquals(false, payload.capabilities["sms"])
        assertEquals("/api/events", payload.endpoints["events"])
        assertEquals(1_750_000_000_000L, payload.tsMs)
    }

    @Test
    fun mailListEnvelopeKeepsPaginationTokenAndRows() {
        val payload = json.decodeFromString<MailListPayload>(
            """{"messages":[{"id":"m1","subject":"견적","isUnread":true}],"nextPageToken":"next:42"}""",
        )

        assertEquals("m1", payload.messages.single().id)
        assertEquals("견적", payload.messages.single().subject)
        assertTrue(payload.messages.single().isUnread)
        assertEquals("next:42", payload.nextPageToken)
    }

    @Test
    fun scalarPayloadDefaultsAndUnicodeRoundTrip() {
        assertFalse(json.decodeFromString<OkPayload>("{}").ok)
        assertEquals("", json.decodeFromString<AskPayload>("{}").answer)
        assertEquals("한글 답변 🚀", json.decodeFromString<AskPayload>("""{"answer":"한글 답변 🚀"}""").answer)
        assertEquals("ocr text", json.decodeFromString<CaptureImagePayload>("""{"text":"ocr text"}""").text)
        assertEquals("asr text", json.decodeFromString<CaptureAudioPayload>("""{"text":"asr text"}""").text)
    }

    @Test
    fun senderContextDefaultsNestedFieldsSafely() {
        val payload = json.decodeFromString<SenderContextPayload>("{}")

        assertEquals("", payload.sender)
        assertEquals("", payload.email)
        assertEquals("", payload.displayName)
        assertNull(payload.recent)
        assertEquals(emptyList(), payload.wikiHits)
        assertEquals("", payload.wikiFacts)
    }

    @Test
    fun wikiPageEnvelopePreservesRelationshipAndFrozenCodeFields() {
        val original = WikiPagePayload(
            path = "projects/alpha.md",
            title = "Alpha",
            summary = "Summary",
            category = "projects",
            code = "ALPHA-001",
            tags = listOf("solar", "priority"),
            related = listOf("people/kim.md", "projects/beta.md"),
            updated = "2026-07-11",
            body = "# Alpha\n본문",
        )

        assertEquals(original, json.decodeFromString<WikiPagePayload>(json.encodeToString(original)))
    }

    @Test
    fun observationBehaviorDecodesToolAndErrorAggregates() {
        val payload = json.decodeFromString<ObserveBehavior>(
            """
            {
              "runs":10,"proactiveRuns":3,"compactedRuns":2,
              "totalInputTokens":1000,"totalOutputTokens":200,"cacheReadTokens":50,
              "tools":[{"name":"mail.search","calls":8,"errors":1,"avgMs":250,"repaired":1,"blocked":2}],
              "proactiveDecisions":{"delivered":5,"suppressed:contentless":2},
              "backgroundJobs":{"sync":9},
              "backgroundErrors":{"sync":4,"push":1}
            }
            """.trimIndent(),
        )

        assertEquals(10, payload.runs)
        assertEquals(3, payload.proactiveRuns)
        assertEquals(2, payload.compactedRuns)
        assertEquals(1000L, payload.totalInputTokens)
        assertEquals(8, payload.tools.single().calls)
        assertEquals(250L, payload.tools.single().avgMs)
        assertEquals(1, payload.tools.single().repaired)
        assertEquals(5, payload.proactiveDecisions["delivered"])
        assertEquals(9, payload.backgroundJobs["sync"])
        assertEquals(4, payload.backgroundErrors["sync"])
    }

    @Test
    fun observationHealthDecodesCaptureAndVllmGlance() {
        val payload = json.decodeFromString<ObserveHealth>(
            """
            {
              "captureEnabled":true,"agentLogEnabled":true,
              "ringCapacity":5000,"ringUsed":120,"recentErrors":3,
              "runs24h":40,"proactiveRuns24h":5,"compactedRuns24h":2,"backgroundErrors24h":1,
              "vllmPrefixCache":[{"model":"main","queries":8,"hits":3,"hitRatePct":37.5}]
            }
            """.trimIndent(),
        )

        assertEquals(true, payload.captureEnabled)
        assertEquals(5000, payload.ringCapacity)
        assertEquals(40, payload.runs24h)
        assertEquals(37.5, payload.vllmPrefixCache.single().hitRatePct)
    }

    @Test
    fun observationLogsPreserveOrderAndCountIndependently() {
        val payload = ObserveLogsPayload(
            lines = listOf(
                ObserveLogLine(ts = 1, level = "INFO", msg = "started", runId = "r1", session = "s1"),
                ObserveLogLine(ts = 2, level = "ERROR", msg = "failed", runId = "r1", session = "s1"),
            ),
            count = 99,
        )

        val decoded = json.decodeFromString<ObserveLogsPayload>(json.encodeToString(payload))

        assertEquals(listOf("started", "failed"), decoded.lines.map { it.msg })
        assertEquals(listOf("s1", "s1"), decoded.lines.map { it.session })
        assertEquals(99, decoded.count)
    }

    @Test
    fun envelopeCollectionsRejectObjectShapeInsteadOfSilentlyDroppingIt() {
        assertFailsWith<SerializationException> {
            json.decodeFromString<RecentPayload>("""{"sessions":{"key":"client:main"}}""")
        }
    }
}
