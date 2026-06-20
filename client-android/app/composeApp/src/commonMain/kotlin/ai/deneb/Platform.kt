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

expect suspend fun compressImageBytes(bytes: ByteArray, mimeType: String): ByteArray

expect fun openUrl(url: String): Boolean

/**
 * Launch the KakaoTalk app (a super-app bottom-tab action — not an in-app screen).
 * Android resolves the `com.kakao.talk` launch intent; if it isn't installed this
 * returns false (the caller can fall back to opening the Play Store page). Desktop,
 * iOS, and web have no Android-package concept, so they are no-ops returning false.
 * Unlike `tel:` (a URI any platform's UriHandler can open), launching a specific
 * Android package needs the platform intent, hence expect/actual rather than a URI.
 */
expect fun launchKakaoTalk(): Boolean

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

/**
 * Fires a background push notification for a heartbeat that produced a non-trivial
 * response. Android additionally wires a tap-to-open-heartbeat deep link via its
 * PendingIntent; iOS/desktop just surface the message in the OS notification center
 * without deep-linking back to the conversation. No-op on web.
 */
expect fun sendHeartbeatNotification(title: String, body: String)

/**
 * Like [sendHeartbeatNotification] but for proactive gateway reports
 * (morning-letter, email-analysis) pushed over the events stream. On Android,
 * tapping it deep-links to the 업무 (General) topic — where the report was
 * mirrored — rather than the heartbeat conversation. Other platforms surface it
 * the same way as a heartbeat notification.
 */
expect fun sendProactiveReportNotification(title: String, body: String)
