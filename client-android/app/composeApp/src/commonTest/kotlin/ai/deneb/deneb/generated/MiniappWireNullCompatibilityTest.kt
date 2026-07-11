package ai.deneb.deneb.generated

import kotlinx.serialization.json.Json
import kotlin.test.Test
import kotlin.test.assertEquals

/**
 * Field-by-field explicit-null compatibility for generated Go/Kotlin contracts.
 *
 * Go commonly emits nil slices, maps, and optional nested structs as JSON null.
 * Every generated field has a declared Kotlin default specifically so those nulls
 * remain readable across rolling gateway/client upgrades. Testing each field
 * independently identifies the exact contract that regressed instead of hiding it
 * inside one giant fixture.
 */
class MiniappWireNullCompatibilityTest {
    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
    }

    @Test
    fun calendarAttendeeOutCoercesNullEmailToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarAttendeeOut.serializer(),
            """{"email":null}""",
        )

        assertEquals(CalendarAttendeeOut(), decoded)
    }

    @Test
    fun calendarAttendeeOutCoercesNullDisplayNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarAttendeeOut.serializer(),
            """{"displayName":null}""",
        )

        assertEquals(CalendarAttendeeOut(), decoded)
    }

    @Test
    fun calendarAttendeeOutCoercesNullResponseStatusToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarAttendeeOut.serializer(),
            """{"responseStatus":null}""",
        )

        assertEquals(CalendarAttendeeOut(), decoded)
    }

    @Test
    fun calendarAttendeeOutCoercesNullSelfToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarAttendeeOut.serializer(),
            """{"self":null}""",
        )

        assertEquals(CalendarAttendeeOut(), decoded)
    }

    @Test
    fun calendarAttendeeOutCoercesNullOrganizerToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarAttendeeOut.serializer(),
            """{"organizer":null}""",
        )

        assertEquals(CalendarAttendeeOut(), decoded)
    }

    @Test
    fun calendarConferenceOutCoercesNullSolutionToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarConferenceOut.serializer(),
            """{"solution":null}""",
        )

        assertEquals(CalendarConferenceOut(), decoded)
    }

    @Test
    fun calendarConferenceOutCoercesNullUriToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarConferenceOut.serializer(),
            """{"uri":null}""",
        )

        assertEquals(CalendarConferenceOut(), decoded)
    }

    @Test
    fun calendarEventOutCoercesNullIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarEventOut.serializer(),
            """{"id":null}""",
        )

        assertEquals(CalendarEventOut(), decoded)
    }

    @Test
    fun calendarEventOutCoercesNullSummaryToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarEventOut.serializer(),
            """{"summary":null}""",
        )

        assertEquals(CalendarEventOut(), decoded)
    }

    @Test
    fun calendarEventOutCoercesNullDescriptionToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarEventOut.serializer(),
            """{"description":null}""",
        )

        assertEquals(CalendarEventOut(), decoded)
    }

    @Test
    fun calendarEventOutCoercesNullLocationToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarEventOut.serializer(),
            """{"location":null}""",
        )

        assertEquals(CalendarEventOut(), decoded)
    }

    @Test
    fun calendarEventOutCoercesNullStartToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarEventOut.serializer(),
            """{"start":null}""",
        )

        assertEquals(CalendarEventOut(), decoded)
    }

    @Test
    fun calendarEventOutCoercesNullEndToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarEventOut.serializer(),
            """{"end":null}""",
        )

        assertEquals(CalendarEventOut(), decoded)
    }

    @Test
    fun calendarEventOutCoercesNullAllDayToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarEventOut.serializer(),
            """{"allDay":null}""",
        )

        assertEquals(CalendarEventOut(), decoded)
    }

    @Test
    fun calendarEventOutCoercesNullStatusToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarEventOut.serializer(),
            """{"status":null}""",
        )

        assertEquals(CalendarEventOut(), decoded)
    }

    @Test
    fun calendarEventOutCoercesNullHtmlLinkToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarEventOut.serializer(),
            """{"htmlLink":null}""",
        )

        assertEquals(CalendarEventOut(), decoded)
    }

    @Test
    fun calendarEventOutCoercesNullLocalToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarEventOut.serializer(),
            """{"local":null}""",
        )

        assertEquals(CalendarEventOut(), decoded)
    }

    @Test
    fun calendarEventOutCoercesNullCategoryToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarEventOut.serializer(),
            """{"category":null}""",
        )

        assertEquals(CalendarEventOut(), decoded)
    }

    @Test
    fun calendarEventOutCoercesNullOrganizerToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarEventOut.serializer(),
            """{"organizer":null}""",
        )

        assertEquals(CalendarEventOut(), decoded)
    }

    @Test
    fun calendarEventOutCoercesNullAttendeesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarEventOut.serializer(),
            """{"attendees":null}""",
        )

        assertEquals(CalendarEventOut(), decoded)
    }

    @Test
    fun calendarEventOutCoercesNullConferenceToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarEventOut.serializer(),
            """{"conference":null}""",
        )

        assertEquals(CalendarEventOut(), decoded)
    }

    @Test
    fun calendarEventOutCoercesNullHasMeetToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarEventOut.serializer(),
            """{"hasMeet":null}""",
        )

        assertEquals(CalendarEventOut(), decoded)
    }

    @Test
    fun calendarProposalOutCoercesNullIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarProposalOut.serializer(),
            """{"id":null}""",
        )

        assertEquals(CalendarProposalOut(), decoded)
    }

    @Test
    fun calendarProposalOutCoercesNullTitleToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarProposalOut.serializer(),
            """{"title":null}""",
        )

        assertEquals(CalendarProposalOut(), decoded)
    }

    @Test
    fun calendarProposalOutCoercesNullStartToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarProposalOut.serializer(),
            """{"start":null}""",
        )

        assertEquals(CalendarProposalOut(), decoded)
    }

    @Test
    fun calendarProposalOutCoercesNullAllDayToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarProposalOut.serializer(),
            """{"allDay":null}""",
        )

        assertEquals(CalendarProposalOut(), decoded)
    }

    @Test
    fun calendarProposalOutCoercesNullKindToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarProposalOut.serializer(),
            """{"kind":null}""",
        )

        assertEquals(CalendarProposalOut(), decoded)
    }

    @Test
    fun calendarProposalOutCoercesNullSourceSubjectToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarProposalOut.serializer(),
            """{"sourceSubject":null}""",
        )

        assertEquals(CalendarProposalOut(), decoded)
    }

    @Test
    fun calendarProposalOutCoercesNullSourceFromToDeclaredDefault() {
        val decoded = json.decodeFromString(
            CalendarProposalOut.serializer(),
            """{"sourceFrom":null}""",
        )

        assertEquals(CalendarProposalOut(), decoded)
    }

    @Test
    fun contactRowCoercesNullNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ContactRow.serializer(),
            """{"name":null}""",
        )

        assertEquals(ContactRow(), decoded)
    }

    @Test
    fun contactRowCoercesNullPhonesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ContactRow.serializer(),
            """{"phones":null}""",
        )

        assertEquals(ContactRow(), decoded)
    }

    @Test
    fun contactRowCoercesNullEmailsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ContactRow.serializer(),
            """{"emails":null}""",
        )

        assertEquals(ContactRow(), decoded)
    }

    @Test
    fun contactRowCoercesNullOrgToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ContactRow.serializer(),
            """{"org":null}""",
        )

        assertEquals(ContactRow(), decoded)
    }

    @Test
    fun dashboardItemCoercesNullTitleToDeclaredDefault() {
        val decoded = json.decodeFromString(
            DashboardItem.serializer(),
            """{"title":null}""",
        )

        assertEquals(DashboardItem(), decoded)
    }

    @Test
    fun dashboardItemCoercesNullSubtitleToDeclaredDefault() {
        val decoded = json.decodeFromString(
            DashboardItem.serializer(),
            """{"subtitle":null}""",
        )

        assertEquals(DashboardItem(), decoded)
    }

    @Test
    fun dashboardItemCoercesNullSourceToDeclaredDefault() {
        val decoded = json.decodeFromString(
            DashboardItem.serializer(),
            """{"source":null}""",
        )

        assertEquals(DashboardItem(), decoded)
    }

    @Test
    fun dashboardItemCoercesNullRefTypeToDeclaredDefault() {
        val decoded = json.decodeFromString(
            DashboardItem.serializer(),
            """{"refType":null}""",
        )

        assertEquals(DashboardItem(), decoded)
    }

    @Test
    fun dashboardItemCoercesNullRefIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            DashboardItem.serializer(),
            """{"refId":null}""",
        )

        assertEquals(DashboardItem(), decoded)
    }

    @Test
    fun dashboardItemCoercesNullWhenMsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            DashboardItem.serializer(),
            """{"whenMs":null}""",
        )

        assertEquals(DashboardItem(), decoded)
    }

    @Test
    fun dashboardOutCoercesNullLanesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            DashboardOut.serializer(),
            """{"lanes":null}""",
        )

        assertEquals(DashboardOut(), decoded)
    }

    @Test
    fun filesEntryOutCoercesNullTagToDeclaredDefault() {
        val decoded = json.decodeFromString(
            FilesEntryOut.serializer(),
            """{"tag":null}""",
        )

        assertEquals(FilesEntryOut(), decoded)
    }

    @Test
    fun filesEntryOutCoercesNullNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            FilesEntryOut.serializer(),
            """{"name":null}""",
        )

        assertEquals(FilesEntryOut(), decoded)
    }

    @Test
    fun filesEntryOutCoercesNullPathDisplayToDeclaredDefault() {
        val decoded = json.decodeFromString(
            FilesEntryOut.serializer(),
            """{"pathDisplay":null}""",
        )

        assertEquals(FilesEntryOut(), decoded)
    }

    @Test
    fun filesEntryOutCoercesNullPathLowerToDeclaredDefault() {
        val decoded = json.decodeFromString(
            FilesEntryOut.serializer(),
            """{"pathLower":null}""",
        )

        assertEquals(FilesEntryOut(), decoded)
    }

    @Test
    fun filesEntryOutCoercesNullIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            FilesEntryOut.serializer(),
            """{"id":null}""",
        )

        assertEquals(FilesEntryOut(), decoded)
    }

    @Test
    fun filesEntryOutCoercesNullSizeToDeclaredDefault() {
        val decoded = json.decodeFromString(
            FilesEntryOut.serializer(),
            """{"size":null}""",
        )

        assertEquals(FilesEntryOut(), decoded)
    }

    @Test
    fun filesEntryOutCoercesNullServerModifiedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            FilesEntryOut.serializer(),
            """{"serverModified":null}""",
        )

        assertEquals(FilesEntryOut(), decoded)
    }

    @Test
    fun filesListOutCoercesNullEntriesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            FilesListOut.serializer(),
            """{"entries":null}""",
        )

        assertEquals(FilesListOut(), decoded)
    }

    @Test
    fun filesListOutCoercesNullPathToDeclaredDefault() {
        val decoded = json.decodeFromString(
            FilesListOut.serializer(),
            """{"path":null}""",
        )

        assertEquals(FilesListOut(), decoded)
    }

    @Test
    fun filesShareOutCoercesNullUrlToDeclaredDefault() {
        val decoded = json.decodeFromString(
            FilesShareOut.serializer(),
            """{"url":null}""",
        )

        assertEquals(FilesShareOut(), decoded)
    }

    @Test
    fun filesUploadOutCoercesNullEntryToDeclaredDefault() {
        val decoded = json.decodeFromString(
            FilesUploadOut.serializer(),
            """{"entry":null}""",
        )

        assertEquals(FilesUploadOut(), decoded)
    }

    @Test
    fun laneOutCoercesNullKeyToDeclaredDefault() {
        val decoded = json.decodeFromString(
            LaneOut.serializer(),
            """{"key":null}""",
        )

        assertEquals(LaneOut(), decoded)
    }

    @Test
    fun laneOutCoercesNullNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            LaneOut.serializer(),
            """{"name":null}""",
        )

        assertEquals(LaneOut(), decoded)
    }

    @Test
    fun laneOutCoercesNullItemsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            LaneOut.serializer(),
            """{"items":null}""",
        )

        assertEquals(LaneOut(), decoded)
    }

    @Test
    fun mailAnalysisOutCoercesNullIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailAnalysisOut.serializer(),
            """{"id":null}""",
        )

        assertEquals(MailAnalysisOut(), decoded)
    }

    @Test
    fun mailAnalysisOutCoercesNullSubjectToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailAnalysisOut.serializer(),
            """{"subject":null}""",
        )

        assertEquals(MailAnalysisOut(), decoded)
    }

    @Test
    fun mailAnalysisOutCoercesNullFromToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailAnalysisOut.serializer(),
            """{"from":null}""",
        )

        assertEquals(MailAnalysisOut(), decoded)
    }

    @Test
    fun mailAnalysisOutCoercesNullDateToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailAnalysisOut.serializer(),
            """{"date":null}""",
        )

        assertEquals(MailAnalysisOut(), decoded)
    }

    @Test
    fun mailAnalysisOutCoercesNullAnalysisToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailAnalysisOut.serializer(),
            """{"analysis":null}""",
        )

        assertEquals(MailAnalysisOut(), decoded)
    }

    @Test
    fun mailAnalysisOutCoercesNullRelatedProjectsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailAnalysisOut.serializer(),
            """{"relatedProjects":null}""",
        )

        assertEquals(MailAnalysisOut(), decoded)
    }

    @Test
    fun mailAnalysisOutCoercesNullDurationMsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailAnalysisOut.serializer(),
            """{"durationMs":null}""",
        )

        assertEquals(MailAnalysisOut(), decoded)
    }

    @Test
    fun mailAnalysisOutCoercesNullCachedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailAnalysisOut.serializer(),
            """{"cached":null}""",
        )

        assertEquals(MailAnalysisOut(), decoded)
    }

    @Test
    fun mailAnalysisOutCoercesNullCreatedAtToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailAnalysisOut.serializer(),
            """{"createdAt":null}""",
        )

        assertEquals(MailAnalysisOut(), decoded)
    }

    @Test
    fun mailAnalysisOutCoercesNullAnalysisStatusToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailAnalysisOut.serializer(),
            """{"analysisStatus":null}""",
        )

        assertEquals(MailAnalysisOut(), decoded)
    }

    @Test
    fun mailAnalysisOutCoercesNullAnalysisQualityToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailAnalysisOut.serializer(),
            """{"analysisQuality":null}""",
        )

        assertEquals(MailAnalysisOut(), decoded)
    }

    @Test
    fun mailAnalysisOutCoercesNullFeedStatusToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailAnalysisOut.serializer(),
            """{"feedStatus":null}""",
        )

        assertEquals(MailAnalysisOut(), decoded)
    }

    @Test
    fun mailAnalysisOutCoercesNullCalendarProposalCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailAnalysisOut.serializer(),
            """{"calendarProposalCount":null}""",
        )

        assertEquals(MailAnalysisOut(), decoded)
    }

    @Test
    fun mailAnalysisOutCoercesNullTodoCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailAnalysisOut.serializer(),
            """{"todoCount":null}""",
        )

        assertEquals(MailAnalysisOut(), decoded)
    }

    @Test
    fun mailAnalysisOutCoercesNullWorkStateHintToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailAnalysisOut.serializer(),
            """{"workStateHint":null}""",
        )

        assertEquals(MailAnalysisOut(), decoded)
    }

    @Test
    fun mailAttachmentOutCoercesNullIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailAttachmentOut.serializer(),
            """{"id":null}""",
        )

        assertEquals(MailAttachmentOut(), decoded)
    }

    @Test
    fun mailAttachmentOutCoercesNullFilenameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailAttachmentOut.serializer(),
            """{"filename":null}""",
        )

        assertEquals(MailAttachmentOut(), decoded)
    }

    @Test
    fun mailAttachmentOutCoercesNullMimeTypeToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailAttachmentOut.serializer(),
            """{"mimeType":null}""",
        )

        assertEquals(MailAttachmentOut(), decoded)
    }

    @Test
    fun mailAttachmentOutCoercesNullSizeToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailAttachmentOut.serializer(),
            """{"size":null}""",
        )

        assertEquals(MailAttachmentOut(), decoded)
    }

    @Test
    fun mailAttachmentOutCoercesNullTruncatedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailAttachmentOut.serializer(),
            """{"truncated":null}""",
        )

        assertEquals(MailAttachmentOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"id":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullThreadIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"threadId":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullFromToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"from":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullToToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"to":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullCcToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"cc":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullSubjectToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"subject":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullDateToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"date":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullIsUnreadToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"isUnread":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullBodyToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"body":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullBodyTotalToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"bodyTotal":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullRawBodyToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"rawBody":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullRawBodyTotalToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"rawBodyTotal":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullBodyCleanedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"bodyCleaned":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullBodyHiddenBlockCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"bodyHiddenBlockCount":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullBodyHiddenLineCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"bodyHiddenLineCount":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullLabelsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"labels":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullAttachmentsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"attachments":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullAnalysisStatusToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"analysisStatus":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullAnalysisQualityToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"analysisQuality":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullFeedStatusToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"feedStatus":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullCalendarProposalCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"calendarProposalCount":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullTodoCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"todoCount":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullWorkStateHintToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"workStateHint":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailMessageOutCoercesNullRelatedProjectsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailMessageOut.serializer(),
            """{"relatedProjects":null}""",
        )

        assertEquals(MailMessageOut(), decoded)
    }

    @Test
    fun mailNativeMailboxOutCoercesNullNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativeMailboxOut.serializer(),
            """{"name":null}""",
        )

        assertEquals(MailNativeMailboxOut(), decoded)
    }

    @Test
    fun mailNativeMailboxOutCoercesNullTotalToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativeMailboxOut.serializer(),
            """{"total":null}""",
        )

        assertEquals(MailNativeMailboxOut(), decoded)
    }

    @Test
    fun mailNativeMailboxOutCoercesNullUnreadToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativeMailboxOut.serializer(),
            """{"unread":null}""",
        )

        assertEquals(MailNativeMailboxOut(), decoded)
    }

    @Test
    fun mailNativeMailboxOutCoercesNullLocallyReadToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativeMailboxOut.serializer(),
            """{"locallyRead":null}""",
        )

        assertEquals(MailNativeMailboxOut(), decoded)
    }

    @Test
    fun mailNativeMailboxOutCoercesNullLocallyArchivedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativeMailboxOut.serializer(),
            """{"locallyArchived":null}""",
        )

        assertEquals(MailNativeMailboxOut(), decoded)
    }

    @Test
    fun mailNativeMailboxOutCoercesNullLocallyTrashedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativeMailboxOut.serializer(),
            """{"locallyTrashed":null}""",
        )

        assertEquals(MailNativeMailboxOut(), decoded)
    }

    @Test
    fun mailNativeMailboxOutCoercesNullLatestUidToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativeMailboxOut.serializer(),
            """{"latestUid":null}""",
        )

        assertEquals(MailNativeMailboxOut(), decoded)
    }

    @Test
    fun mailNativeMailboxOutCoercesNullAttachmentCapableToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativeMailboxOut.serializer(),
            """{"attachmentCapable":null}""",
        )

        assertEquals(MailNativeMailboxOut(), decoded)
    }

    @Test
    fun mailNativeOverlayOutCoercesNullMessagesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativeOverlayOut.serializer(),
            """{"messages":null}""",
        )

        assertEquals(MailNativeOverlayOut(), decoded)
    }

    @Test
    fun mailNativeOverlayOutCoercesNullReadToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativeOverlayOut.serializer(),
            """{"read":null}""",
        )

        assertEquals(MailNativeOverlayOut(), decoded)
    }

    @Test
    fun mailNativeOverlayOutCoercesNullArchivedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativeOverlayOut.serializer(),
            """{"archived":null}""",
        )

        assertEquals(MailNativeOverlayOut(), decoded)
    }

    @Test
    fun mailNativeOverlayOutCoercesNullTrashedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativeOverlayOut.serializer(),
            """{"trashed":null}""",
        )

        assertEquals(MailNativeOverlayOut(), decoded)
    }

    @Test
    fun mailNativePipelineOutCoercesNullMessagesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativePipelineOut.serializer(),
            """{"messages":null}""",
        )

        assertEquals(MailNativePipelineOut(), decoded)
    }

    @Test
    fun mailNativePipelineOutCoercesNullAnalyzedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativePipelineOut.serializer(),
            """{"analyzed":null}""",
        )

        assertEquals(MailNativePipelineOut(), decoded)
    }

    @Test
    fun mailNativePipelineOutCoercesNullAnalyzingToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativePipelineOut.serializer(),
            """{"analyzing":null}""",
        )

        assertEquals(MailNativePipelineOut(), decoded)
    }

    @Test
    fun mailNativePipelineOutCoercesNullFailedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativePipelineOut.serializer(),
            """{"failed":null}""",
        )

        assertEquals(MailNativePipelineOut(), decoded)
    }

    @Test
    fun mailNativePipelineOutCoercesNullFeedCreatedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativePipelineOut.serializer(),
            """{"feedCreated":null}""",
        )

        assertEquals(MailNativePipelineOut(), decoded)
    }

    @Test
    fun mailNativePipelineOutCoercesNullFeedMissingToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativePipelineOut.serializer(),
            """{"feedMissing":null}""",
        )

        assertEquals(MailNativePipelineOut(), decoded)
    }

    @Test
    fun mailNativePipelineOutCoercesNullCalendarCandidatesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativePipelineOut.serializer(),
            """{"calendarCandidates":null}""",
        )

        assertEquals(MailNativePipelineOut(), decoded)
    }

    @Test
    fun mailNativePipelineOutCoercesNullTodoCandidatesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativePipelineOut.serializer(),
            """{"todoCandidates":null}""",
        )

        assertEquals(MailNativePipelineOut(), decoded)
    }

    @Test
    fun mailNativePipelineOutCoercesNullUpdatedAtToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativePipelineOut.serializer(),
            """{"updatedAt":null}""",
        )

        assertEquals(MailNativePipelineOut(), decoded)
    }

    @Test
    fun mailNativePipelineOutCoercesNullErrorToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativePipelineOut.serializer(),
            """{"error":null}""",
        )

        assertEquals(MailNativePipelineOut(), decoded)
    }

    @Test
    fun mailNativeStatusOutCoercesNullSourceToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativeStatusOut.serializer(),
            """{"source":null}""",
        )

        assertEquals(MailNativeStatusOut(), decoded)
    }

    @Test
    fun mailNativeStatusOutCoercesNullAvailableToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativeStatusOut.serializer(),
            """{"available":null}""",
        )

        assertEquals(MailNativeStatusOut(), decoded)
    }

    @Test
    fun mailNativeStatusOutCoercesNullOfflineCapableToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativeStatusOut.serializer(),
            """{"offlineCapable":null}""",
        )

        assertEquals(MailNativeStatusOut(), decoded)
    }

    @Test
    fun mailNativeStatusOutCoercesNullMailboxesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativeStatusOut.serializer(),
            """{"mailboxes":null}""",
        )

        assertEquals(MailNativeStatusOut(), decoded)
    }

    @Test
    fun mailNativeStatusOutCoercesNullOverlayToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativeStatusOut.serializer(),
            """{"overlay":null}""",
        )

        assertEquals(MailNativeStatusOut(), decoded)
    }

    @Test
    fun mailNativeStatusOutCoercesNullPipelineToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativeStatusOut.serializer(),
            """{"pipeline":null}""",
        )

        assertEquals(MailNativeStatusOut(), decoded)
    }

    @Test
    fun mailNativeStatusOutCoercesNullGeneratedAtToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativeStatusOut.serializer(),
            """{"generatedAt":null}""",
        )

        assertEquals(MailNativeStatusOut(), decoded)
    }

    @Test
    fun mailNativeStatusOutCoercesNullErrorToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailNativeStatusOut.serializer(),
            """{"error":null}""",
        )

        assertEquals(MailNativeStatusOut(), decoded)
    }

    @Test
    fun mailRowOutCoercesNullIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailRowOut.serializer(),
            """{"id":null}""",
        )

        assertEquals(MailRowOut(), decoded)
    }

    @Test
    fun mailRowOutCoercesNullThreadIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailRowOut.serializer(),
            """{"threadId":null}""",
        )

        assertEquals(MailRowOut(), decoded)
    }

    @Test
    fun mailRowOutCoercesNullFromToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailRowOut.serializer(),
            """{"from":null}""",
        )

        assertEquals(MailRowOut(), decoded)
    }

    @Test
    fun mailRowOutCoercesNullSubjectToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailRowOut.serializer(),
            """{"subject":null}""",
        )

        assertEquals(MailRowOut(), decoded)
    }

    @Test
    fun mailRowOutCoercesNullSnippetToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailRowOut.serializer(),
            """{"snippet":null}""",
        )

        assertEquals(MailRowOut(), decoded)
    }

    @Test
    fun mailRowOutCoercesNullDateToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailRowOut.serializer(),
            """{"date":null}""",
        )

        assertEquals(MailRowOut(), decoded)
    }

    @Test
    fun mailRowOutCoercesNullIsUnreadToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailRowOut.serializer(),
            """{"isUnread":null}""",
        )

        assertEquals(MailRowOut(), decoded)
    }

    @Test
    fun mailRowOutCoercesNullLabelsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailRowOut.serializer(),
            """{"labels":null}""",
        )

        assertEquals(MailRowOut(), decoded)
    }

    @Test
    fun mailRowOutCoercesNullMailboxToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailRowOut.serializer(),
            """{"mailbox":null}""",
        )

        assertEquals(MailRowOut(), decoded)
    }

    @Test
    fun mailRowOutCoercesNullHasAttachmentToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailRowOut.serializer(),
            """{"hasAttachment":null}""",
        )

        assertEquals(MailRowOut(), decoded)
    }

    @Test
    fun mailRowOutCoercesNullAttachmentCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailRowOut.serializer(),
            """{"attachmentCount":null}""",
        )

        assertEquals(MailRowOut(), decoded)
    }

    @Test
    fun mailRowOutCoercesNullPriorityToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailRowOut.serializer(),
            """{"priority":null}""",
        )

        assertEquals(MailRowOut(), decoded)
    }

    @Test
    fun mailRowOutCoercesNullPriorityHintToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailRowOut.serializer(),
            """{"priorityHint":null}""",
        )

        assertEquals(MailRowOut(), decoded)
    }

    @Test
    fun mailRowOutCoercesNullAnalysisStatusToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailRowOut.serializer(),
            """{"analysisStatus":null}""",
        )

        assertEquals(MailRowOut(), decoded)
    }

    @Test
    fun mailRowOutCoercesNullAnalysisQualityToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailRowOut.serializer(),
            """{"analysisQuality":null}""",
        )

        assertEquals(MailRowOut(), decoded)
    }

    @Test
    fun mailRowOutCoercesNullFeedStatusToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailRowOut.serializer(),
            """{"feedStatus":null}""",
        )

        assertEquals(MailRowOut(), decoded)
    }

    @Test
    fun mailRowOutCoercesNullCalendarProposalCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailRowOut.serializer(),
            """{"calendarProposalCount":null}""",
        )

        assertEquals(MailRowOut(), decoded)
    }

    @Test
    fun mailRowOutCoercesNullTodoCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailRowOut.serializer(),
            """{"todoCount":null}""",
        )

        assertEquals(MailRowOut(), decoded)
    }

    @Test
    fun mailRowOutCoercesNullWorkStateHintToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailRowOut.serializer(),
            """{"workStateHint":null}""",
        )

        assertEquals(MailRowOut(), decoded)
    }

    @Test
    fun mailRowOutCoercesNullRelatedProjectsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MailRowOut.serializer(),
            """{"relatedProjects":null}""",
        )

        assertEquals(MailRowOut(), decoded)
    }

    @Test
    fun marketQuoteCoercesNullSymbolToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MarketQuote.serializer(),
            """{"symbol":null}""",
        )

        assertEquals(MarketQuote(), decoded)
    }

    @Test
    fun marketQuoteCoercesNullLabelToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MarketQuote.serializer(),
            """{"label":null}""",
        )

        assertEquals(MarketQuote(), decoded)
    }

    @Test
    fun marketQuoteCoercesNullPriceToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MarketQuote.serializer(),
            """{"price":null}""",
        )

        assertEquals(MarketQuote(), decoded)
    }

    @Test
    fun marketQuoteCoercesNullPrevCloseToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MarketQuote.serializer(),
            """{"prevClose":null}""",
        )

        assertEquals(MarketQuote(), decoded)
    }

    @Test
    fun marketQuoteCoercesNullChangePctToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MarketQuote.serializer(),
            """{"changePct":null}""",
        )

        assertEquals(MarketQuote(), decoded)
    }

    @Test
    fun marketQuoteCoercesNullCurrencyToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MarketQuote.serializer(),
            """{"currency":null}""",
        )

        assertEquals(MarketQuote(), decoded)
    }

    @Test
    fun marketSummaryCoercesNullQuotesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MarketSummary.serializer(),
            """{"quotes":null}""",
        )

        assertEquals(MarketSummary(), decoded)
    }

    @Test
    fun marketSummaryCoercesNullAsOfToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MarketSummary.serializer(),
            """{"asOf":null}""",
        )

        assertEquals(MarketSummary(), decoded)
    }

    @Test
    fun marketSummaryCoercesNullStaleToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MarketSummary.serializer(),
            """{"stale":null}""",
        )

        assertEquals(MarketSummary(), decoded)
    }

    @Test
    fun memberOutCoercesNullNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MemberOut.serializer(),
            """{"name":null}""",
        )

        assertEquals(MemberOut(), decoded)
    }

    @Test
    fun memberOutCoercesNullRankToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MemberOut.serializer(),
            """{"rank":null}""",
        )

        assertEquals(MemberOut(), decoded)
    }

    @Test
    fun memberOutCoercesNullPositionToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MemberOut.serializer(),
            """{"position":null}""",
        )

        assertEquals(MemberOut(), decoded)
    }

    @Test
    fun memberOutCoercesNullPhonesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MemberOut.serializer(),
            """{"phones":null}""",
        )

        assertEquals(MemberOut(), decoded)
    }

    @Test
    fun memberOutCoercesNullEmailsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MemberOut.serializer(),
            """{"emails":null}""",
        )

        assertEquals(MemberOut(), decoded)
    }

    @Test
    fun memberOutCoercesNullPersonPathToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MemberOut.serializer(),
            """{"personPath":null}""",
        )

        assertEquals(MemberOut(), decoded)
    }

    @Test
    fun memoryCategoryRowCoercesNullNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MemoryCategoryRow.serializer(),
            """{"name":null}""",
        )

        assertEquals(MemoryCategoryRow(), decoded)
    }

    @Test
    fun memoryCategoryRowCoercesNullPageCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MemoryCategoryRow.serializer(),
            """{"pageCount":null}""",
        )

        assertEquals(MemoryCategoryRow(), decoded)
    }

    @Test
    fun memoryPageRowCoercesNullPathToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MemoryPageRow.serializer(),
            """{"path":null}""",
        )

        assertEquals(MemoryPageRow(), decoded)
    }

    @Test
    fun memoryPageRowCoercesNullTitleToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MemoryPageRow.serializer(),
            """{"title":null}""",
        )

        assertEquals(MemoryPageRow(), decoded)
    }

    @Test
    fun memoryPageRowCoercesNullSummaryToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MemoryPageRow.serializer(),
            """{"summary":null}""",
        )

        assertEquals(MemoryPageRow(), decoded)
    }

    @Test
    fun memoryPageRowCoercesNullUpdatedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MemoryPageRow.serializer(),
            """{"updated":null}""",
        )

        assertEquals(MemoryPageRow(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"id":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"name":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullEnabledToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"enabled":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullAgentIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"agentId":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullSessionTargetToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"sessionTarget":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullScheduleToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"schedule":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullScheduleSpecToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"scheduleSpec":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullScheduleKindToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"scheduleKind":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullTimezoneToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"timezone":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullCronExprToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"cronExpr":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullStaggerMsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"staggerMs":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullPayloadKindToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"payloadKind":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullPromptToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"prompt":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullModelToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"model":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullThinkingToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"thinking":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullTimeoutSecondsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"timeoutSeconds":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullLightContextToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"lightContext":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullRetryCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"retryCount":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullDeliveryChannelToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"deliveryChannel":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullDeliveryToToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"deliveryTo":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullDeliveryThreadIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"deliveryThreadId":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullFailureAlertAfterToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"failureAlertAfter":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullNextRunAtMsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"nextRunAtMs":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullLastSessionKeyToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"lastSessionKey":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullLastDeliveryStatusToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"lastDeliveryStatus":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullLastErrorToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"lastError":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullConsecutiveErrorsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"consecutiveErrors":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullAutoDisabledAtMsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"autoDisabledAtMs":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullCreatedAtMsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"createdAtMs":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronDetailCoercesNullUpdatedAtMsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronDetail.serializer(),
            """{"updatedAtMs":null}""",
        )

        assertEquals(MiniappCronDetail(), decoded)
    }

    @Test
    fun miniappCronRowCoercesNullIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronRow.serializer(),
            """{"id":null}""",
        )

        assertEquals(MiniappCronRow(), decoded)
    }

    @Test
    fun miniappCronRowCoercesNullNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronRow.serializer(),
            """{"name":null}""",
        )

        assertEquals(MiniappCronRow(), decoded)
    }

    @Test
    fun miniappCronRowCoercesNullEnabledToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronRow.serializer(),
            """{"enabled":null}""",
        )

        assertEquals(MiniappCronRow(), decoded)
    }

    @Test
    fun miniappCronRowCoercesNullScheduleToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronRow.serializer(),
            """{"schedule":null}""",
        )

        assertEquals(MiniappCronRow(), decoded)
    }

    @Test
    fun miniappCronRowCoercesNullPayloadKindToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronRow.serializer(),
            """{"payloadKind":null}""",
        )

        assertEquals(MiniappCronRow(), decoded)
    }

    @Test
    fun miniappCronRowCoercesNullPayloadPreviewToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronRow.serializer(),
            """{"payloadPreview":null}""",
        )

        assertEquals(MiniappCronRow(), decoded)
    }

    @Test
    fun miniappCronRowCoercesNullNextRunAtMsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronRow.serializer(),
            """{"nextRunAtMs":null}""",
        )

        assertEquals(MiniappCronRow(), decoded)
    }

    @Test
    fun miniappCronRowCoercesNullConsecutiveErrorsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronRow.serializer(),
            """{"consecutiveErrors":null}""",
        )

        assertEquals(MiniappCronRow(), decoded)
    }

    @Test
    fun miniappCronRowCoercesNullAutoDisabledAtMsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronRow.serializer(),
            """{"autoDisabledAtMs":null}""",
        )

        assertEquals(MiniappCronRow(), decoded)
    }

    @Test
    fun miniappCronRowCoercesNullLastErrorToDeclaredDefault() {
        val decoded = json.decodeFromString(
            MiniappCronRow.serializer(),
            """{"lastError":null}""",
        )

        assertEquals(MiniappCronRow(), decoded)
    }

    @Test
    fun modelAddResultCoercesNullOkToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelAddResult.serializer(),
            """{"ok":null}""",
        )

        assertEquals(ModelAddResult(), decoded)
    }

    @Test
    fun modelAddResultCoercesNullIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelAddResult.serializer(),
            """{"id":null}""",
        )

        assertEquals(ModelAddResult(), decoded)
    }

    @Test
    fun modelAddResultCoercesNullProviderToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelAddResult.serializer(),
            """{"provider":null}""",
        )

        assertEquals(ModelAddResult(), decoded)
    }

    @Test
    fun modelAddResultCoercesNullEndpointToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelAddResult.serializer(),
            """{"endpoint":null}""",
        )

        assertEquals(ModelAddResult(), decoded)
    }

    @Test
    fun modelAddResultCoercesNullModelToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelAddResult.serializer(),
            """{"model":null}""",
        )

        assertEquals(ModelAddResult(), decoded)
    }

    @Test
    fun modelAddResultCoercesNullAddedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelAddResult.serializer(),
            """{"added":null}""",
        )

        assertEquals(ModelAddResult(), decoded)
    }

    @Test
    fun modelDeleteResultCoercesNullOkToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelDeleteResult.serializer(),
            """{"ok":null}""",
        )

        assertEquals(ModelDeleteResult(), decoded)
    }

    @Test
    fun modelDeleteResultCoercesNullIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelDeleteResult.serializer(),
            """{"id":null}""",
        )

        assertEquals(ModelDeleteResult(), decoded)
    }

    @Test
    fun modelDeleteResultCoercesNullRemovedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelDeleteResult.serializer(),
            """{"removed":null}""",
        )

        assertEquals(ModelDeleteResult(), decoded)
    }

    @Test
    fun modelDeleteResultCoercesNullClearedRolesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelDeleteResult.serializer(),
            """{"clearedRoles":null}""",
        )

        assertEquals(ModelDeleteResult(), decoded)
    }

    @Test
    fun modelDeleteResultCoercesNullCurrentToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelDeleteResult.serializer(),
            """{"current":null}""",
        )

        assertEquals(ModelDeleteResult(), decoded)
    }

    @Test
    fun modelOptionCoercesNullIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelOption.serializer(),
            """{"id":null}""",
        )

        assertEquals(ModelOption(), decoded)
    }

    @Test
    fun modelOptionCoercesNullLabelToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelOption.serializer(),
            """{"label":null}""",
        )

        assertEquals(ModelOption(), decoded)
    }

    @Test
    fun modelOptionCoercesNullProviderToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelOption.serializer(),
            """{"provider":null}""",
        )

        assertEquals(ModelOption(), decoded)
    }

    @Test
    fun modelOptionCoercesNullDisplayToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelOption.serializer(),
            """{"display":null}""",
        )

        assertEquals(ModelOption(), decoded)
    }

    @Test
    fun modelOptionCoercesNullHealthToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelOption.serializer(),
            """{"health":null}""",
        )

        assertEquals(ModelOption(), decoded)
    }

    @Test
    fun modelOptionCoercesNullCurrentToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelOption.serializer(),
            """{"current":null}""",
        )

        assertEquals(ModelOption(), decoded)
    }

    @Test
    fun modelOptionCoercesNullCustomToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelOption.serializer(),
            """{"custom":null}""",
        )

        assertEquals(ModelOption(), decoded)
    }

    @Test
    fun modelOptionCoercesNullDeletableToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelOption.serializer(),
            """{"deletable":null}""",
        )

        assertEquals(ModelOption(), decoded)
    }

    @Test
    fun modelOptionCoercesNullUnhealthyToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelOption.serializer(),
            """{"unhealthy":null}""",
        )

        assertEquals(ModelOption(), decoded)
    }

    @Test
    fun modelOptionCoercesNullNoteToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelOption.serializer(),
            """{"note":null}""",
        )

        assertEquals(ModelOption(), decoded)
    }

    @Test
    fun modelSectionCoercesNullTitleToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelSection.serializer(),
            """{"title":null}""",
        )

        assertEquals(ModelSection(), decoded)
    }

    @Test
    fun modelSectionCoercesNullModelsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelSection.serializer(),
            """{"models":null}""",
        )

        assertEquals(ModelSection(), decoded)
    }

    @Test
    fun modelsListResultCoercesNullCurrentToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelsListResult.serializer(),
            """{"current":null}""",
        )

        assertEquals(ModelsListResult(), decoded)
    }

    @Test
    fun modelsListResultCoercesNullRolesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelsListResult.serializer(),
            """{"roles":null}""",
        )

        assertEquals(ModelsListResult(), decoded)
    }

    @Test
    fun modelsListResultCoercesNullSectionsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelsListResult.serializer(),
            """{"sections":null}""",
        )

        assertEquals(ModelsListResult(), decoded)
    }

    @Test
    fun modelsListResultCoercesNullAdvisoriesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelsListResult.serializer(),
            """{"advisories":null}""",
        )

        assertEquals(ModelsListResult(), decoded)
    }

    @Test
    fun modelsListResultCoercesNullMainHasVisionToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ModelsListResult.serializer(),
            """{"mainHasVision":null}""",
        )

        assertEquals(ModelsListResult(), decoded)
    }

    @Test
    fun notebookListOutCoercesNullNotebooksToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NotebookListOut.serializer(),
            """{"notebooks":null}""",
        )

        assertEquals(NotebookListOut(), decoded)
    }

    @Test
    fun notebookOutCoercesNullIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NotebookOut.serializer(),
            """{"id":null}""",
        )

        assertEquals(NotebookOut(), decoded)
    }

    @Test
    fun notebookOutCoercesNullNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NotebookOut.serializer(),
            """{"name":null}""",
        )

        assertEquals(NotebookOut(), decoded)
    }

    @Test
    fun notebookOutCoercesNullDescriptionToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NotebookOut.serializer(),
            """{"description":null}""",
        )

        assertEquals(NotebookOut(), decoded)
    }

    @Test
    fun notebookOutCoercesNullDealRefToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NotebookOut.serializer(),
            """{"dealRef":null}""",
        )

        assertEquals(NotebookOut(), decoded)
    }

    @Test
    fun notebookOutCoercesNullModeToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NotebookOut.serializer(),
            """{"mode":null}""",
        )

        assertEquals(NotebookOut(), decoded)
    }

    @Test
    fun notebookOutCoercesNullSourcesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NotebookOut.serializer(),
            """{"sources":null}""",
        )

        assertEquals(NotebookOut(), decoded)
    }

    @Test
    fun notebookOutCoercesNullUpdatedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NotebookOut.serializer(),
            """{"updated":null}""",
        )

        assertEquals(NotebookOut(), decoded)
    }

    @Test
    fun notebookSourceOutCoercesNullCiteToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NotebookSourceOut.serializer(),
            """{"cite":null}""",
        )

        assertEquals(NotebookSourceOut(), decoded)
    }

    @Test
    fun notebookSourceOutCoercesNullKindToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NotebookSourceOut.serializer(),
            """{"kind":null}""",
        )

        assertEquals(NotebookSourceOut(), decoded)
    }

    @Test
    fun notebookSourceOutCoercesNullRefToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NotebookSourceOut.serializer(),
            """{"ref":null}""",
        )

        assertEquals(NotebookSourceOut(), decoded)
    }

    @Test
    fun notebookSourceOutCoercesNullTitleToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NotebookSourceOut.serializer(),
            """{"title":null}""",
        )

        assertEquals(NotebookSourceOut(), decoded)
    }

    @Test
    fun notebookSourceOutCoercesNullTextToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NotebookSourceOut.serializer(),
            """{"text":null}""",
        )

        assertEquals(NotebookSourceOut(), decoded)
    }

    @Test
    fun notebookSummaryOutCoercesNullIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NotebookSummaryOut.serializer(),
            """{"id":null}""",
        )

        assertEquals(NotebookSummaryOut(), decoded)
    }

    @Test
    fun notebookSummaryOutCoercesNullNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NotebookSummaryOut.serializer(),
            """{"name":null}""",
        )

        assertEquals(NotebookSummaryOut(), decoded)
    }

    @Test
    fun notebookSummaryOutCoercesNullDescriptionToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NotebookSummaryOut.serializer(),
            """{"description":null}""",
        )

        assertEquals(NotebookSummaryOut(), decoded)
    }

    @Test
    fun notebookSummaryOutCoercesNullDealRefToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NotebookSummaryOut.serializer(),
            """{"dealRef":null}""",
        )

        assertEquals(NotebookSummaryOut(), decoded)
    }

    @Test
    fun notebookSummaryOutCoercesNullProjectRefsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NotebookSummaryOut.serializer(),
            """{"projectRefs":null}""",
        )

        assertEquals(NotebookSummaryOut(), decoded)
    }

    @Test
    fun notebookSummaryOutCoercesNullSourceCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NotebookSummaryOut.serializer(),
            """{"sourceCount":null}""",
        )

        assertEquals(NotebookSummaryOut(), decoded)
    }

    @Test
    fun notebookSummaryOutCoercesNullUpdatedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            NotebookSummaryOut.serializer(),
            """{"updated":null}""",
        )

        assertEquals(NotebookSummaryOut(), decoded)
    }

    @Test
    fun orgNodeOutCoercesNullIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            OrgNodeOut.serializer(),
            """{"id":null}""",
        )

        assertEquals(OrgNodeOut(), decoded)
    }

    @Test
    fun orgNodeOutCoercesNullNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            OrgNodeOut.serializer(),
            """{"name":null}""",
        )

        assertEquals(OrgNodeOut(), decoded)
    }

    @Test
    fun orgNodeOutCoercesNullTypeToDeclaredDefault() {
        val decoded = json.decodeFromString(
            OrgNodeOut.serializer(),
            """{"type":null}""",
        )

        assertEquals(OrgNodeOut(), decoded)
    }

    @Test
    fun orgNodeOutCoercesNullParentIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            OrgNodeOut.serializer(),
            """{"parentId":null}""",
        )

        assertEquals(OrgNodeOut(), decoded)
    }

    @Test
    fun orgNodeOutCoercesNullLaneToDeclaredDefault() {
        val decoded = json.decodeFromString(
            OrgNodeOut.serializer(),
            """{"lane":null}""",
        )

        assertEquals(OrgNodeOut(), decoded)
    }

    @Test
    fun orgNodeOutCoercesNullMembersToDeclaredDefault() {
        val decoded = json.decodeFromString(
            OrgNodeOut.serializer(),
            """{"members":null}""",
        )

        assertEquals(OrgNodeOut(), decoded)
    }

    @Test
    fun orgNodeOutCoercesNullKeywordsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            OrgNodeOut.serializer(),
            """{"keywords":null}""",
        )

        assertEquals(OrgNodeOut(), decoded)
    }

    @Test
    fun orgNodeOutCoercesNullCompaniesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            OrgNodeOut.serializer(),
            """{"companies":null}""",
        )

        assertEquals(OrgNodeOut(), decoded)
    }

    @Test
    fun orgSaveOutCoercesNullSavedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            OrgSaveOut.serializer(),
            """{"saved":null}""",
        )

        assertEquals(OrgSaveOut(), decoded)
    }

    @Test
    fun orgSaveOutCoercesNullNodeCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            OrgSaveOut.serializer(),
            """{"nodeCount":null}""",
        )

        assertEquals(OrgSaveOut(), decoded)
    }

    @Test
    fun orgSaveOutCoercesNullHasLanesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            OrgSaveOut.serializer(),
            """{"hasLanes":null}""",
        )

        assertEquals(OrgSaveOut(), decoded)
    }

    @Test
    fun orgTreeOutCoercesNullNodesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            OrgTreeOut.serializer(),
            """{"nodes":null}""",
        )

        assertEquals(OrgTreeOut(), decoded)
    }

    @Test
    fun personRowCoercesNullEmailToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PersonRow.serializer(),
            """{"email":null}""",
        )

        assertEquals(PersonRow(), decoded)
    }

    @Test
    fun personRowCoercesNullNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PersonRow.serializer(),
            """{"name":null}""",
        )

        assertEquals(PersonRow(), decoded)
    }

    @Test
    fun personRowCoercesNullMessageCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PersonRow.serializer(),
            """{"messageCount":null}""",
        )

        assertEquals(PersonRow(), decoded)
    }

    @Test
    fun personRowCoercesNullLastSeenToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PersonRow.serializer(),
            """{"lastSeen":null}""",
        )

        assertEquals(PersonRow(), decoded)
    }

    @Test
    fun personRowCoercesNullLastSubjectToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PersonRow.serializer(),
            """{"lastSubject":null}""",
        )

        assertEquals(PersonRow(), decoded)
    }

    @Test
    fun personRowCoercesNullWikiPathToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PersonRow.serializer(),
            """{"wikiPath":null}""",
        )

        assertEquals(PersonRow(), decoded)
    }

    @Test
    fun personRowCoercesNullWikiSummaryToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PersonRow.serializer(),
            """{"wikiSummary":null}""",
        )

        assertEquals(PersonRow(), decoded)
    }

    @Test
    fun projectDigestRowCoercesNullProjectToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ProjectDigestRow.serializer(),
            """{"project":null}""",
        )

        assertEquals(ProjectDigestRow(), decoded)
    }

    @Test
    fun projectDigestRowCoercesNullHeadlineToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ProjectDigestRow.serializer(),
            """{"headline":null}""",
        )

        assertEquals(ProjectDigestRow(), decoded)
    }

    @Test
    fun projectDigestRowCoercesNullBulletsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ProjectDigestRow.serializer(),
            """{"bullets":null}""",
        )

        assertEquals(ProjectDigestRow(), decoded)
    }

    @Test
    fun projectDigestRowCoercesNullDueToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ProjectDigestRow.serializer(),
            """{"due":null}""",
        )

        assertEquals(ProjectDigestRow(), decoded)
    }

    @Test
    fun projectDigestRowCoercesNullUpdatedAtMsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ProjectDigestRow.serializer(),
            """{"updatedAtMs":null}""",
        )

        assertEquals(ProjectDigestRow(), decoded)
    }

    @Test
    fun projectDigestRowCoercesNullPathToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ProjectDigestRow.serializer(),
            """{"path":null}""",
        )

        assertEquals(ProjectDigestRow(), decoded)
    }

    @Test
    fun projectDigestRowCoercesNullCodeToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ProjectDigestRow.serializer(),
            """{"code":null}""",
        )

        assertEquals(ProjectDigestRow(), decoded)
    }

    @Test
    fun projectDigestRowCoercesNullClientToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ProjectDigestRow.serializer(),
            """{"client":null}""",
        )

        assertEquals(ProjectDigestRow(), decoded)
    }

    @Test
    fun projectDigestRowCoercesNullRefsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ProjectDigestRow.serializer(),
            """{"refs":null}""",
        )

        assertEquals(ProjectDigestRow(), decoded)
    }

    @Test
    fun projectDigestsOutCoercesNullDigestsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ProjectDigestsOut.serializer(),
            """{"digests":null}""",
        )

        assertEquals(ProjectDigestsOut(), decoded)
    }

    @Test
    fun projectLinkedOutCoercesNullMailToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ProjectLinkedOut.serializer(),
            """{"mail":null}""",
        )

        assertEquals(ProjectLinkedOut(), decoded)
    }

    @Test
    fun projectLinkedOutCoercesNullCalendarToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ProjectLinkedOut.serializer(),
            """{"calendar":null}""",
        )

        assertEquals(ProjectLinkedOut(), decoded)
    }

    @Test
    fun projectLinkedOutCoercesNullTodoToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ProjectLinkedOut.serializer(),
            """{"todo":null}""",
        )

        assertEquals(ProjectLinkedOut(), decoded)
    }

    @Test
    fun projectLinkedOutCoercesNullWorkfeedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ProjectLinkedOut.serializer(),
            """{"workfeed":null}""",
        )

        assertEquals(ProjectLinkedOut(), decoded)
    }

    @Test
    fun projectLinkedOutCoercesNullNotebookToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ProjectLinkedOut.serializer(),
            """{"notebook":null}""",
        )

        assertEquals(ProjectLinkedOut(), decoded)
    }

    @Test
    fun projectRefCoercesNullPathToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ProjectRef.serializer(),
            """{"path":null}""",
        )

        assertEquals(ProjectRef(), decoded)
    }

    @Test
    fun projectRefCoercesNullTitleToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ProjectRef.serializer(),
            """{"title":null}""",
        )

        assertEquals(ProjectRef(), decoded)
    }

    @Test
    fun projectRefCoercesNullSummaryToDeclaredDefault() {
        val decoded = json.decodeFromString(
            ProjectRef.serializer(),
            """{"summary":null}""",
        )

        assertEquals(ProjectRef(), decoded)
    }

    @Test
    fun promptDetailOutCoercesNullIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptDetailOut.serializer(),
            """{"id":null}""",
        )

        assertEquals(PromptDetailOut(), decoded)
    }

    @Test
    fun promptDetailOutCoercesNullTitleToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptDetailOut.serializer(),
            """{"title":null}""",
        )

        assertEquals(PromptDetailOut(), decoded)
    }

    @Test
    fun promptDetailOutCoercesNullDescriptionToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptDetailOut.serializer(),
            """{"description":null}""",
        )

        assertEquals(PromptDetailOut(), decoded)
    }

    @Test
    fun promptDetailOutCoercesNullCategoryToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptDetailOut.serializer(),
            """{"category":null}""",
        )

        assertEquals(PromptDetailOut(), decoded)
    }

    @Test
    fun promptDetailOutCoercesNullTextToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptDetailOut.serializer(),
            """{"text":null}""",
        )

        assertEquals(PromptDetailOut(), decoded)
    }

    @Test
    fun promptDetailOutCoercesNullDefaultTextToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptDetailOut.serializer(),
            """{"defaultText":null}""",
        )

        assertEquals(PromptDetailOut(), decoded)
    }

    @Test
    fun promptDetailOutCoercesNullEditableToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptDetailOut.serializer(),
            """{"editable":null}""",
        )

        assertEquals(PromptDetailOut(), decoded)
    }

    @Test
    fun promptDetailOutCoercesNullOverriddenToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptDetailOut.serializer(),
            """{"overridden":null}""",
        )

        assertEquals(PromptDetailOut(), decoded)
    }

    @Test
    fun promptDetailOutCoercesNullUpdatedAtMsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptDetailOut.serializer(),
            """{"updatedAtMs":null}""",
        )

        assertEquals(PromptDetailOut(), decoded)
    }

    @Test
    fun promptListResponseCoercesNullPromptsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptListResponse.serializer(),
            """{"prompts":null}""",
        )

        assertEquals(PromptListResponse(), decoded)
    }

    @Test
    fun promptListResponseCoercesNullCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptListResponse.serializer(),
            """{"count":null}""",
        )

        assertEquals(PromptListResponse(), decoded)
    }

    @Test
    fun promptRowCoercesNullIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptRow.serializer(),
            """{"id":null}""",
        )

        assertEquals(PromptRow(), decoded)
    }

    @Test
    fun promptRowCoercesNullTitleToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptRow.serializer(),
            """{"title":null}""",
        )

        assertEquals(PromptRow(), decoded)
    }

    @Test
    fun promptRowCoercesNullDescriptionToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptRow.serializer(),
            """{"description":null}""",
        )

        assertEquals(PromptRow(), decoded)
    }

    @Test
    fun promptRowCoercesNullCategoryToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptRow.serializer(),
            """{"category":null}""",
        )

        assertEquals(PromptRow(), decoded)
    }

    @Test
    fun promptRowCoercesNullEditableToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptRow.serializer(),
            """{"editable":null}""",
        )

        assertEquals(PromptRow(), decoded)
    }

    @Test
    fun promptRowCoercesNullOverriddenToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptRow.serializer(),
            """{"overridden":null}""",
        )

        assertEquals(PromptRow(), decoded)
    }

    @Test
    fun promptRowCoercesNullUpdatedAtMsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptRow.serializer(),
            """{"updatedAtMs":null}""",
        )

        assertEquals(PromptRow(), decoded)
    }

    @Test
    fun promptTunerReportCoercesNullRanToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptTunerReport.serializer(),
            """{"ran":null}""",
        )

        assertEquals(PromptTunerReport(), decoded)
    }

    @Test
    fun promptTunerReportCoercesNullChangedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptTunerReport.serializer(),
            """{"changed":null}""",
        )

        assertEquals(PromptTunerReport(), decoded)
    }

    @Test
    fun promptTunerReportCoercesNullReasonToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptTunerReport.serializer(),
            """{"reason":null}""",
        )

        assertEquals(PromptTunerReport(), decoded)
    }

    @Test
    fun promptTunerReportCoercesNullErrorToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptTunerReport.serializer(),
            """{"error":null}""",
        )

        assertEquals(PromptTunerReport(), decoded)
    }

    @Test
    fun promptTunerReportCoercesNullLeafSummariesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptTunerReport.serializer(),
            """{"leafSummaries":null}""",
        )

        assertEquals(PromptTunerReport(), decoded)
    }

    @Test
    fun promptTunerReportCoercesNullMinSummariesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptTunerReport.serializer(),
            """{"minSummaries":null}""",
        )

        assertEquals(PromptTunerReport(), decoded)
    }

    @Test
    fun promptTunerReportCoercesNullProposedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptTunerReport.serializer(),
            """{"proposed":null}""",
        )

        assertEquals(PromptTunerReport(), decoded)
    }

    @Test
    fun promptTunerReportCoercesNullAddedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptTunerReport.serializer(),
            """{"added":null}""",
        )

        assertEquals(PromptTunerReport(), decoded)
    }

    @Test
    fun promptTunerReportCoercesNullBeforeCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptTunerReport.serializer(),
            """{"beforeCount":null}""",
        )

        assertEquals(PromptTunerReport(), decoded)
    }

    @Test
    fun promptTunerReportCoercesNullAfterCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptTunerReport.serializer(),
            """{"afterCount":null}""",
        )

        assertEquals(PromptTunerReport(), decoded)
    }

    @Test
    fun promptTunerRunResponseCoercesNullTargetToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptTunerRunResponse.serializer(),
            """{"target":null}""",
        )

        assertEquals(PromptTunerRunResponse(), decoded)
    }

    @Test
    fun promptTunerRunResponseCoercesNullReportToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PromptTunerRunResponse.serializer(),
            """{"report":null}""",
        )

        assertEquals(PromptTunerRunResponse(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullSystemToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"system":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullStateToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"state":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullTotalToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"total":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullGenesisToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"genesis":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullEvolvedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"evolved":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullReviewToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"review":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullRejectedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"rejected":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullRolledBackToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"rolledBack":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullAttentionToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"attention":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullLatestAtToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"latestAt":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullLatestTypeToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"latestType":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullLatestSkillToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"latestSkill":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullDoctrineVersionToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"doctrineVersion":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullDoctrineToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"doctrine":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullSourcePapersToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"sourcePapers":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullFilteredSourcesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"filteredSources":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullPrinciplesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"principles":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullQualityGatesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"qualityGates":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullNextActionsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"nextActions":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullCoverageStateToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"coverageState":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullCoverageGapsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"coverageGaps":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullNextCueToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"nextCue":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullQualityGateToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"qualityGate":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun propusLifecycleSummaryCoercesNullAttentionCueToDeclaredDefault() {
        val decoded = json.decodeFromString(
            PropusLifecycleSummary.serializer(),
            """{"attentionCue":null}""",
        )

        assertEquals(PropusLifecycleSummary(), decoded)
    }

    @Test
    fun qATurnCoercesNullQToDeclaredDefault() {
        val decoded = json.decodeFromString(
            QATurn.serializer(),
            """{"q":null}""",
        )

        assertEquals(QATurn(), decoded)
    }

    @Test
    fun qATurnCoercesNullAToDeclaredDefault() {
        val decoded = json.decodeFromString(
            QATurn.serializer(),
            """{"a":null}""",
        )

        assertEquals(QATurn(), decoded)
    }

    @Test
    fun roleModelCoercesNullRoleToDeclaredDefault() {
        val decoded = json.decodeFromString(
            RoleModel.serializer(),
            """{"role":null}""",
        )

        assertEquals(RoleModel(), decoded)
    }

    @Test
    fun roleModelCoercesNullModelToDeclaredDefault() {
        val decoded = json.decodeFromString(
            RoleModel.serializer(),
            """{"model":null}""",
        )

        assertEquals(RoleModel(), decoded)
    }

    @Test
    fun searchAllResultCoercesNullWikiToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SearchAllResult.serializer(),
            """{"wiki":null}""",
        )

        assertEquals(SearchAllResult(), decoded)
    }

    @Test
    fun searchAllResultCoercesNullDiaryToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SearchAllResult.serializer(),
            """{"diary":null}""",
        )

        assertEquals(SearchAllResult(), decoded)
    }

    @Test
    fun searchAllResultCoercesNullPeopleToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SearchAllResult.serializer(),
            """{"people":null}""",
        )

        assertEquals(SearchAllResult(), decoded)
    }

    @Test
    fun searchDiaryHitCoercesNullFileToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SearchDiaryHit.serializer(),
            """{"file":null}""",
        )

        assertEquals(SearchDiaryHit(), decoded)
    }

    @Test
    fun searchDiaryHitCoercesNullHeaderToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SearchDiaryHit.serializer(),
            """{"header":null}""",
        )

        assertEquals(SearchDiaryHit(), decoded)
    }

    @Test
    fun searchDiaryHitCoercesNullContentToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SearchDiaryHit.serializer(),
            """{"content":null}""",
        )

        assertEquals(SearchDiaryHit(), decoded)
    }

    @Test
    fun searchDiaryHitCoercesNullAtToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SearchDiaryHit.serializer(),
            """{"at":null}""",
        )

        assertEquals(SearchDiaryHit(), decoded)
    }

    @Test
    fun searchDiaryHitCoercesNullScoreToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SearchDiaryHit.serializer(),
            """{"score":null}""",
        )

        assertEquals(SearchDiaryHit(), decoded)
    }

    @Test
    fun searchWikiHitCoercesNullPathToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SearchWikiHit.serializer(),
            """{"path":null}""",
        )

        assertEquals(SearchWikiHit(), decoded)
    }

    @Test
    fun searchWikiHitCoercesNullTitleToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SearchWikiHit.serializer(),
            """{"title":null}""",
        )

        assertEquals(SearchWikiHit(), decoded)
    }

    @Test
    fun searchWikiHitCoercesNullSummaryToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SearchWikiHit.serializer(),
            """{"summary":null}""",
        )

        assertEquals(SearchWikiHit(), decoded)
    }

    @Test
    fun searchWikiHitCoercesNullCategoryToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SearchWikiHit.serializer(),
            """{"category":null}""",
        )

        assertEquals(SearchWikiHit(), decoded)
    }

    @Test
    fun searchWikiHitCoercesNullSnippetToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SearchWikiHit.serializer(),
            """{"snippet":null}""",
        )

        assertEquals(SearchWikiHit(), decoded)
    }

    @Test
    fun searchWikiHitCoercesNullScoreToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SearchWikiHit.serializer(),
            """{"score":null}""",
        )

        assertEquals(SearchWikiHit(), decoded)
    }

    @Test
    fun selfCorrectionCandidateCoercesNullIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfCorrectionCandidate.serializer(),
            """{"id":null}""",
        )

        assertEquals(SelfCorrectionCandidate(), decoded)
    }

    @Test
    fun selfCorrectionCandidateCoercesNullStatusToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfCorrectionCandidate.serializer(),
            """{"status":null}""",
        )

        assertEquals(SelfCorrectionCandidate(), decoded)
    }

    @Test
    fun selfCorrectionCandidateCoercesNullScopeToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfCorrectionCandidate.serializer(),
            """{"scope":null}""",
        )

        assertEquals(SelfCorrectionCandidate(), decoded)
    }

    @Test
    fun selfCorrectionCandidateCoercesNullSkillNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfCorrectionCandidate.serializer(),
            """{"skillName":null}""",
        )

        assertEquals(SelfCorrectionCandidate(), decoded)
    }

    @Test
    fun selfCorrectionCandidateCoercesNullSessionKeyToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfCorrectionCandidate.serializer(),
            """{"sessionKey":null}""",
        )

        assertEquals(SelfCorrectionCandidate(), decoded)
    }

    @Test
    fun selfCorrectionCandidateCoercesNullTitleToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfCorrectionCandidate.serializer(),
            """{"title":null}""",
        )

        assertEquals(SelfCorrectionCandidate(), decoded)
    }

    @Test
    fun selfCorrectionCandidateCoercesNullCandidateToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfCorrectionCandidate.serializer(),
            """{"candidate":null}""",
        )

        assertEquals(SelfCorrectionCandidate(), decoded)
    }

    @Test
    fun selfCorrectionCandidateCoercesNullEvidenceToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfCorrectionCandidate.serializer(),
            """{"evidence":null}""",
        )

        assertEquals(SelfCorrectionCandidate(), decoded)
    }

    @Test
    fun selfCorrectionCandidateCoercesNullReasonToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfCorrectionCandidate.serializer(),
            """{"reason":null}""",
        )

        assertEquals(SelfCorrectionCandidate(), decoded)
    }

    @Test
    fun selfCorrectionCandidateCoercesNullTargetFilesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfCorrectionCandidate.serializer(),
            """{"targetFiles":null}""",
        )

        assertEquals(SelfCorrectionCandidate(), decoded)
    }

    @Test
    fun selfCorrectionCandidateCoercesNullProposedChangeToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfCorrectionCandidate.serializer(),
            """{"proposedChange":null}""",
        )

        assertEquals(SelfCorrectionCandidate(), decoded)
    }

    @Test
    fun selfCorrectionCandidateCoercesNullRiskToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfCorrectionCandidate.serializer(),
            """{"risk":null}""",
        )

        assertEquals(SelfCorrectionCandidate(), decoded)
    }

    @Test
    fun selfCorrectionCandidateCoercesNullSourceToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfCorrectionCandidate.serializer(),
            """{"source":null}""",
        )

        assertEquals(SelfCorrectionCandidate(), decoded)
    }

    @Test
    fun selfCorrectionCandidateCoercesNullReviewerToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfCorrectionCandidate.serializer(),
            """{"reviewer":null}""",
        )

        assertEquals(SelfCorrectionCandidate(), decoded)
    }

    @Test
    fun selfCorrectionCandidateCoercesNullReviewNoteToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfCorrectionCandidate.serializer(),
            """{"reviewNote":null}""",
        )

        assertEquals(SelfCorrectionCandidate(), decoded)
    }

    @Test
    fun selfCorrectionCandidateCoercesNullEvidenceKindsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfCorrectionCandidate.serializer(),
            """{"evidenceKinds":null}""",
        )

        assertEquals(SelfCorrectionCandidate(), decoded)
    }

    @Test
    fun selfCorrectionCandidateCoercesNullReviewActionsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfCorrectionCandidate.serializer(),
            """{"reviewActions":null}""",
        )

        assertEquals(SelfCorrectionCandidate(), decoded)
    }

    @Test
    fun selfCorrectionCandidateCoercesNullCreatedAtToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfCorrectionCandidate.serializer(),
            """{"createdAt":null}""",
        )

        assertEquals(SelfCorrectionCandidate(), decoded)
    }

    @Test
    fun selfCorrectionCandidateCoercesNullUpdatedAtToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfCorrectionCandidate.serializer(),
            """{"updatedAt":null}""",
        )

        assertEquals(SelfCorrectionCandidate(), decoded)
    }

    @Test
    fun selfImprovementCodingFunnelCoercesNullLastCaptureAtToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfImprovementCodingFunnel.serializer(),
            """{"lastCaptureAt":null}""",
        )

        assertEquals(SelfImprovementCodingFunnel(), decoded)
    }

    @Test
    fun selfImprovementCodingFunnelCoercesNullLastReviewAtToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfImprovementCodingFunnel.serializer(),
            """{"lastReviewAt":null}""",
        )

        assertEquals(SelfImprovementCodingFunnel(), decoded)
    }

    @Test
    fun selfImprovementCodingFunnelCoercesNullRejections7dToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfImprovementCodingFunnel.serializer(),
            """{"rejections7d":null}""",
        )

        assertEquals(SelfImprovementCodingFunnel(), decoded)
    }

    @Test
    fun selfImprovementCodingFunnelCoercesNullPromotableRejections7dToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfImprovementCodingFunnel.serializer(),
            """{"promotableRejections7d":null}""",
        )

        assertEquals(SelfImprovementCodingFunnel(), decoded)
    }

    @Test
    fun selfImprovementCodingFunnelCoercesNullLastRejectionAtToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfImprovementCodingFunnel.serializer(),
            """{"lastRejectionAt":null}""",
        )

        assertEquals(SelfImprovementCodingFunnel(), decoded)
    }

    @Test
    fun selfImprovementCodingFunnelCoercesNullLastNudgeAtToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfImprovementCodingFunnel.serializer(),
            """{"lastNudgeAt":null}""",
        )

        assertEquals(SelfImprovementCodingFunnel(), decoded)
    }

    @Test
    fun selfImprovementCodingListResponseCoercesNullCandidatesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfImprovementCodingListResponse.serializer(),
            """{"candidates":null}""",
        )

        assertEquals(SelfImprovementCodingListResponse(), decoded)
    }

    @Test
    fun selfImprovementCodingListResponseCoercesNullCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfImprovementCodingListResponse.serializer(),
            """{"count":null}""",
        )

        assertEquals(SelfImprovementCodingListResponse(), decoded)
    }

    @Test
    fun selfImprovementCodingListResponseCoercesNullStatusCountsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfImprovementCodingListResponse.serializer(),
            """{"statusCounts":null}""",
        )

        assertEquals(SelfImprovementCodingListResponse(), decoded)
    }

    @Test
    fun selfImprovementCodingListResponseCoercesNullFunnelToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfImprovementCodingListResponse.serializer(),
            """{"funnel":null}""",
        )

        assertEquals(SelfImprovementCodingListResponse(), decoded)
    }

    @Test
    fun selfImprovementCodingStatusCountCoercesNullStatusToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfImprovementCodingStatusCount.serializer(),
            """{"status":null}""",
        )

        assertEquals(SelfImprovementCodingStatusCount(), decoded)
    }

    @Test
    fun selfImprovementCodingStatusCountCoercesNullCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SelfImprovementCodingStatusCount.serializer(),
            """{"count":null}""",
        )

        assertEquals(SelfImprovementCodingStatusCount(), decoded)
    }

    @Test
    fun senderRecentOutCoercesNullCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SenderRecentOut.serializer(),
            """{"count":null}""",
        )

        assertEquals(SenderRecentOut(), decoded)
    }

    @Test
    fun senderRecentOutCoercesNullLastReceivedAtToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SenderRecentOut.serializer(),
            """{"lastReceivedAt":null}""",
        )

        assertEquals(SenderRecentOut(), decoded)
    }

    @Test
    fun senderRecentOutCoercesNullWindowDaysToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SenderRecentOut.serializer(),
            """{"windowDays":null}""",
        )

        assertEquals(SenderRecentOut(), decoded)
    }

    @Test
    fun senderRecentOutCoercesNullTruncatedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SenderRecentOut.serializer(),
            """{"truncated":null}""",
        )

        assertEquals(SenderRecentOut(), decoded)
    }

    @Test
    fun senderWikiHitOutCoercesNullPathToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SenderWikiHitOut.serializer(),
            """{"path":null}""",
        )

        assertEquals(SenderWikiHitOut(), decoded)
    }

    @Test
    fun senderWikiHitOutCoercesNullTitleToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SenderWikiHitOut.serializer(),
            """{"title":null}""",
        )

        assertEquals(SenderWikiHitOut(), decoded)
    }

    @Test
    fun senderWikiHitOutCoercesNullSummaryToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SenderWikiHitOut.serializer(),
            """{"summary":null}""",
        )

        assertEquals(SenderWikiHitOut(), decoded)
    }

    @Test
    fun senderWikiHitOutCoercesNullCategoryToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SenderWikiHitOut.serializer(),
            """{"category":null}""",
        )

        assertEquals(SenderWikiHitOut(), decoded)
    }

    @Test
    fun sessionRowOutCoercesNullKeyToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SessionRowOut.serializer(),
            """{"key":null}""",
        )

        assertEquals(SessionRowOut(), decoded)
    }

    @Test
    fun sessionRowOutCoercesNullKindToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SessionRowOut.serializer(),
            """{"kind":null}""",
        )

        assertEquals(SessionRowOut(), decoded)
    }

    @Test
    fun sessionRowOutCoercesNullStatusToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SessionRowOut.serializer(),
            """{"status":null}""",
        )

        assertEquals(SessionRowOut(), decoded)
    }

    @Test
    fun sessionRowOutCoercesNullChannelToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SessionRowOut.serializer(),
            """{"channel":null}""",
        )

        assertEquals(SessionRowOut(), decoded)
    }

    @Test
    fun sessionRowOutCoercesNullModelToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SessionRowOut.serializer(),
            """{"model":null}""",
        )

        assertEquals(SessionRowOut(), decoded)
    }

    @Test
    fun sessionRowOutCoercesNullLabelToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SessionRowOut.serializer(),
            """{"label":null}""",
        )

        assertEquals(SessionRowOut(), decoded)
    }

    @Test
    fun sessionRowOutCoercesNullUpdatedAtMsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SessionRowOut.serializer(),
            """{"updatedAtMs":null}""",
        )

        assertEquals(SessionRowOut(), decoded)
    }

    @Test
    fun sessionRowOutCoercesNullStartedAtMsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SessionRowOut.serializer(),
            """{"startedAtMs":null}""",
        )

        assertEquals(SessionRowOut(), decoded)
    }

    @Test
    fun sessionRowOutCoercesNullRuntimeMsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SessionRowOut.serializer(),
            """{"runtimeMs":null}""",
        )

        assertEquals(SessionRowOut(), decoded)
    }

    @Test
    fun sessionRowOutCoercesNullTotalTokensToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SessionRowOut.serializer(),
            """{"totalTokens":null}""",
        )

        assertEquals(SessionRowOut(), decoded)
    }

    @Test
    fun skillDetailResponseCoercesNullSkillToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillDetailResponse.serializer(),
            """{"skill":null}""",
        )

        assertEquals(SkillDetailResponse(), decoded)
    }

    @Test
    fun skillDetailResponseCoercesNullBodyToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillDetailResponse.serializer(),
            """{"body":null}""",
        )

        assertEquals(SkillDetailResponse(), decoded)
    }

    @Test
    fun skillDetailResponseCoercesNullBodyTruncatedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillDetailResponse.serializer(),
            """{"bodyTruncated":null}""",
        )

        assertEquals(SkillDetailResponse(), decoded)
    }

    @Test
    fun skillDetailResponseCoercesNullPathToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillDetailResponse.serializer(),
            """{"path":null}""",
        )

        assertEquals(SkillDetailResponse(), decoded)
    }

    @Test
    fun skillLifecycleEventCoercesNullTypeToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillLifecycleEvent.serializer(),
            """{"type":null}""",
        )

        assertEquals(SkillLifecycleEvent(), decoded)
    }

    @Test
    fun skillLifecycleEventCoercesNullSkillNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillLifecycleEvent.serializer(),
            """{"skillName":null}""",
        )

        assertEquals(SkillLifecycleEvent(), decoded)
    }

    @Test
    fun skillLifecycleEventCoercesNullAtToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillLifecycleEvent.serializer(),
            """{"at":null}""",
        )

        assertEquals(SkillLifecycleEvent(), decoded)
    }

    @Test
    fun skillLifecycleEventCoercesNullVersionToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillLifecycleEvent.serializer(),
            """{"version":null}""",
        )

        assertEquals(SkillLifecycleEvent(), decoded)
    }

    @Test
    fun skillLifecycleEventCoercesNullDetailToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillLifecycleEvent.serializer(),
            """{"detail":null}""",
        )

        assertEquals(SkillLifecycleEvent(), decoded)
    }

    @Test
    fun skillLifecycleEventCoercesNullRouteToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillLifecycleEvent.serializer(),
            """{"route":null}""",
        )

        assertEquals(SkillLifecycleEvent(), decoded)
    }

    @Test
    fun skillLifecycleEventCoercesNullEvidenceToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillLifecycleEvent.serializer(),
            """{"evidence":null}""",
        )

        assertEquals(SkillLifecycleEvent(), decoded)
    }

    @Test
    fun skillLifecycleEventCoercesNullTargetSignatureToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillLifecycleEvent.serializer(),
            """{"targetSignature":null}""",
        )

        assertEquals(SkillLifecycleEvent(), decoded)
    }

    @Test
    fun skillLifecycleEventCoercesNullEditedSurfaceToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillLifecycleEvent.serializer(),
            """{"editedSurface":null}""",
        )

        assertEquals(SkillLifecycleEvent(), decoded)
    }

    @Test
    fun skillLifecycleEventCoercesNullExpectedBehaviorChangeToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillLifecycleEvent.serializer(),
            """{"expectedBehaviorChange":null}""",
        )

        assertEquals(SkillLifecycleEvent(), decoded)
    }

    @Test
    fun skillLifecycleEventCoercesNullRegressionRiskToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillLifecycleEvent.serializer(),
            """{"regressionRisk":null}""",
        )

        assertEquals(SkillLifecycleEvent(), decoded)
    }

    @Test
    fun skillRowCoercesNullNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillRow.serializer(),
            """{"name":null}""",
        )

        assertEquals(SkillRow(), decoded)
    }

    @Test
    fun skillRowCoercesNullDescriptionToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillRow.serializer(),
            """{"description":null}""",
        )

        assertEquals(SkillRow(), decoded)
    }

    @Test
    fun skillRowCoercesNullCategoryToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillRow.serializer(),
            """{"category":null}""",
        )

        assertEquals(SkillRow(), decoded)
    }

    @Test
    fun skillRowCoercesNullHomepageToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillRow.serializer(),
            """{"homepage":null}""",
        )

        assertEquals(SkillRow(), decoded)
    }

    @Test
    fun skillRowCoercesNullTagsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillRow.serializer(),
            """{"tags":null}""",
        )

        assertEquals(SkillRow(), decoded)
    }

    @Test
    fun skillRowCoercesNullRelatedSkillsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillRow.serializer(),
            """{"relatedSkills":null}""",
        )

        assertEquals(SkillRow(), decoded)
    }

    @Test
    fun skillRowCoercesNullSourceToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillRow.serializer(),
            """{"source":null}""",
        )

        assertEquals(SkillRow(), decoded)
    }

    @Test
    fun skillRowCoercesNullVersionToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillRow.serializer(),
            """{"version":null}""",
        )

        assertEquals(SkillRow(), decoded)
    }

    @Test
    fun skillRowCoercesNullOriginToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillRow.serializer(),
            """{"origin":null}""",
        )

        assertEquals(SkillRow(), decoded)
    }

    @Test
    fun skillRowCoercesNullCreatedAtToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillRow.serializer(),
            """{"createdAt":null}""",
        )

        assertEquals(SkillRow(), decoded)
    }

    @Test
    fun skillRowCoercesNullEvolveCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillRow.serializer(),
            """{"evolveCount":null}""",
        )

        assertEquals(SkillRow(), decoded)
    }

    @Test
    fun skillRowCoercesNullLastEvolvedAtToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillRow.serializer(),
            """{"lastEvolvedAt":null}""",
        )

        assertEquals(SkillRow(), decoded)
    }

    @Test
    fun skillRowCoercesNullTotalUsesToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillRow.serializer(),
            """{"totalUses":null}""",
        )

        assertEquals(SkillRow(), decoded)
    }

    @Test
    fun skillRowCoercesNullLastUsedAtToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillRow.serializer(),
            """{"lastUsedAt":null}""",
        )

        assertEquals(SkillRow(), decoded)
    }

    @Test
    fun skillRowCoercesNullCuratorStateToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillRow.serializer(),
            """{"curatorState":null}""",
        )

        assertEquals(SkillRow(), decoded)
    }

    @Test
    fun skillRowCoercesNullEditableToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillRow.serializer(),
            """{"editable":null}""",
        )

        assertEquals(SkillRow(), decoded)
    }

    @Test
    fun skillRowCoercesNullDeletableToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillRow.serializer(),
            """{"deletable":null}""",
        )

        assertEquals(SkillRow(), decoded)
    }

    @Test
    fun skillRowCoercesNullDependencySummaryToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillRow.serializer(),
            """{"dependencySummary":null}""",
        )

        assertEquals(SkillRow(), decoded)
    }

    @Test
    fun skillRowCoercesNullInstallSummaryToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillRow.serializer(),
            """{"installSummary":null}""",
        )

        assertEquals(SkillRow(), decoded)
    }

    @Test
    fun skillsLifecycleResponseCoercesNullEventsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillsLifecycleResponse.serializer(),
            """{"events":null}""",
        )

        assertEquals(SkillsLifecycleResponse(), decoded)
    }

    @Test
    fun skillsLifecycleResponseCoercesNullCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillsLifecycleResponse.serializer(),
            """{"count":null}""",
        )

        assertEquals(SkillsLifecycleResponse(), decoded)
    }

    @Test
    fun skillsLifecycleResponseCoercesNullSummaryToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillsLifecycleResponse.serializer(),
            """{"summary":null}""",
        )

        assertEquals(SkillsLifecycleResponse(), decoded)
    }

    @Test
    fun skillsListResponseCoercesNullSkillsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillsListResponse.serializer(),
            """{"skills":null}""",
        )

        assertEquals(SkillsListResponse(), decoded)
    }

    @Test
    fun skillsListResponseCoercesNullCountToDeclaredDefault() {
        val decoded = json.decodeFromString(
            SkillsListResponse.serializer(),
            """{"count":null}""",
        )

        assertEquals(SkillsListResponse(), decoded)
    }

    @Test
    fun todoOutCoercesNullIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TodoOut.serializer(),
            """{"id":null}""",
        )

        assertEquals(TodoOut(), decoded)
    }

    @Test
    fun todoOutCoercesNullTitleToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TodoOut.serializer(),
            """{"title":null}""",
        )

        assertEquals(TodoOut(), decoded)
    }

    @Test
    fun todoOutCoercesNullNoteToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TodoOut.serializer(),
            """{"note":null}""",
        )

        assertEquals(TodoOut(), decoded)
    }

    @Test
    fun todoOutCoercesNullDueToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TodoOut.serializer(),
            """{"due":null}""",
        )

        assertEquals(TodoOut(), decoded)
    }

    @Test
    fun todoOutCoercesNullDueAllDayToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TodoOut.serializer(),
            """{"dueAllDay":null}""",
        )

        assertEquals(TodoOut(), decoded)
    }

    @Test
    fun todoOutCoercesNullDoneToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TodoOut.serializer(),
            """{"done":null}""",
        )

        assertEquals(TodoOut(), decoded)
    }

    @Test
    fun todoOutCoercesNullDoneAtToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TodoOut.serializer(),
            """{"doneAt":null}""",
        )

        assertEquals(TodoOut(), decoded)
    }

    @Test
    fun topicDocOutCoercesNullKeyToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TopicDocOut.serializer(),
            """{"key":null}""",
        )

        assertEquals(TopicDocOut(), decoded)
    }

    @Test
    fun topicDocOutCoercesNullNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TopicDocOut.serializer(),
            """{"name":null}""",
        )

        assertEquals(TopicDocOut(), decoded)
    }

    @Test
    fun topicDocOutCoercesNullContentToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TopicDocOut.serializer(),
            """{"content":null}""",
        )

        assertEquals(TopicDocOut(), decoded)
    }

    @Test
    fun topicDocOutCoercesNullSizeToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TopicDocOut.serializer(),
            """{"size":null}""",
        )

        assertEquals(TopicDocOut(), decoded)
    }

    @Test
    fun topicDocOutCoercesNullModifiedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TopicDocOut.serializer(),
            """{"modified":null}""",
        )

        assertEquals(TopicDocOut(), decoded)
    }

    @Test
    fun topicDocWriteOutCoercesNullKeyToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TopicDocWriteOut.serializer(),
            """{"key":null}""",
        )

        assertEquals(TopicDocWriteOut(), decoded)
    }

    @Test
    fun topicDocWriteOutCoercesNullNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TopicDocWriteOut.serializer(),
            """{"name":null}""",
        )

        assertEquals(TopicDocWriteOut(), decoded)
    }

    @Test
    fun topicDocWriteOutCoercesNullSizeToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TopicDocWriteOut.serializer(),
            """{"size":null}""",
        )

        assertEquals(TopicDocWriteOut(), decoded)
    }

    @Test
    fun topicDocWriteOutCoercesNullModifiedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TopicDocWriteOut.serializer(),
            """{"modified":null}""",
        )

        assertEquals(TopicDocWriteOut(), decoded)
    }

    @Test
    fun topicDocWriteOutCoercesNullAppliedToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TopicDocWriteOut.serializer(),
            """{"applied":null}""",
        )

        assertEquals(TopicDocWriteOut(), decoded)
    }

    @Test
    fun transcriptAttachmentOutCoercesNullTypeToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TranscriptAttachmentOut.serializer(),
            """{"type":null}""",
        )

        assertEquals(TranscriptAttachmentOut(), decoded)
    }

    @Test
    fun transcriptAttachmentOutCoercesNullMimeTypeToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TranscriptAttachmentOut.serializer(),
            """{"mimeType":null}""",
        )

        assertEquals(TranscriptAttachmentOut(), decoded)
    }

    @Test
    fun transcriptAttachmentOutCoercesNullUrlToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TranscriptAttachmentOut.serializer(),
            """{"url":null}""",
        )

        assertEquals(TranscriptAttachmentOut(), decoded)
    }

    @Test
    fun transcriptAttachmentOutCoercesNullDataToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TranscriptAttachmentOut.serializer(),
            """{"data":null}""",
        )

        assertEquals(TranscriptAttachmentOut(), decoded)
    }

    @Test
    fun transcriptAttachmentOutCoercesNullNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TranscriptAttachmentOut.serializer(),
            """{"name":null}""",
        )

        assertEquals(TranscriptAttachmentOut(), decoded)
    }

    @Test
    fun transcriptAttachmentOutCoercesNullSizeToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TranscriptAttachmentOut.serializer(),
            """{"size":null}""",
        )

        assertEquals(TranscriptAttachmentOut(), decoded)
    }

    @Test
    fun transcriptMsgOutCoercesNullIdToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TranscriptMsgOut.serializer(),
            """{"id":null}""",
        )

        assertEquals(TranscriptMsgOut(), decoded)
    }

    @Test
    fun transcriptMsgOutCoercesNullRoleToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TranscriptMsgOut.serializer(),
            """{"role":null}""",
        )

        assertEquals(TranscriptMsgOut(), decoded)
    }

    @Test
    fun transcriptMsgOutCoercesNullContentToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TranscriptMsgOut.serializer(),
            """{"content":null}""",
        )

        assertEquals(TranscriptMsgOut(), decoded)
    }

    @Test
    fun transcriptMsgOutCoercesNullAttachmentsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TranscriptMsgOut.serializer(),
            """{"attachments":null}""",
        )

        assertEquals(TranscriptMsgOut(), decoded)
    }

    @Test
    fun transcriptMsgOutCoercesNullTimestampMsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            TranscriptMsgOut.serializer(),
            """{"timestampMs":null}""",
        )

        assertEquals(TranscriptMsgOut(), decoded)
    }

    @Test
    fun wormholeModelOutCoercesNullNameToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WormholeModelOut.serializer(),
            """{"name":null}""",
        )

        assertEquals(WormholeModelOut(), decoded)
    }

    @Test
    fun wormholeModelOutCoercesNullProtocolToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WormholeModelOut.serializer(),
            """{"protocol":null}""",
        )

        assertEquals(WormholeModelOut(), decoded)
    }

    @Test
    fun wormholeModelOutCoercesNullLocalToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WormholeModelOut.serializer(),
            """{"local":null}""",
        )

        assertEquals(WormholeModelOut(), decoded)
    }

    @Test
    fun wormholeModelOutCoercesNullThinkingToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WormholeModelOut.serializer(),
            """{"thinking":null}""",
        )

        assertEquals(WormholeModelOut(), decoded)
    }

    @Test
    fun wormholeModelOutCoercesNullSourceToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WormholeModelOut.serializer(),
            """{"source":null}""",
        )

        assertEquals(WormholeModelOut(), decoded)
    }

    @Test
    fun wormholeModelOutCoercesNullKeyHealthToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WormholeModelOut.serializer(),
            """{"keyHealth":null}""",
        )

        assertEquals(WormholeModelOut(), decoded)
    }

    @Test
    fun wormholeStatusOutCoercesNullReachableToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WormholeStatusOut.serializer(),
            """{"reachable":null}""",
        )

        assertEquals(WormholeStatusOut(), decoded)
    }

    @Test
    fun wormholeStatusOutCoercesNullListenToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WormholeStatusOut.serializer(),
            """{"listen":null}""",
        )

        assertEquals(WormholeStatusOut(), decoded)
    }

    @Test
    fun wormholeStatusOutCoercesNullLocalOnlyToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WormholeStatusOut.serializer(),
            """{"localOnly":null}""",
        )

        assertEquals(WormholeStatusOut(), decoded)
    }

    @Test
    fun wormholeStatusOutCoercesNullEffortRoutingToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WormholeStatusOut.serializer(),
            """{"effortRouting":null}""",
        )

        assertEquals(WormholeStatusOut(), decoded)
    }

    @Test
    fun wormholeStatusOutCoercesNullAutoToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WormholeStatusOut.serializer(),
            """{"auto":null}""",
        )

        assertEquals(WormholeStatusOut(), decoded)
    }

    @Test
    fun wormholeStatusOutCoercesNullModelsToDeclaredDefault() {
        val decoded = json.decodeFromString(
            WormholeStatusOut.serializer(),
            """{"models":null}""",
        )

        assertEquals(WormholeStatusOut(), decoded)
    }
}
