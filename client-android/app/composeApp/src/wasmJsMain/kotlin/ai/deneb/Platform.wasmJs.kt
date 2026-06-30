package ai.deneb

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.ui.draganddrop.DragAndDropEvent
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.toComposeImageBitmap
import androidx.compose.ui.graphics.vector.ImageVector
import com.russhwolf.settings.Settings
import com.russhwolf.settings.StorageSettings
import io.github.vinceglb.filekit.FileKit
import io.github.vinceglb.filekit.PlatformFile
import io.github.vinceglb.filekit.download
import io.ktor.client.HttpClient
import io.ktor.client.HttpClientConfig
import io.ktor.client.engine.js.Js
import kotlin.coroutines.CoroutineContext
import kotlin.coroutines.EmptyCoroutineContext

actual fun httpClient(config: HttpClientConfig<*>.() -> Unit): HttpClient = HttpClient(Js) {
    config(this)
}

actual fun getBackgroundDispatcher(): CoroutineContext = EmptyCoroutineContext

actual fun onDragAndDropEventDropped(event: DragAndDropEvent): PlatformFile? = null

actual val BackIcon: ImageVector = Icons.AutoMirrored.Filled.ArrowBack

actual val currentPlatform: Platform = Platform.Web

actual val defaultUiScale: Float = 1.0f

actual val isEmailSupported: Boolean = false

actual val isSmsSupported: Boolean = false

actual suspend fun compressImageBytes(bytes: ByteArray, mimeType: String): ByteArray = bytes

actual fun getAppFilesDirectory(): String {
    // Web uses localStorage, return empty string as no file path is needed
    return ""
}

actual fun createSecureSettings(): Settings {
    // Web has no secure storage - using localStorage
    return StorageSettings()
}

actual fun createLegacySettings(): Settings? = null // Same storage location, no migration needed

// No durable mirror needed: web localStorage already persists across reloads.
actual fun createDurableSettings(): Settings? = null

actual fun openUrl(url: String): Boolean = try {
    kotlinx.browser.window.open(url, "_blank")
    true
} catch (_: Exception) {
    false
}

// The 카톡 tab launches the Android package; web has no equivalent — no-op.

// Web can only open a URL in a new tab; telephony/SMS/camera/app-launch have no
// browser equivalent, so every non-open_url phone action no-ops.
actual fun executePhoneAction(action: String, args: Map<String, String>): Boolean = if (action == "open_url") openUrl(args["url"].orEmpty()) else false

actual fun decodeToImageBitmap(bytes: ByteArray): ImageBitmap? = try {
    org.jetbrains.skia.Image.makeFromEncoded(bytes).toComposeImageBitmap()
} catch (_: Exception) {
    null
}

@androidx.compose.runtime.Composable
actual fun PlatformBackHandler(enabled: Boolean, onBack: () -> Unit) {
    // No system back gesture on web
}

actual suspend fun saveFileToDevice(bytes: ByteArray, baseName: String, extension: String) {
    FileKit.download(bytes = bytes, fileName = "$baseName.$extension")
}

// No web share sheet wired — fall back to the browser download.
actual suspend fun shareImageToApps(bytes: ByteArray, baseName: String, extension: String) = saveFileToDevice(bytes, baseName, extension)

// Web notifications API isn't wired up; stub.
actual fun sendHeartbeatNotification(title: String, body: String) = Unit

actual fun sendProactiveReportNotification(title: String, body: String) = Unit
