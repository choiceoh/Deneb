package ai.deneb.deneb

import ai.deneb.deneb.generated.MailAnalysisOut
import ai.deneb.deneb.generated.MailMessageOut
import ai.deneb.deneb.generated.MailNativeStatusOut
import ai.deneb.deneb.generated.MailRowOut
import ai.deneb.deneb.generated.QATurn
import io.ktor.client.call.body
import io.ktor.client.plugins.timeout
import io.ktor.client.request.get
import io.ktor.http.encodeURLParameter
import kotlinx.coroutines.flow.update
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.add
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonArray

/**
 * Mail surface of [DenebGatewayClient] (`miniapp.gmail.*`): recent mail list +
 * pagination, message detail, read/archive/trash mutations, AI analysis, Q&A,
 * attachments, and sender context. Extensions so the gateway client stays one
 * facade while each RPC domain lives in its own file.
 */

/** Cap on the session read-overlay so a very long session can't grow it without
 *  bound (EmailStore caps its pending queue for the same reason). */
private const val MAX_LOCAL_READ_IDS = 1000
private const val MAIL_LIST_PAGE_SIZE = 60

/**
 * Re-apply the session read-overlay to a freshly fetched page: a mail the user
 * has read this session shows no unread dot even when the server's row still
 * says unread. The gateway caches list_recent for 30s and mark_read deliberately
 * does NOT invalidate it (mailbox membership is unchanged), leaning on this
 * optimistic clear to mask the stale dot. Phone back-nav recomposes the list and
 * re-runs refreshMail within that window, so without re-applying here the cached
 * unread would resurrect a dot the user already cleared. Identity (no allocation)
 * when the overlay is empty, the common case.
 */
internal fun applyReadOverlay(rows: List<MailMessage>, locallyRead: Set<String>): List<MailMessage> = if (locallyRead.isEmpty()) {
    rows
} else {
    rows.map { if (it.unread && it.id in locallyRead) it.copy(unread = false) else it }
}

/** Record [id] as read in [into] (most-recent-last), evicting the oldest beyond [max]. */
internal fun recordReadId(into: LinkedHashSet<String>, id: String, max: Int = MAX_LOCAL_READ_IDS) {
    into.remove(id) // re-insert to refresh recency so eviction drops the longest-untouched
    into.add(id)
    if (into.size > max) {
        val it = into.iterator()
        it.next()
        it.remove()
    }
}

/** Refresh the mail list. With a [query] it runs a full-mailbox mail search
 *  (native/archive-aware syntax: keywords, `from:`, `has:attachment`, …); null/blank falls
 *  back to the server's default recent-mail view. Returns false on a fetch
 *  failure so the screen can show a retry instead of a misleading empty state. */
suspend fun DenebGatewayClient.refreshMail(query: String? = null): Boolean {
    val q = query?.trim()?.ifBlank { null }
    // Pin the credential epoch: if the user switches gateways while this fetch is in
    // flight, the old-account mail list must neither become visible nor repopulate
    // the (just-cleared) cache under the new credentials.
    val epoch = credEpoch
    // Cache-then-network for the default recent mail view (no query): render the encrypted
    // local copy instantly so the mail tab has no spinner on open, then revalidate.
    // Query searches are not cached (query-specific, transient).
    if (q == null && _denebMail.value.isEmpty()) {
        loadCachedMail()?.let {
            if (epoch != credEpoch) return@let // credentials switched — don't show the old account's cache
            _denebMail.value = applyReadOverlay(it, locallyReadMailIds)
            // The cached rows ARE the default recent mail view, so drop any stale search
            // cursor/query left over from a prior paged view — otherwise a "더 보기"
            // tap before the network refresh below would append the wrong page.
            denebMailActiveQuery = null
            _denebMailNextToken.value = null
        }
    }
    val payload = callRpc<MailListPayload>(
        "miniapp.gmail.list_recent",
        buildJsonObject {
            put("limit", MAIL_LIST_PAGE_SIZE)
            q?.let { put("query", it) }
        },
    ) ?: return false
    // Credentials switched mid-flight: this response is the old account — drop it so it
    // can't surface under the new gateway (onCredentialsChanged already cleared the view).
    if (epoch != credEpoch) return false
    denebMailActiveQuery = q
    val rows = payload.messages
        .filter { it.id.isNotBlank() }
        .map { it.toDomainMailMessage() }
    val overlaid = applyReadOverlay(rows, locallyReadMailIds)
    _denebMail.value = overlaid
    _denebMailNextToken.value = payload.nextPageToken.ifBlank { null }
    // Cache the OVERLAID rows (not the raw server rows): the gateway caches list_recent
    // for ~30s and won't reflect a just-read mail, so persisting raw rows would let a
    // cold start resurrect a unread dot the user already cleared.
    if (q == null) storeCachedMail(overlaid)
    refreshMailNativeStatus()
    return true
}

suspend fun DenebGatewayClient.refreshMailNativeStatus(): MailNativeStatus? {
    val p = callRpc<MailNativeStatusOut>("miniapp.gmail.native_status", buildJsonObject {}) ?: return null
    val status = MailNativeStatus(
        source = p.source,
        available = p.available,
        offlineCapable = p.offlineCapable,
        mailboxes = p.mailboxes.map {
            MailNativeMailbox(
                name = it.name,
                total = it.total,
                unread = it.unread,
                locallyRead = it.locallyRead,
                locallyArchived = it.locallyArchived,
                locallyTrashed = it.locallyTrashed,
                latestUid = it.latestUid,
                attachmentCapable = it.attachmentCapable,
            )
        },
        overlay = MailNativeOverlay(
            messages = p.overlay.messages,
            read = p.overlay.read,
            archived = p.overlay.archived,
            trashed = p.overlay.trashed,
        ),
        pipeline = MailNativePipeline(
            messages = p.pipeline.messages,
            analyzed = p.pipeline.analyzed,
            analyzing = p.pipeline.analyzing,
            failed = p.pipeline.failed,
            feedCreated = p.pipeline.feedCreated,
            feedMissing = p.pipeline.feedMissing,
            calendarCandidates = p.pipeline.calendarCandidates,
            todoCandidates = p.pipeline.todoCandidates,
            updatedAt = p.pipeline.updatedAt,
        ),
        generatedAt = p.generatedAt,
        error = p.error,
    )
    _denebMailNativeStatus.value = status
    return status
}

// --- Default recent mail cache (cache-then-network) -----------------------
// Only the no-query recent mail list is cached, encrypted in settings, for an instant
// mail-tab render. The owner fingerprint prevents a prior gateway/account cache
// from rendering under the current URL/token if credential migration or a manual
// settings edit bypassed the normal cache purge path. The network refresh above
// overwrites with the authoritative list.
private val mailCacheJson = Json { ignoreUnknownKeys = true }

@Serializable
private data class MailCacheEnvelope(
    val owner: String = "",
    val rows: List<MailMessage> = emptyList(),
)

internal fun mailCacheOwner(url: String, token: String): String {
    val normalizedUrl = url.trim().trimEnd('/')
    return "$normalizedUrl#${stableMailCacheFingerprint(token)}"
}

private fun stableMailCacheFingerprint(value: String): String {
    var hash = 1125899906842597L
    value.forEach { hash = hash * 131 + it.code }
    return "${value.length}:$hash"
}

internal fun encodeMailCache(rows: List<MailMessage>, owner: String): String = mailCacheJson.encodeToString(
    MailCacheEnvelope(owner = owner, rows = rows),
)

internal fun decodeMailCache(json: String, expectedOwner: String): List<MailMessage>? = runCatching {
    mailCacheJson.decodeFromString<MailCacheEnvelope>(json)
}.getOrNull()
    ?.takeIf { it.owner == expectedOwner }
    ?.rows
    ?.takeIf { it.isNotEmpty() }

internal fun DenebGatewayClient.loadCachedMail(): List<MailMessage>? {
    val json = appSettings.getCachedMailList() ?: return null
    return decodeMailCache(json, mailCacheOwner(gatewayUrl, clientToken))
}

internal fun DenebGatewayClient.storeCachedMail(rows: List<MailMessage>) {
    appSettings.putCachedMailList(encodeMailCache(rows, mailCacheOwner(gatewayUrl, clientToken)))
}

/**
 * Apply a mail mutation (read/archive/trash) to the cached default recent copy so
 * that an app kill or unreachable gateway before the next successful refresh can't
 * resurrect a cleared unread dot or a removed row from the cache. Reads the cache
 * directly (not [_denebMail], which may currently hold a search view), so it's
 * correct regardless of what the user is looking at. No-op when there's no cache.
 */
internal fun DenebGatewayClient.patchCachedMail(transform: (List<MailMessage>) -> List<MailMessage>) {
    val cached = loadCachedMail() ?: return
    storeCachedMail(transform(cached))
}

/** Append the next page of the current view (recent mail or active search) to the list. */
suspend fun DenebGatewayClient.loadMoreMail() {
    val epoch = credEpoch
    val token = _denebMailNextToken.value ?: return
    val payload = callRpc<MailListPayload>(
        "miniapp.gmail.list_recent",
        buildJsonObject {
            put("limit", MAIL_LIST_PAGE_SIZE)
            put("pageToken", token)
            denebMailActiveQuery?.let { put("query", it) }
        },
    ) ?: return
    // Credentials switched while the page was in flight — don't append old-account
    // rows under the new gateway (onCredentialsChanged already cleared the list).
    if (epoch != credEpoch) return
    val seen = _denebMail.value.mapTo(HashSet()) { it.id }
    _denebMail.value = _denebMail.value + applyReadOverlay(
        payload.messages
            .filter { it.id.isNotBlank() && it.id !in seen }
            .map { it.toDomainMailMessage() },
        locallyReadMailIds,
    )
    _denebMailNextToken.value = payload.nextPageToken.ifBlank { null }
}

suspend fun DenebGatewayClient.fetchMailDetail(id: String, full: Boolean = false): MailDetail? {
    val row = callRpc<MailMessageOut>(
        "miniapp.gmail.get",
        buildJsonObject {
            put("id", id)
            // full=true asks for the untruncated body (still server-bounded);
            // the default keeps the light 3000-char cap for the list→detail flow.
            if (full) put("full", true)
        },
    ) ?: return null
    return MailDetail(
        id = row.id,
        from = row.from,
        to = row.to,
        cc = row.cc,
        subject = row.subject,
        date = row.date,
        body = row.body,
        bodyTotal = row.bodyTotal,
        attachments = row.attachments
            .filter { it.id.isNotBlank() }
            .map { MailAttachment(it.id, it.filename.ifBlank { it.mimeType }, it.mimeType, it.size) },
        workState = MailWorkState(
            analysisStatus = row.analysisStatus,
            analysisQuality = row.analysisQuality,
            feedStatus = row.feedStatus,
            calendarProposalCount = row.calendarProposalCount,
            todoCount = row.todoCount,
            hint = row.workStateHint,
        ),
    )
}

/** Mark read on the server and optimistically clear the unread dot in the list. */
suspend fun DenebGatewayClient.markMailRead(id: String): Boolean {
    val ok = callRpc<OkPayload>("miniapp.gmail.mark_read", buildJsonObject { put("id", id) })?.ok == true
    if (ok) {
        // Remember the read so a later list refetch can't resurrect the dot: on phone,
        // popping back from the mail recomposes the list and re-runs refreshMail inside
        // the gateway's 30s list cache, which still reports the mail unread.
        recordReadId(locallyReadMailIds, id)
        _denebMail.update { list -> list.map { if (it.id == id) it.copy(unread = false) else it } }
        patchCachedMail { list -> list.map { if (it.id == id) it.copy(unread = false) else it } }
        refreshMailNativeStatus()
    }
    return ok
}

/** Archive (drop from the active list); optimistically removes the row from the list. */
suspend fun DenebGatewayClient.archiveMail(id: String): Boolean {
    val ok = callRpc<OkPayload>("miniapp.gmail.archive", buildJsonObject { put("id", id) })?.ok == true
    if (ok) {
        _denebMail.update { list -> list.filterNot { it.id == id } }
        patchCachedMail { list -> list.filterNot { it.id == id } }
        refreshMailNativeStatus()
    }
    return ok
}

/** Move to Trash; optimistically removes the row from the list. */
suspend fun DenebGatewayClient.trashMail(id: String): Boolean {
    val ok = callRpc<OkPayload>("miniapp.gmail.trash", buildJsonObject { put("id", id) })?.ok == true
    if (ok) {
        _denebMail.update { list -> list.filterNot { it.id == id } }
        patchCachedMail { list -> list.filterNot { it.id == id } }
        refreshMailNativeStatus()
    }
    return ok
}

/** Instant cached analysis (no LLM call) if one was already produced on poll or earlier. */
suspend fun DenebGatewayClient.fetchCachedAnalysis(id: String): MailAnalysis? = callRpc<MailAnalysisOut>("miniapp.gmail.analysis_cached", buildJsonObject { put("id", id) })?.toAnalysis()

/** Run AI analysis; force=true reruns the LLM instead of returning the cached result. */
suspend fun DenebGatewayClient.analyzeMail(id: String, force: Boolean = false): MailAnalysis? = callRpc<MailAnalysisOut>(
    "miniapp.gmail.analyze",
    buildJsonObject {
        put("id", id)
        if (force) put("force", true)
    },
)?.toAnalysis()

private fun MailAnalysisOut.toAnalysis(): MailAnalysis? = if (analysis.isBlank()) {
    null
} else {
    MailAnalysis(
        text = analysis,
        related = relatedProjects.map { RelatedProject(it.path, it.title, it.summary) },
        cached = cached,
        createdAt = createdAt,
        durationMs = durationMs,
        workState = MailWorkState(
            analysisStatus = analysisStatus,
            analysisQuality = analysisQuality,
            feedStatus = feedStatus,
            calendarProposalCount = calendarProposalCount,
            todoCount = todoCount,
            hint = workStateHint,
        ),
    )
}

/** Ask a follow-up about a message; prior Q&A is sent as history for multi-turn context. */
suspend fun DenebGatewayClient.askMail(id: String, question: String, history: List<Pair<String, String>> = emptyList()): String? = callRpc<AskPayload>(
    "miniapp.gmail.ask",
    buildJsonObject {
        put("id", id)
        put("question", question)
        // History items use the generated QATurn wire shape (json q/a) so the
        // gateway's []QATurn binding actually receives them — the old hand-rolled
        // {question, answer} keys silently dropped all prior-turn context.
        putJsonArray("history") {
            history.forEach { (q, a) -> add(jsonCodec.encodeToJsonElement(QATurn.serializer(), QATurn(q = q, a = a))) }
        }
    },
)?.answer?.ifBlank { null }

/**
 * Download an attachment's raw bytes for in-app rendering (inline image
 * previews). Reuses [attachmentUrl] so auth matches the browser download path
 * exactly. Returns null on any failure — callers fall back to the plain chip.
 */
suspend fun DenebGatewayClient.fetchAttachmentBytes(messageId: String, att: MailAttachment): ByteArray? = runCatching {
    http.get(attachmentUrl(messageId, att)) {
        timeout {
            requestTimeoutMillis = 30_000
            connectTimeoutMillis = 6_000
        }
    }.body<ByteArray>()
}.getOrNull()

/**
 * Browser-openable attachment download URL. The download endpoint can't read
 * the X-Deneb-Client-Token header from a browser opening a link, so the token
 * rides in the query string (acceptable in this single-user local setup).
 */
fun DenebGatewayClient.attachmentUrl(messageId: String, att: MailAttachment): String {
    fun e(s: String) = s.encodeURLParameter()
    return "$gatewayUrl/api/v1/miniapp/gmail/attachment" +
        "?messageId=${e(messageId)}&attachmentId=${e(att.id)}" +
        "&filename=${e(att.filename)}&mimeType=${e(att.mimeType)}&clientToken=${e(clientToken)}"
}

/** Fetch wiki / relationship context for a message's sender. */
suspend fun DenebGatewayClient.fetchSenderContext(sender: String): SenderContext? {
    val p = callRpc<SenderContextPayload>(
        "miniapp.gmail.sender_context",
        buildJsonObject { put("sender", sender) },
    ) ?: return null
    return SenderContext(
        displayName = p.displayName.ifBlank { p.sender },
        email = p.email,
        recentCount = p.recent?.count ?: 0,
        windowDays = p.recent?.windowDays ?: 0,
        wikiHits = p.wikiHits.map { SenderWikiHit(it.title.ifBlank { it.path }, it.summary, it.category, it.path) },
        wikiFacts = p.wikiFacts,
    )
}

/** Recent messages from a specific sender (`list_recent` with a from: query). */
suspend fun DenebGatewayClient.fetchRecentFromSender(email: String, limit: Int = 15): List<MailMessage> {
    if (email.isBlank()) return emptyList()
    val payload = callRpc<MailListPayload>(
        "miniapp.gmail.list_recent",
        buildJsonObject {
            put("query", "from:\"$email\"")
            put("limit", limit)
        },
    ) ?: return emptyList()
    return applyReadOverlay(
        payload.messages
            .filter { it.id.isNotBlank() }
            .map { it.toDomainMailMessage() },
        locallyReadMailIds,
    )
}

private fun MailRowOut.toDomainMailMessage(): MailMessage = MailMessage(
    id = id,
    from = from,
    subject = subject,
    snippet = snippet,
    date = date,
    unread = isUnread,
    priority = priority,
    priorityHint = priorityHint,
    mailbox = mailbox,
    hasAttachment = hasAttachment || attachmentCount > 0,
    attachmentCount = attachmentCount,
    workState = MailWorkState(
        analysisStatus = analysisStatus,
        analysisQuality = analysisQuality,
        feedStatus = feedStatus,
        calendarProposalCount = calendarProposalCount,
        todoCount = todoCount,
        hint = workStateHint,
    ),
)
