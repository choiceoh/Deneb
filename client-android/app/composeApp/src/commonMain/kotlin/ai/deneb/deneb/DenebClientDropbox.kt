package ai.deneb.deneb

import ai.deneb.deneb.generated.DropboxEntryOut
import ai.deneb.deneb.generated.DropboxListOut
import ai.deneb.deneb.generated.DropboxShareOut
import ai.deneb.deneb.generated.DropboxUploadOut
import ai.deneb.ui.chat.History
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlin.io.encoding.Base64
import kotlin.io.encoding.ExperimentalEncodingApi

/**
 * The native deep-link URI Dropbox redirects to after consent, for the auto-
 * capture OAuth flow. Android returns "deneb://dropbox-auth" (a registered
 * intent-filter catches the redirect → [DropboxAuthBridge]); platforms without
 * custom-scheme handling (desktop/iOS/web) return null, which keeps the
 * out-of-band paste-code flow. The operator must register this exact URI in the
 * Dropbox App Console's OAuth redirect URIs.
 */
expect fun dropboxRedirectUri(): String?

/**
 * One-shot hand-off of the Dropbox authorization code from the platform deep-link
 * handler (Android MainActivity) to the connect wizard composable. The handler
 * sets [code]; [IntegrationsTab] collects it, exchanges it via dropboxComplete,
 * then clears it. A standalone object (not the DataRepository interface) keeps
 * this narrow one-shot signal out of every repository implementation.
 */
object DropboxAuthBridge {
    val code = MutableStateFlow<String?>(null)
}

/**
 * Dropbox connect surface of [DenebGatewayClient] (`miniapp.dropbox.*`): the
 * native PKCE OAuth wizard that links a Dropbox account from Settings > 연동,
 * replacing the host-side `deneb-dropbox-auth` CLI. Flow: status → begin (returns
 * the consent URL to open) → complete (the out-of-band code Dropbox shows after
 * approval). The refresh token is stored on the gateway host, enabling the
 * dropbox chat tool + artifact backup.
 */
@Serializable
data class DropboxStatusOut(
    val connected: Boolean = false,
    // True when an App key is already saved on the host, so a reconnect can skip
    // re-asking for it (begin reuses the saved app).
    val appConfigured: Boolean = false,
)

@Serializable
private data class DropboxBeginOut(val authUrl: String = "")

/** Current host-side Dropbox link state, or null if the status RPC failed. */
suspend fun DenebGatewayClient.dropboxStatus(): DropboxStatusOut? = callRpc<DropboxStatusOut>("miniapp.dropbox.status", buildJsonObject {})

/** Start the OAuth PKCE flow. Pass the Dropbox App key (blank on reconnect →
 *  the gateway reuses the saved one) and the deep-link redirectUri for auto-
 *  capture ("" → out-of-band paste-code). Returns the consent URL to open, or
 *  null on failure (e.g. missing/invalid App key). */
suspend fun DenebGatewayClient.dropboxBegin(appKey: String, redirectUri: String = ""): String? = callRpc<DropboxBeginOut>(
    "miniapp.dropbox.begin",
    buildJsonObject {
        put("appKey", appKey)
        put("redirectUri", redirectUri)
    },
)?.authUrl

/** Finish the flow by exchanging the pasted authorization code for a token.
 *  Returns false when the exchange fails (bad/expired code). */
suspend fun DenebGatewayClient.dropboxComplete(code: String): Boolean = callRpc<JsonObject>("miniapp.dropbox.complete", buildJsonObject { put("code", code) }) != null

/**
 * Dropbox file-browser surface of [DenebGatewayClient] (`miniapp.dropbox.{list,
 * search,share,upload,analyze}`) — powers [DenebDropboxScreen]. list/search/share/
 * upload are pure RPCs (return null on failure so the screen shows retry, not a
 * misleading empty folder); analyze runs an agent turn whose request + reply land
 * in the chat transcript, exactly like image/audio capture.
 */

/** One Dropbox file or folder for the browser UI (decoded from the wire shape). */
data class DropboxEntry(
    val name: String,
    val pathDisplay: String,
    val pathLower: String,
    val isFolder: Boolean,
    val size: Long,
    val modified: String,
)

private fun DropboxEntryOut.toEntry() = DropboxEntry(
    name = name,
    pathDisplay = pathDisplay.ifBlank { name },
    pathLower = pathLower,
    isFolder = tag == "folder",
    size = size,
    modified = serverModified,
)

/** List a folder's entries (""/"/" = account root). Null on failure. */
suspend fun DenebGatewayClient.dropboxList(path: String = ""): List<DropboxEntry>? = callRpc<DropboxListOut>("miniapp.dropbox.list", buildJsonObject { put("path", path) })
    ?.entries?.map { it.toEntry() }

/** Search the whole account by query (results span folders). Null on failure. */
suspend fun DenebGatewayClient.dropboxSearch(query: String): List<DropboxEntry>? = callRpc<DropboxListOut>("miniapp.dropbox.search", buildJsonObject { put("query", query) })
    ?.entries?.map { it.toEntry() }

/** Create (or fetch the existing) shared link for a file; open it to view/download. */
suspend fun DenebGatewayClient.dropboxShare(path: String): String? = callRpc<DropboxShareOut>("miniapp.dropbox.share", buildJsonObject { put("path", path) })
    ?.url?.ifBlank { null }

/** Upload device bytes to [destPath] (current folder + filename). Returns the
 *  stored file's metadata (autorenamed on a name clash), or null on failure. */
@OptIn(ExperimentalEncodingApi::class)
suspend fun DenebGatewayClient.dropboxUpload(destPath: String, bytes: ByteArray, mimeType: String = ""): DropboxEntry? = callRpc<DropboxUploadOut>(
    "miniapp.dropbox.upload",
    buildJsonObject {
        put("path", destPath)
        put("mimeType", mimeType)
        put("dataBase64", Base64.encode(bytes))
    },
)?.entry?.toEntry()

/** Analyze a Dropbox file via one agent turn (the agent's dropbox tool does
 *  download → extract → reason). The request and reply appear in the chat
 *  transcript, like image/audio capture. */
suspend fun DenebGatewayClient.dropboxAnalyze(path: String) {
    if (clientToken.isEmpty()) return
    _chatHistory.update { it + History(role = History.Role.USER, content = "📄 Dropbox 파일 분석 중… ($path)") }
    val reply = runCatching {
        callRpc<DropboxAnalyzePayload>(
            "miniapp.dropbox.analyze",
            buildJsonObject {
                put("path", path)
                put("sessionKey", sessionKey)
            },
        )?.text?.ifBlank { null } ?: "파일 분석에 실패했습니다."
    }.getOrElse { "⚠️ ${it.message ?: "분석 실패"}" }
    _chatHistory.update { it + History(role = History.Role.ASSISTANT, content = reply) }
    syncNativeStateAsync()
}

@Serializable
private data class DropboxAnalyzePayload(val text: String = "")
