package ai.deneb.deneb

import ai.deneb.deneb.generated.GroupwareApprovalActResponse
import ai.deneb.deneb.generated.GroupwareApprovalAnalysisOut
import ai.deneb.deneb.generated.GroupwareApprovalGetResponse
import ai.deneb.deneb.generated.GroupwareApprovalRow
import ai.deneb.deneb.generated.GroupwareApprovalsListResponse
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/** First-page size persisted to settings (matches the approvals screen page). */
internal const val APPROVALS_PERSIST_PAGE_SIZE = 20

// Session-scoped list cache. Fixed page sizes (20/40/…/100) make the key
// trivial: one entry per folder, and a larger fetch satisfies a smaller
// read via take(limit). Mirrors gmail listCache TTL — long enough for
// list ↔ detail / feed ↔ approvals hops, short enough that new 미결 show up.
private const val APPROVAL_LIST_CACHE_TTL_MS = 30_000L

private data class CachedApprovalsList(
    val folder: String,
    val rows: List<GroupwareApprovalRow>,
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

private fun freshCachedApprovals(folder: String, limit: Int, nowMs: Long): List<GroupwareApprovalRow>? {
    val hit = approvalListCache ?: return null
    if (hit.folder != folder) return null
    if (nowMs - hit.atMs > APPROVAL_LIST_CACHE_TTL_MS) return null
    if (hit.rows.size < limit) return null
    return hit.rows.take(limit)
}

private fun markApprovalActed(rows: List<GroupwareApprovalRow>, docId: String): List<GroupwareApprovalRow> = rows.map { row -> if (row.docId == docId) row.copy(canAct = false) else row }

// --- Settings-backed first-page cache (cache-then-network, like mail) -----
// Only folder=total undated lists are persisted. Owner fingerprint prevents a
// prior gateway/account cache from rendering under new credentials.
private val approvalsCacheJson = Json { ignoreUnknownKeys = true }

@Serializable
private data class ApprovalsCacheEnvelope(
    val owner: String = "",
    val rows: List<GroupwareApprovalRow> = emptyList(),
)

internal fun encodeApprovalsCache(rows: List<GroupwareApprovalRow>, owner: String): String = approvalsCacheJson.encodeToString(
    ApprovalsCacheEnvelope(owner = owner, rows = rows.take(APPROVALS_PERSIST_PAGE_SIZE)),
)

internal fun decodeApprovalsCache(json: String, expectedOwner: String): List<GroupwareApprovalRow>? = runCatching {
    approvalsCacheJson.decodeFromString<ApprovalsCacheEnvelope>(json)
}.getOrNull()
    ?.takeIf { it.owner == expectedOwner }
    ?.rows
    ?.take(APPROVALS_PERSIST_PAGE_SIZE)
    ?.takeIf { it.isNotEmpty() }

internal fun DenebGatewayClient.loadCachedApprovals(): List<GroupwareApprovalRow>? {
    val json = appSettings.getCachedApprovalsList() ?: return null
    return decodeApprovalsCache(json, mailCacheOwner(gatewayUrl, clientToken))
}

internal fun DenebGatewayClient.storeCachedApprovals(rows: List<GroupwareApprovalRow>) {
    appSettings.putCachedApprovalsList(
        encodeApprovalsCache(rows, mailCacheOwner(gatewayUrl, clientToken)),
    )
}

/**
 * Apply a membership/canAct change to the persisted first page so a kill before
 * the next refresh can't resurrect a still-미결 row. No-op when there's no cache.
 */
internal fun DenebGatewayClient.patchCachedApprovals(
    transform: (List<GroupwareApprovalRow>) -> List<GroupwareApprovalRow>,
) {
    val cached = loadCachedApprovals() ?: return
    storeCachedApprovals(transform(cached))
}

/**
 * Recent 전체 결재 (`miniapp.groupware.approvals.list`, folder=total by default).
 * Optional [date] (YYYY-MM-DD) asks the gateway to return only that day's rows.
 * Undated reads hit the session list cache when a large-enough page is fresh;
 * [forceRefresh] bypasses the cache (pull-to-refresh). Null on transport/auth failure.
 */
suspend fun DenebGatewayClient.fetchApprovals(
    folder: String = "total",
    limit: Int = 100,
    date: String? = null,
    forceRefresh: Boolean = false,
): List<GroupwareApprovalRow>? {
    val nowMs = kotlin.time.Clock.System.now().toEpochMilliseconds()
    if (!forceRefresh && date.isNullOrBlank()) {
        freshCachedApprovals(folder, limit, nowMs)?.let { return it }
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
    if (date.isNullOrBlank()) {
        approvalListCache = CachedApprovalsList(folder = folder, rows = rows, atMs = nowMs)
        // Persist the first page for cold-start paint (folder=total only — the
        // screen's default view; other folders aren't cached).
        if (folder == "total") storeCachedApprovals(rows)
    }
    return rows
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
        approvalListCache = approvalListCache?.let { hit ->
            hit.copy(rows = markApprovalActed(hit.rows, id), atMs = nowMs)
        }
        patchCachedApprovals { markApprovalActed(it, id) }
    }
    return out
}

// Session-scoped body cache: opening the same 결재 twice (list ↔ detail hops)
// must not pay the reader roundtrip again. The gateway keeps its own 10-minute
// disk cache; this only bridges in-session navigation.
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
