package ai.deneb.deneb

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

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
