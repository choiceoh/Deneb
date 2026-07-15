package ai.deneb.deneb

import ai.deneb.deneb.generated.GroupwareApprovalActResponse
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
