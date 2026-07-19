package ai.deneb.deneb

import ai.deneb.deneb.generated.GroupwareApprovalActResponse
import ai.deneb.deneb.generated.GroupwareApprovalAnalysisOut
import ai.deneb.deneb.generated.GroupwareApprovalGetResponse
import ai.deneb.deneb.generated.GroupwareApprovalRow
import ai.deneb.deneb.generated.GroupwareApprovalsListResponse
import io.ktor.http.encodeURLParameter
import kotlinx.coroutines.flow.update
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/** First-page size persisted to settings (matches the approvals screen page). */
internal const val APPROVALS_PERSIST_PAGE_SIZE = 20

/** Gateway `maxApprovalsLimit` — requesting more is clamped server-side. */
internal const val APPROVALS_MAX_LIMIT = 100

// Session-scoped list cache for undated first-page reads (TTL). Larger pages
// and afterDocId loads always hit the network.
private const val APPROVAL_LIST_CACHE_TTL_MS = 30_000L

private data class CachedApprovalsList(
    val folder: String,
    val rows: List<GroupwareApprovalRow>,
    val nextAfterDocId: String?,
    val atMs: Long,
)

private var approvalListCache: CachedApprovalsList? = null

/** Drop the in-memory list cache (disk cache is patched separately on act). */
fun invalidateApprovalsListCache() {
    approvalListCache = null
}

/**
 * Last fetched rows for [folder], any age — for painting the screen before a
 * network roundtrip. Null when this session never loaded that folder.
 */
fun peekCachedApprovals(folder: String = "total"): List<GroupwareApprovalRow>? {
    val hit = approvalListCache ?: return null
    if (hit.folder != folder) return null
    return hit.rows
}

private fun freshCachedApprovalsPage(folder: String, limit: Int, nowMs: Long): CachedApprovalsList? {
    val hit = approvalListCache ?: return null
    if (hit.folder != folder) return null
    if (nowMs - hit.atMs > APPROVAL_LIST_CACHE_TTL_MS) return null
    if (hit.rows.size < limit && hit.nextAfterDocId != null) return null
    return hit.copy(rows = hit.rows.take(limit))
}

private fun markApprovalActed(rows: List<GroupwareApprovalRow>, docId: String): List<GroupwareApprovalRow> = rows.map { row -> if (row.docId == docId) row.copy(canAct = false) else row }

// --- Settings-backed first-page cache (cache-then-network, like mail) -----
private val approvalsCacheCodec = OwnedListCacheCodec("rows", GroupwareApprovalRow.serializer(), APPROVALS_PERSIST_PAGE_SIZE)

internal fun encodeApprovalsCache(rows: List<GroupwareApprovalRow>, owner: String): String = approvalsCacheCodec.encode(rows, owner)

internal fun decodeApprovalsCache(json: String, expectedOwner: String): List<GroupwareApprovalRow>? = approvalsCacheCodec.decode(json, expectedOwner)

internal fun DenebGatewayClient.loadCachedApprovals(): List<GroupwareApprovalRow>? = loadCachedOrClear(
    appSettings.getCachedApprovalsList(),
    { decodeApprovalsCache(it, mailCacheOwner(gatewayUrl, clientToken)) },
    appSettings::removeCachedApprovalsList,
)

internal fun DenebGatewayClient.storeCachedApprovals(rows: List<GroupwareApprovalRow>) {
    appSettings.putCachedApprovalsList(
        encodeApprovalsCache(rows, mailCacheOwner(gatewayUrl, clientToken)),
    )
}

internal fun DenebGatewayClient.patchCachedApprovals(
    transform: (List<GroupwareApprovalRow>) -> List<GroupwareApprovalRow>,
) {
    val cached = loadCachedApprovals() ?: return
    storeCachedApprovals(transform(cached))
}

/** Seed StateFlow from session/disk cache for an instant paint. */
internal fun DenebGatewayClient.seedApprovalsFromCache() {
    if (_denebApprovalsReady.value && _denebApprovals.value.isNotEmpty()) return
    val seed = peekCachedApprovals("total") ?: loadCachedApprovals() ?: return
    _denebApprovals.value = seed
    _denebApprovalsReady.value = true
    // Unknown whether more pages exist until the network refresh lands.
    if (_denebApprovalsNextAfter.value == null && seed.size >= APPROVALS_PERSIST_PAGE_SIZE) {
        _denebApprovalsNextAfter.value = seed.lastOrNull()?.docId
    }
}

/**
 * Recent 전체 결재 (`miniapp.groupware.approvals.list`, folder=total by default).
 * Replaces [_denebApprovals] on a first-page fetch; use [loadMoreApprovals] to append.
 * [forceRefresh] bypasses the session TTL cache (pull-to-refresh).
 */
suspend fun DenebGatewayClient.fetchApprovals(
    folder: String = "total",
    limit: Int = APPROVALS_PERSIST_PAGE_SIZE,
    date: String? = null,
    forceRefresh: Boolean = false,
): List<GroupwareApprovalRow>? {
    val nowMs = kotlin.time.Clock.System.now().toEpochMilliseconds()
    if (!forceRefresh && date.isNullOrBlank()) {
        freshCachedApprovalsPage(folder, limit, nowMs)?.let { hit ->
            _denebApprovals.value = hit.rows
            _denebApprovalsNextAfter.value = hit.nextAfterDocId
            _denebApprovalsReady.value = true
            return hit.rows
        }
    }
    val p = callRpc<GroupwareApprovalsListResponse>(
        "miniapp.groupware.approvals.list",
        buildJsonObject {
            put("folder", folder)
            put("limit", limit)
            if (!date.isNullOrBlank()) put("date", date)
        },
    ) ?: return null
    val rows = p.approvals.filter { it.docId.isNotBlank() }
    val nextAfter = p.nextAfterDocId.ifBlank { null }
    if (date.isNullOrBlank()) {
        approvalListCache = CachedApprovalsList(
            folder = folder,
            rows = rows,
            nextAfterDocId = nextAfter,
            atMs = nowMs,
        )
        if (folder == "total") storeCachedApprovals(rows)
        _denebApprovals.value = rows
        _denebApprovalsNextAfter.value = nextAfter
        _denebApprovalsReady.value = true
    }
    return rows
}

/** Append the next page using [denebApprovalsNextAfter]. No-op when exhausted. */
suspend fun DenebGatewayClient.loadMoreApprovals(
    folder: String = "total",
    limit: Int = APPROVALS_PERSIST_PAGE_SIZE,
) {
    val after = _denebApprovalsNextAfter.value ?: return
    if (_denebApprovals.value.size >= APPROVALS_MAX_LIMIT) {
        _denebApprovalsNextAfter.value = null
        return
    }
    val p = callRpc<GroupwareApprovalsListResponse>(
        "miniapp.groupware.approvals.list",
        buildJsonObject {
            put("folder", folder)
            put("limit", limit)
            put("afterDocId", after)
        },
    ) ?: return
    val page = p.approvals.filter { it.docId.isNotBlank() }
    if (page.isEmpty()) {
        _denebApprovalsNextAfter.value = null
        return
    }
    val seen = _denebApprovals.value.mapTo(HashSet()) { it.docId }
    val appended = page.filter { it.docId !in seen }
    val merged = (_denebApprovals.value + appended).take(APPROVALS_MAX_LIMIT)
    _denebApprovals.value = merged
    _denebApprovalsNextAfter.value = when {
        merged.size >= APPROVALS_MAX_LIMIT -> null
        else -> p.nextAfterDocId.ifBlank { null }
    }
    // Keep session cache as first page only; persist stays first page too.
    approvalListCache = approvalListCache?.copy(
        rows = merged.take(APPROVALS_PERSIST_PAGE_SIZE).ifEmpty { merged },
        nextAfterDocId = _denebApprovalsNextAfter.value,
        atMs = kotlin.time.Clock.System.now().toEpochMilliseconds(),
    )
}

/**
 * Operator 승인/반려 (`miniapp.groupware.approvals.act`). Null on failure so the
 * screen can keep the row and offer retry — Amaranth mutate is irreversible.
 */
suspend fun DenebGatewayClient.actApproval(
    docId: String,
    decision: String,
    comment: String = "",
): GroupwareApprovalActResponse? {
    val id = docId.trim()
    if (id.isEmpty()) return null
    val out = callRpc<GroupwareApprovalActResponse>(
        "miniapp.groupware.approvals.act",
        buildJsonObject {
            put("docId", id)
            put("decision", decision)
            if (comment.isNotBlank()) put("comment", comment)
        },
    ) ?: return null
    if (out.ok) {
        val nowMs = kotlin.time.Clock.System.now().toEpochMilliseconds()
        _denebApprovals.update { markApprovalActed(it, id) }
        approvalListCache = approvalListCache?.let { hit ->
            hit.copy(rows = markApprovalActed(hit.rows, id), atMs = nowMs)
        }
        patchCachedApprovals { markApprovalActed(it, id) }
    }
    return out
}

// Session-scoped body cache: opening the same 결재 twice (list ↔ detail hops)
// must not pay the reader roundtrip again. The gateway keeps its own disk
// cache; this only bridges in-session navigation.
private const val APPROVAL_BODY_CACHE_MAX = 24
private const val APPROVAL_BODY_CACHE_TTL_MS = 5 * 60 * 1000L

private data class CachedApprovalBody(val body: ApprovalBody, val atMs: Long)

private val approvalBodyCache = LinkedHashMap<String, CachedApprovalBody>()

/**
 * Document body (`miniapp.groupware.approvals.get`). Null on failure.
 * [folder] is the list row's box hint — the gateway skips the 4-folder scan.
 */
suspend fun DenebGatewayClient.fetchApprovalBody(
    docId: String,
    title: String? = null,
    folder: String? = null,
): ApprovalBody? {
    val id = docId.trim()
    if (id.isEmpty()) return null
    val nowMs = kotlin.time.Clock.System.now().toEpochMilliseconds()
    approvalBodyCache[id]?.let { hit ->
        if (nowMs - hit.atMs <= APPROVAL_BODY_CACHE_TTL_MS) return hit.body
    }
    val fetched = callRpc<GroupwareApprovalGetResponse>(
        "miniapp.groupware.approvals.get",
        buildJsonObject {
            put("docId", id)
            if (!title.isNullOrBlank()) put("title", title)
            if (!folder.isNullOrBlank()) put("folder", folder)
        },
    )?.let { ApprovalBody(docId = it.docId, title = it.title, body = it.body) } ?: return null
    approvalBodyCache[id] = CachedApprovalBody(fetched, nowMs)
    while (approvalBodyCache.size > APPROVAL_BODY_CACHE_MAX) {
        approvalBodyCache.remove(approvalBodyCache.keys.first())
    }
    return fetched
}

/**
 * Browser-openable 결재 첨부 download URL (mail [attachmentUrl] parity). The
 * download endpoint can't read the X-Deneb-Client-Token header from a link
 * open, so the token rides in the query string (single-user local setup).
 * [selector] is the 1-based row index (or filename) parsed from the 첨부 block;
 * [filename] keeps the extension so Android picks the right viewer app.
 */
fun DenebGatewayClient.approvalAttachmentUrl(docId: String, selector: String, filename: String = ""): String {
    fun e(s: String) = s.encodeURLParameter()
    return "$gatewayUrl/api/v1/miniapp/groupware/approval/attachment" +
        "?docId=${e(docId)}&attachment=${e(selector)}" +
        (if (filename.isBlank()) "" else "&filename=${e(filename)}") +
        "&clientToken=${e(clientToken)}"
}

/** Instant cached analysis (no LLM) if one was already produced. */
suspend fun DenebGatewayClient.fetchCachedApprovalAnalysis(docId: String): ApprovalAnalysis? {
    val id = docId.trim()
    if (id.isEmpty()) return null
    return callRpc<GroupwareApprovalAnalysisOut>(
        "miniapp.groupware.approvals.analysis_cached",
        buildJsonObject { put("docId", id) },
    )?.toApprovalAnalysis()
}

/** Run AI analysis; force=true reruns the LLM instead of returning the cached result. */
suspend fun DenebGatewayClient.analyzeApproval(
    docId: String,
    title: String? = null,
    force: Boolean = false,
    drafter: String? = null,
    date: String? = null,
): ApprovalAnalysis? = callRpc<GroupwareApprovalAnalysisOut>(
    "miniapp.groupware.approvals.analyze",
    buildJsonObject {
        put("docId", docId.trim())
        if (!title.isNullOrBlank()) put("title", title)
        if (force) put("force", true)
        if (!drafter.isNullOrBlank()) put("drafter", drafter)
        if (!date.isNullOrBlank()) put("date", date)
    },
)?.toApprovalAnalysis()

private fun GroupwareApprovalAnalysisOut.toApprovalAnalysis(): ApprovalAnalysis? = if (analysis.isBlank()) {
    null
} else {
    ApprovalAnalysis(
        text = analysis,
        importance = importance,
        cached = cached,
        createdAt = createdAt,
        durationMs = durationMs,
    )
}
