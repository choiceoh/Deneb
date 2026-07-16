package ai.deneb.deneb

import ai.deneb.deneb.generated.GroupwareApprovalActResponse
import ai.deneb.deneb.generated.GroupwareApprovalAnalysisOut
import ai.deneb.deneb.generated.GroupwareApprovalGetResponse
import ai.deneb.deneb.generated.GroupwareApprovalRow
import ai.deneb.deneb.generated.GroupwareApprovalsListResponse
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/**
 * Recent 전체 결재 (`miniapp.groupware.approvals.list`, folder=total by default).
 * Optional [date] (YYYY-MM-DD) asks the gateway to return only that day's rows —
 * matching the 메일/피드 day-pager. Null on transport/auth failure.
 */
suspend fun DenebGatewayClient.fetchApprovals(
    folder: String = "total",
    limit: Int = 100,
    date: String? = null,
): List<GroupwareApprovalRow>? {
    val p = callRpc<GroupwareApprovalsListResponse>(
        "miniapp.groupware.approvals.list",
        buildJsonObject {
            put("folder", folder)
            put("limit", limit)
            if (!date.isNullOrBlank()) put("date", date)
        },
    ) ?: return null
    return p.approvals.filter { it.docId.isNotBlank() }
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
    return callRpc(
        "miniapp.groupware.approvals.act",
        buildJsonObject {
            put("docId", id)
            put("decision", decision)
            if (comment.isNotBlank()) put("comment", comment)
        },
    )
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
