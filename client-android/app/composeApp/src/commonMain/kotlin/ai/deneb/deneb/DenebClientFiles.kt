package ai.deneb.deneb

import ai.deneb.deneb.generated.FilesEntryOut
import ai.deneb.deneb.generated.FilesListOut
import ai.deneb.deneb.generated.FilesShareOut
import ai.deneb.deneb.generated.FilesUploadOut
import io.ktor.client.request.get
import io.ktor.client.request.header
import io.ktor.client.statement.bodyAsText
import io.ktor.http.encodeURLParameter
import io.ktor.http.isSuccess
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlin.io.encoding.Base64
import kotlin.io.encoding.ExperimentalEncodingApi

/**
 * Local file-store browser surface of [DenebGatewayClient] (`miniapp.files.{list,
 * search,share,upload}`) — powers [DenebFilesScreen]. The store lives on the
 * gateway host: no OAuth, no connect wizard — it is always available; failures
 * just surface as retry/empty.
 *
 * All four are pure RPCs that return null on failure (so the screen shows retry,
 * not a misleading empty folder). share returns a signed, TTL-bounded download
 * link. "Analyze a file" is intentionally absent here — it runs a full agent
 * turn via the chat bridge.
 */

/** One local file or folder for the browser UI (decoded from the wire shape). */
data class FilesEntry(
    val name: String,
    val pathDisplay: String,
    val pathLower: String,
    val isFolder: Boolean,
    val size: Long,
    val modified: String,
    // Stable LazyColumn key (paths can be missing/duplicated on degenerate search
    // hits; id is the store's stable identifier).
    val id: String = "",
)

private fun FilesEntryOut.toEntry() = FilesEntry(
    name = name,
    pathDisplay = pathDisplay.ifBlank { name },
    pathLower = pathLower,
    isFolder = tag == "folder",
    size = size,
    modified = serverModified,
    id = id,
)

/** List a folder's entries (""/"/" = store root). Null on failure. */
suspend fun DenebGatewayClient.filesList(path: String = ""): List<FilesEntry>? = callRpc<FilesListOut>("miniapp.files.list", buildJsonObject { put("path", path) })
    ?.entries?.map { it.toEntry() }

/**
 * Search the whole store by query (results span folders). Null on failure.
 *
 * Three search scopes (the wire flags are mutually exclusive here):
 * - default (both false): match file names only — fastest.
 * - [content] true: also match inside extracted file text (PDF/Word/Excel/…),
 *   slower since the gateway reads and extracts each file.
 * - [semantic] true: BGE-M3 meaning search (results come back ranked by score
 *   from the backend); takes priority over [content] when both are set. If the
 *   embedding server is down the gateway falls back to name/content on its own,
 *   so the UI needs no special handling.
 */
suspend fun DenebGatewayClient.filesSearch(
    query: String,
    content: Boolean = false,
    semantic: Boolean = false,
): List<FilesEntry>? = callRpc<FilesListOut>(
    "miniapp.files.search",
    buildJsonObject {
        put("query", query)
        // semantic wins over content (exclusive) so we never send both.
        if (semantic) {
            put("semantic", true)
        } else if (content) {
            put("content", true)
        }
    },
)?.entries?.map { it.toEntry() }

/** Mint a signed, TTL-bounded download link for a file; open it to view/download. */
suspend fun DenebGatewayClient.filesShare(path: String): String? = callRpc<FilesShareOut>("miniapp.files.share", buildJsonObject { put("path", path) })
    ?.url?.ifBlank { null }

/** Delete a file or empty folder. Returns null on success, else the gateway's
 *  error message (so the screen can show the exact reason, e.g. a non-empty folder). */
suspend fun DenebGatewayClient.filesDelete(path: String): String? = rpcWrite("miniapp.files.delete", buildJsonObject { put("path", path) })

/** Create a folder (parents included). Returns null on success, else the error message. */
suspend fun DenebGatewayClient.filesMkdir(path: String): String? = rpcWrite("miniapp.files.mkdir", buildJsonObject { put("path", path) })

/** Move/rename [src] to [dst] (a rename is a same-folder move; an existing target
 *  is auto-renamed by the store). Returns null on success, else the error message. */
suspend fun DenebGatewayClient.filesMove(src: String, dst: String): String? = rpcWrite(
    "miniapp.files.move",
    buildJsonObject {
        put("src", src)
        put("dst", dst)
    },
)

/** Upload device bytes to [destPath] (current folder + filename). Returns the
 *  stored file's metadata (autorenamed on a name clash), or null on failure. */
@OptIn(ExperimentalEncodingApi::class)
suspend fun DenebGatewayClient.filesUpload(destPath: String, bytes: ByteArray, mimeType: String = ""): FilesEntry? = callRpc<FilesUploadOut>(
    "miniapp.files.upload",
    buildJsonObject {
        put("path", destPath)
        put("mimeType", mimeType)
        put("dataBase64", Base64.encode(bytes))
    },
)?.entry?.toEntry()

/**
 * Direct download URL for a file, authed by the client token in the query string
 * (the gateway's GET /api/v1/files/download can't read the X-Deneb-Client-Token
 * header from a browser/Coil fetch, so the token rides the URL — acceptable in
 * this single-user local setup, same as [DenebGatewayClient.attachmentUrl]).
 * Coil fetches this directly for the in-app image viewer; it also supports Range,
 * so a large file resumes over a flaky link.
 */
fun DenebGatewayClient.filesDownloadUrl(pathLower: String): String {
    fun e(s: String) = s.encodeURLParameter()
    return "$gatewayUrl/api/v1/files/download?path=${e(pathLower)}&clientToken=${e(clientToken)}"
}

/**
 * Fetch a file's body as text for the in-app text/markdown viewer, capped at
 * [maxBytes] so a huge log can't blow up memory (the body is truncated, with a
 * marker appended). Null on any transport/HTTP failure — the caller then falls
 * back to the share link.
 */
suspend fun DenebGatewayClient.filesDownloadText(pathLower: String, maxBytes: Int = 256 * 1024): String? {
    if (clientToken.isEmpty() || gatewayUrl.isBlank()) return null
    return runCatching {
        val resp = http.get(filesDownloadUrl(pathLower)) {
            header(DenebGatewayClient.CLIENT_TOKEN_HEADER, clientToken)
        }
        if (!resp.status.isSuccess()) return@runCatching null
        val body = resp.bodyAsText()
        // Cap by char count (a safe proxy for the byte budget here — bounded
        // protection, not an exact byte slice). A multibyte tail is fine.
        if (body.length > maxBytes) {
            body.take(maxBytes) + "\n\n…(이하 생략 — 파일이 너무 큽니다)"
        } else {
            body
        }
    }.getOrNull()
}
