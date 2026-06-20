package ai.deneb.deneb

import androidx.compose.runtime.Immutable
import kotlinx.serialization.Serializable

// Domain models surfaced to the native UI by DenebGatewayClient. These are the
// shapes the screens consume — distinct from the on-the-wire RPC types (those
// are generated in deneb/generated/ or kept private to the client). Split out of
// DenebGatewayClient.kt to keep that file focused on transport + state.
//
// Every type here is @Immutable: each is a val-only DTO decoded once from an RPC
// response and never mutated, so the promise holds even for the List/Map members
// Compose otherwise treats as unstable. Marking them lets composables skip
// recomposition when an equal value is re-emitted (a re-fetch, a parent redraw).

/** A switchable Deneb model surfaced in the config screen's model picker. */
@Immutable
data class ModelOption(
    val id: String,
    val display: String,
    val current: Boolean,
    val health: String,
    val custom: Boolean = false,
    /** Picker may remove this model (custom + cloud-catalog; local vLLM/LocalAI can't). */
    val deletable: Boolean = false,
    /** Circuit breaker open: consecutive failures, fallback engaged. */
    val unhealthy: Boolean = false,
    /** Tuner stat line (runs, p95, cache, fallback/stall, probe, floor). */
    val note: String = "",
)

/** Gateway/native API status returned by `miniapp.client.hello`. */
@Immutable
data class ClientStatus(
    val version: String,
    val nativeApiVersion: Int,
    val model: String,
    val capabilities: Map<String, Boolean>,
    val endpoints: Map<String, String>,
    val timestampMs: Long,
)

/** A recent Gmail message shown in the native mail screen. [Serializable] so the
 *  default inbox list can be cached locally for instant render (DenebClientMail). */
@Immutable
@Serializable
data class MailMessage(
    val id: String,
    val from: String,
    val subject: String,
    val snippet: String,
    val date: String,
    val unread: Boolean,
    /** Gateway heuristic priority tier: "urgent" / "attention" / "" (routine). */
    val priority: String = "",
    /** Short Korean signal hint for the tier (e.g. "낙찰 · 마감 표현"). */
    val priorityHint: String = "",
    /** Local archive mailbox that produced this row (e.g. INBOX, Gmail). */
    val mailbox: String = "",
    /** True when the local archive parser saw at least one attachment. */
    val hasAttachment: Boolean = false,
    val attachmentCount: Int = 0,
    /** Deneb-native workflow state layered over the immutable mail archive. */
    val workState: MailWorkState = MailWorkState(),
)

/** Full Gmail message for the native detail screen. */
@Immutable
data class MailDetail(
    val id: String,
    val from: String,
    val to: String,
    val cc: String,
    val subject: String,
    val date: String,
    val body: String,
    val bodyTotal: Int,
    val rawBody: String = "",
    val rawBodyTotal: Int = 0,
    val bodyCleaned: Boolean = false,
    val bodyHiddenBlockCount: Int = 0,
    val bodyHiddenLineCount: Int = 0,
    val attachments: List<MailAttachment>,
    val workState: MailWorkState = MailWorkState(),
)

/** A downloadable attachment on a message (id + size kept for download). */
@Immutable
data class MailAttachment(
    val id: String,
    val filename: String,
    val mimeType: String,
    val size: Int,
)

@Immutable
data class MailNativeStatus(
    val source: String,
    val available: Boolean,
    val offlineCapable: Boolean,
    val mailboxes: List<MailNativeMailbox>,
    val overlay: MailNativeOverlay,
    val pipeline: MailNativePipeline = MailNativePipeline(),
    val generatedAt: String = "",
    val error: String = "",
)

@Immutable
data class MailNativeMailbox(
    val name: String,
    val total: Int,
    val unread: Int,
    val locallyRead: Int,
    val locallyArchived: Int,
    val locallyTrashed: Int,
    val latestUid: String,
    val attachmentCapable: Boolean,
)

@Immutable
data class MailNativeOverlay(
    val messages: Int,
    val read: Int,
    val archived: Int,
    val trashed: Int,
)

@Immutable
@Serializable
data class MailWorkState(
    val analysisStatus: String = "",
    val analysisQuality: String = "",
    val feedStatus: String = "",
    val calendarProposalCount: Int = 0,
    val todoCount: Int = 0,
    val hint: String = "",
)

@Immutable
data class MailNativePipeline(
    val messages: Int = 0,
    val analyzed: Int = 0,
    val analyzing: Int = 0,
    val failed: Int = 0,
    val feedCreated: Int = 0,
    val feedMissing: Int = 0,
    val calendarCandidates: Int = 0,
    val todoCandidates: Int = 0,
    val updatedAt: String = "",
    val error: String = "",
)

/** A wiki project page cited by an analysis, surfaced as a tappable chip. */
@Immutable
data class RelatedProject(val path: String, val title: String, val summary: String)

/** Result of an AI mail analysis (fresh or cached). */
@Immutable
data class MailAnalysis(
    val text: String,
    val related: List<RelatedProject> = emptyList(),
    val cached: Boolean = false,
    val createdAt: String = "",
    val durationMs: Long = 0,
    val workState: MailWorkState = MailWorkState(),
)

/** Sender relationship context (recent volume + cited wiki pages). */
@Immutable
data class SenderContext(
    val displayName: String,
    val email: String,
    val recentCount: Int,
    val windowDays: Int,
    val wikiHits: List<SenderWikiHit>,
    val wikiFacts: String,
)

@Immutable
data class SenderWikiHit(val title: String, val summary: String, val category: String, val path: String = "")

/** Glanceable home-widget data: next meeting, unread count, latest-mail line. */
@Immutable
data class WidgetSummary(
    val meeting: String = "",
    val unread: Int = 0,
    val latestMail: String = "",
    val configured: Boolean = true,
    val ok: Boolean = true,
)

/** An upcoming calendar event shown in the native calendar screen. `local` is
 *  true for gateway-stored events the user can edit/delete (vs read-only Google). */
@Immutable
@Serializable
data class CalendarEvent(
    val id: String,
    val title: String,
    val location: String,
    val start: String,
    val end: String,
    val allDay: Boolean,
    val local: Boolean = false,
    // Month-grid color bucket from the gateway: "mine" | "others" | "deadline"
    // (see eventCategory in calendar.go). Empty falls back to "mine".
    val category: String = "",
)

/** Full calendar event for the detail screen. */
@Immutable
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
    val local: Boolean = false,
)

/** A to-do item shown in the native to-do list and on the calendar day view.
 *  `due` is an RFC3339 instant or "" when the to-do has no due date; `dueAllDay`
 *  marks a whole-day due (time-of-day ignored). All to-dos are gateway-stored and
 *  editable. */
@Immutable
data class Todo(
    val id: String,
    val title: String,
    val note: String = "",
    val due: String = "",
    val dueAllDay: Boolean = false,
    val done: Boolean = false,
)

/** Full cron job detail for the cron screen (`miniapp.crons.get`). */
@Immutable
data class CronDetail(
    val id: String,
    val name: String,
    val enabled: Boolean,
    val schedule: String,
    val scheduleSpec: String,
    val scheduleKind: String,
    val timezone: String,
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
@Immutable
data class SearchResults(
    val wiki: List<SearchHit>,
    val diary: List<SearchHit>,
    val people: List<PersonHit>,
)

@Immutable
data class SearchHit(val path: String, val title: String, val snippet: String, val category: String)

/** One row of the merged people directory: a recent Gmail counterparty, an 인물
 *  wiki person, or both (matched server-side — see miniapp.people.list). A row
 *  with a blank [email] and zero [messageCount] is wiki-only. */
@Immutable
data class PersonHit(
    val name: String,
    val email: String,
    val messageCount: Int,
    val lastSubject: String,
    val wikiPath: String = "",
    val wikiSummary: String = "",
)

/** Full wiki/memory page for the page view. */
@Immutable
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
@Immutable
data class WikiCategory(val name: String, val pageCount: Int)

/** All wiki categories plus corpus totals. */
@Immutable
data class WikiCategories(
    val categories: List<WikiCategory>,
    val totalPages: Int,
    val totalBytes: Long,
)

/** A page reference within a category listing (tap -> wiki page). */
@Immutable
data class WikiPageRef(
    val path: String,
    val title: String,
    val summary: String,
    val updated: String,
)

/** One diary entry for the recent-diary timeline. [header] is the entry's
 *  heading (often a date/time); [file] is the source diary file. */
@Immutable
data class DiaryEntry(
    val header: String,
    val content: String,
    val file: String,
)
