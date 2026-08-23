package ai.deneb

import androidx.compose.ui.draganddrop.DragAndDropEvent
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.vector.ImageVector
import com.russhwolf.settings.Settings
import io.github.vinceglb.filekit.PlatformFile
import io.ktor.client.HttpClient
import io.ktor.client.HttpClientConfig
import kotlin.coroutines.CoroutineContext

expect fun httpClient(config: HttpClientConfig<*>.() -> Unit = {}): HttpClient

expect fun createSecureSettings(): Settings

expect fun createLegacySettings(): Settings?

/**
 * An optional second, *unencrypted* settings store used only to mirror a small
 * whitelist of must-survive keys (the gateway URL + client token — see
 * [ai.deneb.data.DurableMirrorSettings]). Returns null on platforms whose secure
 * store already survives app updates (desktop file key, iOS Keychain).
 *
 * Android returns a real plain SharedPreferences file because its encrypted store
 * (kai_secure_prefs) is deleted and recreated empty whenever it can't be decrypted
 * after an app update / Auto Backup restore (the hardware Keystore key doesn't
 * transfer) — which silently wiped the gateway token every update. The mirror file
 * is not touched by that wipe, so url+token survive.
 */
expect fun createDurableSettings(): Settings?

expect fun getBackgroundDispatcher(): CoroutineContext

expect fun onDragAndDropEventDropped(event: DragAndDropEvent): PlatformFile?

expect val BackIcon: ImageVector

sealed class Platform(val displayName: String) {
    sealed class Mobile(displayName: String) : Platform(displayName) {
        data object Android : Mobile("Android")
        data object Ios : Mobile("iOS")
    }

    sealed class Desktop(displayName: String) : Platform(displayName) {
        data object Mac : Desktop("macOS")
        data object Windows : Desktop("Windows")
        data object Linux : Desktop("Linux")
    }

    data object Web : Platform("Web")
}

expect val currentPlatform: Platform

expect val defaultUiScale: Float

expect fun getAppFilesDirectory(): String

expect val isEmailSupported: Boolean

/**
 * True only on the FOSS Android build. Gated on `READ_SMS` being declared in the
 * merged manifest — the Play Store flavor doesn't declare it, so this returns
 * false there, and the SMS feature is invisible in that build.
 */
expect val isSmsSupported: Boolean

// Downsample an image to at most maxDim on its longer side and re-encode as JPEG.
// Default 2048px keeps a photographed document comfortably readable for the gateway's
// OCR while cutting a 10-15MB camera shot to a few hundred KB. Non-image bytes pass
// through unchanged.
expect suspend fun compressImageBytes(bytes: ByteArray, mimeType: String, maxDim: Int = 2048): ByteArray

expect fun openUrl(url: String): Boolean

@androidx.compose.runtime.Composable
expect fun PlatformBackHandler(enabled: Boolean, onBack: () -> Unit)

expect fun decodeToImageBitmap(bytes: ByteArray): ImageBitmap?

expect suspend fun saveFileToDevice(bytes: ByteArray, baseName: String, extension: String)

// Hand bytes to the OS share surface so the user can send the image to another app
// (messaging, mail, save-to-Photos via the sheet). Android = ACTION_SEND chooser,
// iOS = UIActivityViewController; desktop/web have no share sheet and fall back to
// the save dialog (saveFileToDevice). Best-effort — a no-op if the surface is
// unavailable. Pairs with saveFileToDevice (direct save) for the image viewer's
// two export actions.
expect suspend fun shareImageToApps(bytes: ByteArray, baseName: String, extension: String)

// Share plain text to other apps (a chat message shared onward to 메신저/mail).
// Android = ACTION_SEND chooser, iOS = UIActivityViewController; desktop copies
// to the clipboard (no OS share sheet), web is a stub. Best-effort like
// shareImageToApps.
expect suspend fun shareTextToApps(text: String)

/**
 * Fires a background push notification for a heartbeat that produced a non-trivial
 * response. Android additionally wires a tap-to-open-heartbeat deep link via its
 * PendingIntent; iOS/desktop just surface the message in the OS notification center
 * without deep-linking back to the conversation. No-op on web.
 */
expect fun sendHeartbeatNotification(title: String, body: String)

/**
 * Like [sendHeartbeatNotification] but for proactive gateway reports pushed over
 * the events stream. Android deep-links a `kind=workfeed` notification with a
 * non-blank [ref] to that exact feed card; older payloads without a ref retain
 * the legacy 업무-topic destination. [approveActionId]/[rejectActionId] carry
 * the card's approval:* actions (Trust Inbox): Android adds tray 승인/거절
 * buttons that settle the decision without opening the app. Other platforms
 * surface it like a heartbeat.
 */
expect fun sendProactiveReportNotification(
    title: String,
    body: String,
    kind: String = "",
    ref: String = "",
    approveActionId: String? = null,
    rejectActionId: String? = null,
)

/**
 * Executes a phone Intent action the gateway's phone_write tool dispatched over
 * the events stream (kind=phone_action). [action] is one of open_url / open_app /
 * share / message / dial / photo / alarm / timer; [args] carries that action's
 * parameters as the gateway built them — url, package, number, text, to, and for
 * alarm/timer: hour, minute, seconds, label. Returns true when the intent was
 * launched. Best-effort and platform-subset: Android wires real Intents
 * (alarm/timer via the clock app's ACTION_SET_ALARM / ACTION_SET_TIMER);
 * desktop/web/iOS cover what they can (open_url, dial/message via URI scheme)
 * and no-op the rest, returning false.
 */
expect fun executePhoneAction(action: String, args: Map<String, String>): Boolean
