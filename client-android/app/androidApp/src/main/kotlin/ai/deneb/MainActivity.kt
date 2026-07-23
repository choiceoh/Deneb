package ai.deneb

import ai.deneb.data.AppSettings
import ai.deneb.data.AttachmentRoute
import ai.deneb.data.DataRepository
import ai.deneb.data.MAX_RAW_IMAGE_BYTES
import ai.deneb.data.ThemeMode
import ai.deneb.data.attachmentExtension
import ai.deneb.data.documentCaptureMime
import ai.deneb.data.isWithinAttachmentSize
import ai.deneb.data.routeAttachment
import ai.deneb.deneb.DenebAttachment
import ai.deneb.deneb.DenebGatewayClient
import ai.deneb.deneb.captureBatch
import ai.deneb.ui.DarkColorScheme
import ai.deneb.ui.LightColorScheme
import ai.deneb.ui.chat.composables.CaptureActions
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.provider.OpenableColumns
import android.speech.RecognizerIntent
import android.view.animation.DecelerateInterpolator
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.tooling.preview.Preview
import androidx.core.splashscreen.SplashScreen.Companion.installSplashScreen
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.lifecycleScope
import androidx.navigation.compose.rememberNavController
import io.github.vinceglb.filekit.FileKit
import io.github.vinceglb.filekit.PlatformFile
import io.github.vinceglb.filekit.dialogs.init
import io.github.vinceglb.filekit.extension
import io.github.vinceglb.filekit.name
import io.github.vinceglb.filekit.readBytes
import kotlinx.coroutines.launch
import nl.marc_apps.tts.TextToSpeechEngine
import nl.marc_apps.tts.rememberTextToSpeechOrNull
import org.koin.android.ext.android.get

class MainActivity : ComponentActivity() {
    private var pendingWorkFeedItemId by mutableStateOf<String?>(null)

    // System speech-to-text result -> Deneb chat (the 음성 캡처 app shortcut).
    private val speechLauncher =
        registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
            val spoken = result.data
                ?.getStringArrayListExtra(RecognizerIntent.EXTRA_RESULTS)
                ?.firstOrNull()
                ?.trim()
                .orEmpty()
            if (spoken.isNotEmpty()) {
                lifecycleScope.launch { get<DataRepository>().ask("🎤 $spoken", emptyList(), null) }
            }
        }

    override fun onCreate(savedInstanceState: Bundle?) {
        // SplashScreen API + custom exit: the launcher icon fades/scales out into
        // the content instead of the default hard cut — "opened", not "switched
        // on". Duration/curve mirror the compose motion doctrine's snappy tier
        // (this is a View-layer animator, so the spec is inlined with intent).
        installSplashScreen().setOnExitAnimationListener { splash ->
            // splash.iconView is a null-assertion seam: on Android 12+ the
            // platform SplashScreenView can carry no icon view (warm start /
            // iconless exit) and the compat getter throws NPE — 12 startup
            // crashes on builds 744-759 (Android 36). No icon → nothing to
            // animate; remove the splash immediately instead of crashing.
            val icon = runCatching { splash.iconView }.getOrNull()
            if (icon == null) {
                splash.remove()
                return@setOnExitAnimationListener
            }
            icon.animate()
                .alpha(0f)
                .scaleX(1.12f)
                .scaleY(1.12f)
                .setDuration(220L)
                .setInterpolator(DecelerateInterpolator())
                .withEndAction { splash.remove() }
                .start()
        }
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        FileKit.init(this)
        handleDeepLinkIntent(intent)
        handleShareIntent(intent)
        handleVoiceIntent(intent)

        val dynamicColor = Build.VERSION.SDK_INT >= Build.VERSION_CODES.S
        val appSettings: AppSettings = get()
        setContent {
            val themeMode by appSettings.themeModeFlow.collectAsStateWithLifecycle()
            val systemInDark = isSystemInDarkTheme()
            val isDarkTheme = when (themeMode) {
                ThemeMode.System -> systemInDark
                ThemeMode.Light -> false
                ThemeMode.Dark, ThemeMode.OledBlack -> true
            }
            LaunchedEffect(isDarkTheme) {
                enableEdgeToEdge(
                    statusBarStyle = if (isDarkTheme) {
                        SystemBarStyle.dark(android.graphics.Color.TRANSPARENT)
                    } else {
                        SystemBarStyle.light(
                            android.graphics.Color.TRANSPARENT,
                            android.graphics.Color.TRANSPARENT,
                        )
                    },
                    navigationBarStyle = if (isDarkTheme) {
                        SystemBarStyle.dark(android.graphics.Color.TRANSPARENT)
                    } else {
                        SystemBarStyle.light(
                            android.graphics.Color.TRANSPARENT,
                            android.graphics.Color.TRANSPARENT,
                        )
                    },
                )
            }
            val context = LocalContext.current
            val lightScheme: ColorScheme = if (dynamicColor) dynamicLightColorScheme(context) else LightColorScheme
            val darkScheme: ColorScheme = if (dynamicColor) dynamicDarkColorScheme(context) else DarkColorScheme
            val navController = rememberNavController()
            // Defer TTS initialization until after the first frame
            var ttsReady by remember { mutableStateOf(false) }
            LaunchedEffect(Unit) { ttsReady = true }
            val textToSpeech = if (ttsReady) {
                rememberTextToSpeechOrNull(TextToSpeechEngine.Google)
            } else {
                null
            }
            App(
                navController = navController,
                lightColorScheme = lightScheme,
                darkColorScheme = darkScheme,
                textToSpeech = textToSpeech,
                isKoinStarted = true,
                onAppOpens = { appOpens ->
                    if (appOpens % 5 == 0) {
                        requestReview(this@MainActivity)
                    }
                },
                captureActions = CaptureActions(
                    // The chat input stages the picked files and hands us the whole set
                    // on send, plus the composer text as the caption — we read the bytes
                    // and run one gateway batch capture.
                    onFilesBatch = { files, caption -> captureFilesBatch(files, caption) },
                    onVoiceInput = { launchVoiceCapture() },
                ),
                openWorkFeedItemId = pendingWorkFeedItemId,
                onWorkFeedItemConsumed = { consumedId ->
                    if (pendingWorkFeedItemId == consumedId) pendingWorkFeedItemId = null
                },
            )
        }
    }

    override fun onStart() {
        super.onStart()
        // Opt the window into the panel's fastest display mode for 120Hz scrolling.
        requestHighestRefreshRate()
        // Re-assert the daemon every time the activity is brought to the foreground.
        // `onCreate`-only is not enough: aggressive OEM battery managers (MIUI,
        // EMUI/Huawei) sometimes kill the foreground service while the activity
        // is still alive in the background — without this, the user has to fully
        // close and reopen the app for scheduling to resume. `startForegroundService`
        // is idempotent when the service is already up.
        autoStartDaemon()
        // Register the FCM token so the gateway can push proactive reports when the
        // app is fully closed / in Doze (no live SSE). Idempotent + best-effort:
        // the gateway dedups by token, and a build without Firebase no-ops.
        FcmRegistration.fetchAndRegister(get())
        // Ship any crash reports saved on a previous run (uncaught exceptions the app
        // can't send at crash time) now that the client is up. Best-effort: a no-op
        // when there's nothing saved; undelivered reports retry next foreground.
        (get<DataRepository>() as? DenebGatewayClient)?.let { client ->
            lifecycleScope.launch { CrashReporter.flushPending(applicationContext, client) }
        }
    }

    private fun autoStartDaemon() {
        val daemonController: DaemonController = get()
        val repository: DataRepository = get()
        val shouldStart = repository is DenebGatewayClient ||
            (daemonController is AndroidDaemonController && daemonController.shouldAutoStart())
        if (shouldStart) {
            daemonController.start()
        }
    }

    // 120Hz scrolling. The app otherwise makes no display-mode request, so on a
    // high-refresh panel (Galaxy S26) the window can stay pinned to a 60Hz mode.
    // Request the fastest mode at the current resolution; Compose scroll/animation
    // follow Choreographer, so they then run at the panel's full rate. LTPO panels
    // still idle down when the screen is static, so this raises the ceiling rather
    // than pinning the rate. No effect if the system "Motion smoothness" setting is
    // Standard — that caps every app at 60Hz and can't be overridden.
    private fun requestHighestRefreshRate() {
        val activeDisplay = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            display
        } else {
            @Suppress("DEPRECATION")
            windowManager.defaultDisplay
        } ?: return
        val active = activeDisplay.mode ?: return
        val fastest = activeDisplay.supportedModes
            .filter { it.physicalWidth == active.physicalWidth && it.physicalHeight == active.physicalHeight }
            .maxByOrNull { it.refreshRate } ?: return
        // Only switch when the panel genuinely offers a faster mode at this resolution.
        if (fastest.modeId != active.modeId && fastest.refreshRate > active.refreshRate + 1f) {
            window.attributes = window.attributes.apply { preferredDisplayModeId = fastest.modeId }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        handleDeepLinkIntent(intent)
        handleShareIntent(intent)
        handleVoiceIntent(intent)
    }

    // Voice capture (음성 캡처 app shortcut): launch the system speech recognizer.
    private fun handleVoiceIntent(intent: Intent?) {
        if (intent?.data?.toString() != "deneb://voice") return
        intent.data = null // consume so a configuration change doesn't re-launch
        launchVoiceCapture()
    }

    // launchVoiceCapture fires the system speech recognizer; its transcript is
    // sent to the Deneb chat by speechLauncher. No RECORD_AUDIO permission needed —
    // shared by the 음성 캡처 app shortcut and the drawer's 음성 입력 action.
    private fun launchVoiceCapture() {
        val recognize = Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH).apply {
            putExtra(RecognizerIntent.EXTRA_LANGUAGE_MODEL, RecognizerIntent.LANGUAGE_MODEL_FREE_FORM)
            putExtra(RecognizerIntent.EXTRA_LANGUAGE, "ko-KR")
            putExtra(RecognizerIntent.EXTRA_PROMPT, "Deneb에게 말하세요")
        }
        runCatching { speechLauncher.launch(recognize) }
    }

    // Send a single picked/shared file through the batch/pointer path
    // (miniapp.capture.batch): the gateway materializes it and the agent reads it with a
    // tool, instead of the old single-capture RPCs that dumped the whole extracted text
    // inline into the turn. Matches andromeda, which sends even one file as a batch.
    private suspend fun sendSingleFileBatch(bytes: ByteArray, filename: String, mime: String, caption: String = "") {
        if (bytes.isEmpty()) return
        if (!isWithinAttachmentSize(attachmentExtension(filename, mime), bytes.size.toLong())) return
        (get<DataRepository>() as? DenebGatewayClient)?.captureBatch(listOf(attachmentFor(bytes, filename, mime)), caption)
    }

    // Build an upload attachment, downsampling a captured photo first: a full-resolution
    // camera shot is 10-15MB, and a batch of them base64-encoded would bloat memory and
    // the turn payload while ~2048px stays readable for the gateway's OCR. An image too
    // large to decode safely (past MAX_RAW_IMAGE_BYTES) and every non-image are sent as-is.
    private suspend fun attachmentFor(bytes: ByteArray, filename: String, mime: String): DenebAttachment {
        val prepared = if (mime.startsWith("image/") && bytes.size <= MAX_RAW_IMAGE_BYTES) {
            compressImageBytes(bytes, mime)
        } else {
            bytes
        }
        return DenebAttachment(prepared, filename, mime)
    }

    private fun captureFile(bytes: ByteArray, filename: String, mime: String) {
        lifecycleScope.launch { sendSingleFileBatch(bytes, filename, mime) }
    }

    // captureFromUri reads a picked file's bytes and routes them to the gateway:
    // image -> PaddleOCR, audio -> VibeVoice-ASR. Backs the drawer's 이미지 OCR /
    // 녹음 전사 actions, reusing the same capture paths as the share sheet.
    private fun captureFromUri(uri: Uri, fallbackMime: String, audio: Boolean) {
        val mime = contentResolver.getType(uri) ?: fallbackMime
        val name = sharedDisplayName(uri) ?: if (audio) "recording" else "image"
        val ext = attachmentExtension(name, mime)
        uriContentLength(uri)?.let { len ->
            if (!isWithinAttachmentSize(ext, len)) return
        }
        val bytes = runCatching { contentResolver.openInputStream(uri)?.use { it.readBytes() } }.getOrNull()
        if (bytes == null || bytes.isEmpty()) return
        if (!isWithinAttachmentSize(ext, bytes.size.toLong())) return
        captureFile(bytes, name, mime)
    }

    // Best-effort content length before loading a shared URI into memory.
    private fun uriContentLength(uri: Uri): Long? = runCatching {
        contentResolver.openFileDescriptor(uri, "r")?.use { fd ->
            fd.statSize.takeIf { it > 0 }
        }
    }.getOrNull()

    // Files staged in the composer, sent together on submit -> ONE batch turn: read each,
    // derive its per-type MIME, and hand the set to captureBatch so the agent reads and
    // cross-references them together. The composer text is the caption.
    private fun captureFilesBatch(files: List<PlatformFile>, caption: String) {
        lifecycleScope.launch {
            val attachments = files.mapNotNull { file ->
                val bytes = runCatching { file.readBytes() }.getOrNull()
                if (bytes == null || bytes.isEmpty()) {
                    null
                } else if (!isWithinAttachmentSize(file.extension, bytes.size.toLong())) {
                    null
                } else {
                    attachmentFor(bytes, file.name, pickedFileMime(file.name))
                }
            }
            if (attachments.isEmpty()) return@launch
            (get<DataRepository>() as? DenebGatewayClient)?.captureBatch(attachments, caption)
        }
    }

    // MIME for a picked file by its routed type, mirroring the single-file picker paths
    // so a batch's images still OCR, audio transcribes, and documents extract/convert.
    private fun pickedFileMime(name: String): String = when (routeAttachment(name.substringAfterLast('.', ""), capturesAvailable = true)) {
        AttachmentRoute.IMAGE_CAPTURE -> mimeForFileName(name, audio = false)
        AttachmentRoute.AUDIO_CAPTURE -> mimeForFileName(name, audio = true)
        AttachmentRoute.FILE_ATTACH -> documentCaptureMime(name)
    }

    // mimeForFileName derives a content type from a picked file's extension. The
    // gateway capture sniffs the bytes too, so an "image/*"/"audio/*" fallback is
    // fine for an unmapped extension.
    private fun mimeForFileName(name: String, audio: Boolean): String = when (name.substringAfterLast('.', "").lowercase()) {
        "jpg", "jpeg" -> "image/jpeg"
        "png" -> "image/png"
        "webp" -> "image/webp"
        "gif" -> "image/gif"
        "bmp" -> "image/bmp"
        "heic", "heif" -> "image/heic"
        "m4a" -> "audio/mp4"
        "mp3" -> "audio/mpeg"
        "wav" -> "audio/wav"
        "ogg", "oga", "opus" -> "audio/ogg"
        "aac" -> "audio/aac"
        "flac" -> "audio/flac"
        "amr" -> "audio/amr"
        "3gp" -> "audio/3gpp"
        else -> if (audio) "audio/*" else "image/*"
    }

    private fun handleDeepLinkIntent(intent: Intent?) {
        val feedItemId = intent
            ?.getStringExtra(EXTRA_OPEN_WORK_FEED_ITEM_ID)
            ?.trim()
            ?.takeIf(String::isNotEmpty)
        intent?.removeExtra(EXTRA_OPEN_WORK_FEED_ITEM_ID)
        if (feedItemId != null) {
            pendingWorkFeedItemId = feedItemId
        }
        if (intent?.getBooleanExtra(EXTRA_OPEN_HEARTBEAT, false) == true) {
            val dataRepository: DataRepository = get()
            dataRepository.requestOpenHeartbeat()
            // Drop the extra so a configuration change (screen rotation) doesn't re-trigger
            // the deep-link after ChatViewModel has already consumed it.
            intent.removeExtra(EXTRA_OPEN_HEARTBEAT)
        }
        // Proactive report push → open the 업무 (General) topic where it was mirrored.
        if (intent?.getBooleanExtra(EXTRA_OPEN_WORK_TOPIC, false) == true) {
            if (feedItemId == null) {
                val dataRepository: DataRepository = get()
                dataRepository.requestOpenWorkTopic()
            }
            intent.removeExtra(EXTRA_OPEN_WORK_TOPIC)
        }
        // Android assist gesture (홈버튼 길게 누르기 / 사이드 버튼): with Deneb set as the
        // device assist app, the OS-level "call your assistant" gesture lands in the
        // 업무 chat, ready to type — the same navigation pulse the proactive push uses.
        if (intent?.action == Intent.ACTION_ASSIST) {
            val dataRepository: DataRepository = get()
            dataRepository.requestOpenWorkTopic()
            // Clear the action so a configuration change doesn't re-trigger the
            // navigation after ChatViewModel has already consumed the request.
            intent.action = null
        }
    }

    // Share-sheet capture: text shared from any app (a KakaoTalk message, a URL,
    // an article excerpt) goes straight into the Deneb chat for triage. Home is
    // the chat screen, so the capture appears immediately on launch. This is a
    // native-only capability the retired Telegram bot couldn't offer.
    private fun handleShareIntent(intent: Intent?) {
        if (intent?.action == Intent.ACTION_SEND_MULTIPLE) {
            if (intent.type?.startsWith("image/") == true) handleSharedImages(intent)
            return
        }
        if (intent?.action != Intent.ACTION_SEND) return
        if (intent.type?.startsWith("image/") == true) {
            handleSharedImage(intent)
            return
        }
        if (intent.type?.startsWith("audio/") == true) {
            handleSharedAudio(intent)
            return
        }
        // Document route only when an actual stream rides along: apps share
        // markdown/CSV as inline EXTRA_TEXT too (no EXTRA_STREAM), and those must
        // fall through to the text capture below instead of dying silently in
        // handleSharedDocument's missing-uri return.
        if (isSharedDocumentMime(intent.type) && intent.hasExtra(Intent.EXTRA_STREAM)) {
            handleSharedDocument(intent)
            return
        }
        val text = intent.getStringExtra(Intent.EXTRA_TEXT)?.trim().orEmpty()
        if (text.isEmpty()) return
        val subject = intent.getStringExtra(Intent.EXTRA_SUBJECT)?.trim().orEmpty()
        val captured = if (subject.isNotEmpty()) "📥 공유: $subject\n\n$text" else "📥 공유됨\n\n$text"
        lifecycleScope.launch { get<DataRepository>().ask(captured, emptyList(), null) }
        // Clear so a configuration change doesn't re-send the capture.
        intent.removeExtra(Intent.EXTRA_TEXT)
    }

    // Document mimes the share filter accepts — must stay in lockstep with the
    // manifest SEND filter and the gateway extraction dispatcher
    // (document_extract.go): PDF/CSV/OOXML/Markdown natively, plus legacy binary
    // Office / RTF / ODF which the gateway converts (LibreOffice) before
    // extracting. HWP is not matched here — its share MIME varies by app, so HWP
    // rides the in-app picker (routed by extension) instead of the share sheet.
    private fun isSharedDocumentMime(mime: String?): Boolean = when {
        mime == null -> false
        mime == "application/pdf" -> true
        mime == "text/csv" || mime == "text/comma-separated-values" -> true
        mime == "text/markdown" -> true
        mime.contains("wordprocessingml") || mime.contains("spreadsheetml") || mime.contains("presentationml") -> true
        mime == "application/msword" || mime == "application/vnd.ms-excel" || mime == "application/vnd.ms-powerpoint" -> true
        mime == "application/rtf" || mime == "text/rtf" -> true
        mime.contains("opendocument") -> true
        else -> false
    }

    // Shared image -> gateway OCR -> chat. Reads the image bytes (a temporary read
    // grant rides with the share) and hands them to the gateway, which OCRs via the
    // PaddleOCR sidecar and runs one agent turn over the extracted text.
    private fun handleSharedImage(intent: Intent) {
        @Suppress("DEPRECATION")
        val uri: Uri? = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java)
        } else {
            intent.getParcelableExtra(Intent.EXTRA_STREAM)
        }
        if (uri == null) return
        val bytes = runCatching { contentResolver.openInputStream(uri)?.use { it.readBytes() } }.getOrNull()
        if (bytes == null || bytes.isEmpty()) return
        val mime = intent.type ?: "image/*"
        captureFile(bytes, sharedDisplayName(uri) ?: "image", mime)
        intent.removeExtra(Intent.EXTRA_STREAM)
    }

    // Shared audio -> gateway VibeVoice-ASR -> chat. Reads the recording bytes (a
    // temporary read grant rides with the share) and hands them to the gateway,
    // which transcribes via the ASR sidecar (speaker labels + timestamps) and runs
    // one agent turn over the transcript. A voice memo or a meeting recording
    // shared from a recorder/files app — long-form capture the on-device speech
    // shortcut (음성 캡처) couldn't do (nor could the retired Telegram bot).
    private fun handleSharedAudio(intent: Intent) {
        @Suppress("DEPRECATION")
        val uri: Uri? = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java)
        } else {
            intent.getParcelableExtra(Intent.EXTRA_STREAM)
        }
        if (uri == null) return
        val bytes = runCatching { contentResolver.openInputStream(uri)?.use { it.readBytes() } }.getOrNull()
        if (bytes == null || bytes.isEmpty()) return
        val mime = intent.type ?: "audio/*"
        captureFile(bytes, sharedDisplayName(uri) ?: "recording", mime)
        intent.removeExtra(Intent.EXTRA_STREAM)
    }

    // Shared document (PDF/CSV) -> gateway extraction -> chat. A contract PDF shared
    // from mail/KakaoTalk lands in the chat as extracted text plus one agent turn —
    // this fires immediately (no composer to stage into, unlike the in-app picker).
    private fun handleSharedDocument(intent: Intent) {
        @Suppress("DEPRECATION")
        val uri: Uri? = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java)
        } else {
            intent.getParcelableExtra(Intent.EXTRA_STREAM)
        }
        if (uri == null) return
        val bytes = runCatching { contentResolver.openInputStream(uri)?.use { it.readBytes() } }.getOrNull()
        if (bytes == null || bytes.isEmpty()) return
        val mime = intent.type ?: "application/pdf"
        val name = sharedDisplayName(uri) ?: ("shared" + sharedDocumentExt(mime))
        captureFile(bytes, name, mime)
        intent.removeExtra(Intent.EXTRA_STREAM)
    }

    // Fallback extension when the share carries no display name. The gateway dispatches
    // by filename whenever the MIME is generic (octet-stream), so a wrong extension —
    // the old ".csv" catch-all — made it parse an unrelated binary (e.g. an HWP shared
    // as octet-stream) as CSV. Map every MIME we can; otherwise send NO extension so the
    // gateway falls back to the MIME or an honest "unsupported" instead of misreading it.
    private fun sharedDocumentExt(mime: String): String = when {
        mime == "application/pdf" -> ".pdf"
        mime == "text/markdown" -> ".md"
        mime.contains("csv") -> ".csv"
        mime.contains("wordprocessingml") -> ".docx"
        mime.contains("spreadsheetml") -> ".xlsx"
        mime.contains("presentationml") -> ".pptx"
        mime.contains("opendocument.text") -> ".odt"
        mime.contains("opendocument.spreadsheet") -> ".ods"
        mime.contains("opendocument.presentation") -> ".odp"
        mime.contains("hwp") -> ".hwp"
        mime == "application/msword" -> ".doc"
        mime == "application/vnd.ms-excel" -> ".xls"
        mime == "application/vnd.ms-powerpoint" -> ".ppt"
        else -> ""
    }

    // Multiple shared images (several receipt photos / scan pages): read them all and
    // hand the set to ONE batch turn the agent reads and cross-references, instead of
    // a separate OCR turn per image. Capped — a huge gallery share should not fan out
    // unbounded (the gateway caps again server-side).
    private fun handleSharedImages(intent: Intent) {
        @Suppress("DEPRECATION")
        val uris: List<Uri> = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            intent.getParcelableArrayListExtra(Intent.EXTRA_STREAM, Uri::class.java)
        } else {
            intent.getParcelableArrayListExtra(Intent.EXTRA_STREAM)
        }.orEmpty().filterNotNull().take(SHARED_IMAGES_MAX)
        if (uris.isEmpty()) return
        val mime = intent.type ?: "image/*"
        val client = get<DataRepository>() as? DenebGatewayClient ?: return
        lifecycleScope.launch {
            val files = uris.mapIndexedNotNull { index, uri ->
                val bytes = runCatching { contentResolver.openInputStream(uri)?.use { it.readBytes() } }.getOrNull()
                if (bytes == null || bytes.isEmpty()) {
                    null
                } else {
                    attachmentFor(bytes, sharedDisplayName(uri) ?: "image-${index + 1}", mime)
                }
            }
            if (files.isNotEmpty()) client.captureBatch(files)
        }
        intent.removeExtra(Intent.EXTRA_STREAM)
    }

    // Display name for a shared content URI (the PDF's real filename when the
    // sender provides one — the extraction prompt and transcript read better).
    private fun sharedDisplayName(uri: Uri): String? = runCatching {
        contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { cursor ->
            if (cursor.moveToFirst()) cursor.getString(0) else null
        }
    }.getOrNull()?.takeIf { it.isNotBlank() }

    private companion object {
        const val SHARED_IMAGES_MAX = 5
    }
}

@Preview
@Composable
fun AppAndroidPreview() {
    App(navController = rememberNavController())
}
