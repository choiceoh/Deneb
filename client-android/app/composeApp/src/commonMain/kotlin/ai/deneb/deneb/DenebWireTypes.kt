package ai.deneb.deneb

import ai.deneb.deneb.generated.CalendarEventOut
import ai.deneb.deneb.generated.ContactRow
import ai.deneb.deneb.generated.MailRowOut
import ai.deneb.deneb.generated.MemoryCategoryRow
import ai.deneb.deneb.generated.MemoryPageRow
import ai.deneb.deneb.generated.MiniappCronRow
import ai.deneb.deneb.generated.ModelSection
import ai.deneb.deneb.generated.PersonRow
import ai.deneb.deneb.generated.RoleModel
import ai.deneb.deneb.generated.SenderRecentOut
import ai.deneb.deneb.generated.SenderWikiHitOut
import ai.deneb.deneb.generated.SessionRowOut
import ai.deneb.deneb.generated.TodoOut
import ai.deneb.deneb.generated.TranscriptMsgOut
import ai.deneb.ui.chat.WorkFeedItem
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject

/** Keep stable server order while letting the latest row for an identity win. */
internal inline fun <T, K> Iterable<T>.distinctByLast(crossinline keyOf: (T) -> K): List<T> = toList().asReversed().distinctBy { keyOf(it) }.asReversed()

// On-the-wire RPC response payloads for DenebGatewayClient — the envelopes the
// gateway returns over miniapp.*. Kept thin: element shapes (MailRowOut,
// SessionRowOut, …) are generated from the Go structs in deneb/generated/, so
// these wrappers just name the envelope fields. Split out of DenebGatewayClient
// because they are the surface most edited as the gateway evolves (and so the
// biggest rebase-conflict hotspot). internal, not private, so the client can use
// them across files in this package.

@Serializable
internal data class RecentPayload(val sessions: List<SessionRowOut> = emptyList())

@Serializable
internal data class TranscriptPayload(
    val messages: List<TranscriptMsgOut> = emptyList(),
    val turnRunning: Boolean = false,
)

@Serializable
internal data class WorkFeedPayload(val items: List<WorkFeedItem> = emptyList())

@Serializable
internal data class WorkFeedActionRunPayload(
    val ok: Boolean = false,
    val item: WorkFeedItem = WorkFeedItem(),
    val sessionKey: String = "",
    val prompt: String = "",
    val message: String = "",
    val removeFromFeed: Boolean = false,
)

@Serializable
internal data class WorkFeedFeedbackPayload(
    val ok: Boolean = false,
    val item: WorkFeedItem = WorkFeedItem(),
    val text: String = "",
    val sessionKey: String = "",
)

@Serializable
internal data class NativeSyncPayload(
    val events: List<NativeSyncEvent> = emptyList(),
    val cursor: Long = 0,
    val latestSeq: Long = 0,
    val hasMore: Boolean = false,
)

@Serializable
internal data class NativeSyncEvent(
    val seq: Long = 0,
    val type: String = "",
    val entityId: String = "",
    val sessionKey: String = "",
    val workFeedItemId: String = "",
    val timestampMs: Long = 0,
    val payload: JsonObject? = null,
)

@Serializable
internal data class NativeSyncActionPayload(
    val item: WorkFeedItem = WorkFeedItem(),
    val removeFromFeed: Boolean = false,
)

@Serializable
internal data class MemoryListPayload(val pages: List<MemoryPageRow> = emptyList())

@Serializable
internal data class DiaryRecentPayload(val entries: List<DiaryRecentRow> = emptyList())

@Serializable
internal data class DiaryRecentRow(
    val file: String = "",
    val header: String = "",
    val content: String = "",
    val at: Long = 0,
)

@Serializable
internal data class DeletePagesPayload(val ok: Boolean = false, val deleted: Int = 0)

@Serializable
internal data class MovePagePayload(val ok: Boolean = false)

@Serializable
internal data class CategoriesPayload(
    val categories: List<MemoryCategoryRow> = emptyList(),
    val totalPages: Int = 0,
    val totalBytes: Long = 0,
)

@Serializable
internal data class CronListPayload(val jobs: List<MiniappCronRow> = emptyList())

@Serializable
internal data class ModelsPayload(
    val current: String = "",
    val roles: List<RoleModel> = emptyList(),
    val sections: List<ModelSection> = emptyList(),
    val advisories: List<String> = emptyList(),
    // Whether the main model accepts image input. Defaults true-less (false) so an
    // older gateway that omits it leaves the 비전 role visible (prior behavior).
    val mainHasVision: Boolean = false,
)

@Serializable
internal data class ClientHelloPayload(
    val version: String = "",
    val nativeApiVersion: Int = 0,
    val model: String = "",
    val capabilities: Map<String, Boolean> = emptyMap(),
    val endpoints: Map<String, String> = emptyMap(),
    val tsMs: Long = 0,
)

@Serializable
internal data class MailListPayload(
    val messages: List<MailRowOut> = emptyList(),
    val nextPageToken: String = "",
)

@Serializable
internal data class OkPayload(val ok: Boolean = false)

@Serializable
internal data class AskPayload(val answer: String = "")

@Serializable
internal data class SenderContextPayload(
    val sender: String = "",
    val email: String = "",
    val displayName: String = "",
    val recent: SenderRecentOut? = null,
    val wikiHits: List<SenderWikiHitOut> = emptyList(),
    val wikiFacts: String = "",
)

// Calendar list envelope. The element shape (CalendarEventOut) and its nested
// attendee/conference types are generated from the Go calendarEventOut struct,
// so the list and detail screens share one source of truth with the gateway.
@Serializable
internal data class CalListPayload(val events: List<CalendarEventOut> = emptyList())

// Calendar-proposal (bell) list envelope. CalendarProposalOut is generated from
// the Go struct so the bell list shares one source of truth with the gateway.
@Serializable
internal data class CalProposalsPayload(val proposals: List<ai.deneb.deneb.generated.CalendarProposalOut> = emptyList())

// To-do list envelope. The element shape (TodoOut) is generated from the Go
// todoOut struct, so the to-do list and calendar share one source of truth.
@Serializable
internal data class TodoListPayload(val todos: List<TodoOut> = emptyList())

@Serializable
internal data class PeopleListPayload(val people: List<PersonRow> = emptyList())

@Serializable
internal data class ContactsListPayload(val contacts: List<ContactRow> = emptyList())

/** miniapp.contacts.dedup — the deterministic dedup preview: how many address-book
 *  entries collapse to how many people, plus the safe merge groups. Hand-written
 *  (the Go response is a plain struct, not a //deneb:wire type). */
@Serializable
internal data class ContactsDedupPayload(
    val total: Int = 0,
    val distinct: Int = 0,
    val ambiguous: Int = 0,
    @SerialName("ambiguous_pairs") val ambiguousPairs: List<DedupPairRow> = emptyList(),
    val merges: List<DedupMergeRow> = emptyList(),
)

@Serializable
internal data class DedupMergeRow(
    val canonical: String = "",
    val names: List<String> = emptyList(),
    val phones: List<String> = emptyList(),
    val emails: List<String> = emptyList(),
)

/** One ambiguous pair (same identifier, different name) for the AI to adjudicate. */
@Serializable
internal data class DedupPairRow(
    val a: DedupPartyRow = DedupPartyRow(),
    val b: DedupPartyRow = DedupPartyRow(),
    val shared: String = "",
)

@Serializable
internal data class DedupPartyRow(
    val name: String = "",
    val org: String = "",
    val phones: List<String> = emptyList(),
    val emails: List<String> = emptyList(),
)

/** miniapp.contacts.adjudicate — one verdict per submitted pair ("same"/"diff"/"unsure"). */
@Serializable
internal data class ContactsAdjudicatePayload(val verdicts: List<String> = emptyList())

@Serializable
internal data class WikiPagePayload(
    val path: String = "",
    val title: String = "",
    val summary: String = "",
    val category: String = "",
    val code: String = "", // frozen project code (anchors the deal notebook); "" for non-project pages
    val tags: List<String> = emptyList(),
    val related: List<String> = emptyList(),
    val updated: String = "",
    val body: String = "",
)

// Bulk page export for the offline wiki mirror (miniapp.memory.mirror).
// Row shape mirrors WikiPagePayload minus `related` (the mirror doesn't
// render related links).
@Serializable
internal data class WikiMirrorPayload(
    val pages: List<WikiMirrorPageRow> = emptyList(),
    val nextCursor: String = "",
    val hasMore: Boolean = false,
    val total: Int = 0,
)

@Serializable
internal data class WikiMirrorPageRow(
    val path: String = "",
    val title: String = "",
    val summary: String = "",
    val category: String = "",
    val code: String = "",
    val tags: List<String> = emptyList(),
    val updated: String = "",
    val body: String = "",
)

// Bulk entry export for the offline diary mirror (miniapp.memory.diary_mirror).
@Serializable
internal data class DiaryMirrorPayload(
    val entries: List<DiaryMirrorEntryRow> = emptyList(),
    val nextCursor: String = "",
    val hasMore: Boolean = false,
    val total: Int = 0,
)

@Serializable
internal data class DiaryMirrorEntryRow(
    val file: String = "",
    val header: String = "",
    val content: String = "",
    val at: Long = 0,
)

// Capture results: the gateway runs OCR / ASR / contacts-extract and the agent
// turn, returning the surfaced text.
@Serializable
internal data class CaptureImagePayload(val text: String = "")

@Serializable
internal data class CaptureAudioPayload(val text: String = "")

@Serializable
internal data class CaptureDocumentPayload(val text: String = "")

// Batch capture: N files materialized on the gateway, analyzed in ONE turn. Only
// the surfaced analysis text is read here (the files array is the gateway's).
@Serializable
internal data class CaptureBatchPayload(val text: String = "")

@Serializable
internal data class CaptureContactsPayload(val text: String = "")

// --- Observation plane (miniapp.observe.*) ---

@Serializable
internal data class ObserveToolStat(
    val name: String = "",
    val calls: Int = 0,
    val errors: Int = 0,
    val avgMs: Long = 0,
    val repaired: Int = 0,
    val unknown: Int = 0,
    val blocked: Int = 0,
    val cacheHits: Int = 0,
    val truncated: Int = 0,
)

@Serializable
internal data class ObserveBehavior(
    val runs: Int = 0,
    val proactiveRuns: Int = 0,
    val compactedRuns: Int = 0,
    val totalInputTokens: Long = 0,
    val totalOutputTokens: Long = 0,
    val cacheReadTokens: Long = 0,
    val tools: List<ObserveToolStat> = emptyList(),
    val proactiveDecisions: Map<String, Int> = emptyMap(),
    val backgroundJobs: Map<String, Int> = emptyMap(),
    val backgroundErrors: Map<String, Int> = emptyMap(),
)

@Serializable
internal data class ObserveLogLine(
    val ts: Long = 0,
    val level: String = "",
    val msg: String = "",
    val runId: String = "",
    val session: String = "",
)

@Serializable
internal data class ObserveLogsPayload(
    val lines: List<ObserveLogLine> = emptyList(),
    val count: Int = 0,
)

@Serializable
internal data class ObserveVllmPrefixCache(
    val model: String = "",
    val queries: Long = 0,
    val hits: Long = 0,
    val hitRatePct: Double = 0.0,
)

@Serializable
internal data class ObserveHealth(
    val captureEnabled: Boolean = false,
    val agentLogEnabled: Boolean = false,
    val ringCapacity: Int = 0,
    val ringUsed: Int = 0,
    val recentErrors: Int = 0,
    val runs24h: Int = 0,
    val proactiveRuns24h: Int = 0,
    val compactedRuns24h: Int = 0,
    val backgroundErrors24h: Int = 0,
    val vllmPrefixCache: List<ObserveVllmPrefixCache> = emptyList(),
)
