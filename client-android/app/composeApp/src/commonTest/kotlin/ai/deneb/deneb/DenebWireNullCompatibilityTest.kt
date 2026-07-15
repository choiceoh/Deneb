package ai.deneb.deneb

import kotlinx.serialization.json.Json
import kotlin.test.Test
import kotlin.test.assertEquals

/**
 * Explicit-null compatibility for hand-written mini-app envelopes.
 *
 * These types are not regenerated automatically, so each defaulted field is pinned
 * against Go nil/null payloads independently.
 */
class DenebWireNullCompatibilityTest {
    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
    }

    private fun nullCoercionCases(): List<() -> Unit> = listOf(
        {
            val decoded = json.decodeFromString(
                RecentPayload.serializer(),
                """{"sessions":null}""",
            )

            assertEquals(RecentPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                TranscriptPayload.serializer(),
                """{"messages":null}""",
            )

            assertEquals(TranscriptPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                WorkFeedPayload.serializer(),
                """{"items":null}""",
            )

            assertEquals(WorkFeedPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                WorkFeedActionRunPayload.serializer(),
                """{"ok":null}""",
            )

            assertEquals(WorkFeedActionRunPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                WorkFeedActionRunPayload.serializer(),
                """{"item":null}""",
            )

            assertEquals(WorkFeedActionRunPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                WorkFeedActionRunPayload.serializer(),
                """{"sessionKey":null}""",
            )

            assertEquals(WorkFeedActionRunPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                WorkFeedActionRunPayload.serializer(),
                """{"prompt":null}""",
            )

            assertEquals(WorkFeedActionRunPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                WorkFeedActionRunPayload.serializer(),
                """{"message":null}""",
            )

            assertEquals(WorkFeedActionRunPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                WorkFeedActionRunPayload.serializer(),
                """{"removeFromFeed":null}""",
            )

            assertEquals(WorkFeedActionRunPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                WorkFeedFeedbackPayload.serializer(),
                """{"ok":null}""",
            )

            assertEquals(WorkFeedFeedbackPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                WorkFeedFeedbackPayload.serializer(),
                """{"item":null}""",
            )

            assertEquals(WorkFeedFeedbackPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                WorkFeedFeedbackPayload.serializer(),
                """{"text":null}""",
            )

            assertEquals(WorkFeedFeedbackPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                WorkFeedFeedbackPayload.serializer(),
                """{"sessionKey":null}""",
            )

            assertEquals(WorkFeedFeedbackPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                NativeSyncPayload.serializer(),
                """{"events":null}""",
            )

            assertEquals(NativeSyncPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                NativeSyncPayload.serializer(),
                """{"cursor":null}""",
            )

            assertEquals(NativeSyncPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                NativeSyncPayload.serializer(),
                """{"latestSeq":null}""",
            )

            assertEquals(NativeSyncPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                NativeSyncPayload.serializer(),
                """{"hasMore":null}""",
            )

            assertEquals(NativeSyncPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                NativeSyncEvent.serializer(),
                """{"seq":null}""",
            )

            assertEquals(NativeSyncEvent(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                NativeSyncEvent.serializer(),
                """{"type":null}""",
            )

            assertEquals(NativeSyncEvent(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                NativeSyncEvent.serializer(),
                """{"entityId":null}""",
            )

            assertEquals(NativeSyncEvent(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                NativeSyncEvent.serializer(),
                """{"sessionKey":null}""",
            )

            assertEquals(NativeSyncEvent(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                NativeSyncEvent.serializer(),
                """{"workFeedItemId":null}""",
            )

            assertEquals(NativeSyncEvent(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                NativeSyncEvent.serializer(),
                """{"timestampMs":null}""",
            )

            assertEquals(NativeSyncEvent(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                NativeSyncEvent.serializer(),
                """{"payload":null}""",
            )

            assertEquals(NativeSyncEvent(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                NativeSyncActionPayload.serializer(),
                """{"item":null}""",
            )

            assertEquals(NativeSyncActionPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                NativeSyncActionPayload.serializer(),
                """{"removeFromFeed":null}""",
            )

            assertEquals(NativeSyncActionPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                MemoryListPayload.serializer(),
                """{"pages":null}""",
            )

            assertEquals(MemoryListPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                DiaryRecentPayload.serializer(),
                """{"entries":null}""",
            )

            assertEquals(DiaryRecentPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                DiaryRecentRow.serializer(),
                """{"file":null}""",
            )

            assertEquals(DiaryRecentRow(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                DiaryRecentRow.serializer(),
                """{"header":null}""",
            )

            assertEquals(DiaryRecentRow(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                DiaryRecentRow.serializer(),
                """{"content":null}""",
            )

            assertEquals(DiaryRecentRow(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                DiaryRecentRow.serializer(),
                """{"at":null}""",
            )

            assertEquals(DiaryRecentRow(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                DeletePagesPayload.serializer(),
                """{"ok":null}""",
            )

            assertEquals(DeletePagesPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                DeletePagesPayload.serializer(),
                """{"deleted":null}""",
            )

            assertEquals(DeletePagesPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                MovePagePayload.serializer(),
                """{"ok":null}""",
            )

            assertEquals(MovePagePayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                CategoriesPayload.serializer(),
                """{"categories":null}""",
            )

            assertEquals(CategoriesPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                CategoriesPayload.serializer(),
                """{"totalPages":null}""",
            )

            assertEquals(CategoriesPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                CategoriesPayload.serializer(),
                """{"totalBytes":null}""",
            )

            assertEquals(CategoriesPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                CronListPayload.serializer(),
                """{"jobs":null}""",
            )

            assertEquals(CronListPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ModelsPayload.serializer(),
                """{"current":null}""",
            )

            assertEquals(ModelsPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ModelsPayload.serializer(),
                """{"roles":null}""",
            )

            assertEquals(ModelsPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ModelsPayload.serializer(),
                """{"sections":null}""",
            )

            assertEquals(ModelsPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ModelsPayload.serializer(),
                """{"advisories":null}""",
            )

            assertEquals(ModelsPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ClientHelloPayload.serializer(),
                """{"version":null}""",
            )

            assertEquals(ClientHelloPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ClientHelloPayload.serializer(),
                """{"nativeApiVersion":null}""",
            )

            assertEquals(ClientHelloPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ClientHelloPayload.serializer(),
                """{"model":null}""",
            )

            assertEquals(ClientHelloPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ClientHelloPayload.serializer(),
                """{"capabilities":null}""",
            )

            assertEquals(ClientHelloPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ClientHelloPayload.serializer(),
                """{"endpoints":null}""",
            )

            assertEquals(ClientHelloPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ClientHelloPayload.serializer(),
                """{"tsMs":null}""",
            )

            assertEquals(ClientHelloPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                MailListPayload.serializer(),
                """{"messages":null}""",
            )

            assertEquals(MailListPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                MailListPayload.serializer(),
                """{"nextPageToken":null}""",
            )

            assertEquals(MailListPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                OkPayload.serializer(),
                """{"ok":null}""",
            )

            assertEquals(OkPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                AskPayload.serializer(),
                """{"answer":null}""",
            )

            assertEquals(AskPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                SenderContextPayload.serializer(),
                """{"sender":null}""",
            )

            assertEquals(SenderContextPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                SenderContextPayload.serializer(),
                """{"email":null}""",
            )

            assertEquals(SenderContextPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                SenderContextPayload.serializer(),
                """{"displayName":null}""",
            )

            assertEquals(SenderContextPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                SenderContextPayload.serializer(),
                """{"recent":null}""",
            )

            assertEquals(SenderContextPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                SenderContextPayload.serializer(),
                """{"wikiHits":null}""",
            )

            assertEquals(SenderContextPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                SenderContextPayload.serializer(),
                """{"wikiFacts":null}""",
            )

            assertEquals(SenderContextPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                CalListPayload.serializer(),
                """{"events":null}""",
            )

            assertEquals(CalListPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                CalProposalsPayload.serializer(),
                """{"proposals":null}""",
            )

            assertEquals(CalProposalsPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                TodoListPayload.serializer(),
                """{"todos":null}""",
            )

            assertEquals(TodoListPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                PeopleListPayload.serializer(),
                """{"people":null}""",
            )

            assertEquals(PeopleListPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ContactsListPayload.serializer(),
                """{"contacts":null}""",
            )

            assertEquals(ContactsListPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                WikiPagePayload.serializer(),
                """{"path":null}""",
            )

            assertEquals(WikiPagePayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                WikiPagePayload.serializer(),
                """{"title":null}""",
            )

            assertEquals(WikiPagePayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                WikiPagePayload.serializer(),
                """{"summary":null}""",
            )

            assertEquals(WikiPagePayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                WikiPagePayload.serializer(),
                """{"category":null}""",
            )

            assertEquals(WikiPagePayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                WikiPagePayload.serializer(),
                """{"code":null}""",
            )

            assertEquals(WikiPagePayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                WikiPagePayload.serializer(),
                """{"related":null}""",
            )

            assertEquals(WikiPagePayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                WikiPagePayload.serializer(),
                """{"updated":null}""",
            )

            assertEquals(WikiPagePayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                WikiPagePayload.serializer(),
                """{"body":null}""",
            )

            assertEquals(WikiPagePayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                CaptureImagePayload.serializer(),
                """{"text":null}""",
            )

            assertEquals(CaptureImagePayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                CaptureAudioPayload.serializer(),
                """{"text":null}""",
            )

            assertEquals(CaptureAudioPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                CaptureDocumentPayload.serializer(),
                """{"text":null}""",
            )

            assertEquals(CaptureDocumentPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                CaptureContactsPayload.serializer(),
                """{"text":null}""",
            )

            assertEquals(CaptureContactsPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveToolStat.serializer(),
                """{"name":null}""",
            )

            assertEquals(ObserveToolStat(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveToolStat.serializer(),
                """{"calls":null}""",
            )

            assertEquals(ObserveToolStat(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveToolStat.serializer(),
                """{"errors":null}""",
            )

            assertEquals(ObserveToolStat(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveToolStat.serializer(),
                """{"avgMs":null}""",
            )

            assertEquals(ObserveToolStat(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveToolStat.serializer(),
                """{"repaired":null}""",
            )

            assertEquals(ObserveToolStat(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveToolStat.serializer(),
                """{"unknown":null}""",
            )

            assertEquals(ObserveToolStat(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveToolStat.serializer(),
                """{"blocked":null}""",
            )

            assertEquals(ObserveToolStat(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveToolStat.serializer(),
                """{"cacheHits":null}""",
            )

            assertEquals(ObserveToolStat(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveToolStat.serializer(),
                """{"truncated":null}""",
            )

            assertEquals(ObserveToolStat(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveBehavior.serializer(),
                """{"runs":null}""",
            )

            assertEquals(ObserveBehavior(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveBehavior.serializer(),
                """{"proactiveRuns":null}""",
            )

            assertEquals(ObserveBehavior(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveBehavior.serializer(),
                """{"compactedRuns":null}""",
            )

            assertEquals(ObserveBehavior(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveBehavior.serializer(),
                """{"totalInputTokens":null}""",
            )

            assertEquals(ObserveBehavior(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveBehavior.serializer(),
                """{"totalOutputTokens":null}""",
            )

            assertEquals(ObserveBehavior(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveBehavior.serializer(),
                """{"cacheReadTokens":null}""",
            )

            assertEquals(ObserveBehavior(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveBehavior.serializer(),
                """{"tools":null}""",
            )

            assertEquals(ObserveBehavior(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveBehavior.serializer(),
                """{"proactiveDecisions":null}""",
            )

            assertEquals(ObserveBehavior(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveBehavior.serializer(),
                """{"backgroundJobs":null}""",
            )

            assertEquals(ObserveBehavior(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveBehavior.serializer(),
                """{"backgroundErrors":null}""",
            )

            assertEquals(ObserveBehavior(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveLogLine.serializer(),
                """{"ts":null}""",
            )

            assertEquals(ObserveLogLine(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveLogLine.serializer(),
                """{"level":null}""",
            )

            assertEquals(ObserveLogLine(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveLogLine.serializer(),
                """{"msg":null}""",
            )

            assertEquals(ObserveLogLine(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveLogLine.serializer(),
                """{"runId":null}""",
            )

            assertEquals(ObserveLogLine(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveLogLine.serializer(),
                """{"session":null}""",
            )

            assertEquals(ObserveLogLine(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveLogsPayload.serializer(),
                """{"lines":null}""",
            )

            assertEquals(ObserveLogsPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveLogsPayload.serializer(),
                """{"count":null}""",
            )

            assertEquals(ObserveLogsPayload(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveVllmPrefixCache.serializer(),
                """{"model":null}""",
            )

            assertEquals(ObserveVllmPrefixCache(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveVllmPrefixCache.serializer(),
                """{"queries":null}""",
            )

            assertEquals(ObserveVllmPrefixCache(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveVllmPrefixCache.serializer(),
                """{"hits":null}""",
            )

            assertEquals(ObserveVllmPrefixCache(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveVllmPrefixCache.serializer(),
                """{"hitRatePct":null}""",
            )

            assertEquals(ObserveVllmPrefixCache(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveHealth.serializer(),
                """{"captureEnabled":null}""",
            )

            assertEquals(ObserveHealth(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveHealth.serializer(),
                """{"agentLogEnabled":null}""",
            )

            assertEquals(ObserveHealth(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveHealth.serializer(),
                """{"ringCapacity":null}""",
            )

            assertEquals(ObserveHealth(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveHealth.serializer(),
                """{"ringUsed":null}""",
            )

            assertEquals(ObserveHealth(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveHealth.serializer(),
                """{"recentErrors":null}""",
            )

            assertEquals(ObserveHealth(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveHealth.serializer(),
                """{"runs24h":null}""",
            )

            assertEquals(ObserveHealth(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveHealth.serializer(),
                """{"proactiveRuns24h":null}""",
            )

            assertEquals(ObserveHealth(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveHealth.serializer(),
                """{"compactedRuns24h":null}""",
            )

            assertEquals(ObserveHealth(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveHealth.serializer(),
                """{"backgroundErrors24h":null}""",
            )

            assertEquals(ObserveHealth(), decoded)
        },
        {
            val decoded = json.decodeFromString(
                ObserveHealth.serializer(),
                """{"vllmPrefixCache":null}""",
            )

            assertEquals(ObserveHealth(), decoded)
        },
    )

    @Test
    fun wirePayloadsCoerceNullFieldsToDeclaredDefaults() {
        nullCoercionCases().forEach { it() }
    }
}
