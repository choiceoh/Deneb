package ai.deneb

import androidx.compose.ui.draganddrop.DragAndDropEvent
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.vector.ImageVector
import ai.deneb.network.tools.Tool
import ai.deneb.network.tools.ToolInfo
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

expect fun getAvailableTools(): List<Tool>

/**
 * Returns all raw tool definitions available on this platform.
 * The returned tools have no isEnabled state set - that's handled by RemoteDataRepository.
 * Unlike getAvailableTools(), this returns all tools regardless of enabled state.
 */
expect fun getPlatformToolDefinitions(): List<ToolInfo>

expect val isEmailSupported: Boolean

/**
 * True only on the FOSS Android build. Gated on `READ_SMS` being declared in the
 * merged manifest — the Play Store flavor doesn't declare it, so this returns
 * false there, and the SMS feature is invisible in that build.
 */
expect val isSmsSupported: Boolean

/**
 * True only on the FOSS Android build. Gated on `DenebNotificationListenerService`
 * being declared in the merged manifest — the Play Store flavor doesn't declare
 * it, so this returns false there, and the notification-reading feature is
 * invisible in that build.
 */
expect val isNotificationsSupported: Boolean

expect suspend fun compressImageBytes(bytes: ByteArray, mimeType: String): ByteArray

expect fun openUrl(url: String): Boolean

@androidx.compose.runtime.Composable
expect fun PlatformBackHandler(enabled: Boolean, onBack: () -> Unit)

expect fun decodeToImageBitmap(bytes: ByteArray): ImageBitmap?

expect suspend fun saveFileToDevice(bytes: ByteArray, baseName: String, extension: String)

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
