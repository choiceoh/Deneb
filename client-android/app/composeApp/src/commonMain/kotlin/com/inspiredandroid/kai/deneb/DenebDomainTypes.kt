package com.inspiredandroid.kai.deneb

// Domain models surfaced to the native UI by DenebGatewayClient. These are the
// shapes the screens consume — distinct from the on-the-wire RPC types (those
// are generated in deneb/generated/ or kept private to the client). Split out of
// DenebGatewayClient.kt to keep that file focused on transport + state.

/** A switchable Deneb model surfaced in the config screen's model picker. */
data class ModelOption(
    val id: String,
    val display: String,
    val current: Boolean,
    val health: String,
    val custom: Boolean = false,
)

/** Gateway/native API status returned by `miniapp.client.hello`. */
data class ClientStatus(
    val version: String,
    val nativeApiVersion: Int,
    val model: String,
    val capabilities: Map<String, Boolean>,
    val endpoints: Map<String, String>,
    val timestampMs: Long,
)

/** A recent Gmail message shown in the native mail screen. */
data class MailMessage(
    val id: String,
    val from: String,
    val subject: String,
    val snippet: String,
    val date: String,
    val unread: Boolean,
)

/** Full Gmail message for the native detail screen. */
data class MailDetail(
    val id: String,
    val from: String,
    val to: String,
    val cc: String,
    val subject: String,
    val date: String,
    val body: String,
    val bodyTotal: Int,
    val attachments: List<MailAttachment>,
)

/** A downloadable attachment on a message (id + size kept for download). */
data class MailAttachment(
    val id: String,
    val filename: String,
    val mimeType: String,
    val size: Int,
)

/** A wiki project page cited by an analysis, surfaced as a tappable chip. */
data class RelatedProject(val path: String, val title: String, val summary: String)

/** Result of an AI mail analysis (fresh or cached). */
data class MailAnalysis(
    val text: String,
    val related: List<RelatedProject> = emptyList(),
    val cached: Boolean = false,
    val createdAt: String = "",
    val durationMs: Long = 0,
)

/** Sender relationship context (recent volume + cited wiki pages). */
data class SenderContext(
    val displayName: String,
    val email: String,
    val recentCount: Int,
    val windowDays: Int,
    val wikiHits: List<SenderWikiHit>,
    val wikiFacts: String,
)

data class SenderWikiHit(val title: String, val summary: String, val category: String, val path: String = "")

/** Glanceable home-widget data: next-meeting line + unread-mail count. */
data class WidgetSummary(
    val meeting: String = "",
    val unread: Int = 0,
    val configured: Boolean = true,
    val ok: Boolean = true,
)

/** An upcoming calendar event shown in the native calendar screen. */
data class CalendarEvent(
    val id: String,
    val title: String,
    val location: String,
    val start: String,
    val end: String,
    val allDay: Boolean,
)

/** Full calendar event for the detail screen. */
data class CalendarEventDetail(
    val id: String,
    val title: String,
    val description: String,
    val location: String,
    val start: String,
    val end: String,
    val allDay: Boolean,
    val organizer: String,
    val attendees: List<String>,
    val status: String,
)

/** Full cron job detail for the cron screen (`miniapp.crons.get`). */
data class CronDetail(
    val id: String,
    val name: String,
    val enabled: Boolean,
    val schedule: String,
    val scheduleSpec: String,
    val scheduleKind: String,
    val payloadKind: String,
    val prompt: String,
    val model: String,
    val deliveryChannel: String,
    val deliveryTo: String,
    val nextRunAtMs: Long,
    val lastDeliveryStatus: String,
    val lastError: String,
    val consecutiveErrors: Int,
    val autoDisabledAtMs: Long,
)

/** Unified search results across wiki, diary and people. */
data class SearchResults(
    val wiki: List<SearchHit>,
    val diary: List<SearchHit>,
    val people: List<PersonHit>,
)

data class SearchHit(val path: String, val title: String, val snippet: String, val category: String)

data class PersonHit(val name: String, val email: String, val messageCount: Int, val lastSubject: String)

/** A topic doc file in the hub list. */
data class TopicDocFile(val name: String, val modified: String)

/** A topic doc's content for the read view. */
data class TopicDocContent(val name: String, val content: String, val modified: String)

/** Full wiki/memory page for the page view. */
data class WikiPage(
    val path: String,
    val title: String,
    val summary: String,
    val category: String,
    val tags: List<String>,
    val updated: String,
    val body: String,
)

/** A wiki category with its page count, for the category browser. */
data class WikiCategory(val name: String, val pageCount: Int)

/** All wiki categories plus corpus totals. */
data class WikiCategories(
    val categories: List<WikiCategory>,
    val totalPages: Int,
    val totalBytes: Long,
)

/** A page reference within a category listing (tap -> wiki page). */
data class WikiPageRef(
    val path: String,
    val title: String,
    val summary: String,
    val updated: String,
)

/** One diary entry for the recent-diary timeline. [header] is the entry's
 *  heading (often a date/time); [file] is the source diary file. */
data class DiaryEntry(
    val header: String,
    val content: String,
    val file: String,
)
