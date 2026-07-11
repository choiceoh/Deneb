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

    @Test
    fun recentPayloadCoercesNullSessionsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            RecentPayload.serializer(),
            """{"sessions":null}""",
        )

        assertEquals(RecentPayload(), decoded)
    }

    @Test
    fun transcriptPayloadCoercesNullMessagesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TranscriptPayload.serializer(),
            """{"messages":null}""",
        )

        assertEquals(TranscriptPayload(), decoded)
    }

    @Test
    fun workFeedPayloadCoercesNullItemsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WorkFeedPayload.serializer(),
            """{"items":null}""",
        )

        assertEquals(WorkFeedPayload(), decoded)
    }

    @Test
    fun workFeedActionRunPayloadCoercesNullOkToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WorkFeedActionRunPayload.serializer(),
            """{"ok":null}""",
        )

        assertEquals(WorkFeedActionRunPayload(), decoded)
    }

    @Test
    fun workFeedActionRunPayloadCoercesNullItemToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WorkFeedActionRunPayload.serializer(),
            """{"item":null}""",
        )

        assertEquals(WorkFeedActionRunPayload(), decoded)
    }

    @Test
    fun workFeedActionRunPayloadCoercesNullSessionKeyToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WorkFeedActionRunPayload.serializer(),
            """{"sessionKey":null}""",
        )

        assertEquals(WorkFeedActionRunPayload(), decoded)
    }

    @Test
    fun workFeedActionRunPayloadCoercesNullPromptToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WorkFeedActionRunPayload.serializer(),
            """{"prompt":null}""",
        )

        assertEquals(WorkFeedActionRunPayload(), decoded)
    }

    @Test
    fun workFeedActionRunPayloadCoercesNullMessageToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WorkFeedActionRunPayload.serializer(),
            """{"message":null}""",
        )

        assertEquals(WorkFeedActionRunPayload(), decoded)
    }

    @Test
    fun workFeedActionRunPayloadCoercesNullRemoveFromFeedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WorkFeedActionRunPayload.serializer(),
            """{"removeFromFeed":null}""",
        )

        assertEquals(WorkFeedActionRunPayload(), decoded)
    }

    @Test
    fun workFeedFeedbackPayloadCoercesNullOkToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WorkFeedFeedbackPayload.serializer(),
            """{"ok":null}""",
        )

        assertEquals(WorkFeedFeedbackPayload(), decoded)
    }

    @Test
    fun workFeedFeedbackPayloadCoercesNullItemToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WorkFeedFeedbackPayload.serializer(),
            """{"item":null}""",
        )

        assertEquals(WorkFeedFeedbackPayload(), decoded)
    }

    @Test
    fun workFeedFeedbackPayloadCoercesNullTextToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WorkFeedFeedbackPayload.serializer(),
            """{"text":null}""",
        )

        assertEquals(WorkFeedFeedbackPayload(), decoded)
    }

    @Test
    fun workFeedFeedbackPayloadCoercesNullSessionKeyToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WorkFeedFeedbackPayload.serializer(),
            """{"sessionKey":null}""",
        )

        assertEquals(WorkFeedFeedbackPayload(), decoded)
    }

    @Test
    fun nativeSyncPayloadCoercesNullEventsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NativeSyncPayload.serializer(),
            """{"events":null}""",
        )

        assertEquals(NativeSyncPayload(), decoded)
    }

    @Test
    fun nativeSyncPayloadCoercesNullCursorToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NativeSyncPayload.serializer(),
            """{"cursor":null}""",
        )

        assertEquals(NativeSyncPayload(), decoded)
    }

    @Test
    fun nativeSyncPayloadCoercesNullLatestSeqToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NativeSyncPayload.serializer(),
            """{"latestSeq":null}""",
        )

        assertEquals(NativeSyncPayload(), decoded)
    }

    @Test
    fun nativeSyncPayloadCoercesNullHasMoreToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NativeSyncPayload.serializer(),
            """{"hasMore":null}""",
        )

        assertEquals(NativeSyncPayload(), decoded)
    }

    @Test
    fun nativeSyncEventCoercesNullSeqToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NativeSyncEvent.serializer(),
            """{"seq":null}""",
        )

        assertEquals(NativeSyncEvent(), decoded)
    }

    @Test
    fun nativeSyncEventCoercesNullTypeToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NativeSyncEvent.serializer(),
            """{"type":null}""",
        )

        assertEquals(NativeSyncEvent(), decoded)
    }

    @Test
    fun nativeSyncEventCoercesNullEntityIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NativeSyncEvent.serializer(),
            """{"entityId":null}""",
        )

        assertEquals(NativeSyncEvent(), decoded)
    }

    @Test
    fun nativeSyncEventCoercesNullSessionKeyToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NativeSyncEvent.serializer(),
            """{"sessionKey":null}""",
        )

        assertEquals(NativeSyncEvent(), decoded)
    }

    @Test
    fun nativeSyncEventCoercesNullWorkFeedItemIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NativeSyncEvent.serializer(),
            """{"workFeedItemId":null}""",
        )

        assertEquals(NativeSyncEvent(), decoded)
    }

    @Test
    fun nativeSyncEventCoercesNullTimestampMsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NativeSyncEvent.serializer(),
            """{"timestampMs":null}""",
        )

        assertEquals(NativeSyncEvent(), decoded)
    }

    @Test
    fun nativeSyncEventCoercesNullPayloadToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NativeSyncEvent.serializer(),
            """{"payload":null}""",
        )

        assertEquals(NativeSyncEvent(), decoded)
    }

    @Test
    fun nativeSyncActionPayloadCoercesNullItemToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NativeSyncActionPayload.serializer(),
            """{"item":null}""",
        )

        assertEquals(NativeSyncActionPayload(), decoded)
    }

    @Test
    fun nativeSyncActionPayloadCoercesNullRemoveFromFeedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NativeSyncActionPayload.serializer(),
            """{"removeFromFeed":null}""",
        )

        assertEquals(NativeSyncActionPayload(), decoded)
    }

    @Test
    fun memoryListPayloadCoercesNullPagesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MemoryListPayload.serializer(),
            """{"pages":null}""",
        )

        assertEquals(MemoryListPayload(), decoded)
    }

    @Test
    fun diaryRecentPayloadCoercesNullEntriesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            DiaryRecentPayload.serializer(),
            """{"entries":null}""",
        )

        assertEquals(DiaryRecentPayload(), decoded)
    }

    @Test
    fun diaryRecentRowCoercesNullFileToDeclaredDefault() {
        val decoded = json.decodeFromString(
            DiaryRecentRow.serializer(),
            """{"file":null}""",
        )

        assertEquals(DiaryRecentRow(), decoded)
    }

    @Test
    fun diaryRecentRowCoercesNullHeaderToDeclaredDefault() {
        val decoded = json.decodeFromString(
            DiaryRecentRow.serializer(),
            """{"header":null}""",
        )

        assertEquals(DiaryRecentRow(), decoded)
    }

    @Test
    fun diaryRecentRowCoercesNullContentToDeclaredDefault() {
        val decoded = json.decodeFromString(
            DiaryRecentRow.serializer(),
            """{"content":null}""",
        )

        assertEquals(DiaryRecentRow(), decoded)
    }

    @Test
    fun diaryRecentRowCoercesNullAtToDeclaredDefault() {
        val decoded = json.decodeFromString(
            DiaryRecentRow.serializer(),
            """{"at":null}""",
        )

        assertEquals(DiaryRecentRow(), decoded)
    }

    @Test
    fun deletePagesPayloadCoercesNullOkToDeclaredDefault() {
        val decoded = json.decodeFromString(
            DeletePagesPayload.serializer(),
            """{"ok":null}""",
        )

        assertEquals(DeletePagesPayload(), decoded)
    }

    @Test
    fun deletePagesPayloadCoercesNullDeletedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            DeletePagesPayload.serializer(),
            """{"deleted":null}""",
        )

        assertEquals(DeletePagesPayload(), decoded)
    }

    @Test
    fun movePagePayloadCoercesNullOkToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MovePagePayload.serializer(),
            """{"ok":null}""",
        )

        assertEquals(MovePagePayload(), decoded)
    }

    @Test
    fun categoriesPayloadCoercesNullCategoriesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CategoriesPayload.serializer(),
            """{"categories":null}""",
        )

        assertEquals(CategoriesPayload(), decoded)
    }

    @Test
    fun categoriesPayloadCoercesNullTotalPagesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CategoriesPayload.serializer(),
            """{"totalPages":null}""",
        )

        assertEquals(CategoriesPayload(), decoded)
    }

    @Test
    fun categoriesPayloadCoercesNullTotalBytesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CategoriesPayload.serializer(),
            """{"totalBytes":null}""",
        )

        assertEquals(CategoriesPayload(), decoded)
    }

    @Test
    fun cronListPayloadCoercesNullJobsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CronListPayload.serializer(),
            """{"jobs":null}""",
        )

        assertEquals(CronListPayload(), decoded)
    }

    @Test
    fun modelsPayloadCoercesNullCurrentToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelsPayload.serializer(),
            """{"current":null}""",
        )

        assertEquals(ModelsPayload(), decoded)
    }

    @Test
    fun modelsPayloadCoercesNullRolesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelsPayload.serializer(),
            """{"roles":null}""",
        )

        assertEquals(ModelsPayload(), decoded)
    }

    @Test
    fun modelsPayloadCoercesNullSectionsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelsPayload.serializer(),
            """{"sections":null}""",
        )

        assertEquals(ModelsPayload(), decoded)
    }

    @Test
    fun modelsPayloadCoercesNullAdvisoriesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelsPayload.serializer(),
            """{"advisories":null}""",
        )

        assertEquals(ModelsPayload(), decoded)
    }

    @Test
    fun clientHelloPayloadCoercesNullVersionToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ClientHelloPayload.serializer(),
            """{"version":null}""",
        )

        assertEquals(ClientHelloPayload(), decoded)
    }

    @Test
    fun clientHelloPayloadCoercesNullNativeApiVersionToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ClientHelloPayload.serializer(),
            """{"nativeApiVersion":null}""",
        )

        assertEquals(ClientHelloPayload(), decoded)
    }

    @Test
    fun clientHelloPayloadCoercesNullModelToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ClientHelloPayload.serializer(),
            """{"model":null}""",
        )

        assertEquals(ClientHelloPayload(), decoded)
    }

    @Test
    fun clientHelloPayloadCoercesNullCapabilitiesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ClientHelloPayload.serializer(),
            """{"capabilities":null}""",
        )

        assertEquals(ClientHelloPayload(), decoded)
    }

    @Test
    fun clientHelloPayloadCoercesNullEndpointsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ClientHelloPayload.serializer(),
            """{"endpoints":null}""",
        )

        assertEquals(ClientHelloPayload(), decoded)
    }

    @Test
    fun clientHelloPayloadCoercesNullTsMsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ClientHelloPayload.serializer(),
            """{"tsMs":null}""",
        )

        assertEquals(ClientHelloPayload(), decoded)
    }

    @Test
    fun mailListPayloadCoercesNullMessagesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailListPayload.serializer(),
            """{"messages":null}""",
        )

        assertEquals(MailListPayload(), decoded)
    }

    @Test
    fun mailListPayloadCoercesNullNextPageTokenToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailListPayload.serializer(),
            """{"nextPageToken":null}""",
        )

        assertEquals(MailListPayload(), decoded)
    }

    @Test
    fun okPayloadCoercesNullOkToDeclaredDefault() {
        val decoded = json.decodeFromString(
            OkPayload.serializer(),
            """{"ok":null}""",
        )

        assertEquals(OkPayload(), decoded)
    }

    @Test
    fun askPayloadCoercesNullAnswerToDeclaredDefault() {
        val decoded = json.decodeFromString(
            AskPayload.serializer(),
            """{"answer":null}""",
        )

        assertEquals(AskPayload(), decoded)
    }

    @Test
    fun senderContextPayloadCoercesNullSenderToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SenderContextPayload.serializer(),
            """{"sender":null}""",
        )

        assertEquals(SenderContextPayload(), decoded)
    }

    @Test
    fun senderContextPayloadCoercesNullEmailToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SenderContextPayload.serializer(),
            """{"email":null}""",
        )

        assertEquals(SenderContextPayload(), decoded)
    }

    @Test
    fun senderContextPayloadCoercesNullDisplayNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SenderContextPayload.serializer(),
            """{"displayName":null}""",
        )

        assertEquals(SenderContextPayload(), decoded)
    }

    @Test
    fun senderContextPayloadCoercesNullRecentToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SenderContextPayload.serializer(),
            """{"recent":null}""",
        )

        assertEquals(SenderContextPayload(), decoded)
    }

    @Test
    fun senderContextPayloadCoercesNullWikiHitsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SenderContextPayload.serializer(),
            """{"wikiHits":null}""",
        )

        assertEquals(SenderContextPayload(), decoded)
    }

    @Test
    fun senderContextPayloadCoercesNullWikiFactsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SenderContextPayload.serializer(),
            """{"wikiFacts":null}""",
        )

        assertEquals(SenderContextPayload(), decoded)
    }

    @Test
    fun calListPayloadCoercesNullEventsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalListPayload.serializer(),
            """{"events":null}""",
        )

        assertEquals(CalListPayload(), decoded)
    }

    @Test
    fun calProposalsPayloadCoercesNullProposalsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalProposalsPayload.serializer(),
            """{"proposals":null}""",
        )

        assertEquals(CalProposalsPayload(), decoded)
    }

    @Test
    fun todoListPayloadCoercesNullTodosToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TodoListPayload.serializer(),
            """{"todos":null}""",
        )

        assertEquals(TodoListPayload(), decoded)
    }

    @Test
    fun peopleListPayloadCoercesNullPeopleToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PeopleListPayload.serializer(),
            """{"people":null}""",
        )

        assertEquals(PeopleListPayload(), decoded)
    }

    @Test
    fun contactsListPayloadCoercesNullContactsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ContactsListPayload.serializer(),
            """{"contacts":null}""",
        )

        assertEquals(ContactsListPayload(), decoded)
    }

    @Test
    fun wikiPagePayloadCoercesNullPathToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WikiPagePayload.serializer(),
            """{"path":null}""",
        )

        assertEquals(WikiPagePayload(), decoded)
    }

    @Test
    fun wikiPagePayloadCoercesNullTitleToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WikiPagePayload.serializer(),
            """{"title":null}""",
        )

        assertEquals(WikiPagePayload(), decoded)
    }

    @Test
    fun wikiPagePayloadCoercesNullSummaryToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WikiPagePayload.serializer(),
            """{"summary":null}""",
        )

        assertEquals(WikiPagePayload(), decoded)
    }

    @Test
    fun wikiPagePayloadCoercesNullCategoryToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WikiPagePayload.serializer(),
            """{"category":null}""",
        )

        assertEquals(WikiPagePayload(), decoded)
    }

    @Test
    fun wikiPagePayloadCoercesNullCodeToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WikiPagePayload.serializer(),
            """{"code":null}""",
        )

        assertEquals(WikiPagePayload(), decoded)
    }

    @Test
    fun wikiPagePayloadCoercesNullRelatedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WikiPagePayload.serializer(),
            """{"related":null}""",
        )

        assertEquals(WikiPagePayload(), decoded)
    }

    @Test
    fun wikiPagePayloadCoercesNullUpdatedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WikiPagePayload.serializer(),
            """{"updated":null}""",
        )

        assertEquals(WikiPagePayload(), decoded)
    }

    @Test
    fun wikiPagePayloadCoercesNullBodyToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WikiPagePayload.serializer(),
            """{"body":null}""",
        )

        assertEquals(WikiPagePayload(), decoded)
    }

    @Test
    fun captureImagePayloadCoercesNullTextToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CaptureImagePayload.serializer(),
            """{"text":null}""",
        )

        assertEquals(CaptureImagePayload(), decoded)
    }

    @Test
    fun captureAudioPayloadCoercesNullTextToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CaptureAudioPayload.serializer(),
            """{"text":null}""",
        )

        assertEquals(CaptureAudioPayload(), decoded)
    }

    @Test
    fun captureDocumentPayloadCoercesNullTextToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CaptureDocumentPayload.serializer(),
            """{"text":null}""",
        )

        assertEquals(CaptureDocumentPayload(), decoded)
    }

    @Test
    fun captureContactsPayloadCoercesNullTextToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CaptureContactsPayload.serializer(),
            """{"text":null}""",
        )

        assertEquals(CaptureContactsPayload(), decoded)
    }

    @Test
    fun observeToolStatCoercesNullNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ObserveToolStat.serializer(),
            """{"name":null}""",
        )

        assertEquals(ObserveToolStat(), decoded)
    }

    @Test
    fun observeToolStatCoercesNullCallsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ObserveToolStat.serializer(),
            """{"calls":null}""",
        )

        assertEquals(ObserveToolStat(), decoded)
    }

    @Test
    fun observeToolStatCoercesNullErrorsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ObserveToolStat.serializer(),
            """{"errors":null}""",
        )

        assertEquals(ObserveToolStat(), decoded)
    }

    @Test
    fun observeToolStatCoercesNullAvgMsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ObserveToolStat.serializer(),
            """{"avgMs":null}""",
        )

        assertEquals(ObserveToolStat(), decoded)
    }

    @Test
    fun observeBehaviorCoercesNullRunsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ObserveBehavior.serializer(),
            """{"runs":null}""",
        )

        assertEquals(ObserveBehavior(), decoded)
    }

    @Test
    fun observeBehaviorCoercesNullProactiveRunsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ObserveBehavior.serializer(),
            """{"proactiveRuns":null}""",
        )

        assertEquals(ObserveBehavior(), decoded)
    }

    @Test
    fun observeBehaviorCoercesNullCompactedRunsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ObserveBehavior.serializer(),
            """{"compactedRuns":null}""",
        )

        assertEquals(ObserveBehavior(), decoded)
    }

    @Test
    fun observeBehaviorCoercesNullToolsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ObserveBehavior.serializer(),
            """{"tools":null}""",
        )

        assertEquals(ObserveBehavior(), decoded)
    }

    @Test
    fun observeBehaviorCoercesNullBackgroundErrorsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ObserveBehavior.serializer(),
            """{"backgroundErrors":null}""",
        )

        assertEquals(ObserveBehavior(), decoded)
    }

    @Test
    fun observeLogLineCoercesNullLevelToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ObserveLogLine.serializer(),
            """{"level":null}""",
        )

        assertEquals(ObserveLogLine(), decoded)
    }

    @Test
    fun observeLogLineCoercesNullMsgToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ObserveLogLine.serializer(),
            """{"msg":null}""",
        )

        assertEquals(ObserveLogLine(), decoded)
    }

    @Test
    fun observeLogLineCoercesNullRunIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ObserveLogLine.serializer(),
            """{"runId":null}""",
        )

        assertEquals(ObserveLogLine(), decoded)
    }

    @Test
    fun observeLogsPayloadCoercesNullLinesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ObserveLogsPayload.serializer(),
            """{"lines":null}""",
        )

        assertEquals(ObserveLogsPayload(), decoded)
    }

    @Test
    fun observeLogsPayloadCoercesNullCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ObserveLogsPayload.serializer(),
            """{"count":null}""",
        )

        assertEquals(ObserveLogsPayload(), decoded)
    }
}
