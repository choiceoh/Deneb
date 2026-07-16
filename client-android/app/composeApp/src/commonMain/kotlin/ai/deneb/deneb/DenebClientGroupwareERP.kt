package ai.deneb.deneb

import ai.deneb.deneb.generated.GroupwareBoardPostResponse
import ai.deneb.deneb.generated.GroupwareERPListResponse
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/** Read-only ERP snapshot (`miniapp.groupware.erp.list`). Null on failure. */
suspend fun DenebGatewayClient.fetchERP(
    area: String,
    folder: String? = null,
    query: String? = null,
    limit: Int? = null,
): GroupwareERPListResponse? {
    val a = area.trim()
    if (a.isEmpty()) return null
    return callRpc(
        "miniapp.groupware.erp.list",
        buildJsonObject {
            put("area", a)
            if (!folder.isNullOrBlank()) put("folder", folder)
            if (!query.isNullOrBlank()) put("query", query)
            if (limit != null && limit > 0) put("limit", limit)
        },
    )
}

/** One 게시판 post body by id/title (`miniapp.groupware.board.get`). Null on failure. */
suspend fun DenebGatewayClient.fetchBoardPost(query: String): GroupwareBoardPostResponse? {
    val q = query.trim()
    if (q.isEmpty()) return null
    return callRpc(
        "miniapp.groupware.board.get",
        buildJsonObject { put("query", q) },
    )
}
