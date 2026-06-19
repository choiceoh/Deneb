package ai.deneb.deneb

import ai.deneb.deneb.generated.NotebookListOut
import ai.deneb.deneb.generated.NotebookOut
import ai.deneb.deneb.generated.NotebookSummaryOut
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/**
 * Notebook surface of [DenebGatewayClient]: deal-anchored source collections
 * (`miniapp.notebook.*`). Read-only — pinning and brief synthesis stay in the
 * chat/agent path; this surface just lists notebooks and reads their pinned
 * evidence for the native viewer.
 */

/** All notebooks, most-recently-updated first. Null on a fetch failure so the
 *  screen can offer retry instead of a misleading "empty". */
suspend fun DenebGatewayClient.fetchNotebooks(): List<NotebookSummaryOut>? {
    val p = callRpc<NotebookListOut>("miniapp.notebook.list", buildJsonObject {}) ?: return null
    return p.notebooks
}

/** One notebook with its pinned sources, by id. Null on miss/failure. */
suspend fun DenebGatewayClient.fetchNotebook(id: String): NotebookOut? {
    return callRpc<NotebookOut>(
        "miniapp.notebook.get",
        buildJsonObject { put("id", id) },
    )
}
