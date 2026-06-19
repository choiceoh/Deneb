package ai.deneb.deneb

import ai.deneb.deneb.generated.FilesEntryOut
import ai.deneb.deneb.generated.FilesListOut
import ai.deneb.deneb.generated.FilesShareOut
import ai.deneb.deneb.generated.FilesUploadOut
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

/** Search the whole store by name query (results span folders). Null on failure. */
suspend fun DenebGatewayClient.filesSearch(query: String): List<FilesEntry>? = callRpc<FilesListOut>("miniapp.files.search", buildJsonObject { put("query", query) })
    ?.entries?.map { it.toEntry() }

/** Mint a signed, TTL-bounded download link for a file; open it to view/download. */
suspend fun DenebGatewayClient.filesShare(path: String): String? = callRpc<FilesShareOut>("miniapp.files.share", buildJsonObject { put("path", path) })
    ?.url?.ifBlank { null }

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
